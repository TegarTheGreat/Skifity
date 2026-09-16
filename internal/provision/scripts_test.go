package provision

import (
	"os/exec"
	"strings"
	"testing"
)

// checkShellSyntax runs the generated script through `sh -n`, which parses it
// without running anything. A quoting mistake in a generated script would
// otherwise only show up on a real server, halfway through adding it.
func checkShellSyntax(t *testing.T, name, script string) {
	t.Helper()
	cmd := exec.Command("sh", "-n")
	cmd.Stdin = strings.NewReader(script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s is not valid POSIX shell: %v\n%s\n--- script ---\n%s",
			name, err, output, numberLines(script))
	}
}

func numberLines(s string) string {
	var b strings.Builder
	for i, line := range strings.Split(s, "\n") {
		b.WriteString(strings.TrimSuffix(line, "\r"))
		b.WriteByte('\n')
		_ = i
	}
	return b.String()
}

func TestGeneratedScriptsAreValidShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available to check syntax with")
	}

	checkShellSyntax(t, "PreflightScript", PreflightScript)
	checkShellSyntax(t, "NodeTokenScript", NodeTokenScript)
	checkShellSyntax(t, "UninstallScript", UninstallScript)
	checkShellSyntax(t, "InstallKeyScript",
		InstallKeyScript("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 skifity@panel", "root"))
	checkShellSyntax(t, "InstallKeyScript (non-root)",
		InstallKeyScript("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 skifity@panel", "deploy"))
	checkShellSyntax(t, "FirewallScript",
		FirewallScript([]string{"203.0.113.10", "203.0.113.11"}, true))
	checkShellSyntax(t, "FirewallScript (no members)", FirewallScript(nil, false))
	checkShellSyntax(t, "InstallServerScript",
		InstallServerScript("v1.33.1+k3s1", "a-token", "203.0.113.10", nil))
	checkShellSyntax(t, "JoinServerScript",
		JoinServerScript("", "a-token", "https://203.0.113.10:6443", "203.0.113.11"))
	checkShellSyntax(t, "JoinAgentScript",
		JoinAgentScript("", "a-token", "https://203.0.113.10:6443", "203.0.113.12",
			map[string]string{"skifity.io/location": "frankfurt", "skifity.io/size": "small"}))
	checkShellSyntax(t, "ConnectivityScript", ConnectivityScript("203.0.113.10", 6443))
}

func TestFirewallScriptOpensOnlyToMembers(t *testing.T) {
	script := FirewallScript([]string{"203.0.113.10"}, false)

	// The API server and the pod network must never be open to the world.
	if !strings.Contains(script, `allow_from "203.0.113.10" 6443 "tcp"`) {
		t.Fatal("the API server port was not opened to the other cluster member")
	}
	if strings.Contains(script, "allow_public 6443") {
		t.Fatal("the Kubernetes API was opened to the internet")
	}
	if strings.Contains(script, "allow_public 10250") {
		t.Fatal("the kubelet port was opened to the internet")
	}
	// HTTP and HTTPS are the only public ports.
	if !strings.Contains(script, "allow_public 80 tcp") || !strings.Contains(script, "allow_public 443 tcp") {
		t.Fatal("the web ports were not opened")
	}
	// etcd ports belong only on control plane nodes.
	if strings.Contains(script, "2379") {
		t.Fatal("etcd ports were opened on a worker")
	}

	controlPlane := FirewallScript([]string{"203.0.113.10"}, true)
	if !strings.Contains(controlPlane, `allow_from "203.0.113.10" 2380 "tcp"`) {
		t.Fatal("etcd peer port was not opened on a control plane server")
	}
}

func TestFirewallScriptIsIdempotent(t *testing.T) {
	script := FirewallScript([]string{"203.0.113.10"}, false)
	// iptables -I without a -C check accumulates a duplicate rule on every
	// retry, and retries are expected here.
	if !strings.Contains(script, "iptables -C INPUT") {
		t.Fatal("the iptables path does not check before inserting, so retrying would add duplicate rules")
	}
}

