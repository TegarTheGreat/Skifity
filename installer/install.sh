#!/bin/sh
# Skifity installer.
#
#   curl -fsSL https://get.skifity.io | sh
#
# Turns a fresh Ubuntu or Debian server into a Skifity control plane: k3s, the
# panel, and a URL to open. It is safe to run again: every step checks what is
# already there before changing anything.
#
# Everything it does is written to /var/log/skifity-install.log, so a failure
# can be read afterwards rather than reconstructed from a scrolled-off terminal.
#
# Non-interactive use: set the variables below and it never asks anything.
#
#   SKIFITY_VERSION       image tag to install (default: latest)
#   SKIFITY_IMAGE         full image reference, overrides SKIFITY_VERSION
#   SKIFITY_DOMAIN        the domain the panel will answer on
#   SKIFITY_ACME_EMAIL    the address Let's Encrypt sends expiry warnings to
#   SKIFITY_ACME_STAGING  set to 1 to use Let's Encrypt's staging server
#   SKIFITY_CHANNEL       k3s channel (default: stable)
#   SKIFITY_SKIP_K3S      set to 1 when k3s is already installed and configured
#   SKIFITY_ASSUME_YES    set to 1 to answer every prompt with yes
#
# POSIX sh on purpose: this has to run on a minimal image where bash may not
# be installed.

set -eu

VERSION="${SKIFITY_VERSION:-latest}"
IMAGE="${SKIFITY_IMAGE:-ghcr.io/skifity/skifity:${VERSION}}"
NAMESPACE="skifity-system"
CONFIG_DIR="/etc/skifity"
DATA_DIR="/var/lib/skifity"
LOG_FILE="/var/log/skifity-install.log"
MANIFEST_DIR="/var/lib/skifity/manifests"
K3S_CHANNEL="${SKIFITY_CHANNEL:-stable}"
KUBECONFIG_PATH="/etc/rancher/k3s/k3s.yaml"
ISSUER="skifity-letsencrypt"
# The in-cluster registry built images are pushed to and pulled from. These
# three values must match internal/kube/naming.go; a Go test checks that they do.
BUILDS_NAMESPACE="skifity-builds"
REGISTRY_HOST="skifity-registry.${BUILDS_NAMESPACE}.svc.cluster.local:5000"
REGISTRY_NODE_PORT=30500
CERT_MANAGER_URL="https://github.com/cert-manager/cert-manager/releases/download/v1.21.2/cert-manager.yaml"
# The panel runs as distroless's nonroot user; its directories must be readable
# and writable by that user and by nobody else.
RUN_UID=65532
RUN_GID=65532

# Minimums, chosen so that a 1 GB VPS is usable rather than technically bootable.
MIN_MEMORY_MB=900
MIN_DISK_GB=8

# --- output -----------------------------------------------------------------

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	BOLD=$(printf '\033[1m'); DIM=$(printf '\033[2m'); RED=$(printf '\033[31m')
	GREEN=$(printf '\033[32m'); YELLOW=$(printf '\033[33m'); RESET=$(printf '\033[0m')
else
	BOLD=''; DIM=''; RED=''; GREEN=''; YELLOW=''; RESET=''
fi

log() {
	printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*" >>"$LOG_FILE" 2>/dev/null || true
}

say() { printf '%s\n' "$*"; log "$*"; }
step() { printf '%s==>%s %s\n' "$BOLD" "$RESET" "$*"; log "STEP $*"; }
ok() { printf '  %s✓%s %s\n' "$GREEN" "$RESET" "$*"; log "OK $*"; }
note() { printf '  %s%s%s\n' "$DIM" "$*" "$RESET"; log "NOTE $*"; }
warn() { printf '  %s!%s %s\n' "$YELLOW" "$RESET" "$*"; log "WARN $*"; }

# fail prints a problem the way the panel would: what happened, what it means,
# and what to do about it. A one-line error at this stage leaves someone with a
# half-installed server and nowhere to go.
fail() {
	printf '\n%s%sInstallation stopped%s\n\n' "$BOLD" "$RED" "$RESET" >&2
	printf '%s\n' "$1" >&2
	if [ "${2:-}" != "" ]; then
		printf '\n%sWhat to do%s\n%s\n' "$BOLD" "$RESET" "$2" >&2
	fi
	printf '\nThe full log is at %s\n' "$LOG_FILE" >&2
	log "FAILED $1"
	exit 1
}

