package provision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"skifity/internal/api"
	"skifity/internal/cluster"
	"skifity/internal/crypto"
	"skifity/internal/errdoc"
	"skifity/internal/events"
	"skifity/internal/kube"
	"skifity/internal/notify"
	"skifity/internal/settings"
	"skifity/internal/sshx"
	"skifity/internal/store"
	"skifity/internal/version"
)

// Step keys, in the order they run. They are stable strings because the UI
// translates them and the database stores them.
const (
	StepConnect      = "connect"
	StepPreflight    = "preflight"
	StepInstallKey   = "install-key"
	StepFirewall     = "firewall"
	StepConnectivity = "connectivity"
	StepInstallK3s   = "install-k3s"
	StepWaitReady    = "wait-ready"
)

// addServerSteps is the plan the UI draws before anything runs.
var addServerSteps = []string{
	StepConnect, StepPreflight, StepInstallKey, StepFirewall,
	StepConnectivity, StepInstallK3s, StepWaitReady,
}

// removeServerSteps is the plan for taking a server out.
var removeServerSteps = []string{"cordon", "drain", "delete-node", "uninstall"}

// Provisioner implements api.Provisioner.
type Provisioner struct {
	db       *store.DB
	keyring  *crypto.Keyring
	hub      *events.Hub
	cluster  *cluster.Cluster
	notifier notify.Notifier
	log      *slog.Logger

	// running tracks in-flight operations so they can be cancelled.
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

// New builds a Provisioner. notifier may be nil, and then nothing is sent.
func New(db *store.DB, keyring *crypto.Keyring, hub *events.Hub, c *cluster.Cluster, notifier notify.Notifier, log *slog.Logger) *Provisioner {
	return &Provisioner{
		db: db, keyring: keyring, hub: hub, cluster: c, notifier: notifier, log: log,
		running: map[string]context.CancelFunc{},
	}
}

// AddServer registers a server and starts provisioning it.
//
// The record and the operation are created synchronously so the UI has
// something to show immediately; the work itself runs in the background,
// because it takes minutes and a browser request does not.
func (p *Provisioner) AddServer(ctx context.Context, req api.AddServerRequest) (store.Operation, error) {
	server := store.Server{
		TeamID:  req.TeamID,
		Name:    req.Name,
		Host:    req.Host,
		SSHPort: req.SSHPort,
		SSHUser: req.SSHUser,
		Status:  store.ServerPending,
		Role:    "worker",
	}
	if req.ControlPlane {
		server.Role = "control-plane"
	}
	labels := map[string]string{}
	if req.Location != "" {
		labels[version.LabelKey("location")] = kube.Slugify(req.Location)
	}
	if req.Size != "" {
		labels[version.LabelKey("size")] = kube.Slugify(req.Size)
	}
	if encoded, err := json.Marshal(labels); err == nil {
		server.Labels = string(encoded)
	}

	// A user-supplied key is stored straight away; a password is used once and
	// never stored, so there is nothing to seal in that case.
	if req.PrivateKey != "" {
		sealed, err := p.keyring.Seal([]byte(req.PrivateKey), serverKeyContext(server.ID))
		if err != nil {
			return store.Operation{}, err
		}
		server.SSHKeyEnc = sealed
	}

	if err := p.db.CreateServer(ctx, &server); err != nil {
		return store.Operation{}, err
	}
	// The id is only known after the insert, and the context binds to it.
	if req.PrivateKey != "" {
		sealed, err := p.keyring.Seal([]byte(req.PrivateKey), serverKeyContext(server.ID))
		if err != nil {
			return store.Operation{}, err
		}
		server.SSHKeyEnc = sealed
		if err := p.db.UpdateServer(ctx, &server); err != nil {
			return store.Operation{}, err
		}
	}

	op := store.Operation{
		TeamID: req.TeamID, Kind: "server.add",
		TargetType: "server", TargetID: server.ID, CreatedBy: req.CreatedBy,
	}
	if err := p.db.CreateOperation(ctx, &op, addServerSteps); err != nil {
		return store.Operation{}, err
	}

	p.start(op, func(runCtx context.Context) {
		p.runAddServer(runCtx, op, server.ID, req)
	})
	return p.db.GetOperation(ctx, op.ID)
}

// RetryServer resumes a failed provision from the step that failed.
func (p *Provisioner) RetryServer(ctx context.Context, serverID string) (store.Operation, error) {
	server, err := p.db.GetServer(ctx, serverID)
	if err != nil {
		return store.Operation{}, err
	}
	op, err := p.db.LatestOperation(ctx, "server", serverID)
	if err != nil {
		return store.Operation{}, errdoc.New("server.no_operation", "There is nothing to retry").
			WithCause("No previous attempt to add this server was recorded.").
			WithImpact("Nothing was changed.").
			WithFix("Remove the server and add it again.")
	}
	if op.Status == store.OpRunning {
		return op, nil
	}

	// Reset from the first step that is not finished, so the steps that already
	// succeeded are not repeated.
	from := StepConnect
	for _, step := range op.Steps {
		if step.Status != store.StepSucceeded {
			from = step.Key
			break
		}
	}
	if err := p.db.ResetStepsFrom(ctx, op.ID, from); err != nil {
		return store.Operation{}, err
	}
	if err := p.db.SetOperationStatus(ctx, op.ID, store.OpRunning, "", ""); err != nil {
		return store.Operation{}, err
	}

	// Retrying uses the stored key: the password was deliberately never kept.
	req := api.AddServerRequest{
		TeamID: server.TeamID, Name: server.Name, Host: server.Host,
		SSHPort: server.SSHPort, SSHUser: server.SSHUser,
		ControlPlane: server.Role == "control-plane",
	}
	p.start(op, func(runCtx context.Context) {
		p.runAddServer(runCtx, op, server.ID, req)
	})
	return p.db.GetOperation(ctx, op.ID)
}

// runAddServer is the state machine. Every step is idempotent, so a retry can
// start from any point without undoing what came before.
func (p *Provisioner) runAddServer(ctx context.Context, op store.Operation, serverID string, req api.AddServerRequest) {
	defer p.finish(op.ID)

	_ = p.db.SetOperationStatus(ctx, op.ID, store.OpRunning, "", "")
	_ = p.db.SetServerStatus(ctx, serverID, store.ServerProvisioning, "")
	p.publishOperation(ctx, op.ID)

	done, err := p.completedSteps(ctx, op.ID)
	if err != nil {
		p.failOperation(ctx, op, serverID, "internal", err)
		return
	}

	state := &addState{operationID: op.ID, serverID: serverID, request: req}

	steps := []struct {
		key string
		run func(context.Context, *addState) error
	}{
		{StepConnect, p.stepConnect},
		{StepPreflight, p.stepPreflight},
		{StepInstallKey, p.stepInstallKey},
		{StepFirewall, p.stepFirewall},
		{StepConnectivity, p.stepConnectivity},
		{StepInstallK3s, p.stepInstallK3s},
		{StepWaitReady, p.stepWaitReady},
	}

	for _, step := range steps {
		if done[step.key] {
			// Already succeeded in an earlier attempt. The connection is still
			// needed by later steps, so reconnect quietly.
			if step.key == StepConnect && state.client == nil {
				if err := p.stepConnect(ctx, state); err != nil {
					p.failStep(ctx, op, serverID, step.key, err)
					return
				}
			}
			continue
		}
		p.setStep(ctx, op, step.key, store.StepRunning, "", "")
		if err := step.run(ctx, state); err != nil {
			p.failStep(ctx, op, serverID, step.key, err)
			return
		}
		p.setStep(ctx, op, step.key, store.StepSucceeded, state.lastMessage, state.lastDetail)
		state.lastMessage, state.lastDetail = "", ""
	}

	if state.client != nil {
		_ = state.client.Close()
	}
	_ = p.db.SetOperationStatus(ctx, op.ID, store.OpSucceeded, "", "")
	_ = p.db.SetServerStatus(ctx, serverID, store.ServerReady, "")
	p.publishOperation(ctx, op.ID)
	p.log.Info("server added", "server", serverID, "host", req.Host)

	if p.notifier != nil {
		p.notifier.Notify(ctx, req.TeamID, notify.EventServerAdded, notify.Message{
			Title:  req.Name + " joined the cluster",
			Body:   "The server is ready and can run apps.",
			Level:  "success",
			Path:   "/servers/" + serverID,
			Fields: map[string]string{"Server": req.Name, "Host": req.Host},
		})
	}
}

// addState carries what one step learned to the ones after it.
type addState struct {
	// operationID is where progress and log lines are published.
	operationID string
	serverID    string
	request     api.AddServerRequest
	client      *sshx.Client
	// key is the panel-owned key installed on this server.
	key       sshx.KeyPair
	preflight Preflight
	// joinToken and serverURL come from the existing cluster.
	joinToken string
	serverURL string
	// lastMessage and lastDetail are shown against the step that just finished.
	lastMessage string
	lastDetail  string
}

func (p *Provisioner) stepConnect(ctx context.Context, state *addState) error {
	server, err := p.db.GetServer(ctx, state.serverID)
	if err != nil {
		return err
	}

	creds := sshx.Credentials{
		User:       server.SSHUser,
		Password:   state.request.Password,
		Passphrase: state.request.Passphrase,
	}
	// Prefer the panel's own key when there is one: a retry has no password.
	if server.SSHKeyEnc != "" {
		plaintext, err := p.keyring.Open(server.SSHKeyEnc, serverKeyContext(server.ID))
		if err != nil {
			return fmt.Errorf("read this server's stored key: %w", err)
		}
		creds.PrivateKey = string(plaintext)
	} else if state.request.PrivateKey != "" {
		creds.PrivateKey = state.request.PrivateKey
	}
	if creds.Password == "" && creds.PrivateKey == "" {
		return errdoc.New("ssh.no_credentials", "There are no credentials to connect with").
			WithCause("The password was used once during the first attempt and deliberately not stored.").
			WithImpact("The server cannot be reached to continue.").
			WithFix("Remove this server and add it again with its password or a private key.")
	}

	client, err := sshx.Dial(ctx, sshx.Config{
		Host: server.Host, Port: server.SSHPort,
		Credentials: creds, HostKey: server.HostKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, sshx.ErrHostKeyChanged):
			return errdoc.SSHHostKeyChanged(server.Host, server.HostKey, "a different key")
		case errors.Is(err, sshx.ErrAuthFailed):
			return errdoc.SSHAuthFailed(server.Host, server.SSHUser, creds.PrivateKey != "")
		default:
			return errdoc.SSHUnreachable(server.Host, server.SSHPort, err)
		}
	}
	state.client = client

	// Trust on first use: record what we saw so a later change is detectable.
	if server.HostKey == "" {
		server.HostKey = client.HostKey
		if err := p.db.UpdateServer(ctx, &server); err != nil {
			return err
		}
	}
	state.lastMessage = "Connected to " + server.Host
	state.lastDetail = "Host key " + client.HostKey
	return nil
}

