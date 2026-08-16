# tenant-kek-template — strict per-tenant key-path ACL (TR-1.4).
#
# Rendered per tenant by the Tenant Provisioner (Phase 2.5): every <TENANT_ID>
# below is substituted with the tenant's UUID and the result attached only to
# that tenant's encryption-service role. This is the template committed at
# Phase 0 so the ACL shape is reviewed and frozen before any KEK exists.
#
# Envelope-encryption model (SR-1.1, Phase 2): per-document DEKs are wrapped
# by the tenant KEK held here. No standing human policy can read these paths;
# operator access is break-glass only (break-glass.hcl, SR-1.5).

# Use the tenant KEK for envelope operations.
path "transit/encrypt/tenant-<TENANT_ID>" {
  capabilities = ["update"]
}

path "transit/decrypt/tenant-<TENANT_ID>" {
  capabilities = ["update"]
}

path "transit/rewrap/tenant-<TENANT_ID>" {
  capabilities = ["update"]
}

# Allow the tenant's service to read key metadata (rotation state, type) but
# NOT export the key and NOT rotate on its own — rotation is a control-plane
# operation (SR-1.6).
path "transit/keys/tenant-<TENANT_ID>" {
  capabilities = ["read"]
}

# Explicitly denied even if a broader policy is accidentally attached later:
# key export and plaintext backup of tenant key material.
path "transit/export/tenant-<TENANT_ID>/*" {
  capabilities = ["deny"]
}

path "transit/backup/tenant-<TENANT_ID>" {
  capabilities = ["deny"]
}
