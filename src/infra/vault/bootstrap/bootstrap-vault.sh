#!/usr/bin/env bash
# bootstrap-vault.sh — one-time, idempotent Vault bootstrap (TR-1.4).
#
# Enables (in order): audit device FIRST, then kv-v2, transit, PKI root +
# issuing intermediate, database engine, Kubernetes auth, and ACL policies.
#
# Required environment:
#   VAULT_ADDR     e.g. https://vault.vault.svc:8200 (from env, never literal
#                  in CI — set via runner masked variable or local shell)
#   VAULT_TOKEN    root token from `vault operator init` — used ONLY for this
#                  bootstrap, then revoked (see runbook checklist)
# Optional (for Kubernetes auth wiring):
#   K8S_HOST            API server URL (default: https://kubernetes.default.svc)
#   K8S_SA_JWT_FILE     path to the vault-auth service-account token
#   K8S_CA_CERT_FILE    path to the cluster CA bundle
#
# The script never prints token material. Re-running is safe: existing
# engines, CAs, and policies are skipped or reapplied, never recreated.
set -euo pipefail

POLICIES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../policies" && pwd)"

: "${VAULT_ADDR:?VAULT_ADDR must be set in the environment}"
: "${VAULT_TOKEN:?VAULT_TOKEN must be set in the environment (root token, bootstrap only)}"
export VAULT_ADDR VAULT_TOKEN

K8S_HOST="${K8S_HOST:-https://kubernetes.default.svc:443}"

echo "==> Verifying Vault is initialised and unsealed"
vault status -format=json | grep -q '"initialized": true' || {
  echo "ERROR: Vault is not initialised. Run operator init per src/infra/vault/README.md first." >&2
  exit 1
}
vault status -format=json | grep -q '"sealed": false' || {
  echo "ERROR: Vault is sealed. Complete the unseal ceremony first." >&2
  exit 1
}

# --- 1. Audit device — FIRST, before any secret exists -------------------------

if vault audit list -format=json | grep -q '"file/"'; then
  echo "==> Audit device already enabled — skipping"
else
  echo "==> Enabling file audit device"
  vault audit enable file file_path=/vault/audit/vault-audit.log
fi

# --- 2. Secrets engines ----------------------------------------------------------

enable_engine() {
  local path="$1" type="$2"
  if vault secrets list -format=json | grep -q "\"${path}/\""; then
    echo "==> Engine ${path}/ (${type}) already enabled — skipping"
  else
    echo "==> Enabling ${type} engine at ${path}/"
    vault secrets enable -path="${path}" "${type}"
  fi
}

enable_engine secret kv-v2
enable_engine transit transit
enable_engine database database # dynamic secrets; Postgres connection configured in Phase 1

# --- 3. PKI: internal root CA + issuing intermediate -----------------------------

if vault read pki/cert/ca >/dev/null 2>&1; then
  echo "==> PKI root CA already present — skipping CA generation"
else
  echo "==> Enabling PKI root (pki/) and intermediate (pki_int/)"
  vault secrets enable -path=pki pki
  vault secrets tune -max-lease-ttl=87600h pki
  vault secrets enable -path=pki_int pki
  vault secrets tune -max-lease-ttl=43800h pki_int

  echo "==> Generating internal root CA (key material never leaves Vault)"
  vault write -field=certificate pki/root/generate/internal \
    common_name="EVDR Internal Root CA" \
    issuer_name="evdr-root" \
    ttl=87600h >/dev/null

  echo "==> Generating and signing the issuing intermediate"
  CSR="$(vault write -field=csr pki_int/intermediate/generate/internal \
    common_name="EVDR Internal Issuing CA" \
    issuer_name="evdr-intermediate")"
  CERT="$(vault write -field=certificate pki/root/sign-intermediate \
    csr="${CSR}" \
    format=pem_bundle \
    ttl=43800h)"
  vault write pki_int/intermediate/set-signed certificate="${CERT}"

  vault write pki/config/urls \
    issuing_certificates="${VAULT_ADDR}/v1/pki/ca" \
    crl_distribution_points="${VAULT_ADDR}/v1/pki/crl"
  vault write pki_int/config/urls \
    issuing_certificates="${VAULT_ADDR}/v1/pki_int/ca" \
    crl_distribution_points="${VAULT_ADDR}/v1/pki_int/crl"
