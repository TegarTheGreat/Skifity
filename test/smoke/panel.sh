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

# A token is issued for one team, and that binding was stored and never read,
# so a token made for one team worked on every team its owner belonged to.
SECOND_TEAM=$(curl -fsS -b "$WORKDIR/cookies" -X POST "$BASE/api/teams" \
  -H 'Content-Type: application/json' -H "X-Skifity-CSRF: $CSRF" \
  -d '{"name":"Other"}')
SECOND_TEAM_ID=$(echo "$SECOND_TEAM" | sed -n 's/.*"id":"\(team_[^"]*\)".*/\1/p')
[ -n "$SECOND_TEAM_ID" ] || fail "a second team was not created"

curl -fsS -b "$WORKDIR/cookies" "$BASE/api/teams/$SECOND_TEAM_ID/projects" >/dev/null \
  || fail "the owner cannot reach their own second team"
code=$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $API_TOKEN" \
  "$BASE/api/teams/$SECOND_TEAM_ID/projects")
[ "$code" = "404" ] || fail "a token bound to one team reached another and answered $code, want 404"
pass "a token cannot be used on a team it was not issued for"

# The bound token must also not be told the other team exists, or the CLI
# would pick a team every later request refuses.
curl -fsS -H "Authorization: Bearer $API_TOKEN" "$BASE/api/me" | grep -q "$SECOND_TEAM_ID" \
  && fail "a bound token was shown a team it cannot use"
pass "a bound token is only shown its own team"

# A token's scopes were stored and never consulted, so a read-only token could
# do everything its owner could, including issue a token with no scopes.
READONLY=$(curl -fsS -b "$WORKDIR/cookies" -X POST "$BASE/api/me/tokens" \
  -H 'Content-Type: application/json' -H "X-Skifity-CSRF: $CSRF" \
  -d '{"name":"read only","team_id":"'"$TEAM_ID"'","scopes":"read"}')
READONLY_TOKEN=$(echo "$READONLY" | sed -n 's/.*"secret":"\([^"]*\)".*/\1/p')
[ -n "$READONLY_TOKEN" ] || fail "no read-only token was returned"

curl -fsS -H "Authorization: Bearer $READONLY_TOKEN" "$BASE/api/teams" >/dev/null \
  || fail "a read-only token could not read"
