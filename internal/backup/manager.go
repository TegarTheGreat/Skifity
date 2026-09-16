package backup

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"skifity/internal/cluster"
	"skifity/internal/crypto"
	"skifity/internal/errdoc"
	"skifity/internal/events"
	"skifity/internal/kube"
	"skifity/internal/notify"
	"skifity/internal/store"
)

// Manager implements api.BackupManager.
type Manager struct {
	db       *store.DB
	keyring  *crypto.Keyring
	hub      *events.Hub
	cluster  *cluster.Cluster
	notifier notify.Notifier
	log      *slog.Logger
}

// New builds a Manager. notifier may be nil, and then nothing is sent.
func New(db *store.DB, keyring *crypto.Keyring, hub *events.Hub, c *cluster.Cluster, notifier notify.Notifier, log *slog.Logger) *Manager {
	return &Manager{db: db, keyring: keyring, hub: hub, cluster: c, notifier: notifier, log: log}
}

// Verify checks that the configured storage is usable.
func (m *Manager) Verify(ctx context.Context) error {
	storage, err := LoadStorage(ctx, m.db, m.keyring)
	if err != nil {
		return err
	}
	return storage.Verify(ctx)
}

// Run takes a backup now.
func (m *Manager) Run(ctx context.Context, targetType, targetID, kind string) (store.Backup, error) {
	if targetType != "database" {
		return store.Backup{}, errdoc.BadRequest("Only databases can be backed up at the moment.")
	}
	if m.cluster == nil {
		return store.Backup{}, errdoc.ClusterUnreachable(nil)
	}

	record, err := m.db.GetDatabase(ctx, targetID)
	if err != nil {
		return store.Backup{}, err
	}
	if record.Status != "running" {
		return store.Backup{}, errdoc.New("backup.database_not_running", "This database is not running").
			WithCause("%s is %s, so there is nothing to back up.", record.Name, record.Status).
			WithImpact("No backup was taken.").
			WithFix("Wait for the database to be running, then try again.")
	}

	storage, err := LoadStorage(ctx, m.db, m.keyring)
	if err != nil {
		return store.Backup{}, err
	}

	backup := store.Backup{
		TargetType: targetType, TargetID: targetID,
		Status: "running", Kind: kind,
	}
	backup.Location = ObjectKey(targetType, targetID, record.Name, time.Now())
	if err := m.db.CreateBackup(ctx, &backup); err != nil {
		return store.Backup{}, err
	}

	go m.run(context.WithoutCancel(ctx), storage, backup, record)
	return backup, nil
}

func (m *Manager) run(ctx context.Context, storage *Storage, backup store.Backup, record store.Database) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Hour)
	defer cancel()

	fail := func(err error) {
		problem := errdoc.From(err)
		m.log.Error("backup failed", "backup", backup.ID, "database", record.ID, "error", err)
		_ = m.db.FinishBackup(ctx, backup.ID, "failed", backup.Location, 0, problem.Error())
		m.publish(ctx, record.ID)
		m.notifyFailure(ctx, record, problem)
	}

	env, err := m.db.GetEnvironment(ctx, record.EnvironmentID)
	if err != nil {
		fail(err)
		return
	}

	presigned, err := storage.PresignPut(ctx, backup.Location)
	if err != nil {
		fail(err)
		return
	}

	jobName := JobName("backup-"+record.Slug, backup.ID)
	secretName := jobName + "-url"

	// The URL Secret lives only as long as the job, so an expired signature is
	// not left lying in the namespace.
	defer func() {
		if err := m.cluster.Client().Applier().Delete(ctx, "v1", "Secret", env.Namespace, secretName); err != nil {
			m.log.Warn("could not remove the backup URL secret", "backup", backup.ID, "error", err)
		}
	}()

	if err := m.cluster.Client().Applier().Apply(ctx,
		URLSecret(secretName, env.Namespace, presigned)); err != nil {
		fail(err)
		return
	}

	job, err := BuildJob(JobSpec{
		Name:              jobName,
		Namespace:         env.Namespace,
		Engine:            record.Engine,
		CredentialsSecret: kube.ResourceName(record.Slug, "credentials"),
		URLSecret:         secretName,
		BackupID:          backup.ID,
	})
	if err != nil {
		fail(err)
		return
	}
	_ = m.cluster.Client().Applier().Delete(ctx, "batch/v1", "Job", env.Namespace, jobName)
	if err := m.cluster.Client().Applier().Apply(ctx, job); err != nil {
		fail(err)
		return
	}

	if err := m.waitForJob(ctx, env.Namespace, jobName); err != nil {
		fail(err)
		return
	}

	size, err := storage.Stat(ctx, backup.Location)
	if err != nil {
		// The job reported success, so a failure to stat is a storage quirk
		// rather than a failed backup; record it without the size.
		m.log.Warn("could not read the backup's size", "backup", backup.ID, "error", err)
	}
	if err := m.db.FinishBackup(ctx, backup.ID, "succeeded", backup.Location, size, ""); err != nil {
		m.log.Warn("could not record the finished backup", "backup", backup.ID, "error", err)
	}
	m.publish(ctx, record.ID)
	m.log.Info("backup finished", "backup", backup.ID, "database", record.Name, "bytes", size)

	m.applyRetention(ctx, storage, record.ID)
}

