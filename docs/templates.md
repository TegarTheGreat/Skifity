# Templates

A template installs a known application in one step: the app, the database it
needs, the variables that wire the two together, and the volume its files live
on.

What comes out is ordinary. A templated app is an app: it can be scaled, backed
up, rolled back and deleted like anything else you deploy, and nothing in the
panel treats it differently afterwards. The template is how it started, not what
it is.

## Installing one

**Templates** in the sidebar lists what is there, grouped by category. Choose an
environment, answer whatever the template asks for, and install.

Some templates ask for something only you can know — an admin email, a licence
key. Some ask for a secret and offer to generate it; take the offer, because a
generated one is longer and more random than one you would type. The panel seals
it like any other secret and shows it once.

Databases are created before the services that need them start, so the
connection string exists by the time the app looks for it. That is the
difference between a template that works on the first try and one that
crash-loops until somebody redeploys it.

## What is in the catalogue

Eight applications, which is deliberately few:

| | |
|---|---|
| **WordPress** | The blogging and content platform, with MySQL and a volume for uploads |
| **Ghost** | Publishing and newsletters, with MySQL |
| **n8n** | Workflow automation, with PostgreSQL |
| **Uptime Kuma** | Uptime monitoring for your own services |
| **Plausible** | Privacy-friendly web analytics, with PostgreSQL |
| **Umami** | Web analytics, with PostgreSQL |
| **Vaultwarden** | A Bitwarden-compatible password server |
| **MinIO** | S3-compatible object storage, which can hold your backups |

A template that is not kept working is worse than no template. Coolify and
Dokploy have far larger libraries; this list is the size it is because every
entry is something we can keep an eye on, and it grows when that stays true.

## Versions

Every template names a version of the software it installs. None of them runs
`latest`.

A floating tag is not a version. Two deploys of what looks like the same app run
different software, a rollback restores a tag rather than the thing that worked,
and an upstream release arrives on a restart nobody asked for. Where the project
publishes a series tag that takes patches without breaking changes, that is what
a template uses; where it does not, an exact version is.

So a template does not update itself. Moving one forward is a change to Skifity,
and you move your own installation forward by changing the image on the app, the
same way you would for anything else you run.

## Changing what a template made

Everything a template sets is editable afterwards: variables on the app's
Variables tab, size and scaling on its settings, the database on its own page.
Reinstalling the template does not reconcile anything — it installs a second
copy under a new name.

If you outgrow a template, nothing has to be undone. It left you an app and a
database, and those are the things Skifity actually runs.
