// Package provision turns a bare VPS into a cluster node and back.
package provision

import (
	"fmt"
	"strconv"
	"strings"
)

// Preflight is what the panel learned about a server before touching it.
type Preflight struct {
	OSName    string `json:"os_name"`
	OSVersion string `json:"os_version"`
	OSID      string `json:"os_id"`
	Kernel    string `json:"kernel"`
	Arch      string `json:"arch"`
	CPUCores  int    `json:"cpu_cores"`
	MemoryMB  int    `json:"memory_mb"`
	DiskGB    int    `json:"disk_gb"`
	// PublicIP is what the server believes its own address is.
	PublicIP  string `json:"public_ip"`
	PrivateIP string `json:"private_ip"`
	// Conflicts are things already installed that would get in the way.
	Conflicts []string `json:"conflicts"`
	// HasWireGuard reports whether the kernel can do encrypted pod networking.
	HasWireGuard bool `json:"has_wireguard"`
	// HasSystemd is required: k3s installs as a systemd unit.
	HasSystemd bool `json:"has_systemd"`
	// PortsInUse are the cluster ports something else is already listening on.
	PortsInUse []int `json:"ports_in_use"`
	// K3sInstalled reports whether this machine is already a node, which makes
	// adding it again a no-op rather than a failure.
	K3sInstalled bool `json:"k3s_installed"`
	// Warnings are things that work but are worth saying.
	Warnings []string `json:"warnings"`
}

// Requirements are the minimums a server must meet.
type Requirements struct {
	MinMemoryMB int
	MinDiskGB   int
	MinCPUCores int
}

// DefaultRequirements is what the panel enforces.
//
// 1 GB is the real floor for a k3s agent plus a small app: below that the
// kubelet starts evicting as soon as anything runs. The control plane node
// needs more, which is checked separately.
func DefaultRequirements() Requirements {
	return Requirements{MinMemoryMB: 900, MinDiskGB: 8, MinCPUCores: 1}
}

// ControlPlaneRequirements are stricter, because etcd and the API server live
// there too.
func ControlPlaneRequirements() Requirements {
	return Requirements{MinMemoryMB: 1800, MinDiskGB: 20, MinCPUCores: 2}
}

