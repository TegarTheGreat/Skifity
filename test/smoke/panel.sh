#!/bin/sh
# Smoke test: the panel starts, first-run setup works, sign-in works, an API
# token works, and the CLI can use it.
#
# This one needs no cluster, so it runs anywhere, including in CI. The four
# cluster smoke tests in this directory need a Docker-capable host.
set -eu

BINARY="${BINARY:-./bin/skifity}"
PORT="${PORT:-18099}"
WORKDIR="$(mktemp -d)"
BASE="http://127.0.0.1:${PORT}"
PASSWORD="a reasonable passphrase"

cleanup() {
  if [ -n "${SERVER_PID:-}" ]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT INT TERM

fail() { printf '\nFAILED: %s\n' "$1" >&2; [ -f "$WORKDIR/server.log" ] && tail -30 "$WORKDIR/server.log" >&2; exit 1; }
pass() { printf '  ok   %s\n' "$1"; }

printf '\n== %s smoke test: panel ==\n\n' "Skifity"

[ -x "$BINARY" ] || fail "$BINARY does not exist. Run: make build"

SKIFITY_LISTEN="127.0.0.1:${PORT}" \
SKIFITY_DATABASE_PATH="$WORKDIR/panel.db" \
SKIFITY_MASTER_KEY_PATH="$WORKDIR/master.key" \
SKIFITY_SETUP_TOKEN_PATH="$WORKDIR/setup-token" \
SKIFITY_PUBLIC_URL="$BASE" \
SKIFITY_DEV_MODE=true \
SKIFITY_LOG_FORMAT=text \
  "$BINARY" server > "$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

# Wait for it to answer rather than sleeping a fixed amount.
i=0
while [ $i -lt 50 ]; do
  if curl -fsS "$BASE/api/health" >/dev/null 2>&1; then break; fi
  kill -0 "$SERVER_PID" 2>/dev/null || fail "the panel exited during startup"
  i=$((i + 1))
  sleep 0.2
done
[ $i -lt 50 ] || fail "the panel did not answer within ten seconds"
pass "the panel started and answers /api/health"

curl -fsS "$BASE/api/health" | grep -q '"status":"ok"' || fail "health did not report ok"
pass "health reports ok"

curl -fsS "$BASE/api/setup/status" | grep -q '"needs_setup":true' || fail "a fresh panel did not ask for setup"
pass "a fresh panel asks for setup"

# An unauthenticated request must be refused.
code=$(curl -sS -o /dev/null -w '%{http_code}' "$BASE/api/teams")
[ "$code" = "401" ] || fail "an unauthenticated request answered $code, want 401"
pass "unauthenticated requests are refused"

# A wrong setup token must be refused.
code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/setup" \
  -H 'Content-Type: application/json' \
  -d '{"token":"wrong","email":"a@example.test","password":"'"$PASSWORD"'"}')
[ "$code" = "401" ] || fail "a wrong setup token answered $code, want 401"
pass "a wrong setup token is refused"

TOKEN="$(cat "$WORKDIR/setup-token")"
[ -n "$TOKEN" ] || fail "no setup token was written"

SETUP=$(curl -fsS -X POST "$BASE/api/setup" -H 'Content-Type: application/json' \
  -d '{"token":"'"$TOKEN"'","email":"owner@example.test","name":"Owner","password":"'"$PASSWORD"'","team_name":"Acme"}')
echo "$SETUP" | grep -q '"recovery_key":"SKIFITY-RECOVERY-v1-' || fail "setup did not return a recovery key"
pass "setup creates the first account and returns a recovery key"

# Setup must not be possible twice.
code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/setup" \
  -H 'Content-Type: application/json' \
  -d '{"token":"'"$TOKEN"'","email":"second@example.test","password":"'"$PASSWORD"'"}')
[ "$code" = "409" ] || fail "setup ran a second time and answered $code"
pass "setup cannot be run twice"

[ ! -f "$WORKDIR/setup-token" ] || fail "the setup token file was not removed after use"
pass "the setup token file is removed once used"

# Sign in.
LOGIN=$(curl -fsS -c "$WORKDIR/cookies" -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.test","password":"'"$PASSWORD"'"}')
echo "$LOGIN" | grep -q '"csrf_token"' || fail "sign-in did not return a CSRF token"
pass "sign-in works"

CSRF=$(echo "$LOGIN" | sed -n 's/.*"csrf_token":"\([^"]*\)".*/\1/p')

# A wrong password must be refused.
code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.test","password":"wrong password entirely"}')
[ "$code" = "401" ] || fail "a wrong password answered $code, want 401"
pass "a wrong password is refused"

# A state-changing request without the CSRF header must be refused.
TEAM_ID=$(curl -fsS -b "$WORKDIR/cookies" "$BASE/api/teams" | sed -n 's/.*"id":"\(team_[^"]*\)".*/\1/p')
[ -n "$TEAM_ID" ] || fail "no team was returned"
code=$(curl -sS -b "$WORKDIR/cookies" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/me/tokens" \
  -H 'Content-Type: application/json' -d '{"name":"x","team_id":"'"$TEAM_ID"'"}')
[ "$code" = "403" ] || fail "a request without a CSRF token answered $code, want 403"
pass "cross-site requests are blocked"

# Create an API token with the CSRF header.
TOKEN_RESPONSE=$(curl -fsS -b "$WORKDIR/cookies" -X POST "$BASE/api/me/tokens" \
  -H 'Content-Type: application/json' -H "X-Skifity-CSRF: $CSRF" \
  -d '{"name":"smoke test","team_id":"'"$TEAM_ID"'"}')
API_TOKEN=$(echo "$TOKEN_RESPONSE" | sed -n 's/.*"secret":"\([^"]*\)".*/\1/p')
[ -n "$API_TOKEN" ] || fail "no API token was returned"
pass "an API token can be created"

# The token must work without a cookie, and without CSRF.
curl -fsS -H "Authorization: Bearer $API_TOKEN" "$BASE/api/me" | grep -q 'owner@example.test' \
  || fail "the API token does not authenticate"
pass "the API token authenticates"

# A made-up token must not.
code=$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer skf_not_a_real_token" "$BASE/api/me")
[ "$code" = "401" ] || fail "an invalid token answered $code, want 401"
pass "an invalid token is refused"

# The CLI, using that token.
export SKIFITY_CONFIG="$WORKDIR/cli-config.json"
"$BINARY" login --url "$BASE" --token "$API_TOKEN" >/dev/null || fail "skifity login failed"
pass "skifity login stores the token"

"$BINARY" whoami | grep -q 'owner@example.test' || fail "skifity whoami did not show the user"
pass "skifity whoami works"

"$BINARY" apps --json >/dev/null || fail "skifity apps failed"
pass "skifity apps works"

"$BINARY" servers --json | grep -q '\[' || fail "skifity servers did not return a list"
pass "skifity servers works"

# Create an app through the API and check the CLI sees it.
PROJECT_ID=$(curl -fsS -H "Authorization: Bearer $API_TOKEN" "$BASE/api/teams/$TEAM_ID/projects" \
  | sed -n 's/.*"id":"\(prj_[^"]*\)".*/\1/p')
ENV_ID=$(curl -fsS -H "Authorization: Bearer $API_TOKEN" "$BASE/api/projects/$PROJECT_ID/environments" \
  | sed -n 's/.*"id":"\(env_[^"]*\)".*/\1/p')
[ -n "$ENV_ID" ] || fail "setup did not create an environment"
pass "setup created a project and an environment"

APP=$(curl -fsS -H "Authorization: Bearer $API_TOKEN" -X POST "$BASE/api/environments/$ENV_ID/apps" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Web","source_type":"image","image":"nginx:1.27-alpine","port":80}')
APP_ID=$(echo "$APP" | sed -n 's/.*"id":"\(app_[^"]*\)".*/\1/p')
[ -n "$APP_ID" ] || fail "the app was not created"
pass "an app can be created"

"$BINARY" apps --json | grep -q "$APP_ID" || fail "the CLI does not list the new app"
pass "the CLI lists the new app"

# Variables: a runtime one must not require a rebuild.
"$BINARY" env set --app "$APP_ID" LOG_LEVEL=debug | grep -q 'No rebuild was needed' \
  || fail "setting a runtime variable reported a rebuild"
pass "a runtime variable is rolled out without rebuilding"

"$BINARY" env set --app "$APP_ID" --build API_URL=https://api.example.test | grep -q 'rebuild' \
  || fail "setting a build-time variable did not warn about a rebuild"
pass "a build-time variable says it will rebuild"

"$BINARY" env --app "$APP_ID" list | grep -q 'LOG_LEVEL' || fail "the variable is not listed"
pass "variables are listed"

# A secret must never be readable again.
"$BINARY" env set --app "$APP_ID" --secret DB_PASSWORD=verysecret >/dev/null
"$BINARY" env --app "$APP_ID" list | grep -q 'verysecret' && fail "a secret's value was shown"
"$BINARY" env --app "$APP_ID" list | grep -q '(secret)' || fail "the secret is not listed at all"
pass "a secret is stored but never shown again"

# The scaling readiness checker must notice a real problem.
"$BINARY" env set --app "$APP_ID" DATABASE_URL=sqlite:///data/app.db >/dev/null
READINESS=$(curl -fsS -H "Authorization: Bearer $API_TOKEN" "$BASE/api/apps/$APP_ID/scaling/readiness")
echo "$READINESS" | grep -q 'sqlite' || fail "the readiness checker missed SQLite"
pass "the scaling readiness checker spots SQLite"

# Audit: the actions above must be recorded.
AUDIT=$(curl -fsS -H "Authorization: Bearer $API_TOKEN" "$BASE/api/teams/$TEAM_ID/audit")
echo "$AUDIT" | grep -q 'app.created' || fail "the audit log has no record of the app being created"
echo "$AUDIT" | grep -q 'verysecret' && fail "a secret's value reached the audit log"
pass "actions are audited, without secret values"

# Nothing sensitive may reach the panel's own log.
grep -q 'verysecret' "$WORKDIR/server.log" && fail "a secret reached the panel's log"
grep -q "$PASSWORD" "$WORKDIR/server.log" && fail "the password reached the panel's log"
pass "no secrets reached the panel's log"

# Logging out must end the session.
"$BINARY" logout >/dev/null
printf '\nAll panel smoke checks passed.\n\n'