confirm() {
	[ "${SKIFITY_ASSUME_YES:-}" = "1" ] && return 0
	[ -t 0 ] || return 0
	printf '  %s [y/N] ' "$1"
	read -r answer || answer=""
	case "$answer" in
	y | Y | yes | YES) return 0 ;;
	*) return 1 ;;
	esac
}

have() { command -v "$1" >/dev/null 2>&1; }

# --- preflight --------------------------------------------------------------

preflight() {
	step "Checking this server"

	[ "$(id -u)" = "0" ] || fail \
		"This installer has to run as root: it installs k3s and writes to /etc." \
		"Run it again with sudo:

  curl -fsSL https://get.skifity.io | sudo sh"

	mkdir -p "$(dirname "$LOG_FILE")" 2>/dev/null || true
	: >>"$LOG_FILE" 2>/dev/null || LOG_FILE=/dev/null
	log "skifity installer starting, image ${IMAGE}"

	OS_NAME="unknown"; OS_VERSION=""
	if [ -r /etc/os-release ]; then
		# shellcheck disable=SC1091
		. /etc/os-release
		OS_NAME="${ID:-unknown}"
		OS_VERSION="${VERSION_ID:-}"
	fi
	case "$OS_NAME" in
	ubuntu | debian) ok "${PRETTY_NAME:-$OS_NAME $OS_VERSION}" ;;
	*)
		warn "This is $OS_NAME, and Skifity is tested on Ubuntu 24.04 and Debian 12."
		confirm "Carry on anyway?" || fail \
			"Stopped at your request." \
			"Install on Ubuntu 24.04 or Debian 12, or set SKIFITY_ASSUME_YES=1 to skip this question."
		;;
	esac

	ARCH=$(uname -m)
	case "$ARCH" in
	x86_64 | amd64 | aarch64 | arm64) ok "Architecture $ARCH" ;;
	*) fail \
		"Skifity does not have builds for $ARCH." \
		"Use a 64-bit x86 or ARM server. 32-bit and other architectures are not supported." ;;
	esac

	MEMORY_MB=$(awk '/MemTotal/ {print int($2 / 1024)}' /proc/meminfo 2>/dev/null || echo 0)
	if [ "$MEMORY_MB" -lt "$MIN_MEMORY_MB" ]; then
		fail \
			"This server has ${MEMORY_MB} MB of memory, and Kubernetes plus the panel need about ${MIN_MEMORY_MB} MB before your apps get anything." \
			"Use a server with at least 1 GB of memory. 2 GB is a comfortable starting point."
	fi
	ok "Memory ${MEMORY_MB} MB"

	DISK_GB=$(df -P -k / | awk 'NR==2 {print int($4 / 1024 / 1024)}')
	if [ "$DISK_GB" -lt "$MIN_DISK_GB" ]; then
		fail \
			"There is ${DISK_GB} GB free on /, and a working install needs about ${MIN_DISK_GB} GB for the container images alone." \
			"Free some space, or move to a server with a larger disk, and run this again."
	fi
	ok "Disk ${DISK_GB} GB free"

	if [ ! -d /run/systemd/system ]; then
		fail \
			"This server is not running systemd, and k3s installs itself as a systemd service." \
			"Use a normal Ubuntu or Debian server. Containers without an init system, such as an unprivileged LXC or a Docker container, cannot run k3s this way."
	fi
	ok "systemd is running"

	for port in 80 443 6443; do
		if port_in_use "$port"; then
			# 6443 already listening usually means k3s is installed, which is fine.
			if [ "$port" = "6443" ] && [ -f "$KUBECONFIG_PATH" ]; then
				continue
			fi
			fail \
				"Something is already listening on port ${port}, and Skifity needs it to serve your apps." \
				"Find it with:

  ss -lptn 'sport = :${port}'

Stop that service, or move it to another port, and run this again. A web server such as nginx or Apache installed by your provider is the usual cause."
		fi
	done
	ok "Ports 80, 443 and 6443 are free"

	have curl || fail \
		"The installer needs curl, and it is not on this server." \
		"Install it first:

  apt-get update && apt-get install -y curl"
	ok "curl is available"
}

