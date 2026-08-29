# Hardened Nextcloud — EVDR document storage (Phase 1, TR-2.13)

> Workstream `harness/c-identity` (Wave 1, partition C). Manifests are
> **dry-run validated only** — the k3d lab is unreachable from this worktree
> (harness constraint). Apply happens in a later wave by the orchestrator.

## What this directory delivers

| File | Purpose |
|---|---|
| `nextcloud-deployment.yaml` | Deployment: `nextcloud:34.0.3-apache` (pinned), Postgres + Redis env wiring, non-root restricted-PSA security context, probes |
| `nextcloud-service.yaml` | ClusterIP Service (8080→80) — **internal only** |
| `nextcloud-certificate.yaml` | cert-manager `Certificate` from `vault-pki-issuer` (TLS, TR-2.13) |
| `nextcloud-pvc.yaml` | PVCs: `nextcloud-html` (5Gi) + `nextcloud-data` (20Gi) |
| `nextcloud-networkpolicy.yaml` | NetworkPolicy: same-namespace ingress only; Postgres/Redis egress only (TR-2.13 separation) |
| `nextcloud-secrets.example.yaml` | **Example only** — placeholder Secret shape; real values from Vault |
| `Todo.md` | Workstream-internal traceability to root `Todo.md` (un-ticked) |

## Design decisions

### 1. Namespace: `evdr-system`
Same rationale as Keycloak (`src/infra/network/namespaces.yaml`): Phase 1
application namespace, PSA `restricted` enforced. Nextcloud is an application
service, so the deployment must satisfy the restricted profile (non-root,
no privilege escalation, seccomp RuntimeDefault, capability drop-all —
the image runs as `www-data` uid 33 and binds :80 via `NET_BIND_SERVICE`).

### 2. Data layer: PostgreSQL 16 + Redis 7 via the shared data layer
Workstream B owns the stateful data plane. **Decision: reference it** — no
private StatefulSets here (avoids duplicate storage/backup/Vault wiring).

| Assumed service | Endpoint used here | Purpose |
|---|---|---|
| PostgreSQL 16 | `postgresql.storage.svc:5432` (db `nextcloud`) | primary backend (TR-2.11) |
| Redis 7 | `redis.storage.svc:6379` | distributed cache + **file locking** (TR-2.12/TR-2.13) |

The official image consumes `POSTGRES_*` / `REDIS_HOST*` env vars and writes
them into `config.php`, including `memcache.locking` → Redis (file locking,
TR-2.13) and `memcache.distributed`. Reconcile service names with workstream B
at merge.

### 3. TLS (TR-2.13)
- `nextcloud-certificate.yaml` requests `nextcloud-tls` from the existing
  Vault-backed `vault-pki-issuer` (repo conventions: CN-less, sorted SANs,
  ECDSA P-256, 720h/240h).
- **No edge route ships with this workstream** — Nextcloud is internal-only
  (§7). The certificate is ready for the approved moment an edge route or
  in-cluster TLS is enabled (the Traefik route would reference this secret;
  flip `NEXTCLOUD_TRUSTED_DOMAINS`/`OVERWRITEPROTOCOL` accordingly).

