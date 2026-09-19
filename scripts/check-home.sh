#!/bin/sh
# Fail if anything names a home this project does not own.
#
# The installer used to default to ghcr.io/skifity/skifity and fetch its
# manifests from github.com/skifity/skifity — a user and an organisation nobody
# here has ever owned. That is not a name that happens to fail: it is one that
# would start working the day somebody else registered it, and then run their
# image as root on every server that ran the one-line install.
#
# Where this project publishes is now one line in installer/install.sh and one
# in the Makefile, and every other file derives from those. This check exists so
# that stays true: a copy-pasted image tag, a documentation example or a test
# fixture that reintroduces the old name fails the build instead of sitting
# there until somebody installs from it.
#
# docs/progress.md is exempt. It is the record of what was wrong and when, and
# rewriting history to please a linter is how a project stops being able to
# trust its own notes.
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FAILURES=0

# A name is forbidden, and the reason is printed with it rather than left for
# somebody to work out from a grep.
check() {
	pattern="$1"
	reason="$2"
	hits=$(git grep -n -I -F -e "$pattern" -- \
		':!docs/progress.md' \
		':!scripts/check-home.sh' \
		':!web/dist' 2>/dev/null || true)
	if [ -n "$hits" ]; then
		printf '\n%s\n' "$reason"
		printf '%s\n' "$hits" | sed 's/^/  /'
		FAILURES=$((FAILURES + 1))
	fi
}

check "skifity/skifity" \
	"github.com/skifity and ghcr.io/skifity are not namespaces this project owns.
Derive the name from PROJECT_REPO in installer/install.sh or the Makefile, or
use example/ in a fixture:"

check "get.skifity.com" \
	"get.skifity.com does not resolve and nothing is served from it. The install
command is the installer's raw URL at the release being installed:"

if [ "$FAILURES" -gt 0 ]; then
	printf '\n%s forbidden name(s) found.\n\n' "$FAILURES"
	exit 1
fi
printf 'No forbidden project names.\n'
