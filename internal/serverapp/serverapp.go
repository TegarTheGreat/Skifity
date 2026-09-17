// Package serverapp wires the panel's pieces together and runs the HTTP server.
package serverapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"skifity/internal/api"
	"skifity/internal/auth"
	"skifity/internal/backup"
	"skifity/internal/cluster"
	"skifity/internal/config"
	"skifity/internal/crypto"
	"skifity/internal/dbsvc"
	"skifity/internal/deploy"
	"skifity/internal/events"
	"skifity/internal/kube"
	"skifity/internal/logging"
	"skifity/internal/metrics"
	"skifity/internal/notify"
	"skifity/internal/provision"
	"skifity/internal/runsafe"
	"skifity/internal/settings"
	"skifity/internal/store"
	"skifity/internal/version"
	"skifity/internal/watch"
)

// Run starts the panel and blocks until the context is cancelled.
func Run(ctx context.Context, cfg config.Config, frontend http.Handler) error {
	log := logging.New(os.Stdout, cfg.LogLevel, cfg.LogFormat)
	slog.SetDefault(log)

	log.Info("starting", "product", version.Name, "version", version.Version,
		"commit", version.Commit, "listen", cfg.Listen)

	keyring, err := crypto.InitKeyring(cfg.MasterKeyPath)
	if err != nil {
		return fmt.Errorf("prepare the master key: %w", err)
	}
	log.Info("master key ready", "path", cfg.MasterKeyPath, "active_key", keyring.ActiveID())

	db, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open the panel database: %w", err)
	}
	defer db.Close()
	schemaVersion, _ := db.SchemaVersion(ctx)
	log.Info("database ready", "path", cfg.DatabasePath, "schema_version", schemaVersion)

	if err := seedPodNetwork(ctx, db, cfg, log); err != nil {
		return err
	}

	hub := events.NewHub(256)
	authService := auth.NewService(db, keyring, cfg.SessionTTL, !cfg.DevMode)

	// The cluster is optional at start: the panel must come up and be usable
	// for settings even when Kubernetes is not reachable yet, because that is
	// exactly when someone needs to look at it.
	var clusterAdapter *cluster.Cluster
	kubeClient, err := kube.NewClient(kube.Options{
		KubeconfigPath:  cfg.KubeconfigPath,
		SystemNamespace: cfg.Namespace,
	})
	if err != nil {
		log.Warn("not connected to a cluster; the panel will run with cluster features unavailable",
			"error", err)
	} else {
		clusterAdapter = cluster.New(kubeClient, db, log)
		if err := clusterAdapter.Ping(ctx); err != nil {
			log.Warn("the cluster is not answering yet", "error", err)
		} else {
			if clusterVersion, err := kubeClient.Version(ctx); err == nil {
				log.Info("connected to the cluster", "kubernetes_version", clusterVersion)
			}
		}
	}

	// Notifications are sent by the things that know an event happened, so the
	// dispatcher is built before them and handed to each one.
	dispatcher := notify.NewDispatcher(db, keyring, log, panelAddress(cfg, db, log))
	defer dispatcher.Wait()

	// One registry for the whole process, so a deployment counted by the
	// deployer appears on the same page as a request counted by the API.
	registry := metrics.New()

	deployer := deploy.New(db, keyring, hub, clusterAdapter, dispatcher, log)
	deployer.Metrics = registry
	provisioner := provision.New(provision.Options{
		DB: db, Keyring: keyring, Hub: hub, Cluster: clusterAdapter,
		Notifier: dispatcher, ClusterTokenPath: cfg.ClusterTokenPath, Logger: log,
	})
	databases := dbsvc.New(db, keyring, hub, clusterAdapter, deployer, log)
	backups := backup.New(db, keyring, hub, clusterAdapter, dispatcher, log)
	watcher := watch.New(db, watchCluster(clusterAdapter), hub, dispatcher, log)

	setupToken, err := readSetupToken(cfg, db, log)
	if err != nil {
		return err
	}

	server := api.New(api.Options{
		Config: cfg, DB: db, Keyring: keyring, Auth: authService, Hub: hub, Logger: log,
		Cluster: nilIfNil(clusterAdapter), Provisioner: provisioner, Deployer: deployer,
		Databases: databases, Backups: backups,
		Frontend: frontend, SetupToken: setupToken, Metrics: registry,
	})

	// Anything left running when the panel stopped is marked failed with an
	// explanation, rather than sitting in the UI as apparently in progress.
	if err := provisioner.ResumeInterrupted(ctx); err != nil {
		log.Warn("could not tidy up interrupted operations", "error", err)
	}
	if err := markInterruptedDeployments(ctx, db, log); err != nil {
		log.Warn("could not tidy up interrupted deployments", "error", err)
	}

	background, stopBackground := context.WithCancel(ctx)
	defer stopBackground()
	go server.Background(background)
	go runScheduler(background, db, backups, clusterAdapter, log)
	go watcher.Run(background)

	httpServer := &http.Server{
		Addr:    cfg.Listen,
		Handler: server,
		// A slow client must not be able to hold a connection open forever,
		// but the write timeout has to be generous: log streams are long-lived.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "address", cfg.Listen)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		log.Info("shutting down", "grace", cfg.ShutdownGrace)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	log.Info("stopped")
	return nil
}