### 4. SSO connection to Keycloak via OIDC (TR-6.2)
Wiring uses the `user_oidc` app (Nextcloud's standard OIDC provider). The app
stores providers in its appconfig (not `config.php`), so the **configuration
is performed post-install with `occ`**, using the placeholder values carried
in the Deployment env (`NEXTCLOUD_OIDC_*`, documented as provisioning inputs
— the stock image does not consume them):

```bash
# 1. Install + enable the app (operator, post-deploy):
kubectl -n evdr-system exec deploy/nextcloud -- occ app:install user_oidc
kubectl -n evdr-system exec deploy/nextcloud -- occ app:enable user_oidc

# 2. Register the Keycloak realm as an OIDC provider (values from Vault:
#    secret/evdr/services/nextcloud/oidc — the client secret MUST match the
#    Keycloak realm's `nextcloud` client secret, rotated from Vault first):
kubectl -n evdr-system exec deploy/nextcloud -- occ user_oidc:provider --add \
  --identifier evdr \
  --client-id "nextcloud" \
  --client-secret "$(vault kv get -field=OIDC_CLIENT_SECRET secret/evdr/services/nextcloud/oidc)" \
  --discovery-uri "https://keycloak.evdr.internal/realms/evdr/.well-known/openid-configuration" \
  --scope "openid profile email"
```
- Keycloak side is pre-provisioned: the bootstrap realm ships the `nextcloud`
  OIDC client with matching placeholder secret
  (`src/infra/k8s/identity/keycloak-realm-bootstrap.yaml`).
- After this step, `occ user:report` shows federated principals
  (`principal: evdr`) and the Nextcloud login page gains the "Log in with
  EVDR" button. Keep the local bootstrap admin only for break-glass.
- Firewall note: Nextcloud→Keycloak is intra-namespace (both in
  `evdr-system`); the baseline egress already permits it, and
  `nextcloud-networkpolicy.yaml` does not restrict it further.

### 5. Network separation from external-facing services (TR-2.13)
- **By construction**: no IngressRoute exists for Nextcloud; the Room SPI
  (`src/spi`, AGENTS.md hard rule — never bypass the SPI) is the only
  documented consumer, reached via `nextcloud` ClusterIP service.
- **By policy**: `nextcloud-networkpolicy.yaml` allows ingress from
  same-namespace pods only, and egress only to the data layer (Postgres +
  Redis) beyond the namespace baseline.
- **Known gap, documented honestly**: the `evdr-system` baseline
  `allow-evdr-system-baseline` (`src/infra/network/network-policies.yaml`)
  grants the traefik namespace ingress to every pod in the namespace, and
  NetworkPolicies are additive — so traefik *could* reach Nextcloud at L3/L4.
  Closing that requires narrowing the baseline with a podSelector on
  `allow-evdr-system-baseline` — outside this workstream's partition; tracked
  as a follow-up for the orchestrator/apply wave. Real-world exposure is
  still zero because no edge route exists.

### 6. Backup strategy (TR-2.13, Phase 1 exit criterion "Nextcloud backup restore tested")
Components: metadata (Postgres), files (`nextcloud-data` PVC), config
(`nextcloud-html` PVC / `config.php`), plus the cluster-level snapshot storey
(NFR-3.4: "Postgres + Ceph daily automated snapshots; restore drills" —
cross-phase recurring activity).

| Layer | Mechanism | Frequency | Restore |
|---|---|---|---|
| PostgreSQL 16 | `pg_dump -Fc` (or Vault dynamic-creds `psql`) → Ceph RGW bucket via `rclone`/`restic` | daily + pre-upgrade | `pg_restore` into a fresh DB; `occ maintenance:mode --off` |
| File data | `occ maintenance:mode --on` → `restic backup /var/www/data` (or rsync) → Ceph RGW | daily incremental | `restic restore` to a fresh PVC |
| Config/app | `config.php` + `custom_apps` tar via restic | with file data | restore onto `nextcloud-html` PVC |
| Cluster | etcd snapshots (k3s, every 6h/28 retained) + Ceph RBD snapshots | per k3s config | runbook |

Procedure skeleton (operator runbook, Phase 1 apply wave):

```bash
kubectl -n evdr-system exec deploy/nextcloud -- occ maintenance:mode --on
# pg_dump from the data layer (workstream B) -> restic repo on Ceph RGW
# restic backup /var/www/data
kubectl -n evdr-system exec deploy/nextcloud -- occ maintenance:mode --off
```

Restore drill is the Phase 1 exit criterion (`Nextcloud backup restore
tested`) — owned by the orchestrator's apply wave; the manifests here only
make the data paths durable (PVCs) and documented. **No CronJob is shipped**
in this workstream: scheduled backup jobs belong with the data-layer/backup
workstream (B) to keep a single owner for the backup storey.

### 7. Secrets — Vault pattern, no literals
Same pattern as `src/infra/k8s/identity/` (README §6 there): Deployment
references Secret `nextcloud-secrets`; `nextcloud-secrets.example.yaml` shows
the shape. Provision:

```bash
vault kv put secret/evdr/services/nextcloud/db     POSTGRES_USER=nextcloud POSTGRES_PASSWORD=-
vault kv put secret/evdr/services/nextcloud/admin  NEXTCLOUD_ADMIN_USER=... NEXTCLOUD_ADMIN_PASSWORD=-
vault kv put secret/evdr/services/nextcloud/oidc   OIDC_CLIENT_SECRET=-
vault kv put secret/evdr/services/nextcloud/redis  REDIS_HOST_PASSWORD=-
kubectl -n evdr-system create secret generic nextcloud-secrets \
  --from-literal=POSTGRES_USER="$(vault kv get -field=POSTGRES_USER secret/evdr/services/nextcloud/db)" \
  --from-literal=POSTGRES_PASSWORD="$(vault kv get -field=POSTGRES_PASSWORD secret/evdr/services/nextcloud/db)" \
  --from-literal=REDIS_HOST_PASSWORD="$(vault kv get -field=REDIS_HOST_PASSWORD secret/evdr/services/nextcloud/redis)" \
  --from-literal=NEXTCLOUD_ADMIN_USER="$(vault kv get -field=NEXTCLOUD_ADMIN_USER secret/evdr/services/nextcloud/admin)" \
  --from-literal=NEXTCLOUD_ADMIN_PASSWORD="$(vault kv get -field=NEXTCLOUD_ADMIN_PASSWORD secret/evdr/services/nextcloud/admin)" \
  --from-literal=OIDC_CLIENT_SECRET="$(vault kv get -field=OIDC_CLIENT_SECRET secret/evdr/services/nextcloud/oidc)"
```
At scale: Vault Agent sidecar injector annotations (commented in the
Deployment) + dynamic DB creds at `database/creds/nextcloud`.

### 8. Hardening notes / accepted trade-offs
- `readOnlyRootFilesystem: false` — the stock image writes `config.php`,
  app updates and `/tmp` at runtime; a read-only rootfs needs a custom image
  build (follow-up, Phase 3 container hardening alongside the SPI sidecar).
- Root filesystem at rest is Ceph RBD (encrypted at rest per data
  classification; envelope encryption of documents is Phase 2, SR-1.1).
- No Collabora, no preview generation, no external apps — attack surface is
  kept minimal for the storage role (add via the SPI only, never by enabling
  broad Nextcloud apps; AGENTS.md "never modify Nextcloud core").
- Redis password is optional on the Tier 0 lab (ACL-less), required at
  Tier 1+ — env is wired for both (`optional: true` on the secret ref).

## Apply order (orchestrator / later wave)
1. Data layer (workstream B): Postgres 16 + Redis 7 in `storage`.
2. `kubectl -n evdr-system create secret generic nextcloud-secrets ...` (§7).
3. Keycloak (identity workstream) applied + realm imported.
4. `kubectl apply -f src/infra/k8s/nextcloud/` (Certificate, PVCs, Deployment/Service, NetworkPolicy).
5. Post-install: §4 OIDC `occ` step, §6 backup wiring, restore drill.

## Validation (this workstream, offline)
Every YAML in this directory passed `kubectl apply --dry-run=client
--validate=false -f <file>` (offline discovery from a local cache — the lab
is unreachable, so server-side validation is deferred to the apply wave) and
a `python3` YAML parse. See the commit message / final report for the paste.

## Requirement traceability
| Requirement | Where |
|---|---|
| TR-2.13 hardened Nextcloud (TLS, PG, Redis locking, backup, separation) | this directory |
| TR-2.11 PostgreSQL 16 | referenced data layer (`postgresql.storage.svc`) |
| TR-2.12 Redis 7 file locking | `REDIS_*` env → `memcache.locking` |
| TR-6.2 Nextcloud ← Keycloak OIDC | §4 + identity realm `nextcloud` client |
| TR-1.5 / SR-1.2 TLS | certificate from `vault-pki-issuer` |
| SR-3.1 network isolation | NetworkPolicy + no-edge-by-construction |

## Left-outs / follow-ups
- Edge route for Nextcloud — deliberately absent; review gate before any
  exposure (also closes with the Room SPI client work, workstream A).
- Narrowing `allow-evdr-system-baseline` (traefik→evdr-system ingress) —
  `src/infra/network/`, apply wave.
- Read-only rootfs image build; Redis ACL enforcement; Vault dynamic DB
  creds; object-storage offload to Ceph RGW (primary store decision).
- Backup CronJob + restic repo provisioning — data-layer/backup workstream.
- Nextcloud version pin policy: 34.0.x current stable line; bumps are
  deliberate PRs (33.x available as fallback).
