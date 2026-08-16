# pki-issuer — attached to the cert-manager Vault issuer's auth role.
# cert-manager uses it to sign internal service certificates from the
# intermediate CA (see cert-manager/cluster-issuer-vault.yaml). It can sign
# only under the evdr-internal role — no CA management, no other domains.
#
# Grants /sign/ (CSR signing — honors the CSR's public key), NOT /issue/.
# /issue/ is Vault's server-side key-generation endpoint: it silently ignores
# the csr parameter and returns a generated private_key, which loops
# cert-manager with InvalidKeyPair (Phase 0 lab, root-caused).

path "pki_int/sign/evdr-internal" {
  capabilities = ["create", "update"]
}

path "pki_int/roles/evdr-internal" {
  capabilities = ["read"]
}

# Read-only visibility of the chain for diagnostics.
path "pki_int/ca_chain" {
  capabilities = ["read"]
}

path "pki/ca_chain" {
  capabilities = ["read"]
}
