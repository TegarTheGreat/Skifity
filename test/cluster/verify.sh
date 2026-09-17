#!/usr/bin/env bash
# The verification that has never been run.
#
# Everything else in test/ checks this product against fakes, golden files and
# an in-process SSH server, because the machine it was built on could not start
# a cluster. This is the other half: it installs Skifity on a real server,
# deploys a real application, and then checks the three claims that only a real
# cluster can settle — that an app comes up and answers, that autoscaling has
# numbers to scale on, and that scale-to-zero puts an idle app to sleep and a
# request wakes it.
#
#   THIS INSTALLS k3s AND CHANGES THE MACHINE IT RUNS ON.
#
# Run it on a server you are willing to rebuild — a fresh VPS, not something you
# care about. It never runs itself: it asks first unless SKIFITY_ASSUME_YES=1.
#
#   sudo bash test/cluster/verify.sh
#
# What it needs: Ubuntu 22.04+ or Debian 12+, 2 GB of memory, a public IP, and
# either a built image or SKIFITY_IMAGE pointing at one. With no domain it uses
# nip.io, so there is nothing to configure in DNS.
#
# It writes a report to /var/log/skifity-verify.log and prints the same at the
# end. When something fails it says which check, what it expected, and what to
# look at — the point is a report somebody can act on, not a green tick.

set -euo pipefail

PANEL_NS="skifity-system"
REPORT="/var/log/skifity-verify.log"
: "${SKIFITY_ASSUME_YES:=0}"
: "${SKIFITY_IMAGE:=}"
# A tiny, stable, public image with an HTTP server and a health path. Not built
# from source on purpose: the first run should fail on the cluster if it fails,
# not on somebody's build cache.
: "${VERIFY_APP_IMAGE:=ghcr.io/nginxinc/nginx-unprivileged:1.27-alpine}"
: "${VERIFY_APP_PORT:=8080}"

PASS=0
FAIL=0
SKIP=0

say() { printf '%s\n' "$*" | tee -a "$REPORT"; }
step() { printf '\n== %s\n' "$*" | tee -a "$REPORT"; }
ok() { PASS=$((PASS + 1)); printf '  ok   %s\n' "$*" | tee -a "$REPORT"; }
no() { FAIL=$((FAIL + 1)); printf '  FAIL %s\n' "$*" | tee -a "$REPORT"; }
skip() { SKIP=$((SKIP + 1)); printf '  --   %s (skipped: %s)\n' "$1" "$2" | tee -a "$REPORT"; }

die() {
	no "$*"
	summary
	exit 1
}

summary() {
	step "Result"
	say "  $PASS passed, $FAIL failed, $SKIP skipped"
	say "  The full log is at $REPORT"
	if [ "$FAIL" -gt 0 ]; then
		say ""
		say "  Something above is the first thing this product has ever been told"
		say "  by a real cluster. Send the failing section back with the output of:"
		say "    kubectl -n $PANEL_NS get pods,events --sort-by=.lastTimestamp | tail -40"
	fi
}

confirm() {
	[ "$SKIFITY_ASSUME_YES" = "1" ] && return 0
	printf '\nThis installs k3s and changes this machine permanently.\n'
	printf 'Type the word yes to continue: '
	read -r answer
	[ "$answer" = "yes" ] || { echo "Nothing was changed."; exit 1; }
}

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is not installed, and this needs it"; }

# --- preflight -------------------------------------------------------------

: >"$REPORT"
step "Before anything is installed"

[ "$(id -u)" = "0" ] || die "this has to run as root: it installs k3s"
need curl
need awk

if [ -z "$SKIFITY_IMAGE" ]; then
	die "set SKIFITY_IMAGE to a panel image. No release is published yet, so build one with 'make image' and push it somewhere this server can pull from, or load it into k3s with 'k3s ctr images import'."
fi
ok "the panel image to install is $SKIFITY_IMAGE"