func (p *Provisioner) stepPreflight(ctx context.Context, state *addState) error {
	result, err := state.client.Run(ctx, PreflightScript)
	if err != nil {
		return fmt.Errorf("run the preflight checks: %w", err)
	}
	report := ParsePreflight(result.Stdout)
	state.preflight = report

	requirements := DefaultRequirements()
	if state.request.ControlPlane {
		requirements = ControlPlaneRequirements()
	}
	problems := Evaluate(report, requirements, state.request.ControlPlane)

	server, err := p.db.GetServer(ctx, state.serverID)
	if err != nil {
		return err
	}
	server.OSInfo = strings.TrimSpace(report.OSName + " " + report.OSVersion)
	server.Arch = report.Arch
	server.CPUCores = report.CPUCores
	server.MemoryMB = report.MemoryMB
	server.DiskGB = report.DiskGB
	server.ExternalIP = report.PublicIP
	server.InternalIP = report.PrivateIP
	if err := p.db.UpdateServer(ctx, &server); err != nil {
		return err
	}

	if Fatal(problems) {
		for _, problem := range problems {
			if problem.Fatal {
				return errdoc.PreflightFailed(problem.Check, problem.Detail, problem.Fix).
					With("os", server.OSInfo).
					With("memory_mb", fmt.Sprint(report.MemoryMB)).
					With("disk_gb", fmt.Sprint(report.DiskGB))
			}
		}
	}

	state.lastMessage = fmt.Sprintf("%s, %d cores, %d MB memory, %d GB free",
		server.OSInfo, report.CPUCores, report.MemoryMB, report.DiskGB)
	if len(problems) > 0 {
		var notes []string
		for _, problem := range problems {
			notes = append(notes, problem.Detail+" "+problem.Fix)
		}
		state.lastDetail = strings.Join(notes, "\n")
	}
	return nil
}