fi

echo "==> Ensuring evdr-internal issuance role (cluster-internal names only)"
# ttl must EQUAL the duration requested in Certificate manifests (720h):
# cert-manager does not send a ttl to Vault, so Vault applies the role
# default; if issued duration != requested duration, cert-manager reissues
# in a hot loop (observed in the Phase 0 lab: 85 CertificateRequests in 90s).
# require_cn=false: Certificate manifests must NOT set spec.commonName
# (CN-triggered InvalidKeyPair reissue loop, Phase 0 lab); names live in
# dnsNames, byte-lexically sorted because Vault sorts SANs on issuance.
# key_type=any: cert-manager signs through the pki_int/sign/ endpoint, which
# validates the CSR's key type against the role. With the default (rsa), an
# ECDSA CSR is rejected there — and on pki_int/issue/ (which must NOT be used
# for cert-manager at all) the csr parameter is silently ignored and a
# role-typed key generated instead, producing certs that never match
# cert-manager's key (InvalidKeyPair loop, Phase 0 lab, root-caused via
# audit log + sign/sign-verbatim probes). "any" lets the CSR's algorithm
# through; note Vault permits "any" only on /sign/, not on /issue/.
vault write pki_int/roles/evdr-internal \
  allowed_domains="evdr.internal,svc,cluster.local" \
  allow_subdomains=true \
  allow_bare_domains=true \
  require_cn=false \
  key_type=any \
  max_ttl=720h \
  ttl=720h >/dev/null

# --- 4. Kubernetes auth ------------------------------------------------------------

if vault auth list -format=json | grep -q '"kubernetes/"'; then
  echo "==> Kubernetes auth already enabled — skipping enable"
else
  echo "==> Enabling Kubernetes auth"
  vault auth enable kubernetes
fi

if [[ -n "${K8S_SA_JWT_FILE:-}" && -n "${K8S_CA_CERT_FILE:-}" ]]; then
  echo "==> Configuring Kubernetes auth (${K8S_HOST})"
  vault write auth/kubernetes/config \
    kubernetes_host="${K8S_HOST}" \
    kubernetes_ca_cert=@"${K8S_CA_CERT_FILE}" \
    token_reviewer_jwt=@"${K8S_SA_JWT_FILE}"
else
  echo "==> K8S_SA_JWT_FILE / K8S_CA_CERT_FILE not set — configure auth/kubernetes/config per runbook"
fi

# --- 5. ACL policies -----------------------------------------------------------------

echo "==> Applying ACL policies from ${POLICIES_DIR}"
for policy_file in "${POLICIES_DIR}"/*.hcl; do
  name="$(basename "${policy_file}" .hcl)"
  # Templates are stored for review/provisioning, not applied directly.
  case "${name}" in
    *-template)
      echo "    skip template: ${name} (rendered per instance by the provisioner)"
      continue
      ;;
  esac
  vault policy write "${name}" "${policy_file}"
  echo "    applied: ${name}"
done

# cert-manager's Kubernetes-auth role for the Vault PKI issuer.
echo "==> Ensuring cert-manager issuer role"
vault write auth/kubernetes/role/cert-manager \
  bound_service_account_names=cert-manager \
  bound_service_account_namespaces=cert-manager \
  policies=pki-issuer \
  ttl=24h >/dev/null

cat <<'DONE'

Bootstrap complete. Remaining manual steps (runbook checklist):
  1. Verify: vault audit list   (must show file/ — empty = security incident)
  2. Store the K3s cluster token:  vault kv put secret/evdr/k3s/cluster token=-
        (read from stdin; never as a shell argument)
  3. REVOKE THE ROOT TOKEN now — but create the admin token as an ORPHAN
     first, or the root revocation cascade will kill it too (verified in
     the Phase 0 lab: parent revocation revokes all child tokens):
        vault token create -policy=evdr-admin -ttl=768h -renewable=true -orphan
        vault token revoke -mode=orphan "${VAULT_TOKEN}"
  4. Distribute/retire unseal shares per the ceremony in src/infra/vault/README.md.
DONE