MEM_MB=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
say "  this machine has ${MEM_MB} MB of memory"
[ "$MEM_MB" -ge 1800 ] || no "under 2 GB; the README says 2 GB and this is the first chance to find out whether that is true"

PUBLIC_IP="${SKIFITY_PUBLIC_IP:-$(curl -fsS --max-time 10 https://api.ipify.org || true)}"
[ -n "$PUBLIC_IP" ] || die "could not work out this machine's public IP; set SKIFITY_PUBLIC_IP"
ok "public IP is $PUBLIC_IP"
DOMAIN="${SKIFITY_DOMAIN:-${PUBLIC_IP}.nip.io}"
say "  the panel will answer on https://$DOMAIN"

confirm

# --- install ---------------------------------------------------------------

step "Installing"

HERE="$(cd "$(dirname "$0")/../.." && pwd)"
[ -f "$HERE/installer/install.sh" ] || die "run this from a clone of the repository: installer/install.sh is not next to it"

START_INSTALL=$(date +%s)
if SKIFITY_IMAGE="$SKIFITY_IMAGE" SKIFITY_DOMAIN="$DOMAIN" SKIFITY_ASSUME_YES=1 \
	SKIFITY_MANIFEST_BASE="file://$HERE/deploy" \
	sh "$HERE/installer/install.sh" >>"$REPORT" 2>&1; then
	ok "the installer finished in $(( $(date +%s) - START_INSTALL ))s"
else
	die "the installer failed; the last lines of $REPORT say where"
fi

need kubectl
kubectl get nodes >/dev/null 2>&1 || die "kubectl cannot reach the cluster the installer just made"
ok "the cluster answers"

# --- the panel is up -------------------------------------------------------

step "The panel"

for _ in $(seq 1 60); do
	if kubectl -n "$PANEL_NS" get deploy skifity-panel -o jsonpath='{.status.readyReplicas}' 2>/dev/null | grep -q '^[1-9]'; then
		break
	fi
	sleep 5
done
kubectl -n "$PANEL_NS" get deploy skifity-panel -o jsonpath='{.status.readyReplicas}' 2>/dev/null | grep -q '^[1-9]' \
	|| die "the panel never became ready; kubectl -n $PANEL_NS describe pod says why"
ok "the panel pod is ready"

PANEL="http://127.0.0.1:$(kubectl -n "$PANEL_NS" get svc skifity-panel -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || echo 0)"
# The NodePort is not guaranteed; a port-forward always works and is what a
# check should depend on.
kubectl -n "$PANEL_NS" port-forward svc/skifity-panel 18080:80 >/dev/null 2>&1 &
FORWARD=$!
trap 'kill $FORWARD 2>/dev/null || true' EXIT
sleep 3
PANEL="http://127.0.0.1:18080"

api() {
	local method="$1" path="$2" body="${3:-}"
	if [ -n "$body" ]; then
		curl -fsS -X "$method" "$PANEL$path" -H 'Content-Type: application/json' \
			${TOKEN:+-H "Authorization: Bearer $TOKEN"} -d "$body"
	else
		curl -fsS -X "$method" "$PANEL$path" ${TOKEN:+-H "Authorization: Bearer $TOKEN"}
	fi
}

TOKEN=""
api GET /api/health >/dev/null || die "the panel's own health endpoint does not answer"
ok "/api/health answers"

# --- first-run setup -------------------------------------------------------

step "First run"

SETUP_TOKEN=$(cat /etc/skifity/setup-token 2>/dev/null || true)
[ -n "$SETUP_TOKEN" ] || die "no setup token at /etc/skifity/setup-token"

PASSWORD="verify-$(head -c 12 /dev/urandom | od -An -tx1 | tr -d ' \n')"
SETUP=$(api POST /api/setup "{\"token\":\"$SETUP_TOKEN\",\"email\":\"verify@example.test\",\"name\":\"Verify\",\"password\":\"$PASSWORD\",\"team_name\":\"Verify\"}") \
	|| die "first-run setup was refused"
