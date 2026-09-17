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
* `docs/research/stack.md` — component versions verified 2026-09-16, reconciled
  with the code 2026-09-17.
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

### A pass over the code that runs as root on somebody's server

The provisioning path had never been read this session and is where a defect
does the most damage: it runs shell as root on a machine the user owns, and it
can delete the cluster. Two things came out of it, and the second explains why
the first had survived.

* **`%q` is not shell quoting, and every generated script used it.** Go's `%q`
  produces a Go double-quoted literal; a shell reading a double-quoted string
  still expands `$` and a backtick, and `%q` escapes neither. So
  `fmt.Sprintf("TARGET=%q", "1.2.3.4$(id)")` produces `TARGET="1.2.3.4$(id)"`
  and the shell runs `id`. The code reads as though it is defended and is not.
  Every script this panel generates used it — the ones that provision a server
  over SSH and the ones that build an image. `internal/shellsafe` quotes with
  single quotes, the only quoting a POSIX shell does not look inside, and a test
  puts both forms through a real `sh` and shows the difference rather than
  asserting it. The one reachable path was the SSH account name, which was never
  validated; it is now checked against the shape a distribution produces.
* **The guard against destroying the cluster counted the wrong thing.** Removing
  a control plane node is refused when too few would be left, and it counted
  rows in one team's table rather than the nodes that actually run the cluster.
  A panel installed by `install.sh` has no row for the node it runs on, and a
  panel with more than one team splits the rest between them, so the number was
  low by at least one and scoped to the wrong thing. It refused removals from a
  healthy four-node control plane, and at zero it permitted the one removal that
  deletes Kubernetes, every app, and the panel answering the request.

**And the pattern underneath both.** Three handlers checked "is this feature
configured" before the check that mattered — before validating the request,
before authorizing the second resource, before the quorum guard. Beyond the
small leak of telling somebody who may not touch an app whether databases are
configured, it means a safety check only runs on a configured panel, so nothing
can test it without a cluster. That is why this class kept surviving review. The
checks run first now, and the tests for them run against a panel with no cluster
at all.

## Phase 19 — the research, read back against the code

The three research pages were written before the code and never checked against
it afterwards. Reading them back found two promises the product did not keep and
a page that had drifted.

* **The WireGuard fallback did not exist.** The preflight told an operator that
  a kernel without the module was fine and that "traffic between your servers
  will use vxlan". Nothing did that: the backend was hardcoded to
  `wireguard-native` in the panel's install scripts and in `install.sh`, so the
  server joined a cluster it could not exchange a packet with, and neither k3s
  nor the panel reported anything wrong. The pod network is now one choice for
  the whole cluster, stored in `cluster.flannel_backend`. The installer picks it
  on the first node — where the fallback is real, because there is nothing yet
  to disagree with — and hands its choice to the panel, which records it and
  installs every later server the same way. A server whose kernel cannot run the
  cluster's backend is refused, with the two fixes that work.
* **Compose was announced, not implemented.** Detection reported "Docker
  Compose" and said services become separate apps and their links become
  variables. `ConvertCompose` was written, tested, and called by nothing; the
  Compose file's contents were never fetched; and `compose` was a source an app
  could be created with, which the API accepted, stored, and then deployed as a
  Git app with no repository. The reader is wired up now: the file is fetched
  and parsed, the services are listed with what did not carry over named against
  the service it came from, and picking one fills the form in — name, root
  directory, port, image or build, and the service's variables, which the create
  endpoint now seals before the first deploy. Skifity still runs one service per
  app, and the panel says so rather than implying an import.
* **A Dockerfile's `EXPOSE` line was never read.** Same cause: the file was
  found in the tree and then read back as an empty string, because it was not in
  the list of files fetched. The port detection it fed had never once produced
  an answer.
* **`docs/research/stack.md` had drifted** — `--write-kubeconfig-mode=0644`
  against 0600 in the code, a node label the code never sets, `--secrets-encryption`
  missing, self-hosted fonts the frontend deliberately does not use, and a link
  to a page that does not exist. It is reconciled, and a test now reads the
  flags out of the page and fails when no generated script passes one.

## Phase 20 — the tooling, and the catalogue

* **`make check` could not be run.** The README calls it "what CI runs" and it
  failed twice on tooling rather than code: golangci-lint refuses a module whose
  `go` directive is newer than the Go it was built with, and took the whole
  suite down with it, so nothing here had ever been linted; and
  `npm --prefix web exec -- tsc -b` keeps the directory it was called from, so
  tsc looked for a tsconfig.json at the repository root. `scripts/lint-go.sh`
  now builds the linter with this module's own toolchain, which cannot be out of
  step with it, and CI runs the same script with `LINT_STRICT=1` so it can never
  skip there. With the linter finally running it found eight things, all fixed,
  including two doc comments left behind by a rename.
