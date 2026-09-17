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

## ADR-0002a - One place where user input becomes a shell command

**Context.** On 8 January 2026 Coolify disclosed eleven critical CVEs in one day, five at CVSS 10.0,
all authenticated command injection ending in root on the host; more followed through spring,
including an authorization bypass across teams. The published analysis names the cause as a class
rather than a mistake: user input reaching a shell without sanitisation, in many independent code
paths. Skifity has the same job description — turn a Git URL, a compose file, a database name into
privileged operations on somebody's servers — so it has the same attack surface.

**Decision.** Exactly one function turns a value into a shell word — `Quote`, in `internal/shellsafe` — it is
tested by running a real `sh` against injection payloads rather than by asserting on the string, and
no generated script interpolates a value any other way. Secrets are sealed to the context they are
stored in, so a row copied elsewhere does not open. Authorization is resolved per handler through
`authorizeTeam` and friends, so a handler that reaches the store without one is visible when read.

**Consequences.** This does not make Skifity safe; it makes one class of bug have one place to be. The
same bug was in this repository in September 2026 — every generated script used Go's `%q`, which
reads as shell quoting and is not — and it was one change to fix because there was one place. Claiming
a security advantage over a product that has actually been attacked would be unearned: Coolify has
eleven CVEs because 52,890 people run it and researchers look, and Skifity has none because nobody
has. See `docs/research/competitors.md`.

---

## ADR-0003 - `wireguard-native` flannel backend

**Context.** Users combine cheap VPSes from different providers, so pod traffic crosses the public
internet unencrypted with the default vxlan backend.

**Decision.** `--flannel-backend=wireguard-native`, chosen once for the whole cluster and stored in
`cluster.flannel_backend`. The installer picks it on the first node, falling back to `vxlan` with a
visible warning when the kernel has no `wireguard` module, and passes its choice to the panel
(`SKIFITY_POD_NETWORK`), which records it. Every server added afterwards is installed with the
recorded backend, and preflight refuses a server whose kernel cannot run it.

**Consequences.** Some minimal kernels lack the module. That is a choice made once, on the first
node, where there is nothing yet to disagree with — not a per-server fallback: nodes on different
backends join without any error and then never exchange a packet, so a server that cannot match the
cluster is refused with the two fixes that work (install `wireguard-tools`, or move the whole cluster
to vxlan). Changing the setting on a running cluster only affects servers added after it, which the
setting's help text says.

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

Compose is in that order as a reader, not a builder. A Compose file describes several services and an
app runs one, so finding one means reading it and offering its services to choose from, with what did
not carry over named against the service it came from. There is no `compose` build, and `compose` is
not a source an app can have: the choice produces an ordinary Git or image app. For a while it was
one — the API accepted it, stored it, and then deployed the app as a Git app with no repository.

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

**Correction, 2026-09-17.** That context was checked rather than assumed for the first time, and it is
not what it says. The container runs as root with nearly every capability, `docker` and `k3d` are both
installed, and `/dev/kmsg` and `/dev/net/tun` are present — the pieces k3d needs. What actually stops a
cluster being started is that the session's permission layer refuses to start a Docker daemon, which is
a different fact with a different remedy: it needs a person to allow it, not a different sandbox. The
decision below is unchanged, because no cluster has been run either way. What changes is that "it is
impossible here" was not true, and it had been written down for months.

**Options.** (a) Skip Kubernetes testing, (b) mock everything, (c) test against fakes that implement the
real interfaces, and keep the cluster-dependent smoke tests as runnable scripts.

**Decision.** (c).

* Kubernetes logic is tested against `k8s.io/client-go/kubernetes/fake` and the dynamic fake client,
  so the same code path that talks to a real API server is exercised.
* Manifest generation is covered by golden files.
* The SSH provisioning flow is tested against a real in-process SSH server (`golang.org/x/crypto/ssh`)
  that records the commands it receives, so command construction, idempotency and error handling are
  genuinely verified.
* `test/smoke/` holds two scripts, `panel.sh` and `installer.sh`. Neither touches a cluster: one runs
  the real binary through first-run setup, sign-in, tokens and the CLI, the other checks the
  installer's logic without installing anything.

  This bullet used to say four cluster smoke tests lived there and that `docs/progress.md` recorded
  which had been run. Neither was true — they were never written — and the claim survived because
  nothing checks that a document's description of the repository matches the repository. Something
  does now. The gap itself stays: writing scripts that could not be run here would have widened it.

**Consequences.** Anything that can only fail against a real kernel (k3s installation itself, WireGuard,
UFW) is verified by script review and by the installer's own preflight, not by execution here.

And more than that: no part of this product has ever spoken to a Kubernetes API server. The fakes
exercise the same code path, which is worth something and is not the same thing. Deploying an app,
provisioning a database, taking a backup and restoring one are each written, unit-tested against the
objects they render, and unrun. `docs/progress.md` says so plainly, which is the whole of what this
ADR can offer.

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

