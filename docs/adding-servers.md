# Adding servers

You already have one. The machine you installed Skifity on is the cluster's
first node, and it is listed under **Servers** from the moment you finish
setup — Skifity records it rather than asking you to add a machine you are
already looking at. It is marked as one Skifity found rather than one it
installed, which is why it has no Remove or Promote button: there is no key to
it. To take it out of the cluster, run the uninstaller on the machine itself.

Everything below is about the *second* server.

You give Skifity an IP address and a way to sign in. It does the rest.

## What it does, step by step

The panel shows these as they happen, and says what each one found.

1. **Connecting over SSH.** The server's host key is recorded the first time,
   and checked on every connection after that. A changed key stops the process
   rather than continuing, because that is what a machine-in-the-middle looks
   like.
2. **Checking the server.** Operating system, architecture, memory, disk,
   whether a firewall is in the way, whether something already has port 6443.
   Anything fatal stops here, before the server has been changed.
3. **Installing a key.** Skifity generates a key pair that belongs to this
   server alone and installs the public half. It then reconnects with that key
   to prove it works before going any further.
4. **Configuring the firewall.** The cluster's ports are opened to the other
   servers' addresses only, never to the world. The rules are checked before
   they are added, so running this again changes nothing.
5. **Checking the network.** Both directions, before anything long-running
   starts. Without this step, a firewall in the provider's control panel shows
   up as a five-minute Kubernetes install that times out with no explanation.
6. **Installing Kubernetes.** k3s joins the existing cluster, with the same pod
   network as every other server. That is normally WireGuard, which encrypts
   traffic between servers; a server whose kernel has no WireGuard module is
   refused before this step rather than joined into a cluster it could not
   reach. Settings -> Cluster -> "How servers talk to each other" shows which
   network this cluster uses.
7. **Joining the cluster.** The panel waits for the node to report ready.

## The password

If you sign in with a password, it is used exactly once: to install the key from
step 3. It is never written to the database, never logged, and is gone from
memory once the step succeeds.

Once the key is installed, everything afterwards uses it.

## What happens after

Skifity starts placing new instances on the server straight away. Existing
instances are not moved — that would restart apps for no reason — so a new
server fills up as you deploy.

## How traffic reaches your apps

Every server runs the ingress, so an app answers on **every** server's address,
wherever its instances happen to be. What decides which address people use is
DNS, and DNS names one.

That is the honest limit of a several-server install. If an app has three
instances across three servers and the server your domain points at goes down,
the app is still running and the name is still dead. Kubernetes moved the work;
it cannot move your DNS record.

There are three ways out, and Skifity does not need to know which you chose:

* **Round-robin DNS.** Add an A record per server. Free, and a browser retries
  the next address on a refused connection, though not always quickly.
* **A floating IP.** Most providers sell one — Hetzner, DigitalOcean, Vultr. It
  moves between servers, so the name never changes.
* **A provider's load balancer.** The most reliable and the one that costs
  money.

With a floating IP or a load balancer, put its address in **Settings → Domains →
Cluster public IP**. That is the address the panel then uses for the free
`sslip.io` names it hands out, instead of a single server's own.

## Control plane servers

The first server runs the control plane. Others join as workers unless you
promote them.

With one control plane server, rebooting it means the cluster cannot be changed
for a few minutes. Your apps keep running the whole time; you just cannot deploy.

With three, losing one changes nothing at all. Two is worse than one: a
two-member etcd cluster cannot form a majority when either member is lost, so
the panel refuses to leave you there.

Promoting a server moves its instances elsewhere first, then rejoins it as a
control plane member.

## When a step fails

Every failure names what happened, what it means and what to do.

**Could not connect.** The address or port is wrong, the server is off, or a
firewall is blocking SSH. Try `ssh root@<address>` yourself: if that fails, so
will Skifity.

**The password was refused.** Password authentication may be turned off. Use a
private key instead, which is the better option anyway.

**The host key changed.** The server was rebuilt, or something is sitting
between you and it. If you rebuilt it, remove the server in the panel and add it
again. If you did not, stop and find out why.

**Preflight found a problem.** The message names it: too little memory, too
little disk, an unsupported distribution, or a port already in use. Fix it and
press Retry.

**The network check failed.** Something is dropping traffic between your
servers. Providers such as Hetzner and DigitalOcean have their own firewall in
the control panel, which the server cannot see or change. That is the usual
cause.

**Kubernetes did not install.** The log at
`/var/log/skifity/provision-<id>.log` on the panel's server has the installer's
own output.

Retry picks up where it stopped. Every step is safe to repeat.

## Removing a server

Instances are moved off first, then the server leaves the cluster. Nothing is
lost.

Tick **Also remove Kubernetes from the machine** to clean the server up
completely, leaving it as you found it.

Skifity refuses to remove a control plane server if that would leave the cluster
without a majority, and explains why rather than letting you break it.
