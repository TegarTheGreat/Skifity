package provision

import (
	"strings"
	"testing"
)

// realPreflightOutput is what the script prints on a healthy Ubuntu server.
const realPreflightOutput = `os_name=Ubuntu
os_version=24.04
os_id=ubuntu
kernel=6.8.0-45-generic
arch=x86_64
has_systemd=yes
cpu_cores=2
memory_mb=3936
disk_gb=38
private_ip=10.0.0.5
public_ip=203.0.113.10
has_wireguard=yes
k3s_installed=no
conflicts=
ports_in_use=
`

func TestParsePreflight(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)

	if p.OSID != "ubuntu" || p.OSVersion != "24.04" {
		t.Fatalf("distribution parsed as %q %q", p.OSID, p.OSVersion)
	}
	if p.Arch != "x86_64" {
		t.Fatalf("architecture is %q", p.Arch)
	}
	if p.CPUCores != 2 || p.MemoryMB != 3936 || p.DiskGB != 38 {
		t.Fatalf("resources parsed as %d cores, %d MB, %d GB", p.CPUCores, p.MemoryMB, p.DiskGB)
	}
	if p.PublicIP != "203.0.113.10" || p.PrivateIP != "10.0.0.5" {
		t.Fatalf("addresses parsed as %q and %q", p.PublicIP, p.PrivateIP)
	}
	if !p.HasWireGuard || !p.HasSystemd {
		t.Fatal("capability flags were not read")
	}
	if len(p.Conflicts) != 0 || len(p.PortsInUse) != 0 {
		t.Fatalf("empty lists became %v and %v", p.Conflicts, p.PortsInUse)
	}
}

func TestParsePreflightWithProblems(t *testing.T) {
	output := `os_name=Debian GNU/Linux
os_id=DEBIAN
arch=aarch64
has_systemd=yes
memory_mb=512
disk_gb=4
conflicts=docker nginx
ports_in_use=80 443
has_wireguard=no
k3s_installed=yes
# a comment line
not a key value line
`
	p := ParsePreflight(output)
	if p.OSID != "debian" {
		t.Fatalf("the distribution id was not lowercased: %q", p.OSID)
	}
	if len(p.Conflicts) != 2 || p.Conflicts[0] != "docker" {
		t.Fatalf("conflicts parsed as %v", p.Conflicts)
	}
	if len(p.PortsInUse) != 2 || p.PortsInUse[0] != 80 {
		t.Fatalf("ports parsed as %v", p.PortsInUse)
	}
	if !p.K3sInstalled {
		t.Fatal("an existing k3s installation was not noticed")
	}
	// A comment and a junk line must not derail the rest.
	if p.MemoryMB != 512 {
		t.Fatalf("memory is %d after a junk line", p.MemoryMB)
	}
}

func TestParsePreflightHandlesQuotedValues(t *testing.T) {
	// /etc/os-release quotes its values, and the script echoes them through.
	p := ParsePreflight(`os_name="Ubuntu"` + "\n" + `os_version="24.04"` + "\n")
	if p.OSName != "Ubuntu" || p.OSVersion != "24.04" {
		t.Fatalf("quotes were not stripped: %q %q", p.OSName, p.OSVersion)
	}
}

func TestEvaluateAcceptsAHealthyServer(t *testing.T) {
	problems := Evaluate(ParsePreflight(realPreflightOutput), DefaultRequirements(), false)
	if Fatal(problems) {
		t.Fatalf("a healthy Ubuntu server was rejected: %+v", problems)
	}
	if len(problems) != 0 {
		t.Fatalf("a healthy server produced warnings: %+v", problems)
	}
}

func TestEvaluateRejectsTooSmall(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)
	p.MemoryMB = 480
	problems := Evaluate(p, DefaultRequirements(), false)
	if !Fatal(problems) {
		t.Fatal("a 480 MB server was accepted")
	}
	found := false
	for _, problem := range problems {
		if problem.Check == "memory" {
			found = true
			// The message must say what to do, not just what is wrong.
			if problem.Fix == "" {
				t.Fatal("the memory problem has no suggested fix")
			}
			if !strings.Contains(problem.Detail, "480") {
				t.Fatalf("the problem does not say how much memory was found: %q", problem.Detail)
			}
		}
	}
	if !found {
		t.Fatalf("no memory problem was reported: %+v", problems)
	}
}