// Problem is one reason a server cannot join, with the fix.
type Problem struct {
	Check  string `json:"check"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
	// Fatal problems stop the process; others are warnings.
	Fatal bool `json:"fatal"`
}

// ClusterPorts are the ports cluster members must reach on each other.
var ClusterPorts = []struct {
	Port     int
	Protocol string
	Purpose  string
	// ControlPlaneOnly ports are needed only on servers running etcd.
	ControlPlaneOnly bool
}{
	{Port: 6443, Protocol: "tcp", Purpose: "Kubernetes API"},
	{Port: 10250, Protocol: "tcp", Purpose: "kubelet metrics and logs"},
	{Port: 8472, Protocol: "udp", Purpose: "pod network (vxlan)"},
	{Port: 51820, Protocol: "udp", Purpose: "pod network (WireGuard IPv4)"},
	{Port: 51821, Protocol: "udp", Purpose: "pod network (WireGuard IPv6)"},
	{Port: 2379, Protocol: "tcp", Purpose: "etcd client", ControlPlaneOnly: true},
	{Port: 2380, Protocol: "tcp", Purpose: "etcd peers", ControlPlaneOnly: true},
}

// supportedDistros are the ones the installer is tested on. Others are allowed
// with a warning rather than refused: k3s runs on far more than we can test.
var supportedDistros = map[string]bool{
	"ubuntu": true,
	"debian": true,
}

// knownWorkingDistros run k3s fine but are not covered by our smoke tests.
var knownWorkingDistros = map[string]bool{
	"almalinux": true,
	"rocky":     true,
	"fedora":    true,
	"centos":    true,
	"rhel":      true,
	"opensuse":  true,
	"sles":      true,
	"alpine":    true,
	"arch":      true,
}

// Evaluate checks a preflight report against the requirements.
func Evaluate(p Preflight, req Requirements, controlPlane bool) []Problem {
	var problems []Problem

	if !p.HasSystemd {
		problems = append(problems, Problem{
			Check:  "systemd",
			Detail: "This server does not appear to use systemd.",
			Fix:    "Skifity installs Kubernetes as a systemd service. Use a distribution with systemd, such as Ubuntu 24.04 or Debian 12. Containers without an init system will not work.",
			Fatal:  true,
		})
	}

	switch {
	case supportedDistros[p.OSID]:
	case knownWorkingDistros[p.OSID]:
		problems = append(problems, Problem{
			Check:  "os",
			Detail: fmt.Sprintf("%s %s is not one of the distributions Skifity is tested on.", p.OSName, p.OSVersion),
			Fix:    "It should work, but if you hit trouble, Ubuntu 24.04 and Debian 12 are the tested options.",
		})
	case p.OSID == "":
		problems = append(problems, Problem{
			Check:  "os",
			Detail: "Skifity could not work out which distribution this server runs.",
			Fix:    "Check that /etc/os-release exists and is readable. Ubuntu 24.04 and Debian 12 are the tested options.",
		})
	default:
		problems = append(problems, Problem{
			Check:  "os",
			Detail: fmt.Sprintf("%s is not a distribution Skifity recognises.", p.OSName),
			Fix:    "Use Ubuntu 24.04 or Debian 12. Other systemd distributions usually work, but are not tested.",
		})
	}

	switch p.Arch {
	case "x86_64", "amd64", "aarch64", "arm64":
	case "":
		problems = append(problems, Problem{
			Check: "arch", Detail: "The server's CPU architecture could not be determined.",
			Fix: "Check that `uname -m` works over SSH.", Fatal: true,
		})
	default:
		problems = append(problems, Problem{
			Check:  "arch",
			Detail: fmt.Sprintf("This server is %s, which Skifity does not build for.", p.Arch),
			Fix:    "Use a 64-bit x86 (amd64) or ARM (arm64) server.",
			Fatal:  true,
		})
	}

	if p.MemoryMB > 0 && p.MemoryMB < req.MinMemoryMB {
		role := "a worker"
		if controlPlane {
			role = "a control plane server"
		}
		problems = append(problems, Problem{
			Check:  "memory",
			Detail: fmt.Sprintf("This server has %d MB of memory; %s needs at least %d MB.", p.MemoryMB, role, req.MinMemoryMB),
			Fix:    "Resize the server, or add it as a worker rather than a control plane server.",
			Fatal:  true,
		})
	}
	if p.DiskGB > 0 && p.DiskGB < req.MinDiskGB {
		problems = append(problems, Problem{
			Check:  "disk",
			Detail: fmt.Sprintf("This server has %d GB of free disk; at least %d GB is needed.", p.DiskGB, req.MinDiskGB),
			Fix:    "Free up space or resize the disk. Container images alone usually need several gigabytes.",
			Fatal:  true,
		})
	}
	if p.CPUCores > 0 && p.CPUCores < req.MinCPUCores {
		problems = append(problems, Problem{
			Check:  "cpu",
			Detail: fmt.Sprintf("This server has %d CPU core(s); at least %d is needed.", p.CPUCores, req.MinCPUCores),
			Fix:    "Resize the server to one with more cores.",
			Fatal:  true,
		})
	}

	for _, conflict := range p.Conflicts {
		switch conflict {
		case "docker":
			problems = append(problems, Problem{
				Check:  "conflict",
				Detail: "Docker is already running on this server.",
				Fix:    "Docker and k3s can coexist, but they compete for memory and both manage iptables rules. On a small server, remove Docker first with: systemctl disable --now docker",
			})
		case "nginx", "apache2", "caddy":
			problems = append(problems, Problem{
				Check:  "conflict",
				Detail: fmt.Sprintf("%s is listening on this server.", conflict),
				Fix:    fmt.Sprintf("It is using ports 80 and 443, which the cluster's ingress needs. Stop it with: systemctl disable --now %s", conflict),
				Fatal:  true,
			})
		case "k3s", "k8s":
			// Not a problem: this is how re-adding a server works.
		default:
			problems = append(problems, Problem{
				Check:  "conflict",
				Detail: fmt.Sprintf("%s is running on this server.", conflict),
				Fix:    "Check that it does not use ports 80, 443 or 6443.",
			})
		}
	}

	for _, port := range p.PortsInUse {
		problems = append(problems, Problem{
			Check:  "port",
			Detail: fmt.Sprintf("Something is already listening on port %d.", port),
			Fix:    fmt.Sprintf("Find it with: ss -lntup 'sport = :%d' and stop it, or use a server with that port free.", port),
			Fatal:  port == 80 || port == 443 || port == 6443,
		})
	}

	if !p.HasWireGuard {
		problems = append(problems, Problem{
			Check:  "wireguard",
			Detail: "This kernel does not have the WireGuard module.",
			Fix:    "Traffic between your servers will use vxlan, which is not encrypted. If your servers are with different providers, install WireGuard first: apt-get install -y wireguard-tools",
		})
	}

	if p.PublicIP == "" {
		problems = append(problems, Problem{
			Check:  "network",
			Detail: "Skifity could not determine this server's public address.",
			Fix:    "Servers at different providers must reach each other by public address. Check that the server has one, and that outbound connections work.",
		})
	}

	return problems
}

// Fatal reports whether any problem stops the server from being added.
func Fatal(problems []Problem) bool {
	for _, p := range problems {
		if p.Fatal {
			return true
		}
	}
	return false
}

// ParsePreflight reads the output of the preflight script.
//
// The script writes `key=value` lines, which keeps the parsing trivial and the
// script readable. Anything it could not determine is simply absent, and the
// zero value means "unknown" rather than "zero".
func ParsePreflight(output string) Preflight {
	var p Preflight
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)

		switch key {
		case "os_name":
			p.OSName = value
		case "os_version":
			p.OSVersion = value
		case "os_id":
			p.OSID = strings.ToLower(value)
		case "kernel":
			p.Kernel = value
		case "arch":
			p.Arch = value
		case "cpu_cores":
			p.CPUCores = atoi(value)
		case "memory_mb":
			p.MemoryMB = atoi(value)
		case "disk_gb":
			p.DiskGB = atoi(value)
		case "public_ip":
			p.PublicIP = value
		case "private_ip":
			p.PrivateIP = value
		case "has_wireguard":
			p.HasWireGuard = value == "yes"
		case "has_systemd":
			p.HasSystemd = value == "yes"
		case "k3s_installed":
			p.K3sInstalled = value == "yes"
		case "conflicts":
			for _, c := range strings.Fields(strings.ReplaceAll(value, ",", " ")) {
				if c != "" {
					p.Conflicts = append(p.Conflicts, strings.ToLower(c))
				}
			}
		case "ports_in_use":
			for _, portText := range strings.Fields(strings.ReplaceAll(value, ",", " ")) {
				if port := atoi(portText); port > 0 {
					p.PortsInUse = append(p.PortsInUse, port)
				}
			}
		}
	}
	return p
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