* **The template catalogue had no tests, and a wrong template is silent.** A
  database whose `link_to` names no service was created and then never linked,
  so the app came up without the one variable it cannot run without and
  crash-looped with nothing on screen saying why. That now fails the install.
  The catalogue is checked for it, and for engines Skifity does not provision,
  variable names a container could not carry, services that ask for more memory
  than they are allowed, and templates nothing can open.
* **Four templates ran `latest`.** n8n, Vaultwarden, Umami and MinIO. A floating
  tag is not a version: two deploys of the same app run different software, a
  rollback restores a tag rather than the thing that worked, and an upstream
  release arrives on a restart nobody asked for — in a product whose whole point
  is that a rollback works. All four are pinned, the card shows the version, and
  a test refuses anything ending in `latest`.
* **`Icon` was a field the UI never read.** It is gone, and the card shows what
  the template installs instead.
* **Templates had no documentation page.** `docs/templates.md`, served by the
  panel like the rest.
* **The configuration file had three ways to be wrong quietly.** The struct's
  toml tag said `kubeconfig_path` and the parser looked for `kubeconfig`, so a
  file written from the struct was read, accepted and ignored. A `#` anywhere in
  a value cut the value short, whether or not it was inside quotes, so a path
  with a hash in it became a shorter path and what failed was whatever used it.
  A quote that was never closed was accepted, taking the comment somebody
  thought they were writing along with it. `internal/config` had no tests at
  all; it has them now, and one of them derives the list of settings from the
  struct and fails when any of them is missing from `docs/configuration.md` —
  which two of them already were.

### The claims, read back one by one

Every checkable claim in `README.md`, `llms.txt` and the documentation was put
against the code. Most held — the seven provisioning steps, the password that is
never stored and the test that scans every column for it, all fourteen MCP
tools, all thirty-two documented API routes, the three scaling risks named by
name, the copy-for-AI button, the placeholder check in the i18n script. These
did not:

* **"It does not contact any server but yours"**, and in the FAQ, "it does not
  contact any server at all". Skifity reaches Let's Encrypt, GitHub for the
  component manifests, the user's Git provider and S3 bucket — and the preflight
  asks a public-IP service for the address of a server being added, which is the
  one call a privacy-minded reader would actually want named. "It never phones
  home" is true and stays; the absolute around it is gone.
* **"Every command takes `--json`."** `open`, `logout`, `admin reset-password`
  and `admin backup-db` did not. That claim is aimed at assistants, who read it
  literally and get "flag provided but not defined". All four have it now, and a
  test reads the package and fails when a command is added without it.
* **The measured figures had drifted.** Idle memory is 35 MiB, not 34. The
  frontend is 314 KiB gzipped across nine files, not 278 across five — the chunk
  split in Phase 15 changed both numbers and neither was re-measured.
* **"Any server can be promoted."** True of the code, and that was the bug — see
  below.
* **Every published install path points at nothing.** See the open issues.

The API route table is now checked against the router itself: llms.txt is what
an assistant reads before calling anything, and a route that moved does not read
as a documentation mistake, it reads as the product being broken.

### Promotion wiped the node before checking it

The panel refuses to add a control plane server below 2 GB and 20 GB. It would
promote one without looking. Promotion drains the node, removes it from the
cluster and uninstalls Kubernetes before reinstalling it as a control plane
member, so the first thing to notice a machine too small for etcd was the
install at the very end — with the apps already moved off and a working worker
turned into a machine with nothing on it. The check runs first now, before
anything is touched.

### One panic used to end the panel

The HTTP handlers have recovered from a panic since the beginning. Nothing else
did. Every deployment, every server being added, every database, backup,
restore, health pass, scheduler tick and notification runs in a goroutine of its
own, and a panic in any of them took the process down — on a self-hosted install
that is the panel you would use to find out why, so the deployment that crashed
it also removed the way to diagnose it.

`internal/runsafe` recovers, logs the stack, and hands the failure to whatever
was running so it is marked failed rather than left at "running" forever. It is
wired into the two choke points every deployment and every provisioning
operation already pass through, into the backup, restore, volume-backup and
database goroutines beside the `fail` each of them already had, and into the
dispatcher. The three long-running loops recover per iteration rather than
around the loop: a panel that is alive with no scheduler is worse than one that
restarted, because nothing says so and the backups simply stop.

The test for it does not report a failure when it regresses. It takes the test
binary down, which is the point.

### The guards that stopped guarding

