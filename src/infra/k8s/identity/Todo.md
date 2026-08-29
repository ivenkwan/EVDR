# Workstream-internal traceability — partition `src/infra/k8s/identity/`
# (harness/c-identity, Wave 1 workstream C).
#
# NOTE: workers NEVER tick the root Todo.md — the orchestrator verifies
# evidence and ticks after merge. These boxes track THIS workstream's state.
#
# Maps to root Todo.md → Phase 1 → "Identity & Access" activities and exit
# criteria (FR-4.1, FR-4.2, TR-6.1) and the harness deployment map (C → TR-6.1).

## Identity — Keycloak (TR-6.1)

- [ ] Keycloak Deployment + Service authored (`keycloak-deployment.yaml`, `keycloak-service.yaml`)
  - image pinned `quay.io/keycloak/keycloak:26.7.2`, production `start` mode
  - secrets referenced via `keycloak-secrets` (Vault pattern, no literals)
- [ ] Realm bootstrap ConfigMap authored with starter realm JSON (`keycloak-realm-bootstrap.yaml`)
  - realm `evdr`: realm-per-tenant template (TR-6.5 prep)
  - SAML 2.0 enabled (SP client template + SAML IdP placeholder) (TR-6.1, FR-4.1)
  - OIDC enabled; `nextcloud` client pre-created for TR-6.2
  - TOTP + WebAuthn 2FA policies + enforced enrollment (FR-4.2)
- [ ] PostgreSQL 16 backend: references shared data layer (`postgresql.storage.svc:5432`)
  - decision documented in README §2 (choice: shared data layer, not own StatefulSet)
- [ ] TLS via cert-manager Vault cluster issuer (`keycloak-certificate.yaml`, `vault-pki-issuer`)
- [ ] Edge exposure via Traefik IngressRoute with baseline middlewares (`keycloak-ingressroute.yaml`)
- [ ] NetworkPolicy: edge + intra-namespace ingress; Postgres egress only (`keycloak-networkpolicy.yaml`)
- [ ] Vault provisioning procedure documented (README §6/§7) incl. AD/LDAP federation step (FR-4.1)
- [ ] Offline validation: every YAML passes `kubectl apply --dry-run=client --validate=false` + python yaml parse (evidence: README "Validation")

## Orchestrator-owned (apply wave, NOT this workstream)

- [ ] Lab apply: realm import verified, SSO + MFA working for internal users (Phase 1 exit criterion)
- [ ] Vault secrets provisioned (`keycloak-secrets`) before apply
- [ ] Data layer (workstream B) merged; service-name contract reconciled (`postgresql.storage.svc`)
- [ ] Root Todo.md Phase 1 "Identity & Access" items ticked with evidence by orchestrator
