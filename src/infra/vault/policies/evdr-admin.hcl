# evdr-admin — day-to-day Vault administration for platform operators.
# Deliberately NOT root-equivalent: no access to tenant KEK material and no
# ability to read arbitrary secrets. Root token is revoked after bootstrap;
# operations needing more go through a new approval, not a standing policy.

# Manage ACL policies and auth methods (platform configuration surface).
path "sys/policies/acl/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}

path "auth/*" {
  capabilities = ["create", "read", "update", "delete", "list", "sudo"]
}

path "sys/auth" {
  capabilities = ["read", "list"]
}

path "sys/auth/*" {
  capabilities = ["create", "read", "update", "delete", "sudo"]
}

# Mounts and engines lifecycle.
path "sys/mounts" {
  capabilities = ["read", "list"]
}

path "sys/mounts/*" {
  capabilities = ["create", "read", "update", "delete", "sudo"]
}

# Platform (non-tenant) secret paths.
path "secret/data/evdr/platform/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}

path "secret/metadata/evdr/platform/*" {
  capabilities = ["read", "list", "delete"]
}

# PKI issuance-role management (role definitions are git-managed in
# bootstrap-vault.sh; operators apply reviewed changes). No CA material
# access: root/intermediate keys stay with break-glass.
path "pki_int/roles/*" {
  capabilities = ["create", "read", "update", "delete", "list"]
}

# Sign leaf certificates under the internal role (infra-service TLS such as
# gitlab.evdr.internal). Signing via /sign/ uses the requester's CSR — Vault
# never generates or returns private keys here, so this grants no key
# material beyond what the requester already holds.
path "pki_int/sign/evdr-internal" {
  capabilities = ["create", "update"]
}

# Health, seal status, audit device listing (operational visibility).
path "sys/health" {
  capabilities = ["read", "sudo"]
}

path "sys/seal-status" {
  capabilities = ["read"]
}

path "sys/audit" {
  capabilities = ["read", "list", "sudo"]
}

# Explicitly NO access to:
#   transit/*/tenant-*        (tenant KEKs — break-glass only, SR-1.5)
#   sys/audit-hash, sys/config Auditing controls stay with security officers.
#   database/creds/*          (dynamic creds are leased to services, not humans)