---

## ADR-0013 - The panel runs in the cluster, pinned to the first control plane node

**Context.** The panel has to survive a reboot, be upgradeable, and hold a SQLite
database and a master key that only one process may write at a time. It also has to
be usable *while* the cluster is unhealthy, because that is when someone opens it.

**Options.** (a) A systemd service on the host, (b) a Deployment with a
PersistentVolumeClaim, (c) a Deployment pinned to one node with host paths.

**Decision.** (c). One replica, `Recreate` strategy, `nodeSelector` on the first
control plane node's hostname, `/etc/skifity` and `/var/lib/skifity` mounted as host
paths, and tolerations for the control plane taints.

**Reason.** A systemd service (a) cannot be upgraded by the panel itself and needs a
second delivery mechanism for the binary. A PVC (b) makes the panel depend on the
storage provisioner starting first, and a broken provisioner is precisely the thing
an operator opens the panel to fix. Host paths have no such dependency. `Recreate`
and one replica are not a scaling compromise: two panels writing one SQLite file is
corruption, so the rollout must stop the old one before starting the new one.

**Consequences.** The panel is tied to one node. If that node is lost, the panel is
restored by pointing a new install at a restored `/etc/skifity` and `/var/lib/skifity`.
The panel's own availability is therefore lower than the apps it manages, which is
the right way round: apps survive a node failure, the control panel is rebuilt.
`internal/manifests` tests assert the pinning, the strategy and the hardening, so a
refactor cannot quietly undo any of it.

---

## ADR-0014 - The panel is granted cluster-admin, and says so

**Context.** The panel creates a namespace per environment and installs cluster
components on demand: cert-manager, CloudNativePG, KEDA and Longhorn each bring
CustomResourceDefinitions and their own ClusterRoles.

**Decision.** The panel's ServiceAccount is bound to `cluster-admin`, and the
manifest carries a comment explaining why rather than leaving it to be discovered.

**Reason.** A subject that may create ClusterRoles can grant itself anything, so a
role listing every verb the panel needs today would be cluster-admin with extra
steps, and would break the first time a component's installer changed. Pretending
otherwise would be security theatre.

**Consequences.** The isolation that matters is between tenants, not between the
panel and the cluster, and it is enforced on the namespaces the panel creates:
restricted Pod Security, a ResourceQuota that refuses LoadBalancer and NodePort
Services, and default-deny NetworkPolicies. Anyone who reaches the panel has the
cluster, which is why the panel has login rate limiting, TOTP, and an audit log.

---

## ADR-0015 - No certificate for the sslip.io address

**Context.** A server with no domain still needs a URL. sslip.io resolves any IP
embedded in the hostname, so `203-0-113-10.sslip.io` works with no DNS setup.

**Decision.** The installer serves the panel over plain HTTP on the sslip.io
address, and installs cert-manager and requests a certificate only once a domain of
the operator's own is configured.

**Reason.** Let's Encrypt rate limits are per registered domain, and every Skifity
install in the world would share sslip.io's. Certificates would start failing for
everyone as soon as the product had any users, and the failure would look like a
Skifity bug. Skipping cert-manager until it is useful also keeps a fresh install
smaller, which is the same reason every other component installs on first use.

**Consequences.** The first sign-in is over HTTP, and the panel says so rather than
hiding it. Adding a domain in Settings installs cert-manager and switches the
Ingress, which is one step and needs no reinstall. An operator who wants HTTPS from
the first minute passes `SKIFITY_DOMAIN` to the installer.

---

## ADR-0016 - Builds run in their own namespace, at the privileged profile

**Context.** Builds originally ran in the panel's own namespace, alongside the
builder and the image registry. That namespace also holds the master key, the
panel's database and a service account that can reach the whole cluster. It
enforces the baseline Pod Security profile, which refuses an unconfined seccomp
profile — and rootless BuildKit needs exactly that to use user namespaces, so the
builder was rejected by admission and never started.

**Decision.** A separate `skifity-builds` namespace holds the builder, the
registry and every build Job. It is the only namespace in Skifity that enforces
the privileged Pod Security profile, and it is audited and warned at restricted so
anything else placed there is noticed. It carries the same default-deny
NetworkPolicy every environment gets: out to the internet for the repository and
the packages, and nothing towards the node network or the cloud metadata service.

**Reason.** A build runs code out of somebody's repository. It is not hostile by
assumption, but it is not the panel either, and the two should not share a
namespace, a service account or a set of Secrets. Relaxing the panel namespace's
profile so one builder could start would have been exactly backwards. The
alternative to rootless BuildKit is a privileged container holding the host's
container runtime socket, which is worse in every way: this is one namespace, with
one builder in it, and that builder still runs as an unprivileged user.

**Consequences.** The namespace's name is fixed rather than derived from the
panel's, because every node's container runtime is configured to reach the
registry inside it and that configuration is written before the panel exists. A
cluster runs one Skifity panel, which was already true of the registry.