// watchCluster keeps a nil *cluster.Cluster from becoming a non-nil
// watch.Cluster, for the same reason as nilIfNil below.
func watchCluster(c *cluster.Cluster) watch.Cluster {
	if c == nil {
		return nil
	}
	return c
}

// seedPodNetwork records the pod network the installer started this cluster
// with, once, so every server added later is installed with the same one.
//
// A node that joins a wireguard-native cluster with vxlan joins without error
// and then never exchanges a packet with the others, so the panel cannot guess
// and cannot leave it unset. It only ever writes the setting when there is
// nothing there: after the first start the setting is the answer, including
// when an operator has changed it.
func seedPodNetwork(ctx context.Context, db *store.DB, cfg config.Config, log *slog.Logger) error {
	if cfg.PodNetwork == "" {
		return nil
	}
	current, _, err := db.GetSetting(ctx, settings.KeyFlannelBackend)
	if err != nil {
		return fmt.Errorf("read the pod network setting: %w", err)
	}
	if current != "" {
		return nil
	}
	if err := db.SetSetting(ctx, settings.KeyFlannelBackend, cfg.PodNetwork, false, "system"); err != nil {
		return fmt.Errorf("record the pod network setting: %w", err)
	}
	log.Info("recorded the pod network this cluster was installed with", "backend", cfg.PodNetwork)
	return nil
}

// panelAddress works out how to link back into the panel from a notification.
//
// The configured public URL wins; otherwise the address an operator saved in
// settings is used, because a panel installed with a hostname knows it there
// and nowhere else. An empty answer means a notification carries no link,
// which is better than a link to somewhere that does not exist.
func panelAddress(cfg config.Config, db *store.DB, log *slog.Logger) func(context.Context) string {
	return func(ctx context.Context) string {
		if cfg.PublicURL != "" {
			return cfg.PublicURL
		}
		value, _, err := db.GetSetting(ctx, settings.KeyPanelURL)
		if err != nil {
			log.Debug("no panel address for notification links", "error", err)
			return ""
		}
		return value
	}
}

// nilIfNil keeps a nil *cluster.Cluster from becoming a non-nil interface, which
// would make every `if s.cluster != nil` check in the API layer wrong.
func nilIfNil(c *cluster.Cluster) api.Cluster {
	if c == nil {
		return nil
	}
	return c
}

