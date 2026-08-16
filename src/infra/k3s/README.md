# K3s Platform Baseline (TR-1.1)

K3s is the container runtime for Tier 0/small cells; the same manifests target full K8s at scale. This directory holds the pinned, hardened cluster configuration and install scripts. The full build order is in `docs/runbooks/phase-0-foundation-rebuild.md`.

## Files

| Path | Purpose |
|---|---|
| `config/server.yaml` | Hardened K3s server config (etcd, admission, audit, TLS ciphers) |
| `config/agent.yaml` | Agent config (only needed when workers are separate from servers) |
| `scripts/install-server.sh` | Bootstrap first server (`init`) or join HA servers (`join`) |
| `scripts/install-agent.sh` | Join a worker node |

## Security properties (what and why)

- **`secrets-encryption: true`** — etcd at-rest encryption for K8s Secrets from first boot (retrofitting requires re-encryption; doing it at bootstrap is free).
- **Embedded Traefik disabled** — we deploy our own Traefik with a strict, version-controlled TLS policy (TR-1.5, `src/infra/traefik`). Two ingress controllers fighting over :443 is an incident, not a choice.
- **local-storage disabled** — Rook/Ceph provides storage classes in Phase 1 (TR-2.5); the default provisioner would create un-managed persistence.
- **TLS cipher suites pinned** to TLS 1.2/1.3 strong suites (SR-1.2) for apiserver/kubelet/etcd. (TLS 1.3 suites are fixed by the Go runtime and need no listing.)
- **API server audit logging** to `/var/lib/rancher/k3s/server/logs/audit.log`, rotated; shipped to Loki in Phase 1 (TR-7.5).
- **`NodeRestriction` admission plugin** — kubelets may only mutate their own node/pod objects.
- **`protect-kernel-defaults`** — kubelet refuses to start on unsafe kernel tuning rather than silently weakening it.
- **etcd snapshots every 6h, 28 retained** — cluster state recovery point preceding the full backup storey (NFR-3.4).
- **kubeconfig mode 0600** — root-only read on nodes.
- **Network policy:** K3s ships an embedded network-policy controller; the baseline policies in `src/infra/network/` are enforced from the moment they are applied.

## Node firewall matrix (allow, all other inter-node traffic denied)

| Port | Proto | Source | Purpose |
|---|---|---|---|
| 6443 | TCP | nodes, operator subnets | K8s API |
| 2379–2381 | TCP | server nodes | etcd client/peer/metrics |
| 8472 | UDP | all nodes | flannel VXLAN |
| 10250 | TCP | all nodes | kubelet |
| 51820 | UDP | all nodes | (only if flannel-wireguard backend is adopted) |

## Versions

K3s is pinned via `K3S_VERSION` in the install scripts (default recorded there). Upgrades are deliberate PRs: bump the pin, rebuild the lab per the runbook, then roll real nodes.
