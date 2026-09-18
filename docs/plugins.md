# Writing a Skifity plugin

A plugin is a **container image and a manifest**. Nothing else.

Skifity runs your image in a namespace of its own, gives it an API token
carrying exactly the permissions your manifest asked for and an administrator
granted, and posts the events you subscribed to. Write it in any language.

## Why not a library

Skifity is one static Go binary with CGO turned off, and Go cannot load code
into one. The `plugin` package needs CGO, works on three operating systems, and
its own documentation warns that "runtime crashes are likely to occur unless all
parts of the program (the application and all its plugins) are compiled using
exactly the same version of the toolchain, the same build tags, and the same
values of certain flags and environment variables". Recompiling the panel to add
a plugin, which is how Caddy and Traefik's static plugins work, means every user
needs a Go toolchain.

A container has none of those problems, and three properties a library cannot
offer: your crash is not the panel's crash, your language is your choice, and
what you may do is a list an administrator read before saying yes.

## The manifest

```yaml
apiVersion: plugin.skifity.io/v1
id: com.example.backup-to-b2
name: Backup to Backblaze B2
description: Copies every database backup to a Backblaze bucket as it is taken.
version: 1.2.0
homepage: https://example.com/skifity-b2
license: MIT
author:
  name: Example Ltd
  url: https://example.com

image: ghcr.io/example/skifity-b2@sha256:0000000000000000000000000000000000000000000000000000000000000000

requires:
  skifity: ">=1.0.0 <2.0.0"

permissions:
  - databases:read
  - backups:read
  - backups:write

events:
  - event: backup.completed
  - event: backup.failed

settings:
  - key: bucket
    label: Bucket
    required: true
  - key: application_key
    label: Application key
    kind: password
    secret: true

runtime:
  port: 8080
  health: /healthz
  memoryMB: 64
```

That exact document is a test in `internal/plugins`. If it ever stops being
valid, the standard changed and it was not on purpose.

### The rules that will surprise you

* **`id` is reverse-DNS and never changes.** You own a domain, so nobody has to
  run a registry of names for yours to be unique.
* **`image` is a digest, never a tag.** A tag can be moved by whoever controls
  the registry, and this image is about to be handed an API token. "The plugin
  you approved" has to mean the bytes you approved.
* **The field is `event:`, not `on:`.** `on` is a boolean in YAML 1.1, so
  `on: backup.completed` parses in some readers as the key `true`. GitHub
  Actions carries that scar in every workflow file ever written.
* **An unknown field is an error, not a warning.** A plugin written against a
  later panel refuses to install rather than installing and quietly losing half
  of what it declared.

## Permissions

A permission is a resource and an action: `apps:read`, `backups:write`. The
resources are `apps`, `deployments`, `databases`, `backups`, `projects`,
`servers`, `templates`, `operations`, `events`, `teams`, `settings` and
`account`.

**`read` and `write` on their own are refused for a plugin.** They mean every
resource, which is not something anybody can meaningfully agree to on a screen.
Name what you need.

There is no permission that grants the master key, because there is no route
that exposes it, and no permission that reaches another team, because that check
is separate and runs anyway. Ask for the least you can: an administrator reads
this list, and a plugin asking for `servers:write` to copy a file is a plugin
they will not install.

## Events

| | |
|---|---|
| `app.created`, `app.deleted` | |
| `deploy.before` | **Can be blocking.** You may refuse the deploy. |
| `deploy.succeeded`, `deploy.failed` | |
| `backup.completed`, `backup.failed` | |
| `database.created` | |
| `server.added`, `server.removed` | |

Subscribing to an event the panel does not send is refused at install time, not
discovered six months later when you notice your plugin has never run.

**Blocking is only for an event that happens before something.** Refusing a
deploy is a decision; refusing to acknowledge that a backup already finished is
not, so `blocking: true` on anything else is refused. A blocking hook holds up
somebody watching a deployment page, so the panel caps it at ten seconds
whatever you asked for. If you need longer, answer straight away and do the work
afterwards.

