package provision

import (
	"fmt"
	"strings"
)

// The shell that runs on the servers being added.
//
// Everything here is POSIX sh, is safe to run twice, and prints machine-readable
// output. It is kept as Go string constants rather than uploaded files so that
// one binary is genuinely all a user needs.

// PreflightScript inspects a server without changing anything on it.
const PreflightScript = `
set -u

# Distribution
if [ -r /etc/os-release ]; then
  . /etc/os-release
  echo "os_name=${NAME:-unknown}"
  echo "os_version=${VERSION_ID:-unknown}"
  echo "os_id=${ID:-unknown}"
else
  echo "os_name=unknown"
  echo "os_id="
fi

echo "kernel=$(uname -r 2>/dev/null || echo unknown)"
echo "arch=$(uname -m 2>/dev/null || echo unknown)"

# systemd is required: k3s installs as a unit.
if [ -d /run/systemd/system ]; then echo "has_systemd=yes"; else echo "has_systemd=no"; fi

# Cores, memory and free disk on the root filesystem.
cores=$(nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 0)
echo "cpu_cores=${cores}"

if [ -r /proc/meminfo ]; then
  mem_kb=$(awk '/^MemTotal:/ {print $2}' /proc/meminfo)
  echo "memory_mb=$((mem_kb / 1024))"
fi

disk=$(df -BG --output=avail / 2>/dev/null | tail -n1 | tr -dc '0-9')
[ -n "${disk}" ] && echo "disk_gb=${disk}"

# Addresses. The default route's source address is the one other servers can
# reach, which is what matters for a cluster spanning providers.
private_ip=$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for(i=1;i<=NF;i++) if($i=="src") print $(i+1); exit}')
[ -n "${private_ip}" ] && echo "private_ip=${private_ip}"

public_ip=""
for url in https://api.ipify.org https://ifconfig.me/ip https://icanhazip.com; do
  public_ip=$(curl -fsS --max-time 5 "$url" 2>/dev/null | tr -dc '0-9.') || true
  [ -n "${public_ip}" ] && break
done
# A server behind NAT with no outbound access still has its route address.
[ -z "${public_ip}" ] && public_ip="${private_ip}"
[ -n "${public_ip}" ] && echo "public_ip=${public_ip}"

# WireGuard, for encrypted traffic between providers.
if modprobe wireguard 2>/dev/null || lsmod 2>/dev/null | grep -q '^wireguard' || [ -d /sys/module/wireguard ]; then
  echo "has_wireguard=yes"
else
  echo "has_wireguard=no"
fi

# Is this already a node?
if command -v k3s >/dev/null 2>&1 || [ -x /usr/local/bin/k3s ]; then
  echo "k3s_installed=yes"
else
  echo "k3s_installed=no"
fi

# Software that would get in the way.
conflicts=""
for svc in docker nginx apache2 caddy containerd microk8s; do
  if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet "$svc" 2>/dev/null; then
    conflicts="${conflicts} ${svc}"
  fi
done
echo "conflicts=${conflicts# }"

# Ports the cluster needs, that something else already holds.
in_use=""
for port in 80 443 6443 10250 2379 2380; do
  if command -v ss >/dev/null 2>&1; then
    if ss -lnt 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${port}\$"; then
      in_use="${in_use} ${port}"
    fi
  elif command -v netstat >/dev/null 2>&1; then
    if netstat -lnt 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${port}\$"; then
      in_use="${in_use} ${port}"
    fi
  fi
done
echo "ports_in_use=${in_use# }"
`

// InstallKeyScript adds the panel's public key to authorized_keys, once.
//
// Appending only when absent is what makes the whole add-server flow safe to
// retry: running it five times leaves one key, not five.
func InstallKeyScript(publicKey, user string) string {
	home := "/root"
	if user != "root" {
		home = "/home/" + user
	}
	return fmt.Sprintf(`
set -eu
HOME_DIR=%q
KEY=%q

mkdir -p "$HOME_DIR/.ssh"
chmod 700 "$HOME_DIR/.ssh"
touch "$HOME_DIR/.ssh/authorized_keys"
chmod 600 "$HOME_DIR/.ssh/authorized_keys"

# Only append when the key is not already there, so retrying is harmless.
if ! grep -qF "$KEY" "$HOME_DIR/.ssh/authorized_keys" 2>/dev/null; then
  printf '%%s\n' "$KEY" >> "$HOME_DIR/.ssh/authorized_keys"
fi

# An authorized_keys file the group can write is ignored by sshd, which fails
# in a way that looks like a rejected key.
chown -R %s "$HOME_DIR/.ssh" 2>/dev/null || true
echo "key_installed=yes"
`, home, strings.TrimSpace(publicKey), user)
}

