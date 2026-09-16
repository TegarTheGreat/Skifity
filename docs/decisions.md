# Architecture decision records

Short ADRs. Newest at the bottom. Format: context, options, decision, reason, consequences.

---

## ADR-0001 - Kubernetes (k3s) instead of Docker Swarm

**Context.** Every mainstream self-hosted PaaS (Coolify, Dokploy, CapRover) orchestrates with Docker
or Docker Swarm. Their multi-server story is the part users complain about: Coolify still labels Swarm
experimental in its own documentation, Dokploy puts multi-node behind a source-available licence, and
Swarm itself is in maintenance upstream.

**Options.** (a) Docker + SSH fan-out, (b) Docker Swarm, (c) Nomad, (d) k3s.

**Decision.** k3s with embedded etcd.

**Reason.** Self-healing, rolling updates, rollback, node failover, HPA and topology spreading already
exist and are maintained by someone else. k3s is a ~60 MB binary, so "lightweight" and "Kubernetes" are
not in conflict. Nomad is elegant but its ecosystem (cert-manager, CloudNativePG, KEDA, Longhorn) does
not exist.

**Consequences.** We inherit Kubernetes' complexity, so hiding it becomes a *product requirement*, not a
nicety: plain-language errors, human nouns in the UI, and Kubernetes objects only behind "Advanced".

---

## ADR-0002 - `--cluster-init` from the very first node

**Context.** k3s defaults to SQLite for a single server. Moving to HA later then means a rebuild.

**Decision.** Always install the first server with `--cluster-init` (embedded etcd).

**Reason.** A one-node etcd costs a little RAM but makes "Promote to control plane" a button rather
than a migration.

**Consequences.** Slightly higher idle memory on a one-node install. Accepted.

---

## ADR-0003 - `wireguard-native` flannel backend

**Context.** Users combine cheap VPSes from different providers, so pod traffic crosses the public
internet unencrypted with the default vxlan backend.

**Decision.** `--flannel-backend=wireguard-native`, with preflight detection of the `wireguard` kernel
module and an explicit fallback to vxlan with a warning shown in the panel.

**Consequences.** Some minimal kernels lack the module; the fallback keeps those usable but flags the
security downgrade instead of hiding it.

---

## ADR-0004 - SQLite (modernc.org/sqlite) for the panel database

**Options.** SQLite (cgo `mattn`), SQLite (pure Go `modernc`), embedded Postgres, CloudNativePG.

**Decision.** `modernc.org/sqlite`, WAL mode, on a PersistentVolume.

**Reason.** `CGO_ENABLED=0` keeps cross-compilation for arm64 trivial and the binary static. The panel
is a single writer with low write volume. Using Postgres would require the platform to provision a
database before the component that provisions databases is running.

**Consequences.** The panel is a single replica. That is acceptable: it is a control plane, and apps keep
running while it restarts. Documented in `docs/architecture.md`.

---

## ADR-0005 - Server-Sent Events, not WebSocket

**Decision.** SSE for install progress, build logs, app logs and cluster events.

**Reason.** All of it is server to client. SSE is plain HTTP, reconnects on its own, and passes through
Traefik and corporate proxies without an upgrade dance. The one bidirectional feature (container exec)
uses a WebSocket, because it has to.

---

## ADR-0006 - Envelope encryption with a master key kept outside the database

**Decision.** Each secret gets a random 256-bit data key; the data key is wrapped with the master key
using AES-256-GCM. The master key lives in a Kubernetes Secret and on disk at
`/etc/skifity/master.key`, never in the panel database.

**Reason.** A leaked database backup is useless without the master key. Per-secret data keys make
master key rotation an unwrap/rewrap of key material only, so it does not need to read or rewrite
plaintext secrets.

**Consequences.** Losing the master key means losing every secret, so the installer prints a recovery
key and the panel nags until it is downloaded.

---

## ADR-0007 - Separate build inputs from runtime inputs

**Context.** The most common Coolify complaint is that changing a setting triggers a full rebuild.

