# Troubleshooting

Every error in Skifity says what happened, what it means and how to fix it, and
has a button that copies the whole thing for an AI assistant. Start there. This
page is for the things that happen outside the panel, or when the panel itself
is the problem.

## The panel does not load

**Nothing at all.** Check it is running:

```sh
kubectl -n skifity-system get pods
```

If the pod is not `Running`, ask why:

```sh
kubectl -n skifity-system describe pod -l app.kubernetes.io/component=panel
kubectl -n skifity-system logs -l app.kubernetes.io/component=panel
```

**The page times out.** The server's firewall may be blocking ports 80 and 443.
Some providers have a firewall in their control panel that the server cannot
see.

**A certificate warning.** With no domain of your own, the panel is served over
plain HTTP at an `sslip.io` address, on purpose: Let's Encrypt rate limits are
per domain, and every Skifity install in the world shares sslip.io's. Add a
domain in Settings and HTTPS is turned on for it automatically.

## I have lost the setup token

It is on the server:

```sh
sudo cat /etc/skifity/setup-token
```

If the file is gone but no account was ever created, restart the panel and it
writes a new one and logs it:

```sh
kubectl -n skifity-system rollout restart deployment/skifity-panel
kubectl -n skifity-system logs -l app.kubernetes.io/component=panel | grep setup_token
```

## I have forgotten my password

There is no email reset: a self-hosted panel has no mail server it can trust.
Reset it from the server instead:

```sh
sudo skifity admin reset-password you@example.com
```

## A deployment failed

The deployment's own page has the build log and the reason. The common ones:

**Out of memory during the build.** Builds are the most memory-hungry thing
Skifity does. Add a server, or raise the build memory limit in Settings.

**No Dockerfile and nothing detected.** Skifity could not tell what the project
is. Add a Dockerfile, or set the builder explicitly in the app's settings.

**The app starts and immediately stops.** Almost always the port. Your app must
listen on the port in the `PORT` variable, which Skifity sets. Listening on a
hardcoded 3000 when Skifity asked for 8080 produces exactly this.

**Health checks never pass.** The health check path returns something other
than 200, or the app takes longer to start than the check allows. Both are in
the app's settings.

## An app is crashing

Open the app: the instance list shows the restart count and the last reason.
The Logs tab has the output from before it died.

If the app worked before, **Roll back**. That restores the previous image *and*
the settings it ran with, so a bad variable is undone too.

## A domain does not work

The panel shows the DNS record to create. Check it has actually taken effect:

```sh
dig +short app.example.com
```

That has to return your server's IP address. If it returns nothing, DNS has not
propagated yet, which can take up to an hour.

If DNS is right but the certificate is not issued, cert-manager is still
working. Watch it:

```sh
kubectl get certificate -A
kubectl describe certificate -n <environment-namespace> <name>
```

Let's Encrypt rate-limits per domain. If you have been experimenting, you may be
paused for a week; the certificate's events say so.

## The cluster is unreachable

The panel keeps working and says so on every page. Your apps keep running: they
do not need the control plane to serve traffic.

On the control plane server:

```sh
sudo systemctl status k3s
sudo journalctl -u k3s -n 100 --no-pager
```

The usual cause is the server running out of disk. Check with `df -h`.

## A server says "not responding"

The node stopped reporting. The server may be off, out of disk, or unreachable.
Instances that were on it are rescheduled elsewhere automatically if there is
capacity.

If the server is gone for good, remove it in the panel: its work is already
elsewhere.

## Restoring after losing the panel's server

The panel's database and master key live on the first control plane server, in
`/var/lib/skifity` and `/etc/skifity`. To move to a new server:

1. Install Skifity on the new server.
2. Stop the panel: `kubectl -n skifity-system scale deploy/skifity-panel --replicas=0`
3. Copy `/var/lib/skifity/panel.db` and `/etc/skifity/master.key` across.
4. Start it again: `kubectl -n skifity-system scale deploy/skifity-panel --replicas=1`

Without the master key the database is unreadable, which is the point of the
recovery key you were asked to download. [Configuration](configuration.md) has
the full list of what to back up.

## Everything is fine but I want to look underneath

Every app's **Advanced** tab shows the exact Kubernetes objects the panel
applies, ready to copy. `kubectl` is on the control plane server and works
normally; Skifity uses server-side apply with its own field manager, so it will
not fight you over a field you change by hand — but it will change it back on
the next deployment.