func TestControlPlaneNeedsMore(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)
	p.MemoryMB = 1024
	p.CPUCores = 1

	// A 1 GB single-core server is a fine worker.
	if Fatal(Evaluate(p, DefaultRequirements(), false)) {
		t.Fatal("a 1 GB server was rejected as a worker")
	}
	// But etcd and the API server need more than that.
	if !Fatal(Evaluate(p, ControlPlaneRequirements(), true)) {
		t.Fatal("a 1 GB single-core server was accepted as a control plane server")
	}
}

func TestEvaluateBlocksWebServerConflicts(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)
	p.Conflicts = []string{"nginx"}
	problems := Evaluate(p, DefaultRequirements(), false)
	if !Fatal(problems) {
		t.Fatal("a server already running nginx on ports 80 and 443 was accepted")
	}
	for _, problem := range problems {
		if problem.Check == "conflict" && !strings.Contains(problem.Fix, "systemctl disable") {
			t.Fatalf("the conflict message does not give the command to fix it: %q", problem.Fix)
		}
	}
}

func TestDockerIsAWarningNotAFailure(t *testing.T) {
	// Docker and k3s can coexist; refusing would be unhelpful.
	p := ParsePreflight(realPreflightOutput)
	p.Conflicts = []string{"docker"}
	problems := Evaluate(p, DefaultRequirements(), false)
	if Fatal(problems) {
		t.Fatal("a server running Docker was refused outright")
	}
	if len(problems) == 0 {
		t.Fatal("Docker was not mentioned at all")
	}
}

func TestExistingK3sIsNotAConflict(t *testing.T) {
	// Re-adding a server that is already a node must work: that is how a
	// retry after a partial failure gets through.
	p := ParsePreflight(realPreflightOutput)
	p.Conflicts = []string{"k3s"}
	p.K3sInstalled = true
	if problems := Evaluate(p, DefaultRequirements(), false); len(problems) != 0 {
		t.Fatalf("an existing k3s installation was treated as a problem: %+v", problems)
	}
}

func TestMissingWireGuardWarnsAboutEncryption(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)
	p.HasWireGuard = false
	problems := Evaluate(p, DefaultRequirements(), false)
	if Fatal(problems) {
		t.Fatal("a kernel without WireGuard was refused; vxlan still works")
	}
	found := false
	for _, problem := range problems {
		if problem.Check == "wireguard" {
			found = true
			if !strings.Contains(problem.Detail+problem.Fix, "not encrypted") {
				t.Fatalf("the warning does not say the traffic will be unencrypted: %q %q", problem.Detail, problem.Fix)
			}
		}
	}
	if !found {
		t.Fatal("no warning about unencrypted pod traffic")
	}
}

func TestNoSystemdIsFatal(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)
	p.HasSystemd = false
	if !Fatal(Evaluate(p, DefaultRequirements(), false)) {
		t.Fatal("a server without systemd was accepted, but k3s installs as a systemd unit")
	}
}

func TestUnknownArchitectureIsFatal(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)
	p.Arch = "riscv64"
	if !Fatal(Evaluate(p, DefaultRequirements(), false)) {
		t.Fatal("an architecture with no builds was accepted")
	}
	// arm64 must be accepted: cheap ARM VPSes are a large part of the audience.
	for _, arch := range []string{"aarch64", "arm64", "amd64", "x86_64"} {
		p.Arch = arch
		if Fatal(Evaluate(p, DefaultRequirements(), false)) {
			t.Fatalf("%s was rejected", arch)
		}
	}
}

func TestUntestedDistroIsAWarning(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)
	p.OSID = "almalinux"
	p.OSName = "AlmaLinux"
	problems := Evaluate(p, DefaultRequirements(), false)
	if Fatal(problems) {
		t.Fatal("AlmaLinux was refused; k3s runs on it fine")
	}
	if len(problems) == 0 {
		t.Fatal("an untested distribution produced no warning at all")
	}
}

func TestPortsInUseAreFatalForTheOnesThatMatter(t *testing.T) {
	p := ParsePreflight(realPreflightOutput)
	p.PortsInUse = []int{443}
	if !Fatal(Evaluate(p, DefaultRequirements(), false)) {
		t.Fatal("a server with port 443 taken was accepted; the ingress could never start")
	}

	p.PortsInUse = []int{10250}
	problems := Evaluate(p, DefaultRequirements(), false)
	if Fatal(problems) {
		t.Fatal("port 10250 being in use was treated as fatal; it is worth a warning")
	}
	if len(problems) == 0 {
		t.Fatal("a busy cluster port produced no warning")
	}
}
