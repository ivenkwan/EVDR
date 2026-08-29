# EVDR Agent Harness — Deployment Map

> **Owner:** Iven Kwan | **Deployed:** 2026-08-29 | **Mode:** Agentic harness (parallel workstreams, wave-by-wave)
>
> The harness executes `Todo.md` phase-by-phase via isolated agent workstreams.
> Each workstream = a git branch + a dedicated worktree. The orchestrator
> (Hermes) verifies every workstream's evidence, merges, and ticks Todo.md —
> **workers never tick Todo.md**; exit criteria are verified by the orchestrator.

## Operating protocol

1. **Wave** = a batch of independent workstreams, dispatched in parallel.
2. Each workstream gets: a branch (`harness/<id>-<slug>`), a worktree
   (`~/EVDR-work/<id>`), a file partition (no overlap ⇒ no merge conflicts),
   and a verification contract (build/tests/dry-run must pass before push).
3. Orchestrator merges branches into `main` in dependency order, runs the
   full verification suite again post-merge, then ticks `Todo.md` activities
   with evidence.
4. **Todo.md is the live task registry.** Every new task requested by the user
   is appended to `Todo.md` (with FR/TR/SR traceability where applicable)
   before or as it is executed. Todo.md is kept current at all times — ticks
   happen with evidence, new tasks land in the relevant phase or a dedicated
   section.
5. Every merged change must keep Phase 0 gates green: SPI contract frozen
   (`ContractVersion = "0.1.0"`), Go module builds, CI semantics preserved.

## Wave 1 — Phase 1 unblocking workstreams

| ID | Workstream | Branch | Partition (own files only) | Verification |
|---|---|---|---|---|
| A | NextcloudAdapter + SPI conformance suite (Go, TDD) | `harness/a-nextcloud-adapter` | `src/spi/` (new files only; contract frozen) | `go build ./... && go test ./... -race && go vet ./...` |
| B | Data layer manifests: PostgreSQL 16, Redis 7, Ceph Rook RGW | `harness/b-data-layer` | `src/infra/k8s/data-layer/` (new) | `kubectl apply --dry-run=client -f` per manifest |
| C | Identity + storage manifests: Keycloak, hardened Nextcloud | `harness/c-identity` | `src/infra/k8s/identity/`, `src/infra/k8s/nextcloud/` (new) | `kubectl apply --dry-run=client -f` per manifest |
| D | Next.js 15 portal scaffold (portal + admin apps) | `harness/d-portal` | `src/frontend/` (new) | `pnpm install && pnpm build` green |

Todo.md traceability (Phase 1 activities): A → TR-2.1/TR-2.2/TR-2.4 · B → TR-2.5/TR-2.11/TR-2.12 ·
C → TR-2.13/TR-6.1/TR-6.2 · D → TR-3.1/TR-3.2/TR-3.3/TR-3.4.

## Wave 1 status

- [x] Toolchain provisioned: Go 1.27.0 (`~/.local/go`), pnpm 11.24.0 (`~/.local/bin`)
- [ ] Workstream A merged + verified (NextcloudAdapter + conformance suite)
- [ ] Workstream B merged + verified (data layer manifests)
- [ ] Workstream C merged + verified (identity + Nextcloud manifests)
- [ ] Workstream D merged + verified (portal scaffold)
- [ ] Todo.md Phase 1 activities ticked with evidence

## Out of scope (this deployment)

- Lab cluster operations (k3d lab unreachable — manifests are dry-run validated only, apply happens in a later wave)
- Feature-complete frontend (Wave 1 = scaffold baseline only)
- Phase 2+ workstreams
