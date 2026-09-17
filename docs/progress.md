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
| 9 | Differentiating features | done |
| 10 | Hardening and release | done, except a run on real hardware |

## What is done

### Phase 0 — done

* `docs/research/competitors.md` — 13 products compared, weaknesses we design against.
* `docs/research/stack.md` — component versions verified 2026-09-16.
* `docs/architecture.md` — layers, deploy path, add-server state machine, data model, isolation.
* `docs/decisions.md` — ADR-0001 to ADR-0017.

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
* Connecting a Git account and choosing where to be notified, both of which had
  a complete API and no way to reach it from the panel. Connecting a Git account
  shows the webhook address once, with a copy button, because a self-hosted
  Gitea whose token cannot register a webhook needs it pasted in by hand.
* `docs/configuration.md` and `docs/backups.md`.
* The documentation is served by the panel itself, from inside the binary, so
  the roughly twenty links in the error catalogue resolve on a server with no
  outbound network — which is when they matter. A test walks every `WithDocs`
  link and every link between pages, and fails on a missing page or a missing
  anchor. Both failure modes were confirmed by breaking a link on purpose.

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

### After an adversarial audit

The whole product was read back against what it claims, one dimension at a time,
looking for the class of defect a sandbox with no cluster cannot catch: code that
compiles, tests that pass, and a feature that was never once executed. What came
out of it:

* **Notifications were configured and never sent.** `notify.Send` was called from
  exactly one place in the codebase — the "send a test message" button. No
  deployment failure, backup failure, lost server or certificate failure ever
  produced one, although the panel let an operator subscribe to all of them.
  There is a dispatcher now, and the producers call it.
* **Nothing watched the cluster between user actions.** A node that stopped
  answering at three in the morning, an app whose last instance crashed, a
  certificate cert-manager had given up on: all three were visible only to
  somebody already looking at the right page. `internal/watch` compares the
  cluster with the database once a minute. It also fills in two columns the
  panel had written a query for and never called: a server's last seen time, and
  a domain's certificate status, which said "waiting for DNS" forever.
* **No app ever got a URL.** `kube.AutoHostname` was written, tested and called
  by nothing, so the free address the product promises on every page existed
  only as a function. A deploy now gives an app its automatic domain.
* **An app created from the panel had no port**, because the form does not ask,
  and a port of zero renders no Service, no Ingress and no URL.
* **A build could send a team's Git token to any host**, because the clone
  rewrote the URL to include the token whatever host was typed into the form.
* **An API token's team and scopes were stored and never read**, so a token
  issued for one team worked on every team, and a read-only token could do
  anything its owner could.
* **Two-factor recovery codes were generated, returned by the API, and dropped**
  by both sides; the account page never even rendered them.
* **Deleting a volume** authorized the app in the URL and then deleted whatever
  volume id came after it.
* **A backup job could not have run**: rejected by Pod Security for naming no
  seccomp profile and no user, then killed by its own package-install line, then
  refused by S3 for uploading from a pipe with no Content-Length.
* **The builder could never start**: rootless BuildKit needs a seccomp profile
  the panel's namespace refuses, and its readiness probe looked for a socket
  that is not there. Builds moved to their own namespace (ADR-0016).
* **Built images could never be pulled**, because they are tagged with a Service
  name the host's containerd cannot resolve (ADR-0017).
* **Every build was a cold build**: the cache was exported inline and imported
  from a tag nothing ever wrote.
* **Adding a server built a second cluster.** The panel decided a server was
  the first one by looking for a control-plane row in its own database, and a
  panel installed by `install.sh` has none. The first server anyone added was
  given `--cluster-init`, and its token could never have matched the real one
  anyway.
* **A second control-plane node joined with a different network backend** than
  the first, so the two never exchanged a packet.
* **A fork's pull request was handed the project's secrets**, because a preview
  environment copied every variable into a container built from the pull
  request's own code.
* **Scale to zero installed KEDA and changed nothing**: the flag was carried
  into the app spec and read by no manifest.
* **The MCP server's every tool answered "this token is not tied to a team"**
  for a token set up the documented way, and it had no way to create anything.
* **The logs tab could not show why a crash-looping app crashed**, because the
  container that printed it had already been replaced.
* **A superseded deployment kept building** and could roll out an older version
  after the newer one.
* **The event hub never forgot a topic**, so a panel up for a month held the
  build output of every deploy since it started.
* **Half the audit log was invisible**: every panel-wide event — password
  changes, settings, key rotation — was recorded with no team and the list
  filtered on the team alone.
