# Phase 0 Runbook — Tear-Down / Rebuild of the Foundation

This runbook is the documented procedure for the Phase 0 exit criterion: *"IaC baseline reproducible — full tear-down/rebuild of K3s + Vault + Traefik from code."* Every step is from version-controlled artifacts; if you find yourself doing something not written here, the runbook is wrong — fix the runbook.

**Target outcome:** 3-node (or 1-node lab) K3s cluster with cert-manager, Vault (HA, TLS, bootstrapped), Traefik edge with automatic TLS 1.2/1.3, and the network isolation baseline — verifiably working, then torn down and rebuilt to prove reproducibility.

## 0. Prerequisites

| Item | Source |
|---|---|
| Hypervisor with libvirt + Terraform ≥ 1.7, Helm ≥ 3.16, kubectl, vault CLI | operator workstation |
| Ubuntu 24.04 cloud image on the hypervisor | `base_image` tfvar |
| Admin SSH key pair | operator |
| This repo checked out | `git clone` |
| Chart pins | recorded in §2 of each infra README; add repos: `helm repo add hashicorp https://helm.releases.hashicorp.com && helm repo add jetstack https://charts.jetstack.io && helm repo add traefik https://traefik.github.io/charts && helm repo update` |

Environment variables used throughout — set from your shell or a secrets manager; **never** write values into repo files:

```bash
export KUBECONFIG=~/.kube/evdr-t0-lab.yaml
# Vault (set later, at step 5):
# export VAULT_ADDR=https://vault.vault.svc:8200
# export VAULT_TOKEN=<root token from init — bootstrap use only>
```

## 1. Provision VMs (Terraform)

```bash
cd src/infra/terraform/live/tier0-lab
cp terraform.tfvars.example terraform.tfvars   # edit: base_image, ssh_authorized_keys
terraform init
terraform apply -var-file=terraform.tfvars
terraform output node_ips                       # note: node 0 = initial server
```

**Verify:** `ssh evdr-admin@<node0-ip> 'uname -a'` works; swap is off (`swapon --show` empty); data disks visible (`lsblk` shows the 200G volume — Rook consumes it in Phase 1).

## 2. Install K3s

```bash
# On node 0 (initial server):
export K3S_TOKEN="$(openssl rand -hex 32)"       # bootstrap-only; stored in Vault at step 6
sudo -E src/infra/k3s/scripts/install-server.sh init

# On nodes 1 and 2 (HA join):
sudo -E src/infra/k3s/scripts/install-server.sh join <node0-ip>
```

Copy `/etc/rancher/k3s/k3s.yaml` from node 0 to `$KUBECONFIG` and point the server address at node 0's IP.

**Verify:** `kubectl get nodes` → 3 `Ready`, `control-plane,master`. `kubectl -n kube-system get pods` all Running. Confirm secrets encryption: `kubectl get secret -A -o jsonpath='{.items[0].metadata.name}'` round-trip works and etcd contains ciphertext (spot-check per K3s docs).

## 3. Network isolation baseline

```bash
kubectl apply -f src/infra/network/namespaces.yaml
kubectl apply -f src/infra/network/network-policies.yaml
```

Apply the host-level egress firewall per `src/infra/network/egress-allowlist.md` §3 on each node.

**Verify:** default-deny works — `kubectl -n vault run tmp --rm -i --image=alpine:3.21 -- wget -T2 -qO- https://kubernetes.default.svc` must **fail** pre-allow-rules, and the vault allow-rule paths succeed. Egress: from a node, `curl -sI https://example.com` must fail; `curl -sI https://registry-1.docker.com` must succeed.

## 4. cert-manager + bootstrap CA

```bash
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager --create-namespace \
  -f src/infra/cert-manager/helm-values.yaml \
  --version v1.17.1
kubectl apply -f src/infra/cert-manager/bootstrap-ca.yaml
kubectl apply -f src/infra/cert-manager/vault-certificate.yaml
```

**Verify:** `kubectl -n cert-manager get certificate` → `internal-bootstrap-ca` Ready=True; `kubectl -n vault get secret vault-tls` exists (issued by bootstrap CA).

## 5. Vault deploy, init, unseal

```bash
helm upgrade --install vault hashicorp/vault \
  --namespace vault --create-namespace \
  -f src/infra/vault/helm-values.yaml \
  --version 0.29.1

kubectl -n vault exec vault-0 -- vault operator init -key-shares=5 -key-threshold=3 -format=json
# → distribute 5 shares to the 3 named officers; KEEP OUTPUT OUT OF SHELL HISTORY/FILES
kubectl -n vault exec vault-0 -- vault operator unseal   # 3 shares, any officer present
kubectl -n vault exec vault-1 -- vault operator raft join https://vault-0.vault-internal:8200
kubectl -n vault exec vault-2 -- vault operator raft join https://vault-0.vault-internal:8200
# unseal vault-1 and vault-2 as well
```