// FirewallScript opens the cluster ports to the other members only.
//
// It prefers ufw where it is active, because that is what the server's owner
// will look at later, and falls back to iptables. Ports are opened per source
// address rather than to the world: the pod network and the API server should
// never be reachable from the internet.
func FirewallScript(memberIPs []string, controlPlane bool) string {
	var rules strings.Builder
	for _, port := range ClusterPorts {
		if port.ControlPlaneOnly && !controlPlane {
			continue
		}
		for _, ip := range memberIPs {
			if ip == "" {
				continue
			}
			fmt.Fprintf(&rules, "allow_from %q %d %q\n", ip, port.Port, port.Protocol)
		}
	}

	return fmt.Sprintf(`
set -u

HAS_UFW=no
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | head -n1 | grep -qi active; then
  HAS_UFW=yes
fi

allow_from() {
  ip="$1"; port="$2"; proto="$3"
  if [ "$HAS_UFW" = yes ]; then
    # ufw is idempotent: adding a rule twice keeps one.
    ufw allow from "$ip" to any port "$port" proto "$proto" >/dev/null 2>&1 || true
  elif command -v iptables >/dev/null 2>&1; then
    # -C checks first, so this does not accumulate duplicate rules.
    if ! iptables -C INPUT -p "$proto" -s "$ip" --dport "$port" -j ACCEPT 2>/dev/null; then
      iptables -I INPUT -p "$proto" -s "$ip" --dport "$port" -j ACCEPT 2>/dev/null || true
    fi
  fi
}

allow_public() {
  port="$1"; proto="$2"
  if [ "$HAS_UFW" = yes ]; then
    ufw allow "$port"/"$proto" >/dev/null 2>&1 || true
  elif command -v iptables >/dev/null 2>&1; then
    if ! iptables -C INPUT -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null; then
      iptables -I INPUT -p "$proto" --dport "$port" -j ACCEPT 2>/dev/null || true
    fi
  fi
}

%s
# HTTP and HTTPS are the only ports the world needs.
allow_public 80 tcp
allow_public 443 tcp

# Pods talk to each other over the cluster network; without this the CNI
# traffic that is not on a fixed port is dropped.
if [ "$HAS_UFW" = yes ]; then
  ufw allow in on cni0 >/dev/null 2>&1 || true
  ufw allow in on flannel.1 >/dev/null 2>&1 || true
  ufw allow in on flannel-wg >/dev/null 2>&1 || true
fi

# Persist iptables rules where the tooling exists, so a reboot does not undo
# this. ufw persists on its own.
if [ "$HAS_UFW" = no ] && command -v netfilter-persistent >/dev/null 2>&1; then
  netfilter-persistent save >/dev/null 2>&1 || true
fi

echo "firewall_configured=${HAS_UFW}"
`, rules.String())
}

// InstallServerScript installs the first control plane node.
func InstallServerScript(version, token, publicIP string, extraArgs []string) string {
	args := []string{
		"--cluster-init",
		"--flannel-backend=wireguard-native",
		"--write-kubeconfig-mode=0644",
		fmt.Sprintf("--tls-san=%s", publicIP),
		fmt.Sprintf("--node-external-ip=%s", publicIP),
		"--node-label=skifity.io/managed=true",
	}
	args = append(args, extraArgs...)
	return installScript(version, token, "", strings.Join(args, " "), true)
}

// JoinServerScript joins another control plane node for high availability.
func JoinServerScript(version, token, serverURL, publicIP string) string {
	args := []string{
		"--server", serverURL,
		fmt.Sprintf("--node-external-ip=%s", publicIP),
		"--node-label=skifity.io/managed=true",
	}
	return installScript(version, token, "", strings.Join(args, " "), true)
}

// JoinAgentScript joins a worker node.
func JoinAgentScript(version, token, serverURL, publicIP string, labels map[string]string) string {
	args := []string{
		fmt.Sprintf("--node-external-ip=%s", publicIP),
		"--node-label=skifity.io/managed=true",
	}
	for _, pair := range sortedPairs(labels) {
		args = append(args, fmt.Sprintf("--node-label=%s=%s", pair[0], pair[1]))
	}
	return installScript(version, token, serverURL, strings.Join(args, " "), false)
}

