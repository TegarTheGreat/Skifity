# What it costs

Numbers that were measured, and numbers that were not. Anything not marked
*measured* is an estimate and says so.

## The panel itself

**Measured** on this build, `linux/amd64`, with an empty database:

| | |
|---|---|
| Resident memory, idle | **34 MiB** |
| Resident memory, after 400 requests | **36 MiB** |
| Binary, stripped | 37 MiB |
| Frontend, gzipped over the wire | 278 KiB across five files |
| Cold start to answering `/api/health` | under a second |

The memory figure does not drift: 400 requests against the interface and the
API moved it by 1.5 MiB, and it did not come back down because Go's allocator
keeps the arena, not because anything leaked.

The binary is large because the whole user interface, in five languages, is
inside it, along with the Kubernetes client. That is the trade for having one
file to copy and nothing to install alongside it.

## The rest of a fresh install

**Not measured here.** A real cluster could never be started in the environment
this was built in, so these are the upstream projects' own figures for a
single-node install:

| | Roughly |
|---|---|
| k3s server, with embedded etcd | 500 MiB |
| Traefik, metrics-server, CoreDNS, local-path | included above |
| Skifity panel | 36 MiB, measured |
| **A fresh install, total** | **around 550 MiB** |

Which is why the requirement is 1 GB: the other 450 MiB is for your apps.

## Components, when you install them

Each is installed the first time it is needed, not at install time. The panel
shows the figure before you agree to it.

| Component | Roughly | Installed when |
|---|---|---|
| cert-manager | 120 MiB | You add a domain |
| Image registry | 60 MiB | You first deploy from source |
| Builder (BuildKit) | 200 MiB | You first deploy from source |
| CloudNativePG | 150 MiB | You create a PostgreSQL database |
| KEDA (scale to zero) | 180 MiB | You turn it on |
| Longhorn | 700 MiB **per server** | You turn it on |
| Prometheus and Grafana | 900 MiB | You turn it on |

A build is the most memory-hungry thing that happens, and it is transient: the
builder needs room to unpack and compile, and on a 1 GB server a large
JavaScript project will run out. The error says so, and says to add a server or
raise the build limit, rather than failing with an exit code.

## Reproducing the panel figures

```sh
make build
SKIFITY_DATABASE_PATH=/tmp/panel.db \
SKIFITY_MASTER_KEY_PATH=/tmp/master.key \
SKIFITY_LISTEN=127.0.0.1:8080 ./bin/skifity server &
grep VmRSS /proc/$!/status
```