ok "an account and a team were created"

TEAM_ID=$(printf '%s' "$SETUP" | grep -o '"team":{"id":"[^"]*' | cut -d'"' -f6)
[ -n "$TEAM_ID" ] || die "setup did not return a team id"

LOGIN=$(api POST /api/auth/login "{\"email\":\"verify@example.test\",\"password\":\"$PASSWORD\"}") \
	|| die "signing in with the account just created was refused"
ok "signing in works"

# An API token, because everything below is a script rather than a browser.
TOKEN=""
COOKIE=$(mktemp)
curl -fsS -c "$COOKIE" -X POST "$PANEL/api/auth/login" -H 'Content-Type: application/json' \
	-d "{\"email\":\"verify@example.test\",\"password\":\"$PASSWORD\"}" >/dev/null
CSRF=$(awk '/skifity_csrf/ {print $7}' "$COOKIE")
TOKEN=$(curl -fsS -b "$COOKIE" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' \
	-X POST "$PANEL/api/me/tokens" -d '{"name":"verify","ttl_hours":2}' \
	| grep -o '"token":"[^"]*' | cut -d'"' -f4)
[ -n "$TOKEN" ] || die "could not create an API token"
ok "an API token was issued"

# --- deploy an application -------------------------------------------------

step "Deploying an application"

PROJECT=$(api POST "/api/teams/$TEAM_ID/projects" '{"name":"Verify"}') || die "a project could not be created"
PROJECT_ID=$(printf '%s' "$PROJECT" | grep -o '"id":"[^"]*' | head -1 | cut -d'"' -f4)
ENVS=$(api GET "/api/projects/$PROJECT_ID/environments") || die "the project has no environments"
ENV_ID=$(printf '%s' "$ENVS" | grep -o '"id":"[^"]*' | head -1 | cut -d'"' -f4)
ok "a project and an environment exist"

APP=$(api POST "/api/environments/$ENV_ID/apps" \
	"{\"name\":\"hello\",\"source_type\":\"image\",\"image\":\"$VERIFY_APP_IMAGE\",\"port\":$VERIFY_APP_PORT,\"health_path\":\"/\",\"deploy\":true}") \
	|| die "the app could not be created"
APP_ID=$(printf '%s' "$APP" | grep -o '"id":"[^"]*' | head -1 | cut -d'"' -f4)
ok "an app was created and a deployment started"

NAMESPACE=$(api GET "/api/environments/$ENV_ID" | grep -o '"namespace":"[^"]*' | cut -d'"' -f4)
for _ in $(seq 1 90); do
	ready=$(kubectl -n "$NAMESPACE" get deploy hello -o jsonpath='{.status.readyReplicas}' 2>/dev/null || true)
	[ "${ready:-0}" -ge 1 ] 2>/dev/null && break
	sleep 5
done
[ "${ready:-0}" -ge 1 ] 2>/dev/null || die "the app never became ready. kubectl -n $NAMESPACE describe pod, and the panel's Instances tab, both say why"
ok "the app is running in $NAMESPACE"

if kubectl -n "$NAMESPACE" run verify-curl --rm -i --restart=Never --image=curlimages/curl:8.11.1 \
	--command -- curl -fsS --max-time 10 "http://hello.$NAMESPACE.svc.cluster.local" >/dev/null 2>&1; then
	ok "the app answers an HTTP request from inside the cluster"
else
	no "the app is running and does not answer on its Service; the port or the health path is wrong"
fi

# --- the three claims only a cluster can settle ----------------------------

step "Autoscaling"

if kubectl top nodes >/dev/null 2>&1; then
	ok "metrics-server is serving numbers"
else
	no "kubectl top nodes does not work, so a HorizontalPodAutoscaler has nothing to scale on. The panel now reports this as a scaling finding; this is the check that proves the finding is right."
fi

api PUT "/api/apps/$APP_ID/scaling" '{"autoscale":true,"min_replicas":1,"max_replicas":3,"cpu_target":70}' >/dev/null \
	|| die "autoscaling could not be turned on"