Four safety checks were written as `if err == nil && <the dangerous
condition>`, which reads as caution and means the opposite: the moment the query
behind the check fails, the check disappears and the destructive path runs. This
is the same shape as the capability-before-check pattern from Phase 18 — a
safety check that only runs when everything else is already well — and one of
these four was that pattern as well.

* **Deleting a database** skipped the "apps still use this" check when the links
  could not be read, and ran that check after the cluster capability check, so
  nothing could test it without a cluster. Both fixed; a test drops the
  `database_links` table and asks for the delete.
* **Restoring a backup** over a live database did the same thing with the same
  query. A failure to read the links is not an empty list of them.
* **Demoting the last owner** skipped the last-owner check when the membership
  or the owner count could not be read, leaving a team nobody can administer.
  Not being a member yet is the ordinary case and still passes; anything else
  now refuses. A test drops the `memberships` table.
* **Removing an owner** allowed it when the actor's own role could not be read.
  Nothing can reach that branch — `authorizeTeam` refuses a caller with no
  readable membership first — so there is no test for it, only the change. The
  ordinary guard, that an admin may not remove an owner, had no test either and
  has one now.

Two more in the same handler. Deleting a database asked which team it belonged
to *after* the row was gone, so the deletion was recorded with no team — filed
with the panel-wide events, which belong to no team by design, and shown in the
audit log of every team that person is in rather than in the one whose database
it was. And `dbsvc.Delete` removed the apps' database variables inside an
`if err == nil`, so a query failure left apps holding a connection string to
something that no longer exists, silently; it now says so.

`internal/errdoc` also had no tests. The catalogue is the product's central
promise — cause, impact and fix on every failure — and an entry missing one of
those still compiles and still renders. A test now reads the file as source and
checks every entry for all three, for a code shaped like a code, and for codes
that do not collide, since the UI picks a translation by code.

## Phase 21 — the competitors, researched properly

`docs/research/competitors.md` was written before the code and never checked
against the market again. Re-researched against live 2026 sources, it changed
the conclusions rather than confirming them.

* **Coolify disclosed eleven critical CVEs on 8 January 2026, five at CVSS
  10.0**, all authenticated command injection ending in root, plus a readable
  root SSH key and later a cross-team authorization bypass. Censys counted
  52,890 publicly reachable dashboards. The published cause is a class, not a
  mistake: user input reaching a shell in many independent code paths — which is
  exactly what `internal/shellsafe` exists for and exactly the bug found in this
  repository in September. ADR-0002a now records why there is one place for it.
* **Railpack is no longer a differentiator.** Dokploy ships it already, along
  with SSO/SAML that Skifity does not have.
* **aaPanel is not a competitor.** It replaces cPanel — websites, PHP, mail, a
  WordPress toolkit — and is sold to agencies running hundreds of client
  servers. It was researched and removed from the comparison rather than left in
  as a name.
* **Kubernetes is a category mismatch as much as an advantage.** Comparison
  sites exclude k3s from "self-hosted PaaS" as a different abstraction layer
  entirely, and one of the most-read 2026 guides is titled "Best Self-Hosted
  PaaS to Replace Heroku (No Kubernetes)". The audience is selecting away from
  what this is built on, which makes hiding it well the whole bet rather than a
  nice touch.
* **The footprint gap is the clearest measurable win.** 35 MiB against Coolify's
  500 MB–1.2 GB and Dokploy's ~350 MB.
* **Eight templates against Coolify's 280+** is the single most-cited reason
  people choose Coolify, and it is a content problem rather than an engineering
  one.

## Phase 22 — the catalogue, closed

Eight templates against Coolify's 342 was the largest gap we had, and the
research said it was a content problem rather than an engineering one. It was
both: a Go literal is fine for eight and impossible for three hundred, which is
why Coolify's catalogue is files and grew by contribution.

* **The catalogue is data now.** `internal/templates/catalogue`, one YAML file
  per template, embedded at build time. Adding one touches no Go, and the
  directory's README has the format and the two rules that are not obvious.
* **212 templates**, up from eight. 204 were converted from Coolify's catalogue
  by `hack/import_templates.py`, which is kept so the next batch is a re-run.
* **Every image was resolved to a version and verified.** `hack/resolve_tags.py`
  asks each registry for the tags, prefers a series tag that takes patches over
  an exact pin, and then fetches the manifest to prove the tag exists. 225 of
  269 single-service templates resolved; the 44 that did not were dropped rather
  than shipped on `latest`. More than half of Coolify's own entries ship
  `latest`.