port_in_use() {
	if have ss; then
		ss -lntH "sport = :$1" 2>/dev/null | grep -q . && return 0
		return 1
	fi
	if have netstat; then
		netstat -lnt 2>/dev/null | awk '{print $4}' | grep -qE "[:.]$1\$" && return 0
		return 1
	fi
	# No way to tell. k3s will report the conflict itself if there is one.
	return 1
}

# --- k3s --------------------------------------------------------------------

install_k3s() {
	step "Installing Kubernetes"

	if [ "${SKIFITY_SKIP_K3S:-}" = "1" ]; then
		note "Skipped: SKIFITY_SKIP_K3S is set"
		return 0
	fi

	# Before the early return below: an install that is being re-run still has
	# to end up with the mirror configured.
	configure_registry_mirror

	if have k3s && systemctl is-active --quiet k3s 2>/dev/null; then
		ok "k3s is already installed and running"
		return 0
	fi

	note "This downloads and starts k3s, which takes a minute or two."
	# --cluster-init starts embedded etcd even on one node, so a second and third
	# control plane server can join later without rebuilding the cluster.
	#
	# WireGuard encrypts traffic between nodes. On a single server it costs
	# nothing, and it means adding a server over the public internet later is
	# not a change of security model.
	curl -fsSL https://get.k3s.io >/tmp/skifity-k3s-install.sh 2>>"$LOG_FILE" || fail \
		"Could not download the k3s installer from get.k3s.io." \
		"Check that this server can reach the internet:

  curl -fsSL https://get.k3s.io | head"

	INSTALL_K3S_CHANNEL="$K3S_CHANNEL" \
		INSTALL_K3S_EXEC="server --cluster-init --flannel-backend=wireguard-native --write-kubeconfig-mode=0600 --secrets-encryption" \
		sh /tmp/skifity-k3s-install.sh >>"$LOG_FILE" 2>&1 || fail \
		"k3s did not install." \
		"The last lines of ${LOG_FILE} say why. The usual causes are no outbound network access to get.k3s.io, or a kernel without the modules k3s needs.

Try the download on its own to see the error:

  sh /tmp/skifity-k3s-install.sh"

	rm -f /tmp/skifity-k3s-install.sh
	ok "k3s installed"
}

# containerd runs on the host, not in the cluster. It cannot resolve the
# registry's Kubernetes Service name, and it refuses plain HTTP to anything
# that is not loopback, so an image the panel builds could never be pulled.
# The mirror sends that name to the registry's NodePort on this machine.
#
# It is written before k3s starts, because that is when k3s reads it.
configure_registry_mirror() {
	mkdir -p /etc/rancher/k3s
	cat >/etc/rancher/k3s/registries.yaml.new <<EOF
# Written by Skifity. Do not edit.
mirrors:
  "${REGISTRY_HOST}":
    endpoint:
      - "http://127.0.0.1:${REGISTRY_NODE_PORT}"
EOF
	if cmp -s /etc/rancher/k3s/registries.yaml.new /etc/rancher/k3s/registries.yaml 2>/dev/null; then
		rm -f /etc/rancher/k3s/registries.yaml.new
		return 0
	fi
	mv /etc/rancher/k3s/registries.yaml.new /etc/rancher/k3s/registries.yaml
	ok "the container runtime knows where the panel's registry is"

	# k3s reads this at start-up only, so a change to a running node needs a
	# restart. A fresh install has not started yet and skips this.
	if systemctl is-active --quiet k3s 2>/dev/null; then
		note "Restarting k3s so it picks up the registry configuration"
		systemctl restart k3s >>"$LOG_FILE" 2>&1 || warn "k3s could not be restarted; do it by hand"
	fi
}

