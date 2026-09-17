# Quick start

From an empty server to an app on the internet. About ten minutes, most of it
waiting for things to download.

## What you need

* A server running Ubuntu 24.04 or Debian 12, with at least 1 GB of memory and
  8 GB of free disk, and root access over SSH. Any VPS will do.
* Optionally, a domain name. Skifity works without one.

### Which systems

**The servers.** Skifity installs k3s as a systemd unit and manages it with
`systemctl`, so systemd is the one hard requirement.

| | |
|---|---|
| **Tested** | Ubuntu 24.04, Debian 12 |
| **Expected to work, not tested** | AlmaLinux, Rocky, RHEL, CentOS, Fedora, openSUSE, SLES, Arch |
| **Will not work** | Alpine, or anything else on OpenRC — no systemd. An unprivileged LXC or a Docker container, for the same reason. |
| **Architectures** | x86-64 and arm64. Nothing else, and the preflight says so before it changes anything. |

Adding a server checks all of this before touching the machine, and says which
requirement was not met rather than failing partway through an install.

**Your own machine**, for the `skifity` CLI and the MCP server — which are the
same binary as the panel:

| | |
|---|---|
| Linux | x86-64, arm64 |
| macOS | Intel, Apple Silicon |
| Windows | x86-64, arm64 |

The CLI needs nothing installed: one static file, no libc, no runtime. The panel
half of that binary is only meant to run on Linux, because what it installs is
Linux.

**Anyone using the panel** needs a browser. It is a web page.

## 1. Install

> **Not published yet.** `get.skifity.io` does not resolve, there is no
> `skifity/skifity` repository on GitHub, and no image has been pushed to
> `ghcr.io/skifity/skifity`. The command below is what the install *will* be. To
> try it today, clone the repository and run the installer from inside it, which
> reads the manifests from disk instead of fetching them:
>
> ```sh
> git clone https://github.com/TegarTheGreat/Skifity && cd Skifity
> make image                        # builds ghcr.io/skifity/skifity:<version>
> sudo SKIFITY_IMAGE=ghcr.io/skifity/skifity:$(git describe --tags --always --dirty) \
>   sh installer/install.sh
> ```
>
> The image tag is local; nothing is pushed anywhere. Running the installer from
> inside the clone is what makes it read `deploy/*.yaml` from disk rather than
> fetching them from a repository that is not there.
>
> Do that on a VPS you can throw away. See the Status section of the README.

SSH into the server and run:

```sh
curl -fsSL https://get.skifity.io | sudo sh
```

If you already have a domain pointed at the server, tell the installer and it
sets up HTTPS at the same time:

```sh
curl -fsSL https://get.skifity.io | sudo SKIFITY_DOMAIN=panel.example.com sh
```

It checks the server first, installs Kubernetes, starts the panel, and finishes
by printing a link. The link already has the setup token in it, so opening it is
the whole of the next step — no copying a forty-character string across. The
token is in the `#fragment`, which browsers never send to a server, and the page
takes it out of the address bar as soon as it has read it. The plain URL and the
token are printed underneath as well, for when the link is easier to retype than
to click.

It also puts `skifity`, the command line tool, on the server's PATH — served by
the panel it has just started, so it is always the matching version and needs no
internet at all.

If it stops, it says what happened and what to do about it. The full log is at
`/var/log/skifity-install.log`, and running the installer again is safe.

## 2. Create your account

![The setup screen, asking for the token and the first account](images/setup.png)

Open the link. The token is already filled in; choose an email address and a
password, and name your team.

![The recovery key screen, which cannot be passed without acknowledging it](images/recovery-key.png)

You are then shown a **recovery key**. It is the only copy: it can decrypt every
secret Skifity stores, and without it a restored backup is unreadable. Download
it and put it somewhere that is not this server. The panel will keep reminding
you until you say you have.

![The overview on a brand new install](images/overview.png)

## 3. Deploy something

The overview has a **New app** button. Press it, paste a Git repository URL, and
press Create.

That is the whole form. Skifity works out how to build it, gives it a URL, and
sets `PORT` for it to listen on. The deployment page shows the build as it
happens.

When it finishes, the app has a working address with HTTPS. Press it.

> The button is there because setup already made you a project called **First
> project** with a **Production** environment in it, and because the server you
> installed on is already listed under **Servers** — it is the cluster's first
> node, and Skifity records it during setup rather than asking you to add a
> machine you are already looking at. `⌘K` has the same **New app** action from
> anywhere.

## 4. Add your own domain

Open the app, go to **Domains**, and add yours. The panel shows the DNS record
to create. Once it resolves, the certificate is issued automatically and the
domain goes green.

Your app keeps its original address too, so nothing breaks while DNS
propagates.

## 5. Add a second server

One server is fine to start — you already have one, the machine you installed
on. When you want more capacity, or you want the cluster to survive a failure,
go to **Servers** and press **Add a server**.

Enter its IP address and how to sign in. Skifity does the rest: it checks the
server, installs a key of its own, configures the firewall, joins the server to
the cluster, and starts placing instances on it. The password you type is used
once and never stored.

Three control plane servers is the point at which losing one changes nothing.

## What next

* [Concepts](concepts.md) — what Skifity means by a project, an app and an
  instance, and what each one is underneath.
* [Adding servers](adding-servers.md) — what the seven steps do, and what to do
  when one fails.
* [The CLI and AI assistants](cli.md) — deploying from a terminal, and letting
  an assistant do it.
* [Troubleshooting](troubleshooting.md) — when something is wrong.