## Settings

Declare them; the panel collects them. That way the form exists before your
plugin is running, the labels can be translated, and a secret is sealed with the
panel's own keyring rather than your container becoming somewhere credentials
live.

Kinds are `text`, `password`, `number`, `bool` and `choice`. `secret: true`
means it is sealed at rest and never shown again — the same promise the panel
makes about every other secret it holds.

## Commercial plugins

`license: commercial` is a first-class value, and the store shows it before
anything is installed.

**Skifity does not process payments and will not.** A commercial plugin sells
through its own site and validates its own licence key — declare a setting for
it, check it against your own server, and refuse to work without it. That keeps
Skifity out of being a payment processor, and it keeps your customer
relationship yours.

## What your container is given

Everything arrives as environment variables, from one Secret:

| | |
|---|---|
| `SKIFITY_PLUGIN_ID` | your own id |
| `SKIFITY_API` | the panel's address inside the cluster |
| `SKIFITY_TOKEN` | your API token, carrying exactly the permissions you asked for |
| `SKIFITY_SIGNING_SECRET` | the key events are signed with |
| `SKIFITY_SETTING_<KEY>` | one per declared setting, upper-cased |
| `PORT` | the port to listen on |

**Your image has to run as a non-root user.** Your namespace enforces the strict
Pod Security profile, and an image that starts as root is refused. Unlike an
off-the-shelf application image, you control the Dockerfile, so this is a
requirement rather than a problem — and an image that will not start is the most
common reason a plugin looks broken.

You get one replica, a read-only root filesystem, no Kubernetes API token, and a
namespace of your own with a network policy that lets you reach **the panel, DNS
and the internet** — and nothing else. Not another plugin, not an app, not a
database, not the node network, and not the cloud metadata address.

## Receiving events

The panel POSTs to `/events` on your port:

```
POST /events
X-Skifity-Event: deploy.succeeded
X-Skifity-Signature: sha256=<hmac>
Content-Type: application/json

{"event":"deploy.succeeded","at":"2026-09-18T09:00:00Z","data":{...}}
```

**Verify the signature.** It is an HMAC-SHA256 over the exact request body,
keyed with `SKIFITY_SIGNING_SECRET`. Your endpoint is only reachable from the
panel's namespace, which is the first line — but a plugin that does not check
the signature is a plugin that trusts its network. Compare in constant time.

Answer `200`. For a **blocking** event, answer with a verdict:

```json
{"allow": false, "reason": "there is a change freeze until Monday"}
```

Three things about that answer:

* **Silence is not consent.** An empty body is a refusal, because a plugin that
  answered `200` and nothing else has said nothing.
* **Not answering is not a refusal.** If you are down or slow, the deploy goes
  ahead. A plugin that stops every deploy the moment it is upgraded is a plugin
  nobody installs twice.
* **Every blocking plugin has to agree.** One plugin's yes does not overrule
  another's no.

Give a reason. It is shown to the person whose deploy you just stopped, and
without one they are told a plugin refused and nothing else.

### Delivery is not guaranteed

A notification is posted once, with a short timeout. A plugin that was
restarting misses it, and there is no retry queue — saying so plainly is better
than a loop that looks like a guarantee and is not one. If you must not miss
anything, read the state back through the API. That is what your token is for.

## Publishing

Publish the manifest anywhere it can be fetched over HTTPS. An operator installs
it by URL, or finds it in the Skifity store.

An operator who does not want a store at all can point the panel at their own
index, or install from a manifest file on disk. That path is not an afterthought:
a cluster with no way out to the internet has to be able to run plugins too.

> **Never run against a real cluster.** The manifest standard, the permission
> model, the rendered Kubernetes objects, the event delivery and the blocking
> verdicts are all unit-tested. Whether a real plugin image starts in the
> namespace this renders, and whether an event reaches it over a real cluster
> network, has not been tried — the same gap as everything else in ADR-0010.
>
> There is also no store yet: a plugin is installed by pasting a manifest or
> giving its address. See the Status section of the README.
