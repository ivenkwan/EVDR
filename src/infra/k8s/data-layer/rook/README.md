# Ceph RGW via Rook — EVDR object storage (TR-2.5)

S3-compatible object storage for all tiers, primary backing for the NativeAdapter
(S3 + PostgreSQL + Redis). Rook 1.19.10 operator + Ceph Squid v19.2.6, deployed in the
`storage` namespace (pre-created in `src/infra/network/namespaces.yaml` with PSA
`baseline` — Rook daemons need privileges `restricted` cannot grant).

## TR-2.5 requirement coverage

| Requirement | Where it is satisfied |
|---|---|
| S3 API | `ceph-object-store.yaml` — `protocols.enableAPIs: [s3, admin, notifications]` (swift/sts/iam/s3website disabled: attack-surface reduction) |
| SigV2 + V4 | RGW accepts both v2 and v4 signatures by default; nothing to enable. RGW lives behind the cluster Service `rook-ceph-rgw-evdr-rgw.storage.svc` |
| Versioning | Native S3 API (`PUT ?versioning`) — RGW supports it out of the box; per-bucket policy is an app/tenant concern |
| Lifecycle | Native S3 API (`PUT ?lifecycle`). The S3 lifecycle events (`s3:ObjectLifecycle:*`) are also wired into notifications |
| Bucket notifications | `bucket-topic.yaml` + `bucket-notification.yaml` (Rook `CephBucketTopic`/`CephBucketNotification` CRDs, RGW `notifications` API enabled) |

## Files

| Path | Source / purpose |
|---|---|
| `crds.yaml` | Rook v1.19.10 CRDs, verbatim from upstream (sha256 `10e754b7…`; source `https://raw.githubusercontent.com/rook/rook/v1.19.10/deploy/examples/crds.yaml`) |
| `common.yaml` | Upstream RBAC + CSI resources; namespace re-targeted `rook-ceph → storage`; the upstream `Namespace` object was removed (the `storage` namespace already exists) |
| `operator.yaml` | Upstream operator Deployment + config; namespace re-targeted; K3s-specific and single-node adaptations (below) |
| `ceph-cluster.yaml` | CephCluster CR (Squid v19.2.6, digest-pinned) |
| `ceph-block-pool.yaml` | `evdr-block` replicated pool — block storage class for Postgres/Redis PVCs (TR-2.11/2.12 glue) |
| `storageclass-ceph-block.yaml` | csi-rbd StorageClass bound to `evdr-block` |
| `ceph-object-store.yaml` | CephObjectStore CR — RGW gateway |
| `ceph-object-user.yaml` | S3 service user for the platform (keys are operator-synced to Vault) |
| `bucket-topic.yaml`, `bucket-notification.yaml` | TR-2.5 notification examples (placeholder endpoint) |

## Operator adaptations vs upstream (`operator.yaml`)

- `ROOK_CSI_KUBELET_DIR_PATH=/var/lib/k3s/kubelet` — K3s hosts kubelet there (full K8s:
  omit).
- `CSI_PROVISIONER_REPLICAS: "1"` — single-node Phase 1 baseline; HA clusters set `"2"`.
- `ROOK_CSI_ENABLE_CEPHFS: "false"` — Phase 1 data layer is RBD-only; re-enable when a
  CephFS consumer (e.g. Nextcloud) is adopted.
- `ROOK_OPERATOR_METRICS_BIND_ADDRESS: "0"` (upstream default kept) — Prometheus
  integration ships with the observability workstream (TR-11.1).

## CephCluster design decisions (`ceph-cluster.yaml`)

- **Kernel 7.0 cephx ciphers:** hosts run Linux 7.0, so `security.cephx.allowedCiphers:
  [aes256k]` is set and no `keyType: aes` override — exactly the guidance embedded in
  the upstream manifest for 7.0+ installs.
- **Raw-device OSDs via `deviceFilter`** (upstream production pattern): `useAllNodes:
  true` + `useAllDevices: false` + `deviceFilter: "^vd[b-z]"` (libvirt virtio naming).
  The OS disk (`vda`) is excluded. **Operators must confirm the device naming in the
  target environment** (bare-metal: `^sd[b-z]`; NVMe: `^nvme[0-9]n[0-9]`) and attach
  dedicated data disks — `server.yaml` disables local-storage precisely so Ceph owns
  persistence. Cloud/managed-K8s alternative: OSDs on PVCs (`storageClassDeviceSets`),
  see upstream `deploy/examples/cluster-on-pvc.yaml`.
- **mons/mgrs on a single node:** `mon.count: 3` and `mgr.count: 2` with
  `allowMultiplePerNode: true` — this is the Phase 1 lab baseline (one node). On a
  ≥3-node production cell, set `allowMultiplePerNode: false` so each mon/mgr lands on a
  distinct node (Rook's own recommendation; PDBs are managed via
  `disruptionManagement.managePodBudgets`).
- **msgr2-only (`requireMsgr2: true`)** — disables the legacy v1 protocol port (6789);
  all clients (ceph-csi, RGW) support v2 on kernel 7.0. Wire encryption
  (`network.connections.encryption`) is staged with the data-plane TLS pass.
- **Pinned Ceph image** `quay.io/ceph/ceph:v19.2.6@sha256:0e13b880…` — `allowUnsupported:
  false`.
- Dashboard enabled (SSL, no ingress; operator-only via `kubectl port-forward`);
  `monitoring.enabled: false` until TR-11.1.

## Secrets and Vault (Phase 0 pattern)

- `ceph-object-user.yaml` holds **no credentials** — Rook generates the S3 keys into the
  K8s Secret `rook-ceph-object-user-evdr-rgw-evdr-s3` in `storage`. The operator syncs
  those values into Vault (`secret/evdr/services/ceph-rgw`) so consumers never read the
  K8s secret directly (evdr-service-template policy grants the path).
- The upstream commented `security.kms` block (SSE-KMS with Vault) is intentionally left
  commented: it needs a Vault token secret and the Vault KMS backend wiring, which is a
  Phase 1 hardening pass (AGENTS.md §12 secondary at-rest layer).

## Verification

```bash
# YAML parse (every file)
python3 -c 'import yaml,sys; list(yaml.safe_load_all(open(sys.argv[1])))' <file>
# kubectl client-side validation — CR-backed files need the CRDs parsed first in the
# SAME invocation (no cluster required):
kubectl apply --dry-run=client -f crds.yaml -f ceph-cluster.yaml
```
