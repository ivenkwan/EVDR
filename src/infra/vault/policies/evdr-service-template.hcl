# evdr-service-template — baseline for EVDR platform services (rendered per
# service by deployment tooling; <SERVICE> is substituted with the service
# name). Services get only the secrets engines they consume.

# Dynamic database credentials (TR-1.4): short-lived leases, auto-revoked.
path "database/creds/<SERVICE>" {
  capabilities = ["read"]
}

# Service-scoped static configuration secrets.
path "secret/data/evdr/services/<SERVICE>/*" {
  capabilities = ["read", "list"]
}

path "secret/metadata/evdr/services/<SERVICE>/*" {
  capabilities = ["read", "list"]
}

# Transit is NOT granted here. Tenant KEK use goes through the encryption
# service's tenant-scoped policy (tenant-kek-template.hcl) introduced with
# envelope encryption in Phase 2 — a platform service must never hold broad
# transit access across tenants.