// applyRetention deletes backups beyond the configured count.
func (m *Manager) applyRetention(ctx context.Context, storage *Storage, databaseID string) {
	policy, err := m.db.GetBackupPolicy(ctx, "database", databaseID)
	if err != nil {
		return
	}
	expired, err := m.db.ExpiredBackups(ctx, "database", databaseID, policy.Retention)
	if err != nil {
		m.log.Warn("could not list expired backups", "database", databaseID, "error", err)
		return
	}
	for _, old := range expired {
		if old.Location != "" {
			if err := storage.Remove(ctx, old.Location); err != nil {
				// A backup that cannot be deleted from storage stays in the
				// list, so it is not silently forgotten while still costing money.
				m.log.Warn("could not delete an expired backup from storage",
					"backup", old.ID, "error", err)
				continue
			}
		}
		if err := m.db.DeleteBackup(ctx, old.ID); err != nil {
			m.log.Warn("could not delete an expired backup record", "backup", old.ID, "error", err)
		}
	}
}

// Restore puts a backup back.
func (m *Manager) Restore(ctx context.Context, backupID string, overwrite bool) (store.Operation, error) {
	backup, err := m.db.GetBackup(ctx, backupID)
	if err != nil {
		return store.Operation{}, err
	}
	if backup.Status != "succeeded" {
		return store.Operation{}, errdoc.BadRequest("That backup did not finish successfully, so it cannot be restored.")
	}
	record, err := m.db.GetDatabase(ctx, backup.TargetID)
	if err != nil {
		return store.Operation{}, err
	}

	// Restoring over live data is destructive and irreversible, so it has to be
	// asked for explicitly rather than being the default.
	if !overwrite {
		links, err := m.db.ListLinksForDatabase(ctx, record.ID)
		if err == nil && len(links) > 0 {
			return store.Operation{}, errdoc.RestoreRefused(record.Name).
				With("apps_using_it", fmt.Sprint(len(links)))
		}
	}

	env, err := m.db.GetEnvironment(ctx, record.EnvironmentID)
	if err != nil {
		return store.Operation{}, err
	}
	teamID, err := m.db.TeamIDForDatabase(ctx, record.ID)
	if err != nil {
		return store.Operation{}, err
	}

	op := store.Operation{
		TeamID: teamID, Kind: "database.restore",
		TargetType: "database", TargetID: record.ID,
	}
	if err := m.db.CreateOperation(ctx, &op, []string{"prepare", "restore", "verify"}); err != nil {
		return store.Operation{}, err
	}

	go m.runRestore(context.WithoutCancel(ctx), op, backup, record, env)
	return op, nil
}

func (m *Manager) runRestore(ctx context.Context, op store.Operation, backup store.Backup, record store.Database, env store.Environment) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Hour)
	defer cancel()

	fail := func(step string, err error) {
		problem := errdoc.From(err)
		m.log.Error("restore failed", "operation", op.ID, "step", step, "error", err)
		_ = m.db.SetStepStatus(ctx, op.ID, step, store.StepFailed, problem.Title, problem.Text())
		_ = m.db.SetOperationStatus(ctx, op.ID, store.OpFailed, problem.Code, problem.Error())
		m.hub.Publish(events.OperationTopic(op.ID), "failed", problem)
	}

	_ = m.db.SetOperationStatus(ctx, op.ID, store.OpRunning, "", "")

	_ = m.db.SetStepStatus(ctx, op.ID, "prepare", store.StepRunning, "", "")
	storage, err := LoadStorage(ctx, m.db, m.keyring)
	if err != nil {
		fail("prepare", err)
		return
	}
	presigned, err := storage.PresignGet(ctx, backup.Location)
	if err != nil {
		fail("prepare", err)
		return
	}
	jobName := JobName("restore-"+record.Slug, backup.ID)
	secretName := jobName + "-url"
	defer func() {
		_ = m.cluster.Client().Applier().Delete(ctx, "v1", "Secret", env.Namespace, secretName)
	}()
	if err := m.cluster.Client().Applier().Apply(ctx,
		URLSecret(secretName, env.Namespace, presigned)); err != nil {
		fail("prepare", err)
		return
	}
	_ = m.db.SetStepStatus(ctx, op.ID, "prepare", store.StepSucceeded, "Ready to restore", "")

	_ = m.db.SetStepStatus(ctx, op.ID, "restore", store.StepRunning, "", "")
	job, err := BuildJob(JobSpec{
		Name:              jobName,
		Namespace:         env.Namespace,
		Engine:            record.Engine,
		CredentialsSecret: kube.ResourceName(record.Slug, "credentials"),
		URLSecret:         secretName,
		BackupID:          backup.ID,
		Restore:           true,
	})
	if err != nil {
		fail("restore", err)
		return
	}
	_ = m.cluster.Client().Applier().Delete(ctx, "batch/v1", "Job", env.Namespace, jobName)
	if err := m.cluster.Client().Applier().Apply(ctx, job); err != nil {
		fail("restore", err)
		return
	}
	if err := m.waitForJob(ctx, env.Namespace, jobName); err != nil {
		fail("restore", err)
		return
	}
	_ = m.db.SetStepStatus(ctx, op.ID, "restore", store.StepSucceeded,
		fmt.Sprintf("Restored from the backup taken on %s", backup.CreatedAt.Format("2 January 2006 at 15:04")), "")

	_ = m.db.SetStepStatus(ctx, op.ID, "verify", store.StepRunning, "", "")
	// Restarting the apps that use this database clears any connection pool
	// holding a transaction against the old data.
	links, err := m.db.ListLinksForDatabase(ctx, record.ID)
	if err == nil {
		for _, link := range links {
			app, err := m.db.GetApp(ctx, link.AppID)
			if err != nil {
				continue
			}
			if err := m.cluster.RestartApp(ctx, env.Namespace, app.Slug); err != nil {
				m.log.Warn("could not restart an app after the restore", "app", app.ID, "error", err)
			}
		}
	}
	_ = m.db.SetStepStatus(ctx, op.ID, "verify", store.StepSucceeded,
		fmt.Sprintf("Restarted %d app(s) so they reconnect", len(links)), "")

	_ = m.db.SetOperationStatus(ctx, op.ID, store.OpSucceeded, "", "")
	m.hub.Publish(events.OperationTopic(op.ID), "operation", op)
}