// stepInstallKey installs the panel's own key and stops using the password.
//
// One key per server, generated by the panel, means the user's password is used
// exactly once and never stored, and a compromised server does not give access
// to the others.
func (p *Provisioner) stepInstallKey(ctx context.Context, state *addState) error {
	server, err := p.db.GetServer(ctx, state.serverID)
	if err != nil {
		return err
	}
	if server.SSHKeyEnc != "" {
		state.lastMessage = "This server already has a key from Skifity"
		return nil
	}

	pair, err := sshx.GenerateKeyPair(version.Binary + "@panel:" + server.Host)
	if err != nil {
		return err
	}
	state.key = pair

	result, err := state.client.Run(ctx, InstallKeyScript(pair.PublicKey, server.SSHUser))
	if err != nil {
		return fmt.Errorf("install the panel's key: %w", err)
	}
	if result.ExitCode != 0 {
		return errdoc.New("ssh.key_install_failed", "The panel's key could not be installed").
			WithCause("Writing to authorized_keys on %s failed: %s", server.Host, result.Combined()).
			WithImpact("The server was not added.").
			WithFix("Check that %s can write to its own home directory and that the disk is not full.", server.SSHUser).
			Retry()
	}

	// Prove the key works before throwing the password away, or a retry would
	// have no way in at all.
	verify, err := sshx.Dial(ctx, sshx.Config{
		Host: server.Host, Port: server.SSHPort, HostKey: server.HostKey,
		Credentials: sshx.Credentials{User: server.SSHUser, PrivateKey: pair.PrivateKey},
	})
	if err != nil {
		return errdoc.New("ssh.key_verify_failed", "The panel's key was installed but does not work").
			WithCause("Connecting with the new key failed: %s", err.Error()).
			WithImpact("The server was not added. The password was not discarded.").
			WithFix("Check the server's sshd settings: PubkeyAuthentication must be yes, and the home directory must not be group-writable.").
			Retry()
	}
	_ = verify.Close()

	sealed, err := p.keyring.Seal([]byte(pair.PrivateKey), serverKeyContext(server.ID))
	if err != nil {
		return err
	}
	server.SSHKeyEnc = sealed
	if err := p.db.UpdateServer(ctx, &server); err != nil {
		return err
	}

	// The password is now out of scope and was never written anywhere.
	state.request.Password = ""
	state.lastMessage = "Installed a key just for this server; the password was not stored"
	state.lastDetail = pair.Fingerprint
	return nil
}