**Verify:** `kubectl -n vault exec vault-0 -- vault status` → Initialized true, Sealed false, HA Mode raft, 3 nodes in `vault operator raft list-peers`.

## 6. Vault bootstrap

```bash
export VAULT_ADDR="https://vault.vault.svc:8200"
export VAULT_TOKEN="<root token from init>"        # bootstrap only
src/infra/vault/bootstrap/bootstrap-vault.sh

# Store the K3s token (stdin, never argv):
vault kv put secret/evdr/k3s/cluster token=-

# Then REVOKE the root token and fall back to an evdr-admin token:
vault token revoke "$VAULT_TOKEN"; unset VAULT_TOKEN
```

Kubernetes auth wiring (if not done via env in the script): create the `vault-auth` service account token and cluster CA per `src/infra/vault/README.md`, then re-run with `K8S_SA_JWT_FILE`/`K8S_CA_CERT_FILE` set.

**Verify:** `vault audit list` shows `file/` (**empty = stop, security incident**); `vault secrets list` shows `secret/`, `transit/`, `pki/`, `pki_int/`, `database/`; `vault policy list` shows `evdr-admin`, `pki-issuer`, `break-glass`; `vault read pki_int/roles/evdr-internal` exists.

## 7. Vault-backed issuer + Traefik

```bash
kubectl apply -f src/infra/cert-manager/cluster-issuer-vault.yaml
kubectl get clusterissuer vault-pki-issuer        # Ready=True before continuing

helm upgrade --install traefik traefik/traefik \
  --namespace traefik --create-namespace \
  -f src/infra/traefik/helm-values.yaml \
  --version 33.2.1
kubectl apply -f src/infra/traefik/crds/
```

**Verify:** `kubectl -n traefik get certificate traefik-default-cert` → Ready. TLS policy: `openssl s_client -connect <traefik-ip>:443 -tls1_1` must fail; `-tls1_2` and `-tls1_3` must succeed with the `*.evdr.internal` cert chaining to the Vault intermediate. HTTP→HTTPS: `curl -sI http://<traefik-ip>/` returns 301/308 to https.

## 8. GitLab runner + pipeline proof

Follow `docs/runbooks/gitlab-runner.md`, then push a branch and confirm the pipeline goes green through `sbom`; run `deploy:sample-staging` and `dast:zap-baseline` manually.

**Verify (exit-criterion evidence):** green pipeline URL + artifacts: `semgrep.sarif`, `trivy-deps.json`, `sbom-fs.cdx.json`, `trivy-image.json`, `sbom-image.*.json`, `zap-report.html`. `curl https://hello.staging.evdr.internal/version` (with the internal CA trusted) returns the build version.

## 9. Tear-down and rebuild drill (reproducibility proof)

```bash
# Tear down
cd src/infra/terraform/live/tier0-lab
terraform destroy -var-file=terraform.tfvars

# Rebuild from scratch: steps 1–8 again, timed.
```

Pass conditions:

- [ ] Rebuild completes using only repo artifacts + this runbook (no tribal knowledge)
- [ ] All verification checks in steps 1–8 pass on the rebuilt cluster
- [ ] Wall-clock time recorded in the Phase 0 close-out note
- [ ] Vault unseal ceremony re-executed with shares (proves custody process, not just automation)

## Exit-criteria mapping (Phase 0)

| Exit criterion | Evidence produced by |
|---|---|
| IaC reproducible tear-down/rebuild | §9 drill record |
| CI/CD green vs sample service (SAST/DAST/dep/SBOM reporting) | §8 artifacts + pipeline URL |
| Trivy image scanning + alerting + patch SLA (SR-4.4) | §8 + `docs/security/vulnerability-management.md` |
| TLS 1.2/1.3 enforced incl. internal (SR-1.2) | §7 openssl checks; Vault/K3s TLS configs |
| Network isolation + egress controls (SR-3.1) | §3 verification |
| Vault dynamic secrets/EaaS/PKI (TR-1.4) | §6 verification |
| Threat model signed off (SR-4.3) | `docs/security/threat-model.md` approval record |
| Data classification model approved | `docs/security/data-classification-and-retention.md` approval record |
| DRM strategy ADR | `docs/ADR/0001-drm-strategy-view-first.md` |
| Room SPI contract v0.1 frozen | `src/spi/` (ContractVersion = 0.1.0) |
