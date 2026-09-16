# Skifity architecture

## One sentence

A single Go binary runs inside a k3s cluster, talks to the Kubernetes API for everything that runs,
talks to SSH for everything that turns a bare VPS into a cluster member, and serves an embedded
React panel that never says the word "pod".

## Layers

```mermaid
flowchart TB
    subgraph browser["Browser / CLI / AI assistant"]
        UI["Panel (React 19 + shadcn/ui)"]
        CLI["skifity CLI"]
        MCP["MCP client"]
    end

    subgraph panel["skifity binary (one Pod in the cluster)"]
        API["HTTP API (chi)"]
        AUTH["Auth: sessions, TOTP, API tokens, RBAC"]
        CRYPTO["Envelope encryption"]
        STORE["SQLite (WAL) on a PVC"]
        EVENTS["SSE hub"]
        ORCH["Orchestrators: provision / build / deploy / database / backup"]
        KUBE["Kubernetes client (client-go)"]
        SSH["SSH client"]
    end

    subgraph cluster["k3s cluster"]
        APIsrv["kube-apiserver"]
        TRAEFIK["Traefik ingress"]
        CM["cert-manager"]
        REG["in-cluster registry"]
        BK["BuildKit (rootless)"]
        APPS["User apps"]
        DBS["CloudNativePG / Redis / MariaDB"]
    end

    VPS["Bare VPS being added"]

    UI --> API
    CLI --> API
    MCP --> API
    API --> AUTH --> ORCH
    ORCH --> STORE
    ORCH --> CRYPTO
    ORCH --> EVENTS --> UI
    ORCH --> KUBE --> APIsrv
    ORCH --> SSH --> VPS
    APIsrv --> TRAEFIK & CM & REG & BK & APPS & DBS
```

## Request path of a deploy

```mermaid
sequenceDiagram
    participant U as User / webhook
    participant A as Panel API
    participant D as Deploy orchestrator
    participant B as BuildKit
    participant R as Registry
    participant K as Kubernetes

    U->>A: Deploy app (commit sha)
    A->>D: create Deployment record (queued)
    D->>D: compute build fingerprint
    alt fingerprint unchanged
        D->>K: patch Deployment (rollout only)
    else new fingerprint
        D->>B: build job (Railpack or Dockerfile)
        B-->>D: log stream (SSE to the UI)
        B->>R: push image
        D->>K: apply Deployment/Service/Ingress/HPA
    end
    K-->>D: rollout status
    D-->>U: succeeded, or a plain-language failure with a suggested fix
```

## Adding a server

```mermaid
stateDiagram-v2
    [*] --> Connecting: SSH with password or key
    Connecting --> Fingerprint: record host key
    Fingerprint --> Preflight: OS, RAM, disk, ports, conflicts
    Preflight --> KeyInstall: install panel-owned key, forget the password
    KeyInstall --> Firewall: allow cluster ports from member IPs only
    Firewall --> Connectivity: verify both directions
    Connectivity --> Agent: install k3s agent and join
    Agent --> Ready: wait for node Ready, apply labels
    Ready --> [*]
    Connecting --> Failed
    Preflight --> Failed
    Firewall --> Failed
    Agent --> Failed
    Failed --> Connecting: Retry (every step is idempotent)
    Failed --> [*]: Cancel and clean up
```

## Data model

```mermaid
erDiagram
    TEAM ||--o{ MEMBERSHIP : has
    USER ||--o{ MEMBERSHIP : has
    TEAM ||--o{ PROJECT : owns
    PROJECT ||--o{ ENVIRONMENT : has
    ENVIRONMENT ||--o{ APP : contains
    ENVIRONMENT ||--o{ DATABASE : contains
    APP ||--o{ DEPLOYMENT : has
    APP ||--o{ DOMAIN : has
    APP ||--o{ VARIABLE : has
    APP ||--o{ VOLUME : has
    DATABASE ||--o{ BACKUP : has
    TEAM ||--o{ SERVER : owns
    TEAM ||--o{ AUDIT_EVENT : records
    USER ||--o{ API_TOKEN : owns
```

## Naming: what the user sees vs what Kubernetes sees

| Panel word | Kubernetes object |
|---|---|
| App | Deployment + Service + Ingress |
| Instance | Pod |
| Server | Node |
| Domain | Ingress rule + Certificate |
| Variable / Secret | ConfigMap / Secret |
| Database | CloudNativePG Cluster, or a StatefulSet |
| Environment | Namespace (`<team>-<project>-<env>`) |
| Volume | PersistentVolumeClaim |
| Scaling rule | HorizontalPodAutoscaler |

Kubernetes names appear in the "Advanced" tab of a resource and nowhere else.

## Isolation

Each environment is a namespace carrying a `ResourceQuota`, a `LimitRange` (so a forgotten limit cannot
starve a node), and a default-deny `NetworkPolicy` that permits ingress from Traefik and egress to DNS,
the internet and the environment's own namespace. The panel's ServiceAccount is the only identity with
cluster-wide rights; user-facing tokens are scoped through the panel's own RBAC, never by handing out
kubeconfigs.

## What is installed when

| When | What |
|---|---|
| `install.sh` | k3s server, panel, cert-manager, the panel's PVC and master key |
| First build | in-cluster registry + rootless BuildKit |
| First PostgreSQL | CloudNativePG operator |
| First Redis / MySQL | the matching chart |
| Scale-to-zero enabled | KEDA + http-add-on |
| Cross-node volumes enabled | Longhorn (with a RAM warning) |
| Full monitoring enabled | kube-prometheus-stack (optional; a built-in lightweight view exists without it) |

This is what keeps a fresh install small enough for a 2 GB VPS.