* **Preview environments were never reclaimed** except by a webhook that had to
  arrive.
* **An instance's CPU and memory were always an em-dash**, and the panel's own
  database had no way to be copied that did not silently lose data.

### What was missing rather than broken

Three things the audit named repeatedly as gaps rather than defects, now built:

* **A release command and a one-off command.** There was no way to run anything
  in an app's environment, so a migration had nowhere to go. An app's release
  command runs on every deployment between the build and the rollout; a one-off
  command runs once on request. Both run in the app's own image with its own
  variables, as a Job rather than an exec, because the moment you need this
  most is when the app will not start.
* **Scheduled commands.** Cron existed and ran the panel's own backups; an app
  could not have one. Kubernetes does the scheduling now, so a panel restarting
  at three in the morning is not a reason for a job to be skipped.
* **Honest components.** "Full monitoring" had an Install button that always
  failed. It says how to install it instead.

### A second pass over the cluster, the backend and the frontend

The first audit asked "what does this claim that it never does". This one asked
a narrower question of the same three layers: what do the pieces do to each
other. Everything below was found by reading the rendered objects and the
selectors against one another, and each fix carries a test that was confirmed to
fail against the old behaviour.

* **Every followed log ended after thirty seconds.** `rest.Config`'s Timeout is
  the HTTP client's, so it bounds the response body as well as the request. A
  log stream is a request that succeeds at once and is then read for as long as
  somebody watches it, so one deadline for both kinds of call cut it off. The
  browser reconnected and replayed its tail, so live logs repeated themselves
  every half minute and a build log stopped part way through a build that was
  still running. There are two clients from one connection now.
* **A quiet log stream was dropped by whatever sat in front of it**, because it
  had no heartbeat and could not have written one while the handler was waiting
  on the pod.
* **An autoscaling app that could also sleep had two controllers.** It got an
  HorizontalPodAutoscaler and an HTTPScaledObject, and KEDA creates an
  HorizontalPodAutoscaler of its own for the second. Two of them pointed at one
  Deployment do not divide the work: each overwrites the other's replica count
  on every reconcile. KEDA owns the scaling when scale to zero is on, and the
  scaling tab says so rather than leaving two fields on screen that no longer
  decide anything.
* **The disruption budget deadlocked a node drain.** `minAvailable: 1` permits
  two of three instances to go at once and none at all when an autoscaled app is
  sitting at its minimum of one — so `kubectl drain` waited forever for an
  eviction that could never be allowed. `maxUnavailable: 1` is the promise that
  was meant: one at a time, at every instance count.
* **A migration's pod counted as an instance of the app.** It carried the app's
  own selector labels, so it appeared on the instances tab as though it were
  serving traffic, its CPU was averaged into the autoscaler's decision, and it
  could be picked as the pod to read the app's logs from — which is how somebody
  opens the logs tab during a nightly job and reads the job instead.
* **A deleted app kept running.** Deleting it removed the Deployment, the
  Service, the Ingress and the Secret, and left the scheduled commands: the
  nightly job went on firing every night forever against an image nothing would
  pull. The wake Service and the HTTPScaledObject were left too.
* **A scheduled command's history was empty by morning**, because it inherited
  the one-off's hour-long TTL, and the morning is exactly when somebody looks for
  the run that failed.
* **The nixpacks builder could never have built anything.** Its buildctl line
  read `.nixpacks/Dockerfile` and no step ever wrote one. It is reachable from
  the API and the CLI; the panel's own form only offers auto and Dockerfile,
  which is why nobody had hit it.
* **An app and a database could take each other's address.** They are unique
  among themselves, share a namespace, and both render a Service under their
  slug — so a Redis called "web" next to an app called "web" took the app's
  Service over while its Ingress went on pointing at the name. Both creation
  paths now refuse the collision and say what holds it.
* **A render that threw blanked the whole panel.** React unmounts the tree when
  nothing catches it, and what is left is a white page and a console message. On
  a self-hosted panel that is the worst failure there is: the cluster is fine,
  the apps are serving, and the operator cannot tell. There are two error
  boundaries now, and the one inside the shell leaves the navigation working so
  the person can simply go somewhere else.

**What this pass did not find:** the store, the authorization layer and the
event hub came out clean. Every team-scoped handler goes through `authorizeTeam`
or `authorizeApp`, every write is serialised behind one mutex and every raw
statement runs inside `db.Tx`, and the SSE handler already had its heartbeat,
its replay and `X-Accel-Buffering`.

### The roadmap, worked through

