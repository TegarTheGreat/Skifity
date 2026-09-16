# Progress

Read this file, `CLAUDE.md` and `docs/decisions.md` first if you are picking this
project up cold.

## Sandbox constraints (important)

The environment this was built in has Docker but **refuses privileged containers
and host-network relays**, so k3d/kind clusters and VMs could never be started.
See ADR-0010. Nothing below is called verified unless it was actually executed.
Where a cluster is required, the code is written and unit-tested against a fake
clientset and golden manifests, and that is said plainly rather than glossed over.

## Phase status

| Phase | Title | Status |
|---|---|---|
| 0 | Research and key decisions | done |
| 1 | Repository foundation | done |
| 2 | One-command installer | not started |
| 3 | Panel core: accounts, security, encryption | code complete, tested |
| 4 | Server and cluster management | code complete, cluster not exercised |
| 5 | App deployment | code complete, builds not exercised |
| 6 | Scaling | code complete, cluster not exercised |
| 7 | Databases, storage, backups | code complete, cluster not exercised |
| 8 | Developer experience and vibe coding | CLI and MCP done; `llms.txt` and docs pending |
| 9 | Differentiating features | partly done |
| 10 | Hardening and release | not started |

## What is done

### Phase 0 — done

* `docs/research/competitors.md` — 13 products compared, weaknesses we design against.
* `docs/research/stack.md` — component versions verified 2026-09-16.
* `docs/architecture.md` — layers, deploy path, add-server state machine, data model, isolation.
* `docs/decisions.md` — ADR-0001 to ADR-0012.

### Phase 1 — done

* Go module, package layout, `Makefile` with build/dev/test/lint/smoke/release.
* `golangci-lint` configured and clean; `go vet` and `gofmt` clean.
* ESLint and Prettier configured; `npm run lint` clean with zero warnings.
* Frontend: Vite 6, React 19, Tailwind v4, shadcn/ui only, app shell with
  sidebar, header, command palette, dark/light theme applied before first paint.
* Five languages complete (446 keys each), with a checker that fails the build on
  a missing key, an extra key, an empty string, a dropped placeholder, or a
  plural form the language does not have. Deliberately broken translations were
  used to confirm it actually fails.
* The built frontend is embedded in the binary; a deep link reloads correctly and
  fingerprinted assets are cached for a year while `index.html` is not.
* GitHub Actions CI: Go vet, format check, golangci-lint, tests, race tests;
  frontend i18n check, lint, type-check and build; then the smoke test.

**Verified by running it:** `make build` produces one 55 MB binary that serves
the localized UI and the health API; `/apps/app_x` falls back to `index.html`;
`test/smoke/panel.sh` passes all 26 checks against that binary.

### Phase 3 — code complete, tested

Accounts, Argon2id passwords, sessions, TOTP implemented in-house, API tokens,
CSRF double-submit, security headers, an audit log, and envelope encryption with
per-secret DEKs, context binding, master-key rotation and a printable recovery
key. Unit tests cover the envelope format against bit-flips and non-canonical
encodings, and rotation across every encrypted column.

### Phases 4–7 — code complete, cluster not exercised

Adding a server over SSH (seven idempotent steps), preflight checks, firewall
rules written per source address, k3s install and join, promotion and removal
with drain; manifest generation with probes, limits, spread and network policies;
builds through Railpack and BuildKit; deployments, rollback, autoscaling and the
scaling readiness checker; managed PostgreSQL, Redis and MariaDB; backups to S3
through presigned URLs so storage credentials never enter a tenant namespace.

Exercised against a fake clientset, golden manifests, a real in-process SSH
server that records the commands it receives, and `sh -n` on every generated
script. Not exercised against a real cluster.

### Phases 8–9 — partly done

The CLI and the MCP server are built on the same API as the panel, and both
render errors with their cause, impact and fix. The panel has the project canvas,
one-step templates, the error catalogue, and the build fingerprint that keeps a
configuration change from rebuilding an image.

## Next tasks

1. **Phase 2**: `installer/install.sh` — POSIX shell, preflight, k3s, master key,
   panel and cert-manager, setup token, idempotent, logged. Plus `uninstall.sh`,
   a non-interactive mode, and the panel's own manifests in `deploy/`.
2. **Phase 8**: `llms.txt`, and the CLI/API/MCP reference.
3. **Phase 10**: `govulncheck` and `npm audit`, resilience checks, GoReleaser and
   a container image, the documentation set, the README, idle RAM measurements,
   and the Playwright UI smoke test covering setup, sign-in and all five
   languages.

## Open issues

* Cluster smoke tests cannot run in this sandbox. The scripts exist and are
  reviewed, not executed.
* Idle RAM has not been measured yet; it needs a running cluster.

## Idle resource usage

Measured once the installer lands and a cluster can be started somewhere that
allows it.
