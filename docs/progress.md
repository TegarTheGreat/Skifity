# Progress

Read this file, `CLAUDE.md` and `docs/decisions.md` first if you are picking this project up cold.

## Current phase

Phase 1 - Repository foundation.

## Sandbox constraints (important)

The environment this was built in has Docker but **refuses privileged containers and
host-network relays**, so k3d/kind clusters and VMs could not be started. See ADR-0010.
Consequences for verification are tracked per phase below and in
`test/smoke/README.md`. Nothing is reported as verified unless it was actually executed.

## Phase status

| Phase | Title | Status |
|---|---|---|
| 0 | Research and key decisions | done |
| 1 | Repository foundation | in progress |
| 2 | One-command installer | not started |
| 3 | Panel core: accounts, security, encryption | not started |
| 4 | Server and cluster management | not started |
| 5 | App deployment | not started |
| 6 | Scaling | not started |
| 7 | Databases, storage, backups | not started |
| 8 | Developer experience and vibe coding | not started |
| 9 | Differentiating features | not started |
| 10 | Hardening and release | not started |

## Phase 0 - done

* `docs/research/competitors.md` - 13 products compared, weaknesses we design against.
* `docs/research/stack.md` - component versions verified 2026-09-16.
* `docs/architecture.md` - layers, deploy path, add-server state machine, data model, isolation.
* `docs/decisions.md` - ADR-0001 to ADR-0011.

## Next tasks

1. Go module, repo layout, Makefile, linters, CI.
2. Frontend scaffold: Vite + React 19 + Tailwind v4 + shadcn/ui, app shell, 5 languages.
3. Embed the built frontend in the binary; health endpoint; structured logging; config loading.

## Open issues

* Cluster smoke tests (1-4) cannot run in this sandbox. Scripts exist and are reviewed, not executed.

## Idle resource usage

Measured at the end of each phase; see the table in `docs/performance.md` once Phase 1 lands.