* **The database wiring was taken out, and that was a real bug.** A Compose file
  points an app at a sibling container — `DB_HOST=mariadb` — and here the
  database is a managed one that arrives as a connection string. The first
  import carried those over, which would have produced apps that start, fail to
  resolve a hostname nobody recognises, and crash-loop. Bookstack, GLPI,
  Metabase, Redmine and Keycloak were all affected. A test now refuses any
  variable that names a datastore and ends in an address.
* **Nothing was shipped half-filled.** Templates with several application
  services (72), no port (15), or no verifiable image (46) were dropped.
* **The panel did not get heavier**: 34 MiB idle and 39.4 MiB of binary, against
  35 MiB and 39.3 MiB before.

What this does not mean: none of these has been deployed, because nothing in
this product has. What is checked is that each template is structurally sound
and that its image exists.

## Phase 23 — CI had been red on every commit

Twenty-three runs, all of them red, back to the first. Nobody looked, including
whoever wrote `make check`.

The failure was one step: `govulncheck`. `go.mod` said `go 1.26.0`, CI's
setup-go installs exactly what `go.mod` asks for, and Go 1.26.0 shipped with 21
known standard-library vulnerabilities that 1.26.1 fixed — one of them reachable
from `internal/notify`, which dials TLS to send mail. Locally the same command
passed, because `go run …@latest` quietly switches to a newer toolchain and the
scan then reports the newer standard library. The local answer and the CI answer
were about two different Go versions.

Two things were wrong, and the second is worse:

* **The `go` directive named a version with known holes.** It now names a patch
  release, so anyone building this gets a fixed standard library rather than
  whichever one they happen to have.
* **`make check` did not run the step that was failing.** It is documented as
  "what CI runs" and it was not: no `audit`. A gate with a hole in it looks
  exactly like a gate. `check` now includes it.

And the consequence nobody would have guessed from the summary line: because
`govulncheck` runs before them, **the race-detector run, both smoke tests and
the Playwright interface test had never executed on CI**. With the scan fixed
they ran, and all of them passed — except one step that had never run anywhere,
on any machine, and was broken in two ways.

### `make image` had never worked

Building the image needs a Docker daemon, and nothing here has one, so the whole
target was unverified for as long as it existed. The first CI run that ever
reached it failed twice over:

* The frontend stage builds from `web/` alone, and the frontend build copies
  `llms.txt` in from the repository root so the panel can serve it. The
  Dockerfile never copied that file, so the build stopped on a missing file that
  is right there in the repository.
* `.dockerignore` excluded `docs`, which is a Go package — `docs/embed.go`
  embeds the pages into the binary — so even past the first failure the backend
  stage would have failed on a missing import.

Both are the same shape as everything else in this log: a path nothing executes,
so nothing contradicts it. `internal/buildctx` now holds two tests that read the
Dockerfile and the ignore file rather than running Docker: every `//go:embed`
path must survive the build context, and anything the frontend build reads from
outside `web/` must be copied in.

This is also the path the quick start tells somebody to use, since nothing is
published — clone, `make image`, run the installer from inside the clone. It
would not have worked for them either.

## Phase 24 — the multi-service templates, and what they were worth

Seventy-two templates in Coolify's catalogue install more than one application
service. Installing several apps from one template was never the problem —
`installTemplate` already creates an app per service — so this was a converter
problem, and the converter kept being confidently wrong.

The first attempt produced 21 templates. They looked fine and were not: a
Sidekiq worker marked public on the web app's port, Elasticsearch given Kibana's
port, n8n given Postgres's, HeyForm given Redis's. Every heuristic fix revealed
another wrong guess, because a Compose file encodes a topology and the converter
was inferring one.

So it stopped inferring. A service's port now comes from the source saying so —
its own `SERVICE_FQDN` marker, or `expose`, or `ports` — or from a short table of
ports that are documented facts about an image, and that table only answers when
the image appears once in the stack (seaweedfs runs a master and an admin from
one image, and 8333 was wrong for both). Anything else is dropped as ambiguous.

That leaves **seven**, and all seven are right. It also produced three real
changes to the product rather than the converter:

* **`link_to` is a list.** A web app and its worker share one database, and
  linking only the first left the worker starting without the variable it cannot
  run without — the same crash loop as a link that names nothing.
* **A worker is `port: 0`**, which the manifest builder already handled: no
  Service, no probes, no ingress. The catalogue tests now allow it, and refuse a
  service that is public or has a health path with no port to check it on.
* **A shared volume does not carry over**, and that is said rather than
  discovered. A Compose volume is shared between services; a Skifity volume
  belongs to one app and is read-write-once, so Chatwoot's web app and its
  Sidekiq get two different `/app/storage` directories. The template says so and
  points at object storage.