wait_for_cluster() {
	step "Waiting for the cluster"

	i=0
	while [ "$i" -lt 120 ]; do
		if kubectl get --raw /readyz >/dev/null 2>&1; then
			break
		fi
		i=$((i + 1))
		sleep 2
	done
	if [ "$i" -lt 120 ]; then
		ok "The API server is answering"
	else
		fail \
			"The Kubernetes API server did not come up within four minutes." \
			"Look at what k3s is saying:

  journalctl -u k3s -n 50 --no-pager"
	fi

	i=0
	while [ "$i" -lt 90 ]; do
		if [ "$(kubectl get nodes --no-headers 2>/dev/null | awk '$2 == "Ready"' | wc -l)" -ge 1 ]; then
			break
		fi
		i=$((i + 1))
		sleep 2
	done
	[ "$i" -lt 90 ] || fail \
		"The node never became ready." \
		"Look at what is wrong with:

  kubectl describe node
  journalctl -u k3s -n 50 --no-pager"

	NODE_NAME=$(kubectl get nodes --no-headers -o custom-columns=NAME:.metadata.name | head -n 1)
	ok "Node ${NODE_NAME} is ready"
}

# --- the panel --------------------------------------------------------------

prepare_directories() {
	step "Preparing the panel's directories"

	mkdir -p "$CONFIG_DIR" "$DATA_DIR" "$MANIFEST_DIR"
	# 0700 and owned by the user the panel runs as: the master key lives here.
	chown "$RUN_UID:$RUN_GID" "$CONFIG_DIR" "$DATA_DIR" "$MANIFEST_DIR"
	chmod 0700 "$CONFIG_DIR" "$DATA_DIR"
	ok "$CONFIG_DIR and $DATA_DIR"
}

generate_setup_token() {
	step "Preparing first-run setup"

	if [ -s "$CONFIG_DIR/setup-token" ]; then
		SETUP_TOKEN=$(cat "$CONFIG_DIR/setup-token")
		ok "Reusing the setup token from a previous run"
		return 0
	fi

	SETUP_TOKEN=$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' | cut -c1-40)
	[ -n "$SETUP_TOKEN" ] || fail \
		"Could not generate a setup token from /dev/urandom." \
		"That normally means /dev is not mounted properly. Reboot the server and try again."
	printf '%s\n' "$SETUP_TOKEN" >"$CONFIG_DIR/setup-token"
	chown "$RUN_UID:$RUN_GID" "$CONFIG_DIR/setup-token"
	chmod 0600 "$CONFIG_DIR/setup-token"
	ok "Setup token written to $CONFIG_DIR/setup-token"

	# The master key is deliberately not generated here: the panel creates it on
	# first start, so exactly one piece of code decides its format. This keeps
	# an installer that is one version out of step from writing a key the panel
	# cannot read.
	note "The panel will create its master key at $CONFIG_DIR/master.key on first start."
}

choose_hostname() {
	step "Working out the panel's address"

	PUBLIC_IP="${SKIFITY_PUBLIC_IP:-}"
	if [ -z "$PUBLIC_IP" ]; then
		PUBLIC_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="ExternalIP")].address}' 2>/dev/null || true)
	fi
	if [ -z "$PUBLIC_IP" ]; then
		PUBLIC_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null || true)
	fi
	[ -n "$PUBLIC_IP" ] || fail \
		"Could not work out this server's IP address." \
		"Set it yourself and run the installer again:

  SKIFITY_PUBLIC_IP=203.0.113.10 sh install.sh"

	if [ -n "${SKIFITY_DOMAIN:-}" ]; then
		PANEL_HOST="$SKIFITY_DOMAIN"
		PANEL_SCHEME="https"
		ok "The panel will answer on $PANEL_HOST"
		note "Point an A record for $PANEL_HOST at $PUBLIC_IP before opening it."
	else
		# sslip.io resolves any address embedded in the name, so a brand new
		# server has a working hostname without anybody buying a domain.
		PANEL_HOST="$(printf '%s' "$PUBLIC_IP" | tr '.' '-').sslip.io"
		PANEL_SCHEME="http"
		ok "No domain given, so the panel will answer on $PANEL_HOST"
		note "Add your own domain later in Settings, and HTTPS is turned on for it automatically."
	fi
	PUBLIC_URL="${PANEL_SCHEME}://${PANEL_HOST}"
}

