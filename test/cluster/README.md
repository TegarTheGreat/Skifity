# The verification that has never been run

Everything in `test/smoke` checks this product against fakes, golden files and an
in-process SSH server, because the machine it was built on could not start a
cluster. `docs/progress.md` says so plainly, and it is the largest single gap in
this repository.

`verify.sh` is the other half. It installs Skifity on a real server, deploys a
real application, and then checks the three things only a real cluster can
settle:

1. **An app comes up and answers.** Not that the manifest renders — that the
   pod starts, the Service routes, and an HTTP request gets a reply.
2. **Autoscaling has numbers to scale on.** A HorizontalPodAutoscaler with no
   metrics sits at `<unknown>/70%` forever and nothing says so. The panel
   reports that as a scaling finding now; this is what proves the finding is
   right.
3. **Scale to zero is wired the way it is drawn.** KEDA running, an
   HTTPScaledObject for the app, and the app's own HPA *gone* — two autoscalers
   on one Deployment fight, and this is where that stops being a claim.

It also measures what the thing actually costs. `docs/performance.md` says 35
MiB idle, measured on a laptop. The number this prints is the first one measured
on a cluster, and if they disagree the document is what changes.

## Running it

**It installs k3s and changes the machine permanently.** Use a server you are
willing to rebuild — a fresh VPS, not something you care about.

```sh
git clone <this repository> && cd Skifity
make image                                   # or pull one from somewhere
sudo SKIFITY_IMAGE=skifity:local bash test/cluster/verify.sh
```

It asks for a typed `yes` before touching anything. `SKIFITY_ASSUME_YES=1` skips
the question, which is for a machine that is already disposable.

| Variable | What it is |
|---|---|
| `SKIFITY_IMAGE` | **Required.** The panel image. No release is published yet, so build one or push one somewhere the server can pull from. |
| `SKIFITY_DOMAIN` | The panel's domain. Defaults to `<public-ip>.nip.io`, so there is nothing to set up in DNS. |
| `SKIFITY_PUBLIC_IP` | Set it when the machine cannot work its own out. |
| `SKIFITY_ASSUME_YES` | `1` to skip the confirmation. |
| `VERIFY_APP_IMAGE` | The application it deploys. A small public image with an HTTP server, on purpose: the first run should fail on the cluster if it fails, not on a build. |

## What to send back

The whole of `/var/log/skifity-verify.log`. A failure in it is worth more than a
pass: it is the first thing this product has ever been told by a real cluster,
and every check names what it expected so the report can be acted on rather than
interpreted.

## Why it is not in `make check`

It needs a machine to destroy, several minutes, and the internet. CI runs on
every push and must not do any of those. This is a thing a person runs before a
release, and the release is not tagged until it has passed.