Of the 65 that did not convert: 8 need the Docker socket, which cannot run under
a restricted pod security policy at all; 8 are stacks of five to twenty-three
services where getting startup order and shared state right without ever running
them is not a bet worth making; the rest have an image whose version could not be
verified or a service nothing says the port of.

## Phase 25 — the rest of the catalogue, and five resolver bugs

Seven multi-service stacks out of seventy-two, and 44 single-service templates
dropped for an unverifiable image, both looked like the source being awkward.
Most of it was this code being wrong, and each bug read in the log as somebody
else's fault.

**A tag can contain a colon.** `${IMMICH_VERSION:-release}` does, so splitting
the reference on the last colon produced the repository name
`ghcr.io/immich-app/immich-server:${IMMICH_VERSION`, and every registry answered
403. In the log that is the registry refusing us. It was eleven images, Immich,
Outline, Ente and Campfire among them.

**A registry's token comes from its own challenge.** ghcr.io and codeberg.org
were hardcoded and everything else — flipt, rocket.chat, weaviate, gcr.io,
outline, the Docker Hub mirrors — read as "unauthorized", which is what a
registry says when nobody asked it for a token. Reading `WWW-Authenticate` and
asking the realm it names works everywhere.

**A 429 is not "no".** Several vanity registries are pull-through caches in front
of Docker Hub and share its anonymous rate limit. Treating the rate limit as "the
image does not exist" dropped templates whose images were fine. It now backs off
and retries, and a question the registry never answered is recorded as
unverified rather than as absent.

**A tags listing is paginated.** Reading the first page picked
immich-machine-learning v1.132.3's server beside a v1.106.4 model runner — both
tags exist, the stack does not work, and upstream requires the two to match.
`Link` is followed now, and where a Compose file uses one version variable for
several images, the resolved tags are aligned and re-verified before any of them
is used.

**Not every project ships semver.** GitLab ships `19.1.8-ce.0`, SearXNG and
Excalidraw ship dates, DokuWiki ships `version-2026-07-14c`. Each names a build
exactly; insisting on semver threw all of them away. The fallback only applies
where the listing is newest-first, which the Hub API promises and a v2
`tags/list` does not, and it refuses an architecture (`linux-arm-v7`), a runtime
(`php8.3-apache`), a branch build (`…-chore-dependabot-security-36cd703`), a
toolchain variant (`3.8-python3.14-conda`) and anything longer than four parts.
Cockpit publishes `core-` and `pro-` from one repository, so the original tag's
prefix is kept as well.

And the converter stopped needing a port to be in `ports:` to count as written
down:

* **A `SERVICE_FQDN` marker does not have to match the service name.** Coolify
  writes `SERVICE_FQDN_CWA_8083` on a service called `calibre-web-automated`.
  Requiring the names to match was really defending against a shared environment
  block — a web app and its Sidekiq declared with one YAML anchor carry the same
  marker — and that case is visible in the data, so it is counted instead.
* **A healthcheck against localhost states a port.** `curl -fs
  http://localhost:8083` only makes sense if 8083 is open.
* **A peer states a port.** `PLAYWRIGHT_DRIVER_URL=ws://browser-sockpuppet-chrome:3000`
  and `ELASTICSEARCH_HOSTS=http://elasticsearch:9200` each name a service and the
  port it answers on. Matching the host against the service's own name is what
  keeps this from being the environment-scanning that once gave n8n Postgres's
  port: a bare `DB_PORT=5432` names nothing and is ignored.

A peer beats a healthcheck, because the peer's port is the one other apps have to
reach: ZooKeeper's healthcheck talks to its admin server on 8080, which answers
`ruok` and nothing ClickHouse wants.

**282 templates**, up from 230 — 38 of them multi-service, up from seven. Three
more fixes to the output rather than the converter:

* **A worker keeps no port it did not declare.** A healthcheck that shells out,
  or a peer reference naming the web app, is not a worker declaring a port, and
  `openpanel-worker` was public on 3000 before this. A test now refuses any
  service named for a worker that is public.
* **A datastore is not linked to the database.** ClickHouse, MinIO, Meilisearch
  and a headless Chrome do not read a `DATABASE_URL`, and handing one to
  ClickHouse says the analytics store depends on the Postgres, which is not true.
* **A UDP port is not an ingress, and 80000 is not a port.** Palworld publishes
  `8211/udp` and Coolify's metadata says healthchecks listens on 80000. Both
  would have produced a domain that never answers.

## Phase 26 — the unverified images, and what the catalogue says about itself

Forty-three images could not be verified, which sounded like forty-three
upstreams being careless. Four more bugs, one of them shipping:

* **A reference with no tag has no tag.** `busybox` rpartitions to `("", "",
  "busybox")`, and taking that as the tag made the resolver answer "already
  pinned" for the most floating reference there is. Nothing reached the
  catalogue — the writer's own gate refuses an image with no tag — but the
  resolver was telling itself the opposite of the truth.
* **A pinned tag still has to exist.** Coolify's Mealie template names
  `3.17.0`; Mealie publishes `v3.17.0`. Trusting "already pinned" dropped a
  template whose current release was one lookup away.
* **The registry and the Hub API have different limits.** A manifest fetch
  counts against Docker Hub's anonymous pull limit and an API call does not, so
  a vanity host in front of Hub — docker.flipt.io, registry.rocket.chat,
  cr.weaviate.io — could answer 429 forever while the tag was plainly there.
  Asking the Hub API instead recovered Rocket.Chat, Weaviate and Flipt without
  pulling anything.
* **Nothing ever asked whether the catalogue was still true.** A tag that
  existed at import can be deleted afterwards, and MinIO did exactly that to its
  old RELEASE tags on Docker Hub. `hack/verify_catalogue.py` now asks every
  registry about every image in the directory: 290 of 291 present, 0 gone, 1
  rate-limited. It is not in `make check` — CI has no business depending on
  Docker Hub being up, and the anonymous limit would make it flaky — so it is a
  thing somebody runs before a release.

**282 templates**, 38 of them multi-service.

What stays dropped: 8 stacks need the Docker socket, which cannot run under a
restricted pod security policy; 4 are five to twenty-three services; 36 images
have no tag this could verify — `tiredofit/freescout` and `ghcr.io/ente-io/web`
are gone or private, Prefect publishes only toolchain variants, and Excalidraw,
Fizzy and label-studio publish nothing but branch builds. Shipping any of them
means guessing, and the whole point of the previous phase was that guessing is
worse than dropping.

### The number in the prose was wrong, and nothing failed

The README said 219 while the directory held 279. It had been wrong for two
phases. Every count this repository states about the catalogue — in the README
and in `docs/templates.md` — is now read back from the catalogue by a test, and
it caught the next drift on the same afternoon.

### Three things the panel did not say

Thirty-eight multi-service templates made three gaps visible that seven had hidden:

* **A card said `3210 · 6791 · 26.2.4.23 · postgres`.** Joining every service's
  tag with a dot says nothing and looks like a fault. One app still shows its
  version; a stack shows how many apps it is.
* **Installing four apps ended on `/projects`**, with no sign of where they
  went. It now lands on the app when there is one and on its project when there
  are several, and says how many were made.
* **A template's notes were shown before installing and never again.** They are
  the steps Skifity cannot do for you — a bucket to create, a migration to run,
  a shared directory that is two directories here — and the moment they matter
  is after the install, not before it. The comment on the field said they
  "appear after installation"; they did not. They do now, on the page the
  install lands on, until dismissed.

## Phase 27 — reliability and security, looked at properly

Three real defects, one of them a panic with a trigger anybody could hit by
accident, and two spot checks turned into complete ones.

### A closed browser tab could end a deployment

`Hub.Publish` collected its subscribers under the lock, released it, and then
sent. Between those two steps a client whose request context had just ended had
its channel closed by the goroutine watching that context — and a send on a
closed channel is a panic, in whatever happened to be publishing. A build emits
thousands of log events; closing the tab during one is all it takes. The panic
is contained by `internal/runsafe`, so the panel survives, but the deployment
that was publishing does not.

Reproduced in about three hundred iterations of subscribe-and-cancel, and it is
now a test that does exactly that. The sends happen under the lock, which is
safe because every one of them is non-blocking: the default arm drops the event
rather than waiting, so the section never sleeps.

### The third setting that becomes a request, and did not go through netguard

`internal/netguard`'s own comment said "two settings hold an address the panel
then makes a request to". There are three. The URL a cluster component's
manifest is downloaded from is a setting, `ApplyManifestURL` fetched it with
`http.DefaultClient`, and **what comes back is applied to the cluster as
Kubernetes objects** — which makes it the worst of the three to have been able
to point at `169.254.169.254`. It goes through the guarded client now, a scheme
that is not http or https is refused before anything is dialled, and a test asks
for the metadata service and for loopback and requires a `netguard.Blocked`.

### Two things a restart left in progress forever

A deployment interrupted by a panel restart is marked failed at startup, and so
is a provisioning operation. A **backup** written as `running` and a **database**
written as `creating` were not, and they are the same shape: a row only the
goroutine holding it ever finishes. The panel is a Deployment with a self-upgrade
endpoint, so a restart is routine rather than exotic.