// waitForJob waits for a backup or restore Job and explains a failure.
func (m *Manager) waitForJob(ctx context.Context, namespace, name string) error {
	clientset := m.cluster.Client().Clientset()
	deadline := time.Now().Add(2 * time.Hour)

	for {
		job, err := clientset.BatchV1().Jobs(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("read the job's status: %w", err)
		}
		if job.Status.Succeeded > 0 {
			return nil
		}
		if job.Status.Failed > 0 {
			return errdoc.BackupFailed(name, m.jobFailureReason(ctx, namespace, name))
		}
		if time.Now().After(deadline) {
			return errdoc.BackupFailed(name, "it did not finish within two hours")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// jobFailureReason reads the last lines of a failed job's log, which is where
// the actual reason is.
func (m *Manager) jobFailureReason(ctx context.Context, namespace, jobName string) string {
	pods, err := m.cluster.Client().Clientset().CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "job-name=" + jobName,
	})
	if err != nil || len(pods.Items) == 0 {
		return "the job failed and its output could not be read"
	}

	tail := int64(30)
	stream, err := m.cluster.Client().Clientset().CoreV1().Pods(namespace).
		GetLogs(pods.Items[0].Name, &corev1.PodLogOptions{TailLines: &tail}).Stream(ctx)
	if err != nil {
		return "the job failed and its output could not be read"
	}
	defer stream.Close()

	output, err := io.ReadAll(io.LimitReader(stream, 8<<10))
	if err != nil || len(output) == 0 {
		return "the job failed without printing anything"
	}
	return strings.TrimSpace(string(output))
}

func (m *Manager) publish(ctx context.Context, databaseID string) {
	if teamID, err := m.db.TeamIDForDatabase(ctx, databaseID); err == nil {
		backups, err := m.db.ListBackups(ctx, "database", databaseID, 10)
		if err == nil {
			m.hub.Publish(events.TeamTopic(teamID), "backups",
				map[string]any{"database_id": databaseID, "backups": backups})
		}
	}
}

// RunScheduled is called by the scheduler for every enabled policy that is due.
func (m *Manager) RunScheduled(ctx context.Context) {
	policies, err := m.db.ListEnabledBackupPolicies(ctx)
	if err != nil {
		m.log.Warn("could not read the backup schedules", "error", err)
		return
	}
	for _, policy := range policies {
		if !dueNow(policy.Schedule, time.Now().UTC()) {
			continue
		}
		if _, err := m.Run(ctx, policy.TargetType, policy.TargetID, "scheduled"); err != nil {
			m.log.Error("a scheduled backup could not start",
				"target", policy.TargetID, "error", err)
		}
	}
}

// notifyFailure tells the team a backup did not happen.
//
// A backup that silently fails is the worst kind: it is only discovered when a
// restore is attempted, which is the moment it matters most.
func (m *Manager) notifyFailure(ctx context.Context, record store.Database, problem *errdoc.Problem) {
	if m.notifier == nil {
		return
	}
	teamID, err := m.db.TeamIDForDatabase(ctx, record.ID)
	if err != nil {
		m.log.Warn("could not work out which team to notify", "database", record.ID, "error", err)
		return
	}
	m.notifier.Notify(ctx, teamID, notify.EventBackupFailed, notify.Message{
		Title:  "Backing up " + record.Name + " failed",
		Body:   problem.Error() + "\n\n" + problem.Fix,
		Level:  "error",
		Path:   "/databases/" + record.ID,
		Fields: map[string]string{"Database": record.Name, "Reason": problem.Code},
	})
}