func (p *Provisioner) stepFirewall(ctx context.Context, state *addState) error {
	members, err := p.memberAddresses(ctx, state.serverID)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		state.lastMessage = "No other servers yet, so only ports 80 and 443 were opened"
	}

	result, err := state.client.Run(ctx, FirewallScript(members, state.request.ControlPlane))
	if err != nil {
		return fmt.Errorf("configure the firewall: %w", err)
	}
	if result.ExitCode != 0 {
		return errdoc.New("firewall.failed", "The firewall could not be configured").
			WithCause("The firewall commands failed on %s: %s", state.request.Host, result.Combined()).
			WithImpact("The server was not added.").
			WithFix("Check that the account Skifity connects with can run ufw or iptables. Root, or sudo without a password, is needed.").
			Retry()
	}
	if strings.Contains(result.Stdout, "firewall_configured=yes") {
		state.lastMessage = "Opened the cluster ports to the other servers only, using ufw"
	} else {
		state.lastMessage = "Opened the cluster ports to the other servers only, using iptables"
	}
	return nil
}

// stepConnectivity checks that the new server can actually reach the cluster.
//
// This is the check that catches a provider firewall, which SSH cannot open,
// and it is worth a step of its own because the alternative is a k3s install
// that fails five minutes later with a timeout nobody can interpret.
func (p *Provisioner) stepConnectivity(ctx context.Context, state *addState) error {
	primary, err := p.primaryServer(ctx, state.serverID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			state.lastMessage = "This is the first server, so there is nothing to connect to yet"
			return nil
		}
		return err
	}

	target := primary.ExternalIP
	if target == "" {
		target = primary.Host
	}
	result, err := state.client.Run(ctx, ConnectivityScript(target, 6443))
	if err != nil {
		return fmt.Errorf("check connectivity to the cluster: %w", err)
	}
	if !strings.Contains(result.Stdout, "reachable=yes") {
		return errdoc.PortBlocked(target, 6443, "tcp").
			With("from", state.request.Host).
			With("to", target).
			With("hint", "This is usually a firewall in your provider's control panel rather than on the server itself. Skifity can configure ufw and iptables over SSH, but it cannot reach a firewall that sits in front of the machine.")
	}
	state.lastMessage = fmt.Sprintf("Reached %s on port 6443", target)
	return nil
}

