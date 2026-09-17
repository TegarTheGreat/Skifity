# The CLI, the API and AI assistants

Three ways in, one API behind all of them. Anything the panel can do, a terminal
or an assistant can do.

## Signing in

```sh
skifity login
```

It asks for the panel's address and opens a browser to create a token, or takes
one you already have:

```sh
skifity login --url https://panel.example.com --token skf_...
```

The token is stored in your user configuration directory, never in a project, so
it cannot be committed by accident.

### Without signing in

For CI, a container, or an assistant's sandbox, set two environment variables
and nothing has to be signed in at all:

```sh
export SKIFITY_URL=https://panel.example.com
export SKIFITY_TOKEN=skf_...
skifity apps
```

They override a stored configuration, so setting them is never silently ignored.
If your account is in more than one team, add `SKIFITY_TEAM`; with one team the
CLI works it out.

Create tokens in the panel under **Account → API tokens**. A token cannot do
more than the person who created it.

## Deploying

In the directory of the thing you want to deploy:

```sh
skifity init      # writes skifity.toml
skifity deploy
```

`skifity.toml` records which app this directory is, so the commands below need
no arguments:

```toml
app = "app_06gaqcxybny593h2s52m"
name = "web"
environment = "env_06gaqcxy21mdqsdb1z7g"
```

It holds no secrets and belongs in version control.

## Everything else

```sh
skifity status                    # running? how many instances? what URL?
skifity logs --follow             # live output
skifity env list                  # variables
skifity env set LOG_LEVEL=debug   # set one; says whether it rebuilds
skifity env set --secret API_KEY=... # stored encrypted, never shown again
skifity env rm LOG_LEVEL          # remove one
skifity scale --instances 3       # a fixed number
skifity scale --auto --max 5      # or automatically
skifity rollback                  # back to the previous version
skifity run -- npm run migrate    # run a one-off command in the app's image
skifity open                      # print the URLs
skifity apps                      # everything in this environment
skifity servers                   # the machines
skifity export --out ./leaving    # the whole team, as JSON and Kubernetes YAML
```

Any command takes `--app <id>` to act on something other than the current
directory's app, and `--json` for output a script can rely on. The human format
is not stable; the JSON is.

`export` is the one worth knowing about before you need it. It writes
`skifity-export.json` — every project, app, domain, database, variable and
schedule — plus `manifests/<namespace>/<app>.yaml` per app: the Deployment,
Service, Ingress, autoscaler, disruption budget, volume claims and scheduled
commands as plain Kubernetes objects. `kubectl apply -f` them on any cluster and
the apps run there without this panel. Secret values are not included, and the
README it writes says where they already are.

## Recovering access

These run on the panel's own server and read its database directly, so they work
when nobody can sign in — which is the one thing the API cannot help with. They
need root.

```sh
sudo skifity admin list-users
sudo skifity admin reset-password you@example.com
```

Resetting a password signs out every device that was signed in as that account.
Two-factor authentication stays on, so you will still need your authenticator
app.

## AI assistants

The same binary is an MCP server:

```sh
skifity mcp
```

Point your assistant at it. In Claude Code:

```sh
claude mcp add skifity -- skifity mcp
```

It needs credentials the same way the CLI does: either `skifity login` first, or
`SKIFITY_URL` and `SKIFITY_TOKEN` in its environment.

The tools are listed in [llms.txt](../llms.txt). They are named for what someone
would ask for — `get_app_logs`, `check_scaling_readiness`, `rollback_app` — and
every error comes back as a cause, an impact and a suggested fix rather than a
status code, because an assistant acting on "the build ran out of memory; add a
server or raise the build memory limit" does something useful, and one acting on
"500" guesses.

### What to give an assistant

Point it at `llms.txt` on your panel, at `/llms.txt`. It describes the whole
product on one page: the model, the tools, the endpoints, and the handful of
things that are not obvious, such as which variables cause a rebuild.

## The HTTP API

`https://<panel>/api`, with `Authorization: Bearer <token>`.

The endpoint list is in [llms.txt](../llms.txt). Lists come back as
`{"items": [...], "total": n}`, and every error has the same shape:

```json
{
  "error": {
    "code": "deploy.build_failed",
    "title": "The build failed",
    "cause": "...",
    "impact": "...",
    "fix": "...",
    "retryable": true
  }
}
```

Live updates come from server-sent events:

```sh
curl -N -H "Authorization: Bearer $SKIFITY_TOKEN" \
  "$SKIFITY_URL/api/events?topics=team:team_06gaqcvks6teywnwmw1p"
```

Topics are `team:<id>`, `operation:<id>` and `deployment:<id>`. The browser
reconnects and resumes from the last event it saw, so a dropped connection does
not lose the middle of a build log.
