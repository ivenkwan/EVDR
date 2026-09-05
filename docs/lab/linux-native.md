# Lab environment notes — native Linux host (repo relocated 2026-09-05)

**Scope:** deviations from `docs/runbooks/phase-0-foundation-rebuild.md` and the
Windows lab notes (`docs/lab/windows-docker-desktop.md`) for the CURRENT lab
host: native Linux (Docker Engine 29.x, no Docker Desktop/WSL), repo at
`/home/ivenkwan/repo/EVDR`. Anything not written here belongs in the runbook —
fix the doc, not the drift.

## 0. Host facts and toolchain

| Item | Value |
|---|---|
| Docker | `/usr/bin/docker` 29.5.3 (no sudo needed; user in docker group) |
| k3d | `~/.local/bin/k3d` v5.9.0 (GitHub release binary — install scripts need sudo, unavailable) |
| helm | `~/.local/bin/helm` v3.16.4 |
| vault CLI | `~/.local/bin/vault` v1.17.6 |
| go / pnpm | `~/.local/go/bin/go` 1.27.0, `~/.local/bin/pnpm` 11.24.0 |
| Host port 80 | **Occupied by an unknown host service** → k3d maps `8080:80` |
| Lab secrets dir | `/home/ivenkwan/.evdr-lab/` (chmod 700/600) — vault-init.json, vault-admin-token, vault-ca.crt, evdr-pki-ca.crt, vault-sa.jwt, per-service password files |

## 1. Cluster create (netpol contract: subnet pin REQUIRED)

The network policies (`src/infra/network/network-policies.yaml`) hardcode the
k3d node subnet `172.30.0.0/24` for post-DNAT API egress (see the comment in
that file). Creating the cluster on any other docker subnet **silently breaks
default-deny egress** (Vault pods time out reaching their own ClusterIP).
Exact invocation:

```bash
k3d cluster create evdr-lab --servers 3 \
  --image rancher/k3s:v1.32.5-k3s1 \
  --subnet 172.30.0.0/24 \
  --k3s-arg "--config=/etc/rancher/k3s/server-lab.yaml@server:*" \
  --volume /home/ivenkwan/repo/EVDR/src/infra/k3s/config/server-lab.yaml:/etc/rancher/k3s/server-lab.yaml@server:* \
  --port "8080:80@loadbalancer" --port "443:443@loadbalancer" \
  --api-port 6443
```

- Kubeconfig context `k3d-evdr-lab`; admin via kubectl directly (no sudo).
- First create attempt on the docker-default subnet failed exactly this way —
  fixed by deleting + recreating with `--subnet 172.30.0.0/24`.

## 2. What is applied (2026-09-05, Wave 3 state)

- Platform: cert-manager v1.17.1 (bootstrap CA), Vault 0.29.1 HA raft
  (init 5-of-3, audit `file/` enabled first, root token revoked, orphan
  `evdr-admin` token in the lab secrets dir), Traefik 33.2.1 with
  `allowCrossNamespace: true` (shared edge middlewares — required by app
  IngressRoutes) and Vault-PKI default cert.
- Data: PostgreSQL 16.15 + Redis 7.4 in `evdr-system` (lab variants on
  local-path — **Rook/ceph-block cannot provision on k3d**: no block devices,
  local-path is filesystem-only; Rook targets the VM/live lab).
- Identity/storage: Keycloak 26.7.2 (realm `evdr`, one-shot `--import-realm`
  bootstrap — do NOT leave the flag in args, it wipes runtime users on every
  boot), Nextcloud 34.0.3 (occ-installed; Redis locking + user_oidc provider
  configured post-install — see manifest comments).

## 3. Verification shortcuts (all from the host)

```bash
# Edge TLS chain (Vault-issued PKI): trust the lab CA + resolve the name to the LB
curl --cacert ~/.evdr-lab/evdr-pki-ca.crt --resolve keycloak.evdr.internal:443:127.0.0.1 \
     https://keycloak.evdr.internal/realms/evdr/.well-known/openid-configuration
# HTTP→HTTPS edge redirect (host 80 is busy; LB http is on 8080)
curl -sI http://127.0.0.1:8080/
```

No `/etc/hosts` edits (no root): use `--resolve`; in-cluster consumers need
CoreDNS hosts entries (not yet added — SSO browser smoke follow-up).

## 4. Vault ceremony after host/Docker restarts

Vault seals on container restarts. Unseal with 3 of the 5 shares from
`~/.evdr-lab/vault-init.json` against each pod's own address (NOT the ClusterIP
service — load-balancing across not-yet-joined peers yields
"not initialized" 400s):

```bash
kubectl -n vault exec vault-0 -- env VAULT_ADDR=https://127.0.0.1:8200 \
  VAULT_CACERT=/vault/userconfig/vault-tls/ca.crt vault operator unseal <share>
# repeat ×3 shares for vault-0/1/2; peers auto-join the raft leader
```

## 5. Ports and aliases on this host

| Service | Host endpoint |
|---|---|
| Traefik edge (https) | 127.0.0.1:443 (SNI `*.evdr.internal`) |
| Traefik edge (http, redirect only) | 127.0.0.1:8080 |
| Vault port-forward (day-to-day admin) | `kubectl -n vault port-forward svc/vault 8200:8200` |

GitLab CE + runner are NOT yet deployed on this host (registered follow-up —
container pattern in `docs/lab/windows-docker-desktop.md` §4–5, minus the
Windows socket quirks: native `/var/run/docker.sock` mount works).