The database is the worse of the two. One stuck at `creating` cannot be backed
up either — the backup manager refuses a target that is not running — so it is
not merely wrong on screen, it is unusable, and the only way out was to delete it
and start again.

### A key id longer than the header could hold

The envelope header carries the key id's length in one byte, and the id comes out
of the master key file, which an operator edits by hand. Nothing checked it: a
longer id would have been written truncated, every secret sealed afterwards would
have been unopenable, and the first sign of it would have been a decryption
failure on rows that were written correctly weeks earlier. The keyring refuses
one now, at both boundaries, and `encode` refuses to truncate rather than doing
it quietly.

### Two spot checks became complete ones

`TestNothingIsReachableWithoutCredentials` named seven paths out of a hundred and
twenty-three. `TestOneTeamCannotReachAnother` named thirty-three. The route that
matters is always the one nobody thought to add, so both now walk the router:

* **112 routes** refuse an anonymous request; 8 are open on purpose, each with a
  reason written next to it, and a second test fails if one of those 8 stops
  being a route this panel serves.
* **84 team-scoped routes** answer 404 for another team's team, project,
  environment, app, database or server — not 403, which would confirm the thing
  exists.

Neither found a hole. That is the point of running them: "authorization lives in
one place" was a claim about a hundred and twenty-three routes checked at seven.

### What was looked at and was already right

Argon2id at the OWASP parameters, sign-in lockout per account *and* per address
with the correct password refused while locked, constant-time comparisons,
unknown accounts indistinguishable from wrong passwords, CSRF double-submit with
bearer tokens exempt, `HttpOnly`/`Secure`/`SameSite` on the session cookie, a
strict CSP, request body limits, HMAC-verified webhooks, SQLite on WAL with a
busy timeout and every write serialised through one mutex, and `runsafe` on every
background goroutine including the four launched as method calls that a grep for
`go func` misses.

Three things gosec reports here are not findings: SHA-1 in `internal/auth/totp.go`
is what RFC 6238 specifies, the CSRF cookie is readable by the frontend because
that is how double-submit works, and `skifity.toml` is 0644 because it is meant
to be committed.

## Phase 28 — the one thing autoscaling needed and did not check

Scaling on a CPU or memory target reads the metrics API. Without it the
HorizontalPodAutoscaler sits at `<unknown>/70%`: the app never scales up under
load, never scales back down, and neither Kubernetes nor the panel says why.
k3s ships metrics-server by default and this install does not disable it, so
the common case is fine — but an operator who brought their own cluster,
disabled it, or is watching it crash-loop on a small node would have had
autoscaling that looked configured and did nothing.

That is the exact failure the scaling readiness checker exists for, and it was
the one thing the checker did not look at. It checks the eight ways an app
breaks when it is scaled — a read-write-once volume, SQLite, in-memory
sessions, a local-disk cache, local uploads, in-app cron, no health path, a
single instance — and not whether the cluster can supply the number it is meant
to scale on. It does now, as an error rather than a warning, with what to do
about it. Scale-to-zero is exempt: KEDA counts requests, not CPU.

## Phase 29 — single sign-on, and what building it found

**Dokploy has SSO and this did not.** It was the only feature on the competitor
list that was a straight absence rather than a trade-off, so it is built:
OpenID Connect, authorization code with PKCE, against whatever an operator
points it at — Okta, Entra, Authentik, Keycloak, Zitadel, Google.

SAML is deliberately not here. It is a second protocol, a second XML parser and
a second class of signature bug, and every provider a self-hosted panel is
likely to meet speaks OIDC.

Three things about it are load-bearing, because each is an auth bypass when it
is wrong, and none of them is code written here:

* The ID token's signature, issuer, audience and expiry are verified by
  `oidc.IDTokenVerifier`. A hand-rolled JWT check is the usual way to end up
  accepting `alg: none` or a token minted for somebody else's client.
* The nonce is generated per sign-in and has to come back inside the ID token,
  which is what makes a replay of an old one fail.
* The state is generated per sign-in, kept in a short-lived HttpOnly cookie, and
  **spent before the code is exchanged** — so resending the same callback cannot
  replay it.

The issuer is a setting, which makes it the **fourth** address an administrator
types that the panel's own process then connects to. It dials through
`internal/netguard` like the other three. The redirect URI comes from the Panel
URL setting rather than from the request's Host, which a caller controls: that
is the difference between a fixed redirect target and one somebody can register
under their own hostname.

### What building it found in the code that was already there