sleep 10
if kubectl -n "$NAMESPACE" get hpa hello-hpa >/dev/null 2>&1; then
	ok "a HorizontalPodAutoscaler was created"
	TARGETS=$(kubectl -n "$NAMESPACE" get hpa hello-hpa -o jsonpath='{.status.currentMetrics}' 2>/dev/null || true)
	# Up to a minute for the first sample. `<unknown>` past that is the silent
	# failure the readiness checker warns about.
	for _ in $(seq 1 12); do
		[ -n "$TARGETS" ] && [ "$TARGETS" != "null" ] && break
		sleep 5
		TARGETS=$(kubectl -n "$NAMESPACE" get hpa hello-hpa -o jsonpath='{.status.currentMetrics}' 2>/dev/null || true)
	done
	if [ -n "$TARGETS" ] && [ "$TARGETS" != "null" ]; then
		ok "the HPA is reading real numbers, not <unknown>"
	else
		no "the HPA has no metrics after a minute: it will never scale, and nothing in Kubernetes says so"
	fi
else
	no "no HorizontalPodAutoscaler exists after turning autoscaling on"
fi

step "Scale to zero"

if api PUT "/api/apps/$APP_ID/scaling" '{"autoscale":false,"scale_to_zero":true,"replicas":1}' >/dev/null 2>&1; then
	ok "scale to zero was accepted, which installs KEDA on first use"
	for _ in $(seq 1 60); do
		kubectl -n keda get deploy keda-operator -o jsonpath='{.status.readyReplicas}' 2>/dev/null | grep -q '^[1-9]' && break
		sleep 5
	done
	if kubectl -n keda get deploy keda-operator -o jsonpath='{.status.readyReplicas}' 2>/dev/null | grep -q '^[1-9]'; then
		ok "KEDA is running"
	else
		no "KEDA never started; scale to zero cannot work without it"
	fi
	if kubectl -n "$NAMESPACE" get httpscaledobject hello >/dev/null 2>&1; then
		ok "an HTTPScaledObject was created for the app"
	else
		no "no HTTPScaledObject: the app will not be woken by a request"
	fi
	if kubectl -n "$NAMESPACE" get hpa hello-hpa >/dev/null 2>&1; then
		no "the app's own HPA is still there beside KEDA's; two autoscalers on one Deployment fight, which is what ADR says this avoids"
	else
		ok "the app's own HPA was removed, so KEDA's is the only one"
	fi
	say "  A cold start takes as long as the image takes to pull. Leave it idle for"
	say "  the cooldown, then curl the app's domain and time the first response:"
	say "    time curl -fsS -o /dev/null https://hello.$DOMAIN"
	skip "the sleep-then-wake timing" "it needs a wait longer than this script should hold"
else
	no "scale to zero was refused"
fi

# --- what it costs ---------------------------------------------------------

step "What it costs, measured"

sleep 20
K3S_MB=$(ps -o rss= -C k3s-server 2>/dev/null | awk '{s+=$1} END {print int(s/1024)}')
PANEL_POD=$(kubectl -n "$PANEL_NS" get pod -l app=skifity-panel -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
PANEL_MB=$(kubectl -n "$PANEL_NS" top pod "$PANEL_POD" --no-headers 2>/dev/null | awk '{print $3}' | tr -d 'Mi')
USED_MB=$(free -m | awk '/^Mem:/ {print $3}')

say "  k3s server process : ${K3S_MB:-unknown} MB"
say "  the panel pod      : ${PANEL_MB:-unknown} MB"
say "  the whole machine  : ${USED_MB:-unknown} MB of ${MEM_MB} MB in use"
say ""
say "  docs/performance.md says the panel is 35 MiB idle, measured on a laptop."
say "  The number above is the first one measured on a cluster. If they disagree,"
say "  the document is what changes."

summary
[ "$FAIL" -eq 0 ]