install_cert_manager() {
	[ "$PANEL_SCHEME" = "https" ] || return 0
	step "Installing certificate management"

	if kubectl get deployment cert-manager -n cert-manager >/dev/null 2>&1; then
		ok "cert-manager is already installed"
	else
		kubectl apply -f "$CERT_MANAGER_URL" >>"$LOG_FILE" 2>&1 || fail \
			"cert-manager did not install." \
			"Check that this server can reach github.com, then run the installer again. Without cert-manager the panel still works over plain HTTP; you can install it later from Settings."
		kubectl -n cert-manager rollout status deployment/cert-manager-webhook --timeout=180s >>"$LOG_FILE" 2>&1 || fail \
			"cert-manager installed but its webhook never became ready." \
			"Look at it with:

  kubectl -n cert-manager get pods"
		ok "cert-manager is ready"
	fi

	acme_server="https://acme-v02.api.letsencrypt.org/directory"
	[ "${SKIFITY_ACME_STAGING:-}" = "1" ] &&
		acme_server="https://acme-staging-v02.api.letsencrypt.org/directory"

	render deploy/cluster-issuer.yaml |
		sed -e "s|__EMAIL__|${SKIFITY_ACME_EMAIL:-}|g" \
			-e "s|__ACME_SERVER__|${acme_server}|g" \
			>"$MANIFEST_DIR/cluster-issuer.yaml"
	kubectl apply -f "$MANIFEST_DIR/cluster-issuer.yaml" >>"$LOG_FILE" 2>&1 || fail \
		"Could not create the certificate issuer." \
		"Run the installer again. If it keeps failing, the log at ${LOG_FILE} has what kubectl said."
	ok "Certificates will be issued by Let's Encrypt"
}

# render prints a manifest with the common placeholders filled in. The manifests
# come from the installer's own directory when it was downloaded as a file, and
# are embedded below when it was piped from curl.
render() {
	name=$(basename "$1")
	if [ -f "$SOURCE_DIR/$1" ]; then
		cat "$SOURCE_DIR/$1"
	else
		fetch_manifest "$name"
	fi |
		sed -e "s|__NAMESPACE__|${NAMESPACE}|g" \
			-e "s|__IMAGE__|${IMAGE}|g" \
			-e "s|__NODE__|${NODE_NAME}|g" \
			-e "s|__HOST__|${PANEL_HOST}|g" \
			-e "s|__PUBLIC_URL__|${PUBLIC_URL}|g" \
			-e "s|__CONFIG_DIR__|${CONFIG_DIR}|g" \
			-e "s|__DATA_DIR__|${DATA_DIR}|g" \
			-e "s|__ISSUER__|${ISSUER}|g"
}

fetch_manifest() {
	url="${SKIFITY_MANIFEST_BASE:-https://raw.githubusercontent.com/skifity/skifity/main/deploy}/$1"
	curl -fsSL "$url" || fail \
		"Could not download the manifest ${1} from ${url}." \
		"Check that this server can reach the internet, or clone the repository and run installer/install.sh from inside it so the manifests are read from disk."
}

install_panel() {
	step "Installing the panel"

	# Rendered to disk first, so that a failure to fetch a manifest stops the
	# installer rather than being swallowed by a pipeline, and so that the
	# operator can see exactly what was applied.
	render deploy/panel.yaml >"$MANIFEST_DIR/panel.yaml"
	kubectl apply -f "$MANIFEST_DIR/panel.yaml" >>"$LOG_FILE" 2>&1 || fail \
		"The panel's Kubernetes objects could not be applied." \
		"The log at ${LOG_FILE} has what kubectl said. Applying them again is safe."
	ok "Panel objects applied"

	if [ "$PANEL_SCHEME" = "https" ]; then
		render deploy/ingress-tls.yaml >"$MANIFEST_DIR/ingress.yaml"
	else
		render deploy/ingress.yaml >"$MANIFEST_DIR/ingress.yaml"
	fi
	kubectl apply -f "$MANIFEST_DIR/ingress.yaml" >>"$LOG_FILE" 2>&1 || fail \
		"The panel's route in could not be created." \
		"The log at ${LOG_FILE} has what kubectl said."
	ok "Route to the panel created"

	step "Waiting for the panel to start"
	note "The first start pulls the image, which takes a minute on a new server."
	kubectl -n "$NAMESPACE" rollout status deployment/skifity-panel --timeout=300s >>"$LOG_FILE" 2>&1 || fail \
		"The panel did not start within five minutes." \
		"See what it is waiting for:

  kubectl -n ${NAMESPACE} get pods
  kubectl -n ${NAMESPACE} describe pod -l app.kubernetes.io/component=panel
  kubectl -n ${NAMESPACE} logs -l app.kubernetes.io/component=panel

If the image could not be pulled, check that ${IMAGE} exists and that this server can reach the registry."
	ok "The panel is running"
}