// readSetupToken loads the one-time token, generating one when the panel has no
// users and no token yet.
//
// A panel with no account and no token could never be set up, which is a dead
// end an operator would have to fix over SSH. Generating one and logging it is
// the only way out of that state.
func readSetupToken(cfg config.Config, db *store.DB, log *slog.Logger) (string, error) {
	count, err := db.CountUsers(context.Background())
	if err != nil {
		return "", err
	}
	if count > 0 {
		return "", nil
	}

	if cfg.SetupTokenPath != "" {
		data, err := os.ReadFile(cfg.SetupTokenPath)
		if err == nil {
			token := strings.TrimSpace(string(data))
			if token != "" {
				log.Warn("first-run setup is pending",
					"token_file", cfg.SetupTokenPath,
					"setup_token", logging.Public{Value: token})
				return token, nil
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read the setup token at %s: %w", cfg.SetupTokenPath, err)
		}
	}

	token, err := crypto.RandomToken(24)
	if err != nil {
		return "", err
	}
	if cfg.SetupTokenPath != "" {
		if err := os.WriteFile(cfg.SetupTokenPath, []byte(token+"\n"), 0o600); err != nil {
			log.Warn("could not write the setup token to disk", "path", cfg.SetupTokenPath, "error", err)
		}
	}
	// This is the one secret deliberately written to the log, and it is wrapped
	// so the redactor lets it through: without it a fresh panel cannot be
	// claimed at all.
	setupURL := cfg.PublicURL + "/setup"
	if cfg.PublicURL == "" {
		setupURL = "http://<this server>:" + portOf(cfg.Listen) + "/setup"
	}
	log.Warn("first-run setup is pending",
		"open", setupURL,
		"setup_token", logging.Public{Value: token})
	return token, nil
}

// markInterruptedDeployments fails deployments that were running when the panel
// stopped, so they do not sit in the UI forever.
func markInterruptedDeployments(ctx context.Context, db *store.DB, log *slog.Logger) error {
	deployments, err := db.ListUnfinishedDeployments(ctx)
	if err != nil {
		return err
	}
	for _, deployment := range deployments {
		log.Info("marking an interrupted deployment as failed", "deployment", deployment.ID)
		if err := db.UpdateDeploymentStatus(ctx, deployment.ID, store.DeployFailed,
			"deploy.interrupted",
			"The panel restarted while this deployment was running.",
			"Deploy again. Your running instances were not affected by the restart."); err != nil {
			return err
		}
	}
	return nil
}

// runScheduler fires scheduled backups once a minute.
func runScheduler(ctx context.Context, db *store.DB, backups *backup.Manager, c *cluster.Cluster, log *slog.Logger) {
	// Align to the start of the next minute so a schedule of "0 3 * * *" fires
	// at 03:00 rather than at whatever second the panel happened to start.
	timer := time.NewTimer(time.Until(time.Now().Truncate(time.Minute).Add(time.Minute)))
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		// A panic costs this minute, not the scheduler. Recovering around the
		// loop instead would keep the process alive and leave it with no
		// scheduler, which is worse than a crash because nothing says so and
		// the backups simply stop happening.
		func() {
			defer runsafe.Recover(log, "the minute tick", nil)
			backups.RunScheduled(ctx)
			// Maintenance is on the same minute tick rather than a timer of its
			// own, because "due" has to survive a restart: a panel restarted
			// daily would never reach a weekly timer, and the disk would fill
			// anyway.
			pruneHistory(ctx, db, log)
			if c != nil {
				c.MaintainRegistry(ctx)
			}
		}()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// portOf pulls the port out of a listen address, for the setup message.
func portOf(listen string) string {
	if _, port, err := net.SplitHostPort(listen); err == nil && port != "" {
		return port
	}
	return "8080"
}

// pruneHistory removes the records nobody will read again, once a day.
//
// The panel's database is a file on one node's disk, and a deployment record,
// an audit entry and a finished operation were all written and never removed. It
// is not a failure that arrives, it is one that accumulates: every query over
// those tables gets slower and the backup of them gets larger, and both are
// only ever noticed long after they started.
func pruneHistory(ctx context.Context, db *store.DB, log *slog.Logger) {
	if !db.DueEvery(ctx, settings.KeyPrunedAt, 24*time.Hour) {
		return
	}
	report, err := db.Prune(ctx, retentionFrom(ctx, db))
	if err != nil {
		log.Warn("could not prune the panel's history", "error", err)
		return
	}
	// A pass that found nothing is the ordinary case and says nothing. One that
	// removed something says how much, so "the database keeps growing" is a
	// question the log can answer.
	if !report.Empty() {
		log.Info("pruned the panel's history", "removed", report.String())
	}
}

// retentionFrom reads the configured windows, falling back to the defaults.
func retentionFrom(ctx context.Context, db *store.DB) store.Retention {
	keep := store.DefaultRetention()
	if value, _, err := db.GetSetting(ctx, settings.KeyDeploymentHistory); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
			keep.DeploymentsPerApp = n
		}
	}
	if value, _, err := db.GetSetting(ctx, settings.KeyAuditHistoryDays); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && n > 0 {
			keep.AuditDays = n
		}
	}
	return keep
}
