# Workstream-internal traceability — partition `src/infra/k8s/nextcloud/`
# (harness/c-identity, Wave 1 workstream C).
#
# NOTE: workers NEVER tick the root Todo.md — the orchestrator verifies
# evidence and ticks after merge. These boxes track THIS workstream's state.
#
# Maps to root Todo.md → Phase 1 → "Data & Storage Layer" (TR-2.13),
# "Identity & Access" (TR-6.2) and Phase 1 exit criteria.

## Hardened Nextcloud (TR-2.13)

- [ ] Deployment + Service authored (`nextcloud-deployment.yaml`, `nextcloud-service.yaml`)
  - image pinned `nextcloud:34.0.3-apache`; non-root restricted-PSA security context
  - secrets referenced via `nextcloud-secrets` (Vault pattern, no literals)
- [ ] PostgreSQL 16 backend via shared data layer (`postgresql.storage.svc:5432`)
- [ ] Redis 7 file locking (TR-2.13) via `REDIS_HOST*` env → `memcache.locking`
- [ ] TLS via cert-manager Vault cluster issuer (`nextcloud-certificate.yaml`, `vault-pki-issuer`)
- [ ] Persistent volumes (`nextcloud-pvc.yaml`: html 5Gi + data 20Gi)
- [ ] Network separation (`nextcloud-networkpolicy.yaml`: same-ns ingress only;
      no edge route by construction) — TR-2.13 "separate from external-facing services"
- [ ] Backup strategy documented in README §6 (pg_dump + restic/rsync → Ceph RGW;
      restore drill = Phase 1 exit criterion, orchestrator-owned)
- [ ] SSO connection to Keycloak via OIDC prepared (TR-6.2):
  - `occ user_oidc:provider` procedure + placeholder env in Deployment (README §5)
  - Keycloak realm `nextcloud` OIDC client pre-created (identity workstream)
- [ ] Offline validation: every YAML passes `kubectl apply --dry-run=client --validate=false` + python yaml parse (evidence: README "Validation")

## Orchestrator-owned (apply wave, NOT this workstream)

- [ ] Data layer (workstream B) merged; `postgresql.storage.svc` / `redis.storage.svc`
      and `rook-ceph-block` StorageClass contracts reconciled
- [ ] Vault secrets provisioned (`nextcloud-secrets`) before apply
- [ ] Nextcloud backup restore tested (Phase 1 exit criterion)
- [ ] Keycloak SSO + MFA working for internal users; guest OTP access requires no account (exit criterion)
- [ ] Root Todo.md Phase 1 activities ticked with evidence by orchestrator
