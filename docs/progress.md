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
| 2 | One-command installer | done, not run on a real server |
| 3 | Panel core: accounts, security, encryption | code complete, tested |
| 4 | Server and cluster management | code complete, cluster not exercised |
| 5 | App deployment | code complete, builds not exercised |
| 6 | Scaling | code complete, cluster not exercised |
| 7 | Databases, storage, backups | code complete, cluster not exercised |
| 8 | Developer experience and vibe coding | done |
| 9 | Differentiating features | partly done |
| 10 | Hardening and release | done, except a run on real hardware |

## What is done

### Phase 0 — done

* `docs/research/competitors.md` — 13 products compared, weaknesses we design against.
* `docs/research/stack.md` — component versions verified 2026-09-16.
* `docs/architecture.md` — layers, deploy path, add-server state machine, data model, isolation.
* `docs/decisions.md` — ADR-0001 to ADR-0015.

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

### Phase 2 — done, not run on a real server

* `installer/install.sh` — POSIX shell, no bashisms, checks the server before it
  changes anything, installs k3s with embedded etcd and WireGuard between nodes,
  prepares the panel's directories, generates the setup token, works out a
  hostname, applies the panel and prints a URL and a token. Safe to run again:
  every step checks what is already there.
* `installer/uninstall.sh` — removes the panel by default, k3s with `--all`, and
  the data only with `--purge` and a typed confirmation. `--dry-run` prints what
  it would do and changes nothing.
* `deploy/` — the panel's own objects, also usable with `kubectl apply -f`.
* `Dockerfile` — three stages down to a distroless image holding one static
  binary, running as a non-root user.

**Verified:** `test/smoke/installer.sh` runs 26 checks — both scripts parse under
`sh` and `dash`, failures carry a cause and a fix, every manifest renders with
nothing left to substitute, the plain-HTTP route does not claim a certificate it
does not have, and nothing is installed on the machine running the test. A Go
test parses the rendered Deployment and asserts it runs as non-root with a
read-only root filesystem, no capabilities, no CPU limit, the Recreate strategy
and the node pinning. Both were confirmed to fail when the manifests were
deliberately broken.

**Not verified:** the installer has never been run on a real server. It needs
root, systemd and a kernel k3s can use, none of which this sandbox allows.

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

### Phases 8–9 — done

The CLI and the MCP server are built on the same API as the panel, and both
render errors with their cause, impact and fix. The panel has the project canvas,
one-step templates, the error catalogue, and the build fingerprint that keeps a
configuration change from rebuilding an image.

Added since:

* `llms.txt`, describing the whole product on one page, served by the panel at
  `/llms.txt`.
* `SKIFITY_URL` and `SKIFITY_TOKEN`, so the CLI and the MCP server work with no
  interactive sign-in and no stored configuration — which is how CI, a
  container and an assistant's sandbox actually use them. The team is worked
  out from the token's account when there is only one.
* `skifity admin reset-password` and `skifity admin list-users`, which read the
  panel's database directly on the server. Nobody being able to sign in is the
  one thing the API cannot fix, and a self-hosted panel has no mail server it
  can trust to send a reset link.

### Phase 10 — done, except a run on real hardware

* `govulncheck` and `npm audit` are clean and run in CI. The one advisory left
  is `golang.org/x/crypto/openpgp` being unmaintained, in a package this code
  never imports; govulncheck reports zero reachable vulnerabilities.
* A Playwright interface test covering first-run setup, the recovery-key gate,
  the shell, the theme, and all five languages. It runs against the real binary
  serving the embedded frontend, so what is tested is what ships.
* GoReleaser: static binaries for Linux and macOS on both architectures, and a
  multi-architecture distroless image, on a tag.
* The documentation set: quick start, concepts, adding servers, the CLI and AI
  assistants, troubleshooting, questions — plus a README with real screenshots
  taken by the test suite, so an image can never show a screen that no longer
  exists.
* Apache 2.0.

**The interface test found a real bug the completeness checker could not:**
Simplified Chinese resolved to English at runtime. `nonExplicitSupportedLngs`
makes i18next check the *language part* of a code against `supportedLngs`, so
asking for `zh-CN` looked up `zh`, did not find it, and fell back. The locale
file was complete the whole time. Fixed by registering the bundle under `zh` as
well, which also gets `zh-TW` and `zh-HK` a Simplified page rather than an
English one.

## Next tasks

1. Run the installer end to end on a real Ubuntu server and measure idle memory.
   Both need hardware this sandbox cannot provide.
2. Tag a release, which publishes the binaries and the image the installer
   points at.
3. Deploy a real application from Git, end to end, on that server — the one
   flow that has never been exercised against a live cluster.

## Open issues

* Cluster smoke tests cannot run in this sandbox. The scripts exist and are
  reviewed, not executed.
* The installer references `ghcr.io/skifity/skifity`, which is not published
  until the first tag. Until then an install needs `SKIFITY_IMAGE` pointed at an
  image built locally with `make image`.
* The interface test needs a Chromium. It uses one already on the machine when
  `CHROMIUM_PATH` is set, and CI installs its own.
* k3s's memory footprint is not measured; the panel's is, in
  `docs/performance.md`.

## Idle resource usage

`docs/performance.md`. The panel is measured: 34 MiB resident idle, 36 MiB after
400 requests, 37 MiB of binary. k3s's own footprint is not measured here, for
the same reason as everything else that needs a cluster.
