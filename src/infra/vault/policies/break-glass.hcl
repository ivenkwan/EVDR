# break-glass — SR-1.5 emergency operator access to tenant KEKs.
#
# This policy is NOT attached to any standing role. It exists now so the
# Phase 2.5 break-glass mechanism (time-boxed, multi-person approval, audit-
# logged, tenant-admin alerting) has a pre-reviewed, minimal policy to attach
# during an approved window — and so that "what can an operator ever do" is
# auditable in version control from day one.
#
# The provisioner attaches this policy to a short-lived token only after
# multi-person approval, with a hard TTL, and detaches/revokes at window end.
# Every use is emitted to the audit trail and alerts the tenant admin.

# Read key metadata (rotation state) for the specific tenant in scope.
# <TENANT_ID> is substituted at attachment time — never rendered with a wildcard.
path "transit/keys/tenant-<TENANT_ID>" {
  capabilities = ["read"]
}

# Decrypt with the tenant KEK — the actual break-glass capability that makes
# document access possible (SR-1.4). Everything else (encrypt/rewrap/rotate/
# export/destroy) stays denied even during break-glass.
path "transit/decrypt/tenant-<TENANT_ID>" {
  capabilities = ["update"]
}