// installScript builds the k3s installation command.
//
// The official installer is idempotent: run again with the same arguments it
// reconfigures rather than breaking, which is what makes Retry safe.
func installScript(version, token, serverURL, args string, isServer bool) string {
	role := "agent"
	if isServer {
		role = "server"
	}
	var env strings.Builder
	fmt.Fprintf(&env, "INSTALL_K3S_EXEC=%q ", role+" "+args)
	if version != "" {
		fmt.Fprintf(&env, "INSTALL_K3S_VERSION=%q ", version)
	} else {
		// Without a version the installer follows the stable channel, which is
		// what we want by default: it moves forward without a Skifity release.
		env.WriteString(`INSTALL_K3S_CHANNEL="stable" `)
	}
	if token != "" {
		fmt.Fprintf(&env, "K3S_TOKEN=%q ", token)
	}
	if serverURL != "" {
		fmt.Fprintf(&env, "K3S_URL=%q ", serverURL)
	}

	return fmt.Sprintf(`
set -eu

# The panel streams this output, so progress is visible rather than a long wait.
echo "==> Downloading the k3s installer"
if command -v curl >/dev/null 2>&1; then
  curl -sfL https://get.k3s.io -o /tmp/k3s-install.sh
elif command -v wget >/dev/null 2>&1; then
  wget -qO /tmp/k3s-install.sh https://get.k3s.io
else
  echo "Neither curl nor wget is installed on this server." >&2
  echo "Install one of them and try again: apt-get install -y curl" >&2
  exit 20
fi
chmod +x /tmp/k3s-install.sh

echo "==> Installing Kubernetes (k3s)"
%s sh /tmp/k3s-install.sh

echo "==> Waiting for the service to start"
for i in $(seq 1 60); do
  if systemctl is-active --quiet k3s 2>/dev/null || systemctl is-active --quiet k3s-agent 2>/dev/null; then
    echo "k3s is running"
    exit 0
  fi
  sleep 2
done

echo "k3s did not start within two minutes. The last log lines were:" >&2
journalctl -u k3s -u k3s-agent --no-pager -n 40 2>/dev/null >&2 || true
exit 21
`, env.String())
}

// NodeTokenScript reads the join token from the first control plane node.
const NodeTokenScript = `
set -eu
if [ -r /var/lib/rancher/k3s/server/node-token ]; then
  echo "node_token=$(cat /var/lib/rancher/k3s/server/node-token)"
else
  echo "This server is not a control plane node, so it has no join token." >&2
  exit 1
fi
`

// UninstallScript removes k3s from a server.
//
// The official uninstall script is used where it exists, because it knows how
// to unmount everything k3s mounted; leaving those mounts behind makes a
// machine impossible to reuse without a reboot.
const UninstallScript = `
set -u

if [ -x /usr/local/bin/k3s-uninstall.sh ]; then
  echo "==> Removing the k3s server"
  /usr/local/bin/k3s-uninstall.sh || true
elif [ -x /usr/local/bin/k3s-agent-uninstall.sh ]; then
  echo "==> Removing the k3s agent"
  /usr/local/bin/k3s-agent-uninstall.sh || true
else
  echo "k3s is not installed on this server; nothing to remove."
fi

rm -rf /etc/rancher/k3s /var/lib/rancher/k3s 2>/dev/null || true
echo "uninstalled=yes"
`

// ConnectivityScript checks that this server can reach another on a port.
//
// It is run on the server being added and pointed at an existing member, which
// tests the direction that a provider firewall usually blocks.
func ConnectivityScript(targetIP string, port int) string {
	return fmt.Sprintf(`
set -u
TARGET=%q
PORT=%d

# Prefer a real TCP connect; fall back to whatever the machine has.
if command -v nc >/dev/null 2>&1; then
  if nc -z -w 5 "$TARGET" "$PORT" 2>/dev/null; then echo "reachable=yes"; exit 0; fi
elif command -v timeout >/dev/null 2>&1; then
  if timeout 5 sh -c "exec 3<>/dev/tcp/$TARGET/$PORT" 2>/dev/null; then echo "reachable=yes"; exit 0; fi
fi
echo "reachable=no"
`, targetIP, port)
}

// sortedPairs returns a map's entries in key order, so a generated command is
// stable and two runs produce the same thing.
func sortedPairs(m map[string]string) [][2]string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, [2]string{k, m[k]})
	}
	return out
}
