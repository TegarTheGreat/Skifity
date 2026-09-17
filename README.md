<div align="center">

<img src="docs/images/logo.svg" width="72" height="72" alt="">

# Skifity

**Self-hosted apps, powered by Kubernetes.**

You give it a server. It gives you URLs.

</div>

---

```sh
curl -fsSL https://get.skifity.io | sudo sh
```

That is the whole installation. It checks the server, installs Kubernetes,
starts the panel, and prints a URL and a one-time token. Open the URL, create
your account, paste a Git repository, and you have an app on the internet with
HTTPS.

**That command does not work yet.** `get.skifity.io` does not resolve and
nothing has been released, so there is no one-line install to run — see
[Status](#status) for what does work today and what it is waiting on.

<p align="center">
  <img src="docs/images/overview-dark.png" alt="The overview, on a fresh install with no servers yet" width="820">
</p>

## What it is

Coolify and Dokploy run Docker Compose on one machine. Skifity runs Kubernetes,
so several servers are one pool: an app that wants three instances gets them
wherever there is room, and an app whose server dies is restarted elsewhere
without anyone being woken up.

What it does not do is make you learn Kubernetes. The panel talks about **Apps**,
**Instances**, **Servers**, **Domains** and **Databases**. The Kubernetes objects
are one click away under **Advanced**, and `kubectl` works normally — they are
simply not in the way.

## Adding a server is an IP address and a password

<p align="center">
  <img src="docs/images/add-server.png" alt="The add server form: address, username, password" width="820">
</p>

Skifity connects, checks the machine, installs a key of its own, configures the
firewall, verifies the network in both directions, installs k3s and joins the
cluster — seven steps, each one shown as it happens.

Your password is used exactly once, to install that key, and is never stored. A
test asserts it: every column of the database is scanned for it.

## What is different about it

**Changing a setting does not rebuild your app.** The single most common
complaint about panels of this kind. Skifity separates what goes into the image
from what the container reads at start-up, so changing a variable is a rollout
in seconds. The panel tells you which kind you are setting before you save, and
says afterwards whether it rebuilt.

**Rolling back restores the settings too**, not just the image. If a variable
broke the app, rolling back puts the old variable back.

**It checks before you scale.** Before running a second instance, Skifity looks
for what would break — SQLite on a local disk, sessions in memory, a cron job
that assumes it is alone — and says what would go wrong and how to fix it.

**Errors are written to be acted on.** Every failure, in the panel, the CLI, the
API and the MCP server, says what happened, what it means and how to fix it,
with a button that copies the whole thing for an AI assistant.

**It never phones home.** No licence key, no telemetry, no update check, no call
to anything of ours — there is nothing of ours to call. It does reach the
internet for things you asked for: Let's Encrypt for a certificate, GitHub for
the component you just turned on, your Git provider, your S3 bucket. The one
exception worth knowing is that the preflight asks a public-IP service what the
server's address is, because a server behind NAT cannot tell you itself.

## Built for the terminal and for assistants

One binary is the panel, the CLI and an MCP server.

```sh
skifity deploy                    # deploy this directory
skifity logs --follow             # watch it
skifity env set LOG_LEVEL=debug   # change something, without a rebuild
skifity rollback                  # undo it
```

```sh
claude mcp add skifity -- skifity mcp
```

[`llms.txt`](llms.txt) describes the whole product on one page, and the panel
serves it at `/llms.txt`. See [the CLI guide](docs/cli.md).

## Five languages, properly

<p align="center">
  <img src="docs/images/overview-russian.png" alt="The same overview in Russian" width="820">
</p>

English, Indonesian, Hindi, Russian and Simplified Chinese, all complete. A
build fails if any string is missing from any language, if a plural form a
language needs is absent — Russian needs one, few and many — or if a translation
drops a placeholder. Dates, numbers and relative times are formatted by the
language, not translated around.

## What you need

A server with Ubuntu 24.04 or Debian 12, 1 GB of memory, 8 GB of free disk, and
root over SSH. Any VPS will do. 2 GB is comfortable.

A fresh install is deliberately small: Kubernetes and the panel, and nothing
else. The panel is **35 MiB of resident memory idle**, measured, and does not
drift. Certificates, the builder, the PostgreSQL operator and the rest install
themselves the first time you use them, and each one says what it costs first.

## Documentation

Also served by the panel itself, at `/docs`, from inside the binary — which is
where you want it when the cluster is broken and the server has no browser and
no way out to the internet.

| | |
|---|---|
| [Quick start](docs/quick-start.md) | Empty server to a running app |
| [Concepts](docs/concepts.md) | What the words mean, and what they are underneath |
| [Adding servers](docs/adding-servers.md) | The seven steps, and what to do when one fails |
| [The CLI and AI assistants](docs/cli.md) | Terminal, API, MCP |
| [Troubleshooting](docs/troubleshooting.md) | When something is wrong |
| [Questions](docs/faq.md) | Including the ones with awkward answers |
| [Configuration](docs/configuration.md) | Every setting, and what to back up |
| [What it costs](docs/performance.md) | Memory and size, measured |
| [Architecture](docs/architecture.md) | How it fits together |
| [Decisions](docs/decisions.md) | Why it is like this |

## Building it yourself

```sh
make build     # the frontend, then one binary with it inside
make check     # every linter, then the tests
make smoke     # end to end against a real panel
```

Go 1.26 and Node 22. `CGO_ENABLED=0` everywhere, so a release binary is static
and runs on anything.

## Status

Honest version, two parts.

**Nothing is published.** `get.skifity.io` does not resolve. There is no
`skifity/skifity` repository on GitHub — this one lives at
`TegarTheGreat/Skifity`. No image has been pushed to `ghcr.io/skifity/skifity`,
and no release has been tagged, so the CLI download in the installer has nothing
to download. Every one-line install command in this README and in the
documentation is what the install will be, not what it is. What works today:
clone the repository, `make image`, and run `installer/install.sh` from inside
the clone with `SKIFITY_IMAGE` pointed at the image you just built — the
installer then reads its manifests from disk. [Quick
start](docs/quick-start.md) has the exact commands.

**Nothing has run against a real cluster.** The environment this was built in
refuses privileged containers, so k3s could never be started in it. Everything
that needs a cluster is tested against a fake API server, golden manifests, a
real in-process SSH server and shell syntax checks — which exercises the same
code path and is not the same thing as having run.
[`docs/progress.md`](docs/progress.md) says exactly what has and has not been
executed.

Treat it as something to try on a spare VPS, not something to move production
onto this afternoon.

## Licence

Apache 2.0. See [LICENSE](LICENSE).