**Decision.** A deployment is described by a *build fingerprint* (source repo + commit + builder +
build-time args + Dockerfile path) and a *runtime spec* (env vars, replicas, domains, resources,
health checks). If the fingerprint is unchanged, the existing image is reused and the change is a
rollout, not a build.

**Consequences.** Deploy history stores both, so rollback can restore the runtime spec without
rebuilding. Implemented in `internal/deploy`.

---

## ADR-0008 - Railpack as the default builder, Dockerfile wins when present

**Decision.** Detection order: explicit setting > Dockerfile in the repo > Docker Compose > Railpack
auto-detection > prebuilt image. Nixpacks stays selectable.

**Reason.** Railpack is Railway's BuildKit-native successor to Nixpacks (Nixpacks is now
maintenance-only) and produces much smaller images. A repo that ships a Dockerfile has already made a
decision we should not override.

---

## ADR-0009 - react-i18next, with our own completeness check

**Decision.** react-i18next with one JSON file per language, plus `web/scripts/check-i18n.mjs` which
fails the build when any key is missing, extra, or an empty string in any language.

**Reason.** Key-based with a huge ecosystem and correct plural categories through `Intl.PluralRules`
(Russian needs one/few/many). Lingui's advantage is exactly the build-time completeness guarantee,
which is ~120 lines of script to reproduce.

---

## ADR-0010 - Verification strategy in a sandbox that cannot run privileged containers

**Context.** The build environment has Docker but the sandbox policy refuses privileged containers and
host-network relays, so k3d/kind clusters and real VMs cannot be started here.

**Options.** (a) Skip Kubernetes testing, (b) mock everything, (c) test against fakes that implement the
real interfaces, and keep the cluster-dependent smoke tests as runnable scripts.

**Decision.** (c).

* Kubernetes logic is tested against `k8s.io/client-go/kubernetes/fake` and the dynamic fake client,
  so the same code path that talks to a real API server is exercised.
* Manifest generation is covered by golden files.
* The SSH provisioning flow is tested against a real in-process SSH server (`golang.org/x/crypto/ssh`)
  that records the commands it receives, so command construction, idempotency and error handling are
  genuinely verified.
* The four cluster smoke tests live in `test/smoke/` as scripts that require a Docker-capable host.
  `docs/progress.md` records exactly which have been executed and which have not.

**Consequences.** Anything that can only fail against a real kernel (k3s installation itself, WireGuard,
UFW) is verified by script review and by the installer's own preflight, not by execution here. This is
recorded honestly rather than papered over.

---

## ADR-0011 - One binary, many roles

**Decision.** `skifity` is the panel server (`skifity server`), the CLI (`skifity deploy`, ...) and the
MCP server (`skifity mcp`).

**Reason.** One artifact to build, sign, ship and upgrade. The CLI and MCP paths share the API client
and the error-formatting code, so an error looks the same in the panel, the terminal and an AI
assistant.

---

## ADR-0012 - Logical dumps with presigned URLs, not an in-cluster backup agent

**Context.** Backups need to reach S3-compatible storage. CloudNativePG can do
continuous WAL archiving to an object store, but Redis and MySQL cannot, and
putting the storage credentials into every environment's namespace widens the
blast radius of one compromised app.

**Options.** (a) Per-engine native backup with credentials in each namespace,
(b) stream dumps through the panel, (c) a Job per backup that uploads to a
presigned URL the panel generates.

**Decision.** (c). The panel generates a short-lived presigned PUT URL and runs a
Job that pipes `pg_dump` / `mysqldump` / `redis-cli --rdb` through gzip and
`curl -T -` to that URL. Restore is the same in reverse with a presigned GET.

**Reason.** The storage credentials never leave the panel: a namespace only ever
sees a URL that expires. It is one mechanism for all three engines, so restore,
retention and the UI have no per-engine branches. Streaming through the panel
(b) would make the panel a bottleneck and tie a backup's life to the panel's.

**Consequences.** A logical dump is heavier than WAL archiving on a large
PostgreSQL database and gives point-in-time recovery only to the last dump.
CloudNativePG's `barmanObjectStore` remains available for operators who need
continuous archiving, and is documented as the advanced option.
