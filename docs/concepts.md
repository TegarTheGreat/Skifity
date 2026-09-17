# Concepts

Skifity deliberately uses ordinary words. This page says what each one means,
and what it is underneath, so that nothing is a mystery when you need to look.

## The words

| Skifity says | It means | Underneath |
|---|---|---|
| **Team** | Who can see and change things | A row, not a Kubernetes object |
| **Project** | Things that belong together | A group of namespaces |
| **Environment** | Production, staging, a preview | A Namespace |
| **App** | One thing you deployed | A Deployment, a Service and an Ingress |
| **Instance** | One running copy of an app | A Pod |
| **Server** | A machine you own | A Node |
| **Domain** | An address that reaches an app | An Ingress rule and a Certificate |
| **Database** | A managed PostgreSQL, Redis or MySQL | A Cluster or a StatefulSet |
| **Variable** | Something your app reads from the environment | A key in a Secret |
| **Volume** | Storage that survives a restart | A PersistentVolumeClaim |
| **Deployment** *(the noun in the history)* | One attempt to run a new version | A build, then a rollout |

Every app's **Advanced** tab shows the exact objects the panel applies. Nothing
is hidden; it is only kept out of the way.

## Projects and environments

A project is a box. Inside it are environments, and inside those are apps and
databases.

Every project gets a **Production** environment when it is created. Add more for
staging, or let Skifity create one per pull request.

Environments are isolated from each other: each is a namespace with
default-deny networking, so an app in staging cannot reach production's database
by accident.

## Variables, and why some rebuild and some do not

An app's variables become environment variables inside it. There are two kinds,
and the difference matters:

* A **runtime variable** is read by your app when it starts. Changing one
  restarts the app with the new value. That takes seconds, and no rebuild.
* A **build-time variable** is baked into the image, which front-end builds do
  with things like a public API URL. Changing one means the image is wrong, so
  the app is rebuilt.

Skifity works this out from a fingerprint of everything that actually affects
the image. The panel tells you which kind you are setting before you save it,
and says afterwards whether it rebuilt.

This is the thing most often complained about in other panels, where changing
any setting triggers a ten-minute rebuild. Here it does not.

## Deployments and rollback

Each deployment records the image it produced *and* the settings it ran with:
variables, instance count, resources, domains.

Rolling back therefore restores a working state, not just an old image. If you
changed a variable and the app broke, rolling back puts the old variable back
too.

**How far back you can go.** The panel keeps a long list of deployments and the
registry keeps the images for the last ten of them; older images are removed so
the disk does not fill. The list says which versions can still be rolled back
to, and the older ones say "image removed" where the button would be. To go back
further, deploy that commit again — it builds the same code fresh.

## Release command

A command that runs after the image is built and before any traffic reaches the
new version. It is where a database migration belongs.

```
npm run migrate
```

Set it on the app's **Settings** tab. Every deployment runs it, in the app's own
image with the app's own variables, while the previous version keeps serving. If
it fails, the deployment stops there: the new code never sees the old schema,
and nothing changed for your users.

For something you want to run once rather than on every deploy — a backfill, a
console, a look at the data — use a one-off command instead:

```
skifity run --app app_123 -- npm run backfill
```

That runs the same way, in the same image, with the same variables, and its
output comes back as it happens. It is a Job of its own rather than a shell into
a running instance: a migration usually needs to run when the app is not up,
which is exactly when there is nothing to attach to.

## Scheduled commands

A nightly report, an hourly cleanup, a weekly digest. Add one on the app's
**Console** tab with a name, a five-field cron schedule and a command:

```
nightly report    0 3 * * *    npm run digest
```

Schedules are in UTC, because a cluster's idea of local time is not something
anybody chose. Each runs in the app's image, with the app's variables, and is
applied with the app — so a nightly job always runs the version that is
deployed rather than whatever it was when the schedule was written.

Kubernetes does the scheduling, not the panel. A panel that is restarting at
three in the morning is not a reason for a job to be skipped. A job that is
still running when the next one is due does not start a second copy.

## What the panel works out for you

When you paste a repository address, **Check this repository** reads its file
list through your Git provider's API — two requests, no clone — and says what it
thinks it is: "This looks like Next.js", with the port it expects and the
reasons it decided that.

It fills in only the fields you have left empty, and only when the answer came
from a file the repository's author put there, like a `package.json` naming a
framework. A guess from a file extension is shown as a question and fills in
nothing. It never overwrites something you typed.

You do not have to use it. The zero-config builder does its own detection inside
the build, so a repository deploys whether or not you press the button; this is
about the panel being able to say something while you are still looking at the
form.

A private repository needs a connected Git account with access to it. The token
is only ever sent to the host that account is for.

## Instances and scaling

An app runs one instance by default. You can set a fixed number, or let Skifity
add and remove instances based on CPU or memory.

Before you scale, the panel checks whether the app *can* run more than once. It
looks for the things that break: a SQLite file on a local disk, sessions kept in
memory, a background job that assumes it is the only one. Each finding says what
would go wrong and how to fix it.

Instances are spread across servers where possible, so losing a server does not
take an app down.

## Servers

The first server runs the control plane: the Kubernetes API, the panel and its
database. Others join it.

With one control plane server, a reboot means a few minutes of downtime for the
cluster's control — your apps keep running. With three, losing one changes
nothing.

A worker can be promoted to a control plane server, if it is big enough for one:
2 GB of memory and 20 GB of disk, against the 1 GB and 8 GB a worker needs,
because etcd and the API server live there too. The check runs before anything
is touched. It did not always: promotion drains the node, removes it from the
cluster and uninstalls Kubernetes before reinstalling it, so a machine that
turned out to be too small used to be discovered at the end, with the apps
already moved off and nothing left running on it.

## Secrets

Every secret Skifity stores — database passwords, Git tokens, your apps'
variables — is encrypted with a key of its own, and that key is wrapped with a
master key kept outside the database.

A leaked database backup is therefore useless on its own. The flip side is that
losing the master key loses every secret, which is why the panel makes you
download a recovery key before it stops nagging.

Once stored, a secret's value is never shown again, by the panel, the CLI, the
API or an AI assistant.

## Components

A fresh install is small: k3s, the panel, and nothing else. Heavier pieces are
installed the first time you need them.

| Component | Installed when | Roughly |
|---|---|---|
| cert-manager | You add a domain | 120 MB |
| Image registry | You first deploy from source | 60 MB |
| Builder | You first deploy from source | 200 MB |
| PostgreSQL operator | You create a PostgreSQL database | 150 MB |
| Scale to zero | You turn it on | 180 MB |
| Cross-node storage | You turn it on | 700 MB per server |
| Full monitoring | You turn it on | 900 MB |

Settings shows what is installed and what each one costs before you agree to it.
