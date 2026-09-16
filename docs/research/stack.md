# Research: stack and component versions

Verified 2026-09-16. Every version here is a *floor*, not a pin: the panel reads the
actual version to install from configuration so an operator can move forward without
waiting for a Skifity release. See `docs/deployment-config.md`.

## Cluster

| Component | Version found | Notes |
|---|---|---|
| k3s | v1.36.x (Kubernetes 1.36 landed 2026-05-27; 1.35 in 2026-01) | We pin a *channel* (`stable`) by default rather than a version, which is what `get.k3s.io` expects. |
| Flannel backend | `wireguard-native` | Encrypts pod-to-pod traffic between VPSes of different providers. Requires the `wireguard` kernel module; the preflight checks for it and falls back to `vxlan` with a visible warning. |
| Embedded etcd | `--cluster-init` on the first server | Makes the single node HA-ready later without a rebuild. Without it, k3s uses SQLite and cannot be promoted. |
| Traefik | bundled with k3s | Kept. Removing it to install ingress-nginx costs RAM and buys little. |
| metrics-server | bundled with k3s | Needed by HPA and by the server dashboard. |
| local-path-provisioner | bundled with k3s | Default StorageClass for single-node. |

### k3s flags we use

Server (first node):

    --cluster-init
    --flannel-backend=wireguard-native
    --write-kubeconfig-mode=0644
    --tls-san=<public ip>
    --node-external-ip=<public ip>       # required when nodes are on different providers
    --node-label skifity.io/role=control-plane

Agent:

    K3S_URL=https://<server ip>:6443 K3S_TOKEN=<token>
    --node-external-ip=<public ip>
    --node-label skifity.io/managed=true

Ports that must be open *between cluster members only*: 6443/tcp (API), 10250/tcp (kubelet),
8472/udp (flannel vxlan) or 51820/udp + 51821/udp (wireguard-native, IPv4/IPv6),
2379-2380/tcp (etcd, control-plane nodes only). 80/443 are public.

## Add-ons (installed on demand, never at install time)

| Component | Version found | Installed when |
|---|---|---|
| cert-manager | v1.21.2 (2026-09-11) | At install (small, and HTTPS is a day-one promise) |
| CloudNativePG | 1.29 | First PostgreSQL database is created |
| KEDA + http-add-on | KEDA 2.20, http-add-on 0.15 (beta) | Scale-to-zero is first enabled. Marked **Beta** in the UI because the add-on is beta upstream. |
| Longhorn | latest stable | Cross-node volumes are first enabled; warns about the RAM cost |
| Redis / MariaDB | Bitnami-style charts rendered by us | First database of that type is created |

## Builds

Railway replaced Nixpacks with **Railpack** (Go, BuildKit-native, March 2026); Nixpacks is in
maintenance mode with critical fixes only. Railpack reports 38% (Node) to 77% (Python) smaller
images and better cache hits because it drives BuildKit layers directly. Coolify has already
adopted it as a build pack.

**Decision:** Railpack as the default builder, Dockerfile when the repo has one, and a
"prebuilt image" path. Nixpacks is kept as a selectable fallback because Railpack is younger.
Builds run in-cluster on rootless BuildKit, and push to an in-cluster registry.

## Panel

| Concern | Choice | Reason |
|---|---|---|
| Language | Go 1.24+ | Single static binary, good Kubernetes and SSH libraries |
| Kubernetes client | `k8s.io/client-go` v0.34+ | client-go tags track Kubernetes: v0.NN matches v1.NN |
| Database | SQLite through `modernc.org/sqlite` | Pure Go, so `CGO_ENABLED=0` cross-compiles cleanly. The panel is a single writer; WAL mode is enough. Postgres would mean bootstrapping a database before the platform that creates databases exists. |
| HTTP | net/http + `chi` router | Standard library shaped, no framework lock-in |
| Realtime | Server-Sent Events | One-directional (progress, logs) is all we need; survives proxies better than WebSocket and needs no upgrade handling |
| Secrets | AES-256-GCM envelope encryption | FIPS-friendly, in the standard library, hardware accelerated |
| MCP | `github.com/modelcontextprotocol/go-sdk` v1.x (v1.0 froze the API; v1.7.0 supports spec 2026-07-28) | Official SDK, stable API guarantee |

## Frontend

* React 19 + TypeScript + Vite + Tailwind CSS v4 + **shadcn/ui** (new-york style, `data-slot`
  primitives, `sonner` for toasts since the old `toast` component is deprecated).
* i18n: **react-i18next**. Key-based, one JSON file per language, native plural handling through
  `Intl.PluralRules` (which is what Russian needs: one/few/many), ~2.8M weekly downloads, and it
  does not need a Babel/SWC macro step the way Lingui does. Lingui's build-time completeness check
  is the one thing it has over i18next, so we reimplement that as a standalone script
  (`web/scripts/check-i18n.mjs`) wired into `npm run build` and CI.
* Fonts: Inter (Latin/Cyrillic) + Noto Sans Devanagari + Noto Sans SC, self-hosted so the panel
  works on a VPS without outside network access.

## Sources

- https://docs.k3s.io/release-notes/v1.36.X
- https://docs.k3s.io/blog/2026/05/27/K3s-1.36-release
- https://cert-manager.io/docs/releases/
- https://cloudnative-pg.io/releases/cloudnative-pg-1-29.0-released/
- https://keda.sh/http-add-on/0.15/
- https://blog.railway.com/p/introducing-railpack
- https://github.com/railwayapp/railpack
- https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.7.0
- https://ui.shadcn.com/docs/tailwind-v4
- https://tolgee.io/blog/react-i18n-libraries-comparison