install_cli() {
	step "Installing the command line tool"

	# The panel's image is distroless: no shell, no cat, no tar, so the binary
	# cannot be copied out of the running container. It is downloaded instead,
	# at the same version the panel is running.
	case "$ARCH" in
	x86_64 | amd64) cli_arch=amd64 ;;
	aarch64 | arm64) cli_arch=arm64 ;;
	*) cli_arch="" ;;
	esac

	if [ -n "$cli_arch" ]; then
		cli_url="${SKIFITY_CLI_BASE:-https://github.com/skifity/skifity/releases/latest/download}/skifity-linux-${cli_arch}"
		[ "$VERSION" = "latest" ] ||
			cli_url="${SKIFITY_CLI_BASE:-https://github.com/skifity/skifity/releases/download/v${VERSION}}/skifity-linux-${cli_arch}"
		if curl -fsSL "$cli_url" -o /usr/local/bin/skifity.new 2>>"$LOG_FILE"; then
			chmod 0755 /usr/local/bin/skifity.new
			mv /usr/local/bin/skifity.new /usr/local/bin/skifity
			ok "skifity is on your PATH"
			return 0
		fi
		rm -f /usr/local/bin/skifity.new
	fi

	warn "Could not download the command line tool; the panel itself is unaffected."
	note "Get it later from https://github.com/skifity/skifity/releases, or use the panel."
}

finish() {
	printf '\n%s%sSkifity is installed.%s\n\n' "$BOLD" "$GREEN" "$RESET"
	printf '  Open       %s%s/setup%s\n' "$BOLD" "$PUBLIC_URL" "$RESET"
	printf '  Token      %s%s%s\n\n' "$BOLD" "$SETUP_TOKEN" "$RESET"
	printf '  The token is also at %s on this server.\n' "$CONFIG_DIR/setup-token"
	printf '  It creates the first account and then stops working.\n\n'

	if [ "$PANEL_SCHEME" = "http" ]; then
		printf '  %sThis address has no certificate yet.%s Add a domain in Settings and\n' "$YELLOW" "$RESET"
		printf '  Skifity turns on HTTPS for it by itself.\n\n'
	else
		printf '  If the page does not load, the DNS record for %s\n' "$PANEL_HOST"
		printf '  may not have reached your computer yet. Give it a few minutes.\n\n'
	fi

	printf '  Log        %s\n' "$LOG_FILE"
	printf '  Uninstall  skifity-uninstall\n\n'
	log "install completed, panel at ${PUBLIC_URL}"
}

# --- main -------------------------------------------------------------------

# Sourcing this file with SKIFITY_INSTALLER_LIB=1 defines the functions above
# and stops here, so the smoke test can exercise them without installing
# anything. Nothing else reads this variable.
if [ "${SKIFITY_INSTALLER_LIB:-}" = "1" ]; then
	return 0 2>/dev/null || exit 0
fi

SOURCE_DIR="."
case "$0" in
*/*)
	# Running from a checkout: the manifests sit next to this script.
	SOURCE_DIR=$(cd "$(dirname "$0")/.." 2>/dev/null && pwd || echo ".")
	;;
esac

printf '\n%sSkifity%s  self-hosted apps, powered by Kubernetes\n\n' "$BOLD" "$RESET"

preflight
install_k3s

# Every kubectl call from here on talks to the cluster this installer just made.
export KUBECONFIG="$KUBECONFIG_PATH"
have kubectl || PATH="/usr/local/bin:$PATH"
have kubectl || fail \
	"kubectl is not on this server, and k3s should have installed it." \
	"Check that /usr/local/bin is on your PATH, or use the copy k3s ships:

  k3s kubectl get nodes"

wait_for_cluster
prepare_directories
generate_setup_token
choose_hostname
install_cert_manager
install_panel
install_cli
finish
