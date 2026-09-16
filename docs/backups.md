# Backups

Skifity backs up managed databases: PostgreSQL, MariaDB and Redis.

**Volumes are not backed up yet.** An app that writes files to a volume keeps
them as long as the volume exists, and the panel will refuse a backup of one
rather than pretend. If those files matter, either keep the data in a managed
database, which is backed up, or take the volume's snapshot with whatever your
storage provides. This page will say otherwise when that changes.

Nothing is backed up until you say where to put it.

## Storage

Backups go to S3, or to anything that speaks S3 — MinIO, Backblaze B2,
Cloudflare R2, Hetzner Object Storage. Set it up under **Settings → Backup
storage**: an endpoint, a region, a bucket, and a key pair.

Use a bucket and a key that can do nothing else. If the server is compromised,
the blast radius should stop at "the attacker can write backups".

**The credentials never leave the panel.** A backup runs as a Job inside the
cluster, and that Job is handed a URL that already carries a signature and
expires shortly afterwards. It can upload one object, to one path, for a few
minutes. It has no idea what the key is. The same is true in reverse for a
restore.

That is why backup storage is configured once, centrally, rather than per app:
there is nothing to hand out.

## Schedules

Each database has its own schedule, set on its Backups tab, written as cron:

```
0 3 * * *      every day at 03:00
0 3 * * 0      every Sunday at 03:00
0 */6 * * *    every six hours
```

**Keep the last** is how many to hold. Older ones are deleted after a
successful new one, never before: a retention rule must not be able to leave you
with nothing.

Times are the server's, not yours.

## Restoring

A restore replaces everything currently in the database with the contents of
the backup. There is no merge, and there is no undo.

Skifity asks you to confirm in words rather than with a button, and refuses
outright if the backup is from a different engine or a much newer version — a
restore that half-works is worse than one that does not start.

While it runs, the database is unavailable and the apps connected to it will
report errors. That is expected and it says so before you begin.

Take a fresh backup first. The panel will offer.

## Failures

A failed backup is worth knowing about immediately, which is why
`backup.failed` is on by default in a new notification channel.

**No storage configured.** Nothing has been backed up. Set it up under
Settings → Backup storage.

**Access denied, or the bucket does not exist.** The key pair cannot write to
that bucket. Check the bucket name and the region — a wrong region often
presents as a permission error rather than a missing bucket.

**The Job ran out of memory.** A dump of a large database needs room. The
message says how much it wanted.

**It succeeded but the file is tiny.** Almost always a database that is empty
because the app never wrote to the one you think it did. Check which database
the app is actually connected to on its Variables tab.

## What is not backed up

The panel's own state and the master key. They are files on the control plane
server, and [Configuration](configuration.md) says which ones and why they
should not be kept together.

A backup of a database is useless without the master key that decrypts the
credentials stored alongside it, so back that up somewhere else, once, and
properly.