---

## ADR-0017 - Every node mirrors the registry to a NodePort on loopback

**Context.** Built images are pushed to an in-cluster registry and tagged with its
Kubernetes Service name. The thing that pulls them is containerd, which runs on
the host: it cannot resolve a Service name, and it refuses plain HTTP to anything
that is not loopback. Every image the panel built was unpullable, and the symptom
was an ImagePullBackOff with nothing pointing at the cause.

**Decision.** The registry is exposed on a fixed NodePort, and every node gets an
`/etc/rancher/k3s/registries.yaml` mirroring the registry's Service name to
`http://127.0.0.1:<nodeport>`. The installer writes it on the first server and the
provisioner writes it on every server added later, before k3s starts, from the
same Go function.

**Reason.** A NodePort is reachable on `127.0.0.1` from every node, which is both
resolvable without cluster DNS and, to containerd, trusted without TLS. The
alternative — a real certificate for an internal name, or a mirror pointing at a
ClusterIP — needs an address that is only known once the cluster is running, which
is after the point the file has to exist.

**Consequences.** The registry's address, port and node port are three constants
that a shell script and a Go package have to agree on without ever talking to each
other, so a test fails if they drift. Changing them is a change on every node,
which means an installer re-run; the installer detects that the file changed and
restarts k3s itself.

## ADR-0018 - A plugin is a container and a manifest, not a library

**Context.** Skifity needs an ecosystem: other people writing features for it,
including commercial ones, publishing them and having operators install them
from a store. That means Skifity needs a plugin *standard* before it needs a
plugin *store*, because the standard is what everybody else builds against and
the one thing that cannot be changed casually afterwards.

The obvious model is WordPress: drop a file in a directory and it is loaded into
the running process. It works because PHP is interpreted. Skifity is one static
Go binary with `CGO_ENABLED=0` — ADR-0011 — and the two ways to load Go code
into it both fail:

* **`plugin.Open`** requires CGO, works only on Linux, FreeBSD and macOS, and
  its own documentation says "runtime crashes are likely to occur unless all
  parts of the program (the application and all its plugins) are compiled using
  exactly the same version of the toolchain, the same build tags, and the same
  values of certain flags and environment variables", plus identical source for
  every shared dependency. A plugin author cannot meet that, and the failure is
  a crash rather than a refusal.
* **Recompiling the panel**, which is how Caddy's `xcaddy` and Traefik's static
  plugins work, means every operator needs a Go toolchain and a build step. That
  is the opposite of one static file that runs anywhere.

An in-process interpreter was considered and rejected. Yaegi, which Traefik uses
for its dynamic plugins, and WASM through wazero, which is pure Go and needs no
CGO, are both technically available. Both add a whole runtime and a new failure
surface to cover cases the container model already covers — with worse isolation
and a narrower choice of language. WASM keeps one clear future use: a per-request
filter in the request firewall, where an HTTP round trip per request would be too
slow to consider.

**Decision.** A plugin is an OCI image and a manifest. The panel runs the image
as a Deployment in a namespace of its own, gives it an API token carrying exactly
the permissions its manifest declared and an administrator granted, and posts the
events it subscribed to. Dokku's plugin model, translated to Kubernetes.

The standard is `internal/plugins`, not prose: a manifest that parses and
validates there is a valid plugin, and the example in `docs/plugins.md` is a
test. A standard written only in prose is a standard every implementation reads
differently.

Three decisions inside it are worth recording because they are the ones somebody
will otherwise try to relax:

* **The image is a digest, never a tag.** A tag can be moved by whoever controls
  the registry, and this image is about to be handed an API token: "the plugin
  you approved" has to mean the bytes you approved.
* **`read` and `write` on their own are refused for a plugin.** They mean every
  resource, which is not something anybody can meaningfully agree to on a
  screen. This is why API token scopes were narrowed to `resource:action` first:
  without that, "this plugin may only read your apps" would have been a sentence
  on a screen that nothing anywhere enforced.
* **Only an event that happens before something may block.** Refusing a deploy
  is a decision; refusing to acknowledge a backup that already finished is a
  promise with nothing behind it. A blocking hook is capped at ten seconds,
  because the thing on the other end is a person watching a page.

**Consequences.** A plugin can be written in any language. It cannot take the
panel down when it crashes, cannot read the master key, and cannot reach a
resource its manifest did not name. It costs a pod, which is the price of that
isolation and is stated before installation. Skifity does not process payments:
a commercial plugin sells and validates its own licence through its own server,
which keeps the panel out of being a payment processor.

What this cannot do, and should not be made to do: change the panel's own pages.
Third-party JavaScript in the panel's origin can read the session of somebody who
holds the master key. If in-panel pages are ever offered, they will be declarative
— a plugin describing a table or a form that the panel renders — and not a bundle
the panel executes.
