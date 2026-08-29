# Redis 7 — EVDR cache, sessions, and file locking (TR-2.12)

Serves Nextcloud file locking, application caching, session management, and rate
limiting. This manifest deploys the engine; per-service logical databases/keyspaces are
an application-layer concern.

## Files

| Path | Purpose |
|---|---|
| `secret.yaml` | Placeholder ACL file; real password in Vault at `secret/evdr/services/redis` |
| `configmap.yaml` | `redis.conf` baseline (persistence, memory policy, binding) |
| `service.yaml` | Headless Service (`redis.evdr-system.svc`) |
| `statefulset.yaml` | StatefulSet, `redis:7.4.11` digest-pinned, AOF volume |

## Design decisions

- **StatefulSet + PVC (AOF), not a Deployment.** Sessions and locks must survive pod
  restarts; a Deployment with emptyDir would lose both. AOF `everysec` balances durability
  with write amplification. RDB snapshots disabled (`save ""`) — AOF is the single
  persistence path, keeping recovery deterministic.
- **Auth via ACL file, not `requirepass`.** The ACL file is mounted from the Secret
  (`/etc/redis/users.acl`) and passed with `--aclfile`. The password never appears in
  manifests, args, or env. Redis 7 ACLs also let us scope the default user's command
  categories later without changing the auth mechanism.
- **`bind 0.0.0.0` is required** — the stock image default binds 127.0.0.1, which would
  make the service unreachable from other pods. Auth (ACL) is the boundary; the pod's
  network namespace is already isolated by the cluster overlay.
- **`maxmemory-policy noeviction`** — safe default for a multi-use instance: eviction
  could silently drop session tokens or Nextcloud lock keys. If a dedicated cache-only
  instance is added later, `allkeys-lru` becomes appropriate there.
- **`restricted` PSS compliance** (evdr-system): `runAsUser 999` (image's `redis` user),
  `fsGroup 999` for the AOF volume, `readOnlyRootFilesystem: true`, `seccompProfile:
  RuntimeDefault`, `capabilities: drop ALL`.
- **TCP probes, not `redis-cli ping`** — with ACL auth active, `redis-cli ping` without
  credentials answers `NOAUTH`, which would wedge readiness. Port-open probes are the
  honest signal here.

## TLS (SR-1.2) — staged hardening

Redis 7 supports native TLS (`tls-port`, `tls-cert-file`). In-cluster TLS is staged with
the data-plane certificate pass (see `../postgresql/README.md`); traffic is confined to
the overlay with default-deny network policies until then.

## Operations notes

- Storage: 1Gi default for AOF; sessions under load may need more — size per environment.
- No external password rotation knob yet: the ACL file is replaced by the operator
  Vault-sync step, and `CONFIG REWRITE` is not used (config comes from the mounted file).
