#!/bin/sh
# Smoke test: the installer's own logic, without installing anything.
#
# A real install needs root, systemd and a kernel k3s can use, so this checks
# everything that can be checked safely: the scripts parse, the manifests render
# from the repository, a failure explains itself, and the uninstaller refuses to
# delete data without an explicit confirmation.
#
# Running the real installer belongs in a throwaway VM, never on a machine
# anybody cares about.
set -eu

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
WORKDIR="$(mktemp -d)"
FAILURES=0

cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT INT TERM

# Named so they cannot be shadowed: this script sources install.sh, which
# defines its own fail(), ok() and note().
t_pass() { printf '  ok   %s\n' "$1"; }
t_fail() { printf '  FAIL %s\n' "$1" >&2; FAILURES=$((FAILURES + 1)); }
t_check() { if [ "$1" = "0" ]; then t_pass "$2"; else t_fail "$2"; fi; }

printf '\n== Skifity smoke test: installer ==\n\n'

# --- the scripts parse ------------------------------------------------------

for script in "$ROOT"/installer/*.sh; do
  if sh -n "$script" 2>"$WORKDIR/syntax.err"; then
    t_pass "$(basename "$script") is valid POSIX shell"
  else
    t_fail "$(basename "$script"): $(cat "$WORKDIR/syntax.err")"
  fi
done

# The installer must not need bash. A bashism here would fail on the minimal
# images people actually install on.
if command -v dash >/dev/null 2>&1; then
  for script in "$ROOT"/installer/*.sh; do
    if dash -n "$script" 2>/dev/null; then
      t_pass "$(basename "$script") parses under dash"
    else
      t_fail "$(basename "$script") does not parse under dash"
    fi
  done
fi

# --- the installer's helpers ------------------------------------------------

# shellcheck disable=SC1091
SKIFITY_INSTALLER_LIB=1 . "$ROOT/installer/install.sh"

# Every message helper has to work when the output is not a terminal, which is
# how it runs under `curl | sh` in a CI job or a provisioning script.
( say "hello" >/dev/null 2>&1 ) && t_check 0 "messages work without a terminal" || t_check 1 "messages work without a terminal"

# A failure must name a cause and a fix, and exit non-zero. An installer that
# stops with a bare error leaves someone with a half-installed server.
if ( fail "The thing did not work." "Do this instead." >/dev/null 2>&1 ); then
  t_fail "the installer's fail() should exit non-zero"
else
  t_pass "a failure exits non-zero"
fi

fail_text=$( (fail "The thing did not work." "Do this instead.") 2>&1 || true)
case "$fail_text" in
*"The thing did not work."*) t_pass "a failure says what happened" ;;
*) t_fail "a failure should say what happened" ;;
esac
case "$fail_text" in
*"What to do"*"Do this instead."*) t_pass "a failure says how to fix it" ;;
*) t_fail "a failure should say how to fix it" ;;
esac

# --- the manifests render ---------------------------------------------------

SOURCE_DIR="$ROOT"
NAMESPACE="skifity-system"
IMAGE="ghcr.io/skifity/skifity:0.0.0-test"
NODE_NAME="test-node"
PANEL_HOST="panel.example.test"
PUBLIC_URL="https://panel.example.test"
CONFIG_DIR="/etc/skifity"
DATA_DIR="/var/lib/skifity"
ISSUER="skifity-letsencrypt"

for manifest in panel.yaml ingress.yaml ingress-tls.yaml; do
  if render "deploy/$manifest" >"$WORKDIR/$manifest" 2>"$WORKDIR/render.err"; then
    t_pass "deploy/$manifest renders"
  else
    t_fail "deploy/$manifest: $(cat "$WORKDIR/render.err")"
    continue
  fi

  # The cluster-issuer has placeholders the caller fills in separately; these
  # three must come out complete.
  if grep -q '__[A-Z0-9_]*__' "$WORKDIR/$manifest"; then
    t_fail "deploy/$manifest still has a placeholder: $(grep -o '__[A-Z0-9_]*__' "$WORKDIR/$manifest" | head -1)"
  else
    t_pass "deploy/$manifest has nothing left to substitute"
  fi
done

grep -q "image: $IMAGE" "$WORKDIR/panel.yaml" &&
  t_pass "the image is the one asked for" ||
  t_fail "the rendered Deployment does not use $IMAGE"

grep -q "kubernetes.io/hostname: $NODE_NAME" "$WORKDIR/panel.yaml" &&
  t_pass "the panel is pinned to the node holding its data" ||
  t_fail "the rendered Deployment is not pinned to $NODE_NAME"

grep -q "host: $PANEL_HOST" "$WORKDIR/ingress.yaml" &&
  t_pass "the route uses the chosen hostname" ||
  t_fail "the rendered Ingress does not use $PANEL_HOST"

grep -q "cert-manager.io/cluster-issuer: $ISSUER" "$WORKDIR/ingress-tls.yaml" &&
  t_pass "the HTTPS route asks cert-manager for a certificate" ||
  t_fail "the rendered TLS Ingress has no issuer"

# The plain-HTTP route must not claim TLS it does not have.
if grep -q "tls:" "$WORKDIR/ingress.yaml"; then
  t_fail "the plain-HTTP route should not have a tls block"
else
  t_pass "the plain-HTTP route does not pretend to have a certificate"
fi

# --- nothing was installed --------------------------------------------------

# The point of sourcing the installer is that it changes nothing. If any of
# this appeared, a step ran that should not have.
for path in /etc/rancher /var/lib/rancher /usr/local/bin/k3s; do
  if [ -e "$path" ]; then
    t_fail "$path exists: the smoke test must not install anything"
  fi
done
t_pass "nothing was installed on this machine"

# --- the uninstaller refuses to delete data without being told --------------

UNINSTALL="$ROOT/installer/uninstall.sh"

# Every check below is a dry run. The uninstaller deletes the master key, and a
# test that could delete it on the machine running the test is not a test worth
# having, however carefully the confirmation is worded.
out=$(sh "$UNINSTALL" --dry-run --all --purge 2>&1 || true)
case "$out" in
*"Nothing was changed"*) t_pass "a dry run changes nothing" ;;
*) t_fail "a dry run should say nothing was changed, got: $out" ;;
esac
case "$out" in
*"would run: rm -rf"*) t_pass "a dry run says what it would delete" ;;
*) t_fail "a dry run should name what it would delete" ;;
esac
case "$out" in
*"✓"*) t_fail "a dry run must not tick off work it did not do" ;;
*) t_pass "a dry run does not claim work it did not do" ;;
esac

# The typed confirmation is what stands between a stray --purge and an
# unreadable backup, so check it is asked for.
grep -q 'delete my data' "$UNINSTALL" &&
  t_pass "purging asks for a typed confirmation" ||
  t_fail "purging should require a typed confirmation"

out=$(sh "$UNINSTALL" --nonsense 2>&1 || true)
case "$out" in
*"Unknown option"*) t_pass "an unknown option is refused" ;;
*) t_fail "an unknown option should be refused, got: $out" ;;
esac

out=$(sh "$UNINSTALL" --help 2>&1 || true)
case "$out" in
*"--purge"*) t_pass "the uninstaller documents itself" ;;
*) t_fail "--help should list the options" ;;
esac

printf '\n'
if [ "$FAILURES" -gt 0 ]; then
  printf '%s installer smoke check(s) failed.\n\n' "$FAILURES"
  exit 1
fi
printf 'All installer smoke checks passed.\n\n'