code=$(curl -sS -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $READONLY_TOKEN" \
  -X POST "$BASE/api/me/tokens" -H 'Content-Type: application/json' \
  -d '{"name":"escalated","team_id":"'"$TEAM_ID"'"}')
[ "$code" = "403" ] || fail "a read-only token wrote and answered $code, want 403"
pass "a read-only token can read and cannot write"

# A scope nothing enforces must be refused, not stored and ignored.
code=$(curl -sS -b "$WORKDIR/cookies" -o /dev/null -w '%{http_code}' -X POST "$BASE/api/me/tokens" \
  -H 'Content-Type: application/json' -H "X-Skifity-CSRF: $CSRF" \
  -d '{"name":"bogus","team_id":"'"$TEAM_ID"'","scopes":"admin"}')
[ "$code" = "400" ] || fail "an unenforced scope answered $code, want 400"
pass "a scope the panel does not enforce is refused"

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

# An app created without a port used to get zero, which renders no Service, no
# Ingress and no URL: a deploy that succeeds and cannot be reached.
PORTLESS=$(curl -fsS -H "Authorization: Bearer $API_TOKEN" -X POST "$BASE/api/environments/$ENV_ID/apps" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Portless","source_type":"image","image":"nginx:1.27-alpine"}')
echo "$PORTLESS" | grep -q '"port":8080' || fail "an app created without a port did not get the default one"
pass "an app created without a port still gets one"

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


# A CI job, a container or an AI assistant never runs `skifity login`, so the
# CLI has to work from the environment alone, with no stored configuration and
# no team ever chosen.
ENV_OUT=$(SKIFITY_CONFIG="$WORKDIR/does-not-exist.json" \
  SKIFITY_URL="$BASE" SKIFITY_TOKEN="$API_TOKEN" "$BINARY" whoami 2>&1) ||
  fail "the CLI could not authenticate from the environment: $ENV_OUT"
echo "$ENV_OUT" | grep -q "owner@example.test" || fail "whoami from the environment did not report the user"
pass "the CLI authenticates from SKIFITY_URL and SKIFITY_TOKEN"

SKIFITY_CONFIG="$WORKDIR/does-not-exist.json" \
  SKIFITY_URL="$BASE" SKIFITY_TOKEN="$API_TOKEN" "$BINARY" apps --json >/dev/null ||
  fail "the CLI could not find the team from the token alone"
pass "the CLI finds the only team without being told"

# With nothing at all, the error has to name the way out rather than just
# refusing.
NO_AUTH=$(SKIFITY_CONFIG="$WORKDIR/does-not-exist.json" "$BINARY" whoami 2>&1 || true)
echo "$NO_AUTH" | grep -q 'SKIFITY_URL' ||
  fail "signing in with nothing set should mention SKIFITY_URL, got: $NO_AUTH"
pass "an unauthenticated CLI says how to authenticate"

# Connecting a Git account: the token must be stored and never come back, and
# the webhook address the user has to paste in must be returned once.
GIT_SOURCE=$(curl -fsS -b "$WORKDIR/cookies" -X POST "$BASE/api/teams/$TEAM_ID/git-sources" \
  -H 'Content-Type: application/json' -H "X-Skifity-CSRF: $CSRF" \
  -d '{"kind":"gitea","name":"Self-hosted","token":"verysecrettoken","base_url":"https://git.example.test"}')
echo "$GIT_SOURCE" | grep -q '"webhook_url"' || fail "connecting a Git account returned no webhook address"
pass "a Git account can be connected"

SOURCES=$(curl -fsS -b "$WORKDIR/cookies" "$BASE/api/teams/$TEAM_ID/git-sources")
echo "$SOURCES" | grep -q 'verysecrettoken' && fail "a Git token was returned by the API"
echo "$SOURCES" | grep -q 'Self-hosted' || fail "the connected Git account was not listed"
pass "a Git token is stored but never returned"

# A notification channel with a bad address must be refused before it is stored,
# while the person is still looking at the form.
code=$(curl -sS -b "$WORKDIR/cookies" -o "$WORKDIR/channel.json" -w '%{http_code}' \
  -X POST "$BASE/api/teams/$TEAM_ID/notifications" \
  -H 'Content-Type: application/json' -H "X-Skifity-CSRF: $CSRF" \
  -d '{"kind":"discord","name":"Bad","config":{"webhook_url":"https://example.com/nope"},"events":[]}')
[ "$code" = "400" ] || fail "a bad Discord webhook was accepted, answered $code"
grep -q 'does not look like a Discord webhook' "$WORKDIR/channel.json" ||
  fail "the rejection did not say what was wrong"
pass "a notification channel is validated before it is stored"

curl -fsS -b "$WORKDIR/cookies" -X POST "$BASE/api/teams/$TEAM_ID/notifications" \
  -H 'Content-Type: application/json' -H "X-Skifity-CSRF: $CSRF" \
  -d '{"kind":"discord","name":"Ops","config":{"webhook_url":"https://discord.com/api/webhooks/1/verysecrethook"},"events":["deploy.failed"]}' >/dev/null ||
  fail "a valid notification channel was refused"
CHANNELS=$(curl -fsS -b "$WORKDIR/cookies" "$BASE/api/teams/$TEAM_ID/notifications")
echo "$CHANNELS" | grep -q 'verysecrethook' && fail "a channel's webhook URL was returned by the API"
pass "a notification channel is stored without its address coming back"

# Two-factor recovery codes are the only way back in on a panel with no email
# reset by design, so they have to be handed out and then actually remembered.
TOTP=$(curl -fsS -b "$WORKDIR/cookies" -H "X-Skifity-CSRF: $CSRF" -X POST "$BASE/api/me/totp")
echo "$TOTP" | grep -q '"recovery_codes":\[' || fail "two-factor setup returned no recovery codes"
echo "$TOTP" | grep -qE '"[A-Z2-7]{4}-[A-Z2-7]{4}-[A-Z2-7]{4}"' \
  || fail "the recovery codes are not in the shape the account page prints"
pass "two-factor setup hands out recovery codes"

curl -fsS -b "$WORKDIR/cookies" "$BASE/api/me" | grep -q '"recovery_codes_left":8' \
  || fail "the recovery codes were printed and not stored"
pass "the recovery codes are stored, not just printed"

# Nobody can sign in is the one situation the API cannot fix, so the recovery
# path has to work: it reads the database directly, on the server.
"$BINARY" admin list-users --database "$WORKDIR/panel.db" | grep -q 'owner@example.test' ||
  fail "admin list-users did not find the account"
pass "an administrator can list the accounts from the server"

"$BINARY" admin reset-password --database "$WORKDIR/panel.db" \
  --password 'an entirely different passphrase' owner@example.test >/dev/null ||
  fail "admin reset-password failed"

code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.test","password":"'"$PASSWORD"'"}')
[ "$code" = "401" ] || fail "the old password still worked after a reset, answered $code"

code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$BASE/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"email":"owner@example.test","password":"an entirely different passphrase"}')
[ "$code" = "200" ] || fail "the new password did not work after a reset, answered $code"
pass "a password can be reset from the server, and the old one stops working"

# Nothing sensitive may reach the panel's own log. This runs last on purpose:
# every secret above starts with "verysecret", and a leak check that runs
# before the secrets are sent proves nothing.
grep -q 'verysecret' "$WORKDIR/server.log" && fail "a secret reached the panel's log"
grep -q "$PASSWORD" "$WORKDIR/server.log" && fail "the password reached the panel's log"
pass "no secrets reached the panel's log"

# Logging out must end the session.
"$BINARY" logout >/dev/null
printf '\nAll panel smoke checks passed.\n\n'
