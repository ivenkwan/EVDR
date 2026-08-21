# HashiCorp Vault (TR-1.4)

Vault provides: **dynamic secrets** (database credentials), **encryption-as-a-service** (transit — the tenant KEK custody layer that envelope encryption builds on in Phase 2), and **PKI** (internal CA issuing service certificates, SR-1.2). Strict per-tenant key-path ACLs are defined as policy templates now and instantiated per tenant by the provisioner in Phase 2.5.

## Files

| Path | Purpose |
|---|---|
| `helm-values.yaml` | HA (3-node Raft) deployment, TLS, file audit device |
| `bootstrap/bootstrap-vault.sh` | One-time engine/auth/policy bootstrap (idempotent) |
| `policies/*.hcl` | ACL policies: admin, service template, tenant KEK template, PKI issuer, break-glass placeholder |

## Bootstrap order (TLS chicken-and-egg)

Vault must serve TLS from first exposure (SR-1.2), but Vault is also the PKI that issues internal certificates. Resolution:

1. **cert-manager first** with a self-signed bootstrap CA (`src/infra/cert-manager/`) → issues Vault's initial serving certificate (`vault.vault.svc`).
2. **Vault init & unseal** (Shamir 5-of-3 — see ceremony below).
3. **`bootstrap-vault.sh`** enables the audit device **before any secret is stored**, then PKI (root CA internal to Vault — its key never leaves Vault — plus an issuing intermediate), transit, kv-v2, database engine, Kubernetes auth, and ACL policies.
4. **cert-manager Vault issuer** (`cluster-issuer-vault.yaml`) takes over service-cert issuance from the bootstrap CA; Traefik's default certificate is re-issued from Vault PKI. The bootstrap CA remains only as the issuer of Vault's own cert.

## Unseal ceremony (lab and Tier 0)

- Init with 5 key shares, threshold 3: `vault operator init -key-shares=5 -key-threshold=3 -format=json`.
- Shares are distributed to three named officers (no single person holds quorum); the root token is used for bootstrap only and **revoked immediately after** (`docs/runbooks/phase-0-foundation-rebuild.md` checklist).
- The init output is never stored in the repo, CI, or chat tooling.
- Production/Tier 1+: evaluate auto-unseal (transit seal on a separate cluster, or HSM/KMS where the environment offers it) before first real tenant — tracked as an infra decision in the runbook.

## Per-tenant key-path ACLs (TR-1.4)

Tenant KEKs live under `transit/keys/tenant-<TENANT_ID>`. The template policy `policies/tenant-kek-template.hcl` grants a tenant's services exactly their own key paths and nothing else; the Tenant Provisioner (P2.5) renders it per tenant. Break-glass access (SR-1.5, P2.5) attaches `policies/break-glass.hcl` only inside an approved, time-boxed window — there is **no standing policy** that can read tenant KEKs.

## Rules

- Vault address and tokens come from the environment (`VAULT_ADDR`, `VAULT_TOKEN`), never literals — `bootstrap-vault.sh` refuses to run without them.
- The audit device must be the first thing enabled after init; if `vault audit list` is ever empty in a running environment, treat it as a security incident (threat model T-15/T-16).
- No manual Vault changes in production — engines and policies are applied via the bootstrap script / provisioner code (AGENTS.md §7 infrastructure rule).
