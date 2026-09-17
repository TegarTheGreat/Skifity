# Roadmap

Where the work goes next, largest first. `docs/progress.md` is what has been
done; this is what is left and why it is in this order.

The ordering is by how much a person loses without it, not by how hard it is.
A panel that fills its own disk in month two is a worse product than one that
cannot guess a framework, so garbage collection comes before detection.

---

## Phase 11 — A cluster that survives its second month

Everything in the product works on day one. These are the things that quietly
degrade afterwards, which is the failure people remember, because it happens
when they have already trusted it with something.

### 11.1 Garbage-collect the in-cluster registry

Every build pushes an image and nothing ever removes one. Layers are shared and
the images are small, so this is slow rather than sudden, which is exactly why
it goes unnoticed until a node has no disk left and every pod on it stops.

* Delete the manifests an app no longer needs, keeping the last few so a
  rollback still has somewhere to go.
* Run the registry's own `garbage-collect` on a schedule, which is what actually
  frees the blobs.
* Remove an app's images when the app is deleted.

### 11.2 Stop the panel's own database growing forever

Backups have a retention policy and build logs are pruned. Audit entries,
finished deployments and operations are not: a busy panel writes rows nobody
will read again and never removes one, and the database is a file on one node.

* One retention pass, with a setting, run by the watcher that already runs.
* Keep what an audit log is for: the window is long, and a shorter one is the
  operator's choice rather than the default.

---

## Phase 12 — The panel says what is wrong before it is asked

The panel is good at reporting that something failed and vague about why. These
are the two questions people actually ask.

### 12.1 Say why an instance cannot start

A pending pod currently says "waiting for a server with enough free CPU and
memory" whatever the real reason. It is often a quota, a volume that cannot be
bound, a taint, or an image that will not pull, and each has a different fix.
The scheduler already writes the answer into the pod's events.

### 12.2 Show an environment's quota headroom

Every environment gets a ResourceQuota and nothing in the panel shows it, so the
first sign of hitting one is a deployment that fails for a reason that reads
like a bug.

---

## Phase 13 — The panel can be monitored like anything else

The panel watches the cluster and nothing watches the panel. A Prometheus
endpoint is what an operator reaches for, and every self-hosted product that
does not have one gets asked for it.

---

## Phase 14 — The deploy story is finished

Two gaps that are features rather than defects.

### 14.1 Detect the framework before the first build

`builder.Detect` works and is tested and nothing calls it, because the panel
cannot read a repository's file list. Railpack does its own detection inside the
build, so builds work; what is missing is the panel saying "this looks like
Next.js, it listens on 3000" while somebody is still filling in the form.

### 14.2 Back up a volume, not only a database

The panel refuses rather than pretending, which is honest and still a gap: an
app's uploads have nowhere to go.

---

## Phase 15 — Frontend weight and keyboard

One 917 kB chunk, 271 kB compressed, with settings and templates already split
out. It is served from the binary on the same host, so it is not the problem it
would be over a CDN, and it is not small. Plus a pass over what a keyboard and a
screen reader make of the shell and the forms.

---

## Phase 16 — The documentation for all of it

Anything a user can see gets a page, and the error catalogue's links resolve to
an anchor that exists. A test already enforces the second part.

---

## Not on this roadmap, and why

* **An interactive shell into a running instance.** A one-off command runs in
  the app's own image with the app's own variables, can be watched, and leaves a
  log, and it works when the app will not start — which is when people reach for
  a shell. An interactive session is more surface, more risk, and less useful at
  the moment it matters.
* **A second component library.** The panel is shadcn/ui and stays that way.
* **Multi-cluster.** One panel, one cluster, is the product. Somebody running
  two clusters runs two panels, and that is a fine answer.

## The one thing none of this fixes

None of the cluster code has ever been run against a real cluster: the sandbox
this was built in refuses privileged containers, so k3s could never start here
(ADR-0010). Everything is checked against a fake clientset, golden manifests and
a real in-process SSH server, and that is not the same thing. Running the
installer on real hardware and deploying one application end to end remains the
first task, ahead of every phase above.
