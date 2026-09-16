# Research: Competing platforms

Date: 2026-09-16

## Summary table

| Product | Orchestrator | Multi-server | Build | Notable strength | Notable weakness |
|---|---|---|---|---|---|
| Coolify | Docker / Docker Swarm | Swarm, still labelled experimental in its own docs (2026) | Nixpacks, Dockerfile, Compose | Huge template library, large community (~60k users) | Swarm multi-node is experimental; unexpected rebuilds on config change; struggles on 1 GB VPS; CVE disclosures Jan 2026 |
| Dokploy | Docker Swarm | Swarm; multi-node + templates under a source-available licence restricting commercial use | Nixpacks, Dockerfile | Clean modern UI, fast iteration | Pre-1.0 (v0.29.x); licence restructure Jan 2026; Swarm indirection leaks into debugging |
| CapRover | Docker Swarm | Yes | Dockerfile/captain-definition | Very light, stable, old and proven | Dated UI; limited scaling story; little multi-tenant structure |
| Dokku | Docker + herokuish | Single host (scheduler plugins exist) | Buildpacks/Dockerfile | `git push` deploys, tiny footprint | Single host by default, CLI-first, no panel |
| Kubero | Kubernetes | Yes (bring your own cluster) | Buildpacks | Heroku-like on real k8s | Assumes you already run Kubernetes; installation is not "one command on a fresh VPS" |
| Rancher | Kubernetes | Yes | n/a | Excellent cluster lifecycle management | Cluster management tool, not an app platform; heavy |
| k3sup | k3s | Yes | n/a | Dead simple k3s bootstrap over SSH | CLI only, no panel, no app model |
| Railway | Proprietary | n/a | Railpack (was Nixpacks) | Service canvas, shared variables, superb DX | Not self-hostable |
| Vercel | Proprietary | n/a | Framework presets | Preview URL per branch/PR, instant rollback | Not self-hostable, function-shaped |
| Heroku | Proprietary | n/a | Buildpacks | Language auto-detection without a Dockerfile | Expensive, not self-hostable |
| Fly.io / Render | Proprietary | n/a | Dockerfile/buildpacks | Global scheduling, scale-to-zero | Not self-hostable |
| Cloudflare Workers | Proprietary | n/a | n/a | Fast deploys, scale to zero | Only fits the Workers runtime |

## What we take from each

* **Vercel** - deploy from Git, one preview URL per branch/PR, one-click rollback.
* **Railway** - visual canvas of services, variables shared between services of a project.
* **Heroku** - detect the language and build without a Dockerfile.
* **Cloudflare Workers** - scale-to-zero for idle apps.
* **Coolify / Dokploy** - one-click templates, S3 backups, being genuinely self-hosted.
* **Kubernetes** - self-healing, rolling updates, rollback, node failover for free.

## Weaknesses we explicitly design against

1. **Multi-server is the weak spot of every Docker/Swarm-based competitor.** Swarm is in maintenance,
   and both Coolify and Dokploy carry warnings about it. Skifity builds on k3s, where multi-node,
   rescheduling and node failure are first-class. This is our main differentiator.
2. **"Unexpected rebuild on a config change"** (top Coolify complaint). Skifity separates *build inputs*
   (source commit, build config) from *runtime inputs* (env vars, replicas, domains). Changing a runtime
   input performs a rollout of the existing image and never triggers a build. Implemented as a build
   fingerprint - see `internal/deploy`.
3. **Memory footprint.** Coolify can fail to install on a 1 GB VPS. Skifity ships a single Go binary and
   installs heavy components (Longhorn, KEDA, database operators, full monitoring) only when a user first
   enables the feature.
4. **Leaky abstraction.** Swarm task states leak into debugging. Skifity keeps Kubernetes nouns behind an
   "Advanced" view and translates events into plain language with a cause / impact / fix structure.
5. **Licensing.** Skifity is Apache-2.0 for everything, including multi-node and templates.
6. **Security posture.** CVEs in this category are usually missing authz on an endpoint or unencrypted
   credentials at rest. Skifity uses envelope encryption for every secret, deny-by-default authorization
   checks in a single middleware, and an audit log.

## Sources

- https://bex.co/blog/2026/07/28/self-hosted-paas-crowded-2026-coolify-dokku-caprover-dokploy
- https://cloudzy.com/blog/coolify-vs-dokploy/
- https://introserv.com/blog/dokploy-vs-coolify-complete-comparison-of-the-best-self-hosted-paas-platforms-for-vps-and-dedicated-servers-2026/
- https://dev.to/deploynix/self-hosted-paas-showdown-2026-coolify-vs-dokploy-vs-caprover-vs-deploynix-46l3
