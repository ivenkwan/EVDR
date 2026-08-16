# cert-manager + Internal PKI Wiring (TR-1.5, SR-1.2)

cert-manager automates certificate lifecycle for the cluster (**automatic TLS**, TR-1.5). Two issuers exist, in this bootstrap order:

1. **`internal-bootstrap-ca`** (`bootstrap-ca.yaml`) — a self-signed internal CA whose only job is to issue Vault's serving certificate (and anything needed before Vault PKI is live). This breaks the chicken-and-egg: Vault can't issue certs before it exists.
2. **`vault-pki-issuer`** (`cluster-issuer-vault.yaml`) — the steady-state issuer, backed by Vault's `pki_int` intermediate (applied **after** `bootstrap-vault.sh` has run). All internal service certificates (SR-1.2 service-to-service TLS) and Traefik's default certificate come from here.

## Files

| Path | Purpose |
|---|---|
| `helm-values.yaml` | cert-manager deployment (CRDs enabled, HA) |
| `bootstrap-ca.yaml` | self-signed bootstrap ClusterIssuer + CA + ClusterIssuer |
| `vault-certificate.yaml` | Vault's serving certificate (from bootstrap CA) |
| `cluster-issuer-vault.yaml` | Vault-backed ClusterIssuer (apply after Vault bootstrap) |

## Notes

- `cluster-issuer-vault.yaml` requires cert-manager ≥ v1.16 (`caBundleSecretRef`). The referenced secret is `internal-bootstrap-ca` — Vault serves a cert from the bootstrap CA, so that CA is what clients of Vault must trust until Vault's own chain is distributed.
- The Vault auth role `cert-manager` (policy `pki-issuer`) is created by `src/infra/vault/bootstrap/bootstrap-vault.sh`.
- Internal leaf TTLs are capped at 168h by the `evdr-internal` Vault role; cert-manager renews automatically (default: renew at 2/3 lifetime). Short-lived certs are the revocation storey for internal TLS.