func (p *Provisioner) stepInstallK3s(ctx context.Context, state *addState) error {
	server, err := p.db.GetServer(ctx, state.serverID)
	if err != nil {
		return err
	}

	publicIP := server.ExternalIP
	if publicIP == "" {
		publicIP = server.Host
	}

	primary, err := p.primaryServer(ctx, state.serverID)
	isFirst := errors.Is(err, store.ErrNotFound)
	if err != nil && !isFirst {
		return err
	}

	var script string
	switch {
	case isFirst:
		// The first server. The token is generated here and kept encrypted, so
		// later servers can join without anyone reading it off a machine.
		token, err := p.clusterToken(ctx, server.TeamID)
		if err != nil {
			return err
		}
		script = InstallServerScript(p.k3sVersion(ctx), token, publicIP, nil)

	default:
		token, err := p.clusterToken(ctx, server.TeamID)
		if err != nil {
			return err
		}
		primaryIP := primary.ExternalIP
		if primaryIP == "" {
			primaryIP = primary.Host
		}
		state.serverURL = fmt.Sprintf("https://%s:6443", primaryIP)
		state.joinToken = token

		labels := map[string]string{}
		if state.request.Location != "" {
			labels[version.LabelKey("location")] = kube.Slugify(state.request.Location)
		}
		if state.request.Size != "" {
			labels[version.LabelKey("size")] = kube.Slugify(state.request.Size)
		}
		if state.request.ControlPlane {
			script = JoinServerScript(p.k3sVersion(ctx), token, state.serverURL, publicIP)
		} else {
			script = JoinAgentScript(p.k3sVersion(ctx), token, state.serverURL, publicIP, labels)
		}
	}

	// Installation takes minutes and prints progress; streaming it is what
	// turns a blank wait into something a user can watch.
	result, err := state.client.RunStreaming(ctx, script, func(stream, line string) {
		p.hub.Publish(events.OperationTopic(state.operationID), "log",
			map[string]string{"step": StepInstallK3s, "stream": stream, "line": line})
	})
	if err != nil {
		return fmt.Errorf("install Kubernetes: %w", err)
	}
	if result.ExitCode != 0 {
		return errdoc.K3sInstallFailed(server.Host, result.ExitCode, result.Combined())
	}

	state.lastMessage = "Kubernetes is installed and running"
	return nil
}