func TestInstallKeyScriptIsIdempotent(t *testing.T) {
	script := InstallKeyScript("ssh-ed25519 AAAA test@host", "root")
	if !strings.Contains(script, "grep -qF") {
		t.Fatal("the key is appended without checking, so retrying would add it repeatedly")
	}
	if !strings.Contains(script, "chmod 700") || !strings.Contains(script, "chmod 600") {
		t.Fatal("the .ssh permissions are not set; sshd ignores a group-writable authorized_keys")
	}
	// A non-root user's keys go in their own home directory.
	if !strings.Contains(InstallKeyScript("k", "deploy"), "/home/deploy") {
		t.Fatal("a non-root user's home directory was not used")
	}
}

func TestInstallScriptsCarryTheRightFlags(t *testing.T) {
	first := InstallServerScript("", "tok", "203.0.113.10", nil)
	// --cluster-init is what makes the single node HA-ready later (ADR-0002).
	if !strings.Contains(first, "--cluster-init") {
		t.Fatal("the first server is installed without --cluster-init, so it could never be made highly available")
	}
	// wireguard-native is what encrypts traffic between providers (ADR-0003).
	if !strings.Contains(first, "--flannel-backend=wireguard-native") {
		t.Fatal("the pod network is not encrypted")
	}
	if !strings.Contains(first, "--tls-san=203.0.113.10") {
		t.Fatal("the public address is not in the certificate, so kubectl from outside would fail")
	}
	if !strings.Contains(first, "--node-external-ip=203.0.113.10") {
		t.Fatal("the external address is not set, which breaks clusters spanning providers")
	}
	// Without a pinned version the stable channel is followed.
	if !strings.Contains(first, `INSTALL_K3S_CHANNEL="stable"`) {
		t.Fatal("no version and no channel were set")
	}

	pinned := InstallServerScript("v1.33.1+k3s1", "tok", "203.0.113.10", nil)
	if !strings.Contains(pinned, `INSTALL_K3S_VERSION="v1.33.1+k3s1"`) {
		t.Fatal("the pinned version was ignored")
	}
	if strings.Contains(pinned, "INSTALL_K3S_CHANNEL") {
		t.Fatal("both a version and a channel were set, which the installer rejects")
	}

	agent := JoinAgentScript("", "tok", "https://203.0.113.10:6443", "203.0.113.12", nil)
	if !strings.Contains(agent, `K3S_URL="https://203.0.113.10:6443"`) {
		t.Fatal("the agent does not know which server to join")
	}
	if strings.Contains(agent, "--cluster-init") {
		t.Fatal("an agent was given --cluster-init, which would try to start a second cluster")
	}
	if !strings.Contains(agent, `INSTALL_K3S_EXEC="agent`) {
		t.Fatal("the agent role was not set")
	}

	controlPlane := JoinServerScript("", "tok", "https://203.0.113.10:6443", "203.0.113.11")
	if !strings.Contains(controlPlane, `INSTALL_K3S_EXEC="server`) {
		t.Fatal("a promoted server was not installed in server mode")
	}
	if strings.Contains(controlPlane, "--cluster-init") {
		t.Fatal("joining a control plane node used --cluster-init, which starts a separate cluster")
	}
}

func TestAgentLabelsAreStable(t *testing.T) {
	labels := map[string]string{"b": "2", "a": "1", "c": "3"}
	first := JoinAgentScript("", "t", "u", "ip", labels)
	for range 10 {
		if JoinAgentScript("", "t", "u", "ip", labels) != first {
			t.Fatal("the generated command changes between runs, so a retry would look like a different install")
		}
	}
	if !strings.Contains(first, "--node-label=a=1 --node-label=b=2 --node-label=c=3") {
		t.Fatalf("labels are not in a stable order:\n%s", first)
	}
}