`docs/roadmap.md` set out what was left, largest first: the things that decide
whether a panel survives its second month, then the questions people actually
ask it, then two features that were gaps rather than defects. All of it is
built.

* **The registry collects its garbage.** Every build pushed an image and nothing
  ever removed one — the first open issue on this page since the day it was
  written, and the one that ends with a full disk and every pod on the node
  stopping at once. The panel untags what nothing can reach and a Job runs the
  registry's own collector beside the files. Builds are held while it runs,
  because a push concurrent with a collection is the one case the registry's
  documentation says corrupts an image, and the panel is the only thing that
  starts builds.
* **The panel's own database stops growing.** Deployment records, audit entries
  and finished operations were written and never removed. A daily pass, with the
  window in settings; the activity log keeps a year by default, because a log
  that forgets is most of the way to not having one.
* **A pending instance says why.** It used to say "waiting for a server with
  enough free CPU and memory" whatever the reason, which is right about a third
  of the time and otherwise sends somebody to add a server for a quota, a
  volume, or a control-plane node that does not take apps. Kubernetes writes the
  answer onto the pod; the panel now reads it. A quota refusal never reaches a
  pod at all, so that one is read off the Deployment.
* **An environment's limits are visible before they are hit**, once something is
  above sixty per cent.
* **The panel can be monitored.** `GET /api/metrics`, in the Prometheus format,
  behind the same authentication as everything else, written by hand rather than
  pulled in.
* **A repository is read before it is built.** `builder.Detect` had been written,
  tested and called by nothing. The panel reads the tree through the provider's
  API — two requests, no clone — and says what it found while the form is still
  open, as a guess presented as a guess.
* **A volume can be backed up**, mounted read-only, as the app's own user, on
  the node that holds it.
* **The frontend is split by what changes**, and the labels only a screen reader
  hears are translated — "Close", "Loading", "Toggle Sidebar" were all hardcoded
  English in a panel shipping five languages, and the i18n check now fails the
  build on the next one.

### A second pass over tenant isolation

The roadmap put this next because it is the one class of bug where being wrong
is not recoverable. Every route was traced to its authorization helper, every
second id in a URL checked against the first, and the CLI, the MCP server and
the Git webhook followed back to the same checks — the first two go through the
HTTP API with the caller's own token, so there is no second surface to get
wrong, and the webhook skips any app whose team is not the connection's.

Three things came out of it.

* **The panel could be pointed at the cloud metadata service.** A Git
  connection's base URL and a notification channel's webhook are both settings
  holding an address the panel's own process requests. The webhook path echoed
  part of the response back in its error, which turns a test message into a
  read. `internal/netguard` refuses link-local, loopback, the unspecified
  address and multicast at connection time, on the resolved address rather than
  the hostname. Private ranges stay allowed: a self-hosted Gitea on 10.0.0.5 is
  the ordinary case here, not the attack.
* **Unlinking a database authorized the database and not the app.** Linking
  checks both and says so in a comment; unlinking checked one. It removed no
  variable it did not own, and did re-apply that app's configuration to the
  cluster.
* **The api package had no tests.** "Authorization lives in one place" was true
  and unenforced. Six now run against the real router with a real database and
  keyring: every shape of id one team can ask another for, the token's team
  binding, read-only scopes, what is open without credentials, what is
  administrator-only.

What did not turn anything up: the authorize helpers themselves, the SSE topic
authorization, the encryption contexts, secret disclosure through the variables
and credentials endpoints, and the webhook's team scoping.

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
* There is no shell into a running instance, and this is a decision rather than
  a gap. A one-off command runs in the app's own image with the app's own
  variables, can be watched, leaves a log, and works when the app will not
  start — which is when people reach for a shell. See `docs/roadmap.md`.
* A volume backup is a tar taken while the app runs, not a snapshot, so a file
  being written at that moment can be caught half-written. `docs/backups.md`
  says so and says what to do instead.
* The nixpacks builder is written and unit-tested and has never been run: like
  everything else that needs a cluster, it is checked against the rendered Job
  and not against a build. Railpack is the default and the one the product is
  designed around.
* The frontend is 509 kB of the panel's own code, 145 kB compressed, beside
  vendor chunks a browser keeps across upgrades. Served from the binary on the
  same host, so it is not the problem it would be over a CDN.

## Idle resource usage

`docs/performance.md`. The panel is measured: 34 MiB resident idle, 38 MiB after
400 requests, 39 MiB of binary. k3s's own footprint is not measured here, for
the same reason as everything else that needs a cluster.
