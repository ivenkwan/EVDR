# PostgreSQL 16 — EVDR relational store (TR-2.11)

Backing store for Nextcloud, the policy engine, the append-only audit pipeline, room
metadata, and viewer telemetry. PostgreSQL RLS with `tenant_id` is the primary tenant
isolation mechanism (SR-2.2, `Todo.md` Database Rules) — this manifest provides the
engine; the schema/RLS policies ship with the services.

## Files

| Path | Purpose |
|---|---|
| `secret.yaml` | Placeholder credentials; real values live in Vault at `secret/evdr/services/postgresql` |
| `service.yaml` | Headless Service (`postgres.evdr-system.svc`) — DNS resolves to pod IPs |
| `statefulset.yaml` | StatefulSet, `postgres:16.15` digest-pinned, PVC via `volumeClaimTemplates` |

## Design decisions

- **StatefulSet + headless Service, 1 replica.** Postgres is stateful by definition; the
  headless service gives stable DNS for a future read-replica set (TR-2.11: "read replicas
  at scale"). Primary connection strings come from the app layer, not this manifest.
- **PVC via `volumeClaimTemplates`** on `storageClassName: ceph-block` (Rook RBD, see
  `../rook/storageclass-ceph-block.yaml`). In the Phase 1 lab without Ceph, override to
  `local-path` (the lab profile keeps Rancher's local-path provisioner — see
  `src/infra/k3s/config/server-lab.yaml`).
- **`restricted` PSS compliance** (evdr-system enforces it): `runAsNonRoot`/`runAsUser
  999` (the image's `postgres` user), `fsGroup 999` for the PVC, `seccompProfile:
  RuntimeDefault`, `capabilities: drop ALL`, `allowPrivilegeEscalation: false`.
- **Secrets never in env or manifests:** `POSTGRES_USER/PASSWORD/DB` come from the
  placeholder Secret, which an operator step syncs from Vault (`secret/evdr/services/
  postgresql`; the `evdr-service-template.hcl` policy grants `secret/data/evdr/services/
  <SERVICE>/*`). The password is set once by the initdb run; rotating it later is an
  operator procedure (documented in the Phase 1 runbook), not a manifest concern.
- **Probes:** `pg_isready` (no password needed) for readiness/liveness.
- **Audit posture:** this instance hosts the append-only audit pipeline; the audit
  schema enforces append-only at the database level (no UPDATE/DELETE privileges on
  audit tables) — enforced by the services' migrations, per AGENTS.md §8.

## TLS (SR-1.2) — staged hardening

In-cluster Postgres TLS is staged: the image supports `ssl: on` with a server cert, and
Phase 0's Vault-backed `ClusterIssuer` (`cluster-issuer-vault.yaml`) can mint one. A
follow-up pass will mount a cert-manager-issued certificate and flip `ssl: on` +
`ssl_min_protocol_version: TLSv1.2`; this manifest intentionally ships without it so the
first deploy is not blocked on certificate wiring. Until then, traffic is confined to the
cluster overlay with default-deny network policies.

## Operations notes

- Storage: 10Gi default — size per environment (audit tables grow fast; plan capacity).
- `PGDATA=/var/lib/postgresql/data/pgdata` (subdir keeps the mount point clean).
- Backups: snapshot/RDS-style PITR is the Phase 1 backup workstream (NFR-3.4); the
  WAL/`pg_basebackup` story ships with it.