An account created by single sign-on has no password hash. Signing in with a
password was correctly refused — and refused with *"stored password hash is not
in the expected argon2id format"*, which tells an anonymous caller which
addresses are provider-only accounts. That is the exact enumeration
`TestUnknownAccountLooksLikeAWrongPassword` exists to prevent, arriving through
a new door. An empty hash now costs the same Argon2 hash, the same lockout
entry and the same `ErrInvalidCredentials` as any other wrong password.

`TestEveryRouteRefusesAnAnonymousRequest` caught the two new routes on the first
run, which is what a complete check is for: they are open on purpose, and now
say so with a reason next to each.

### And the sandbox claim that was never checked

ADR-0010 has said for months that this environment "refuses privileged
containers", so no cluster could ever run here. Checked today for the first
time: the container is root with nearly every capability, `docker` and `k3d` are
both installed, `/dev/kmsg` and `/dev/net/tun` are there. What actually blocks
it is the session's permission layer refusing to start a Docker daemon — a fact
with a remedy, rather than a wall. The ADR now says so. No cluster has been run
either way, so nothing else changes; what changes is that the reason written
down was not the real one.

## Phase 30 — the release pointed somewhere nobody owns

"Tag a release" was on the roadmap as *the image does not exist until the first
tag*. It was worse than that.

The release published to **`ghcr.io/skifity/skifity`**, and the README two pages
away says plainly that there is no such repository — this one lives at
`TegarTheGreat/Skifity`. So a tag would either fail, or succeed into a namespace
nobody here owns and tell every installer to pull from a name **somebody else
could register**. That is not a missing artefact; it is a supply-chain hazard
written into the release configuration, sitting next to a document that already
knew.

The release now publishes to the repository it is cut from: the workflow derives
`IMAGE_REPO` from `GITHUB_REPOSITORY`, lowercased for ghcr.io, and GoReleaser
uses that for the image, the upgrade command in the release notes, and the
`image.source` label. Right wherever it is released from, and no decision taken
here.

One thing is still a decision rather than a fix, and it is flagged rather than
guessed: `installer/install.sh` carries a literal default, and a shell script a
stranger downloads cannot derive one. That line has to name wherever the project
actually publishes, which is choosing the project's public home.

### And the half of "a second pair of eyes" that can be arranged

The roadmap has asked since Phase 17 for somebody who did not write any of this
to look at the security model. **CodeQL** now runs on every push and weekly with
`security-extended` — a different analyser with a different model of the code,
reading taint from source to sink across packages, reporting into the Security
tab where a finding cannot be quietly forgotten. **Dependabot** opens the update
before `make audit` has to report it, grouped so a Kubernetes bump is one pull
request rather than twelve.

Neither is a person. A tool that agrees with the author is not evidence that the
author was right, and the roadmap still says so.

## The repository itself

`CONTRIBUTING.md` and a pull request template, which a repository this size
should have had from the start, and the README's documentation table now lists
all ten pages the panel serves rather than eight.

## Next tasks

1. Run the installer end to end on a real Ubuntu server and measure idle memory.
   Both need hardware this sandbox cannot provide.
2. Tag a release, which publishes the binaries and the image the installer
   points at.
3. Deploy a real application from Git, end to end, on that server — the one
   flow that has never been exercised against a live cluster.

## Open issues

* **Nothing has ever run against a real cluster.** `test/smoke` holds exactly
  two scripts — the panel and the installer — and neither touches Kubernetes.
  This entry used to say the cluster smoke tests "exist and are reviewed, not
  executed", and `panel.sh` used to name four of them in its header. They were
  never written. Everything that talks to a cluster — `internal/cluster`,
  `internal/deploy`, `internal/dbsvc`, `internal/backup`, the parts of
  `internal/kube` beyond manifest generation — is checked against the objects it
  renders and against a fake clientset, never against an API server. That is the
  largest single gap in this repository, it is a consequence of ADR-0010, and
  writing scripts that could not be run here would have widened it rather than
  closed it.
* **None of the published install path exists.** `get.skifity.io` does not
  resolve. There is no `skifity/skifity` repository on GitHub — this one is
  `TegarTheGreat/Skifity` — so the installer's manifest fetch
  (`raw.githubusercontent.com/skifity/skifity/main/deploy`) and its CLI download
  (`github.com/skifity/skifity/releases`) both point at nothing, and
  `ghcr.io/skifity/skifity` has never been pushed. This entry used to admit only
  the image. The working path is to clone the repository, `make image`, and run
  `installer/install.sh` from inside the clone with `SKIFITY_IMAGE` set, which
  makes it read `deploy/*.yaml` from disk; the README, `llms.txt` and the quick
  start now say so where the one-line command is.
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