func (p *Provisioner) stepWaitReady(ctx context.Context, state *addState) error {
	server, err := p.db.GetServer(ctx, state.serverID)
	if err != nil {
		return err
	}
	if p.cluster == nil {
		state.lastMessage = "Installed, but the panel is not connected to the cluster to confirm"
		return nil
	}

	// The node registers under its hostname, which is not necessarily the name
	// the user gave it, so find it by address.
	deadline := time.Now().Add(5 * time.Minute)
	for {
		summary, err := p.cluster.Client().Summary(ctx)
		if err == nil {
			for _, node := range summary.Nodes {
				if node.ExternalIP != server.ExternalIP && node.InternalIP != server.InternalIP &&
					node.Name != server.Name {
					continue
				}
				if !node.Ready {
					break
				}
				server.NodeName = node.Name
				server.Status = store.ServerReady
				if err := p.db.UpdateServer(ctx, &server); err != nil {
					return err
				}
				if err := p.applyNodeLabels(ctx, server); err != nil {
					p.log.Warn("could not label the node", "node", node.Name, "error", err)
				}
				state.lastMessage = fmt.Sprintf("%s joined the cluster and is ready", node.Name)
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errdoc.New("cluster.node_not_ready", "The server did not join the cluster").
				WithCause("Kubernetes was installed on %s, but no node became ready within five minutes.", server.Host).
				WithImpact("The server is registered but is not running anything yet.").
				WithFix("Check that this server can reach the first server on port 6443, and that the two can reach each other on UDP 51820. A firewall in your provider's control panel is the usual cause.").
				WithDocs("/docs/adding-servers#when-a-step-fails").
				Retry()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// applyNodeLabels puts the user's location and size labels onto the node, so
// apps can be steered without anyone touching kubectl.
func (p *Provisioner) applyNodeLabels(ctx context.Context, server store.Server) error {
	if p.cluster == nil || server.NodeName == "" {
		return nil
	}
	var labels map[string]string
	if server.Labels != "" {
		if err := json.Unmarshal([]byte(server.Labels), &labels); err != nil {
			return fmt.Errorf("read the stored node labels: %w", err)
		}
	}
	labels[version.LabelKey("server-id")] = server.ID
	labels[version.LabelKey("managed")] = "true"

	node, err := p.cluster.Client().Clientset().CoreV1().Nodes().Get(ctx, server.NodeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read the node: %w", err)
	}
	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	for k, v := range labels {
		node.Labels[k] = v
	}
	if _, err := p.cluster.Client().Clientset().CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("label the node: %w", err)
	}
	return nil
}

// clusterToken returns the shared k3s join token, creating it on first use.
//
// Generating it here rather than reading it off the first server means the
// token exists before the first server does, so the install is one pass.
func (p *Provisioner) clusterToken(ctx context.Context, teamID string) (string, error) {
	const key = "cluster.join_token"
	sealed, encrypted, err := p.db.GetSetting(ctx, key)
	if err != nil {
		return "", err
	}
	if sealed != "" {
		if !encrypted {
			return sealed, nil
		}
		plaintext, err := p.keyring.Open(sealed, settings.Context(key))
		if err != nil {
			return "", fmt.Errorf("read the cluster join token: %w", err)
		}
		return string(plaintext), nil
	}

	token, err := crypto.RandomToken(32)
	if err != nil {
		return "", err
	}
	newSealed, err := p.keyring.Seal([]byte(token), settings.Context(key))
	if err != nil {
		return "", err
	}
	if err := p.db.SetSetting(ctx, key, newSealed, true, "system"); err != nil {
		return "", err
	}
	return token, nil
}

// k3sVersion reads the pinned version, or empty for the stable channel.
func (p *Provisioner) k3sVersion(ctx context.Context) string {
	value, _, err := p.db.GetSetting(ctx, "cluster.k3s_version")
	if err != nil {
		return ""
	}
	return value
}

// primaryServer returns the control plane server to join, excluding one.
func (p *Provisioner) primaryServer(ctx context.Context, excludeID string) (store.Server, error) {
	servers, err := p.db.ListAllServers(ctx)
	if err != nil {
		return store.Server{}, err
	}
	for _, s := range servers {
		if s.ID == excludeID || s.Role != "control-plane" {
			continue
		}
		if s.Status == store.ServerReady || s.Status == store.ServerNotReady {
			return s, nil
		}
	}
	return store.Server{}, store.ErrNotFound
}

// memberAddresses lists the addresses of the other cluster members, so the
// firewall opens cluster ports to them and nobody else.
func (p *Provisioner) memberAddresses(ctx context.Context, excludeID string) ([]string, error) {
	servers, err := p.db.ListAllServers(ctx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range servers {
		if s.ID == excludeID {
			continue
		}
		for _, addr := range []string{s.ExternalIP, s.InternalIP} {
			if addr != "" && !contains(out, addr) {
				out = append(out, addr)
			}
		}
	}
	return out, nil
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}

func serverKeyContext(serverID string) string { return "server_key:" + serverID }
