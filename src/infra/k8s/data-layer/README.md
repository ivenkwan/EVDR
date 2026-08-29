# EVDR Phase 1 — Data & Storage Layer (K3s manifests)

Compliance-grade baseline for the Phase 1 data plane, per `Todo.md` **Data & Storage Layer**
build activities. This directory is the single source for the object store, relational
database, and cache that the Room SPI adapters (NativeAdapter: S3 + PostgreSQL, Redis)
program against.

## Components and requirement traceability

| Component | Version | Namespace | Traceability |
|---|---|---|---|
| [PostgreSQL](./postgresql/) | 16.15 (digest-pinned) | `evdr-system` | TR-2.11 (backing store: Nextcloud, policy engine, audit pipeline, room metadata, viewer telemetry) |
| [Redis](./redis/) | 7.4.11 (digest-pinned) | `evdr-system` | TR-2.12 (file locking, app caching, sessions, rate limiting) |
| [Ceph RGW via Rook](./rook/) | Rook 1.19.10 · Ceph Squid v19.2.6 | `storage` | TR-2.5 (S3 API, SigV2+V4, versioning, lifecycle, bucket notifications) |

Namespace placement follows `src/infra/network/namespaces.yaml`: `storage` was pre-created
for Rook/Ceph (platform scope, PSA `baseline` — Rook needs elevated privileges); application
data services live in `evdr-system` (application scope, PSA `restricted`) with the application
workloads they serve.

## Apply order

```bash
# 0. Prerequisites from Phase 0 (already applied in the cluster):
#    src/infra/network/namespaces.yaml  (creates `storage` and `evdr-system`)

# 1. Object storage (TR-2.5) — operator first, then the cluster
kubectl apply -f rook/crds.yaml
kubectl apply -f rook/common.yaml
kubectl apply -f rook/operator.yaml
kubectl apply -f rook/ceph-cluster.yaml        # wait for HEALTH_OK: kubectl -n storage get cephcluster rook-ceph
kubectl apply -f rook/ceph-block-pool.yaml     # block pool backing the ceph-block StorageClass
kubectl apply -f rook/storageclass-ceph-block.yaml
kubectl apply -f rook/ceph-object-store.yaml   # RGW gateway (S3)
kubectl apply -f rook/ceph-object-user.yaml    # S3 service user (keys land in a K8s secret, sync to Vault)
# Optional per-bucket policy CRDs (TR-2.5):
kubectl apply -f rook/bucket-topic.yaml
kubectl apply -f rook/bucket-notification.yaml

# 2. Relational store (TR-2.11)
kubectl apply -f postgresql/secret.yaml postgresql/service.yaml postgresql/statefulset.yaml

# 3. Cache / sessions (TR-2.12)
kubectl apply -f redis/secret.yaml redis/configmap.yaml redis/service.yaml redis/statefulset.yaml
```

## Cross-cutting conventions (inherited from Phase 0)

- **Secrets are placeholders, values live in Vault.** Every Secret below carries
  `CHANGE_ME` placeholder material and a comment naming the Vault KV path
  (`secret/evdr/services/<service>`, readable via the `evdr-service-template.hcl` policy).
  An operator step syncs Vault → K8s Secret (or the consuming service reads Vault directly).
  Never commit real credentials.
- **Images are digest-pinned** (`@sha256:…`) for reproducible, Trivy-scannable supply chain;
  tags are recorded in each README for readability.
- **TLS:** in-cluster data-plane TLS (Postgres `ssl`, Redis `tls-port`, Ceph msgr2
  encryption) is deliberately staged as a Phase 1 hardening pass once the Vault-issued
  certificate flow for data services is wired; see per-component READMEs. The overlay
  network (flannel VXLAN) and `NetworkPolicy`-scoped namespaces are the Phase 1 boundary.
- **Pod Security Standards:** `postgresql/` and `redis/` comply with the `restricted`
  enforcement of `evdr-system`; `storage` runs `baseline` for Rook daemonsets (documented
  exception in `namespaces.yaml`).

## What is deliberately NOT in this directory

- `NetworkPolicy` objects — the per-namespace baseline lives in
  `src/infra/network/network-policies.yaml`. `storage` currently has no default-deny
  policy; adding one plus Rook-specific allow rules is tracked with the Phase 1
  networking workstream. `evdr-system` is default-deny on ingress; per-service
  allow rules (e.g. policy-engine → postgres) ship with the services that need them.
- Prometheus/Grafana wiring — the observability workstream (TR-11.1) owns ServiceMonitors.
- Tenant bucket provisioning (OBC / per-tenant `CephObjectStoreUser`) — application-layer
  work in Phase 1; the RGW and store CRs here are the substrate it builds on.
- Nextcloud (TR-2.13) and Keycloak (TR-6.x) — separate build activities.

## Verification (offline, no cluster required)

```bash
# YAML parse check (every file)
python3 -c 'import yaml,sys; list(yaml.safe_load_all(open(sys.argv[1])))' <file>

# kubectl client-side validation — core kinds validate standalone:
kubectl apply --dry-run=client -f postgresql/statefulset.yaml
# ceph.rook.io kinds resolve via the CRDs parsed earlier in the SAME invocation:
kubectl apply --dry-run=client -f rook/crds.yaml -f rook/ceph-cluster.yaml
```
