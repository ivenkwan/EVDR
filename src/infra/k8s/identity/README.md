# Keycloak — EVDR identity plane (Phase 1, TR-6.1 / FR-4.1 / FR-4.2)

> Workstream `harness/c-identity` (Wave 1, partition C). Manifests are
> **dry-run validated only** — the k3d lab is unreachable from this worktree
> (harness constraint). Apply happens in a later wave by the orchestrator.

## What this directory delivers

| File | Purpose |
|---|---|
| `keycloak-deployment.yaml` | Deployment: `quay.io/keycloak/keycloak:26.7.2`, production `start`, realm auto-import, probes, restricted-PSA security context |
| `keycloak-service.yaml` | ClusterIP Service (`keycloak`, port 8080) |
| `keycloak-certificate.yaml` | cert-manager `Certificate` from the Vault-backed `vault-pki-issuer` (TLS, TR-1.5/SR-1.2) |
| `keycloak-ingressroute.yaml` | Traefik `IngressRoute` — external SSO entry (edge TLS termination) |
| `keycloak-realm-bootstrap.yaml` | ConfigMap with the starter realm JSON (`evdr-realm.json`, auto-imported at boot) |
| `keycloak-networkpolicy.yaml` | NetworkPolicy — edge + intra-namespace ingress only; Postgres egress only |
| `keycloak-secrets.example.yaml` | **Example only** — placeholder Secret shape; real values come from Vault (never commit real credentials) |
| `Todo.md` | Workstream-internal traceability to root `Todo.md` Phase 1 activities (un-ticked — workers never tick) |

## Design decisions

### 1. Namespace: `evdr-system`
`src/infra/network/namespaces.yaml` designates `evdr-system` as the Phase 1
application namespace (PSA `restricted` enforced, `evdr.io/scope: application`).
Keycloak is an application service → it lives there. The namespace object
itself is owned by `src/infra/network/` and is **not** re-created here.

### 2. PostgreSQL 16 backend — reference the shared data layer
Workstream B (`harness/b-data-layer`, partition `src/infra/k8s/data-layer/`)
owns PostgreSQL 16 (TR-2.11) and Redis 7 (TR-2.12). **Decision: reference the
shared data layer rather than shipping a private StatefulSet** — a second
Postgres would duplicate storage, backups, and the Vault dynamic-credentials
story, and would diverge from the Phase 0 runbook's single data plane.

Cross-workstream contract (reconcile at merge if B names services differently):

| Assumed service | Endpoint used here |
|---|---|
| PostgreSQL 16 | `postgresql.storage.svc:5432` (db `keycloak`) |
| Redis 7 | `redis.storage.svc:6379` (Nextcloud file locking) |

The `storage` namespace exists per `namespaces.yaml`; B's StatefulSets land
there. Database name/user/password are injected as secrets (below); the
Keycloak DB user needs `CREATE` privilege on the `keycloak` database for the
initial schema bootstrap (documented in the Vault provisioning snippet below).

### 3. TLS posture — cert-manager + Vault PKI, TLS terminated at the edge
- `keycloak-certificate.yaml` requests `keycloak-tls` from the existing
  `vault-pki-issuer` ClusterIssuer (`src/infra/cert-manager/cluster-issuer-vault.yaml`),
  following the Phase 0 Certificate conventions: CN-less, byte-sorted SANs,
  ECDSA P-256, `duration: 720h` / `renewBefore: 240h` (matches the
  `evdr-internal` Vault role TTL — a mismatch causes a reissue hot loop).
- Traefik terminates TLS at the edge (`keycloak-ingressroute.yaml`, SNI
  `keycloak.evdr.internal`, `traefik/edge-security-headers` +
  `traefik/edge-rate-limit` middlewares); Keycloak serves HTTP on 8080
  internally with `KC_PROXY_HEADERS=xforwarded` and
  `KC_HOSTNAME=https://keycloak.evdr.internal` so issuer/redirect URLs are
  correct behind the proxy.
- The cert secret is mounted at `/etc/keycloak/tls` — flip `KC_HTTPS_ENABLED`
  to `true` (+ `KC_HTTPS_CERTIFICATE_FILE/KEY_FILE`) to serve TLS directly
  from Keycloak if end-to-end TLS is ever required (see follow-ups).

### 4. Realm-per-tenant SSO preparation (TR-6.1, FR-4.1)
The bootstrap realm `evdr` is the Tier 0 single realm and the **template for
realm-per-tenant provisioning** (Phase 2.5 Tenant Provisioner, TR-6.5). It ships:
- **OIDC enabled**: default protocol; `nextcloud` OIDC client pre-created
  (TR-6.2 wiring; secret is a **placeholder**, see §6).
- **SAML 2.0 enabled**: `evdr-saml-sp-placeholder` SAML SP client template
  (disabled until a real SP registers) + `enterprise-saml-idp` SAML IdP
  placeholder (disabled) for enterprise federation.
- **AD/LDAP federation (FR-4.1)**: pre-staged *outside* the realm JSON (see
  §7 snippet) — the LDAP provider is added post-import via the admin API so
  import cannot fail on an unreachable directory.

### 5. 2FA: TOTP + WebAuthn/FIDO2 (FR-4.2)
- Realm OTP policy: TOTP, HMAC-SHA256, 6 digits, 30 s period, look-ahead 1.
- WebAuthn policy: RP `EVDR` / rpId `keycloak.evdr.internal`, user-verification
  `required`, signature algorithms ES256 + RS256, attestation `none`.
- **Enforced enrollment**: `CONFIGURE_TOTP` and `webauthn-register` are set as
  **default required actions** — every internal user must enroll before first
  login completes; the browser flow's conditional-2FA step then presents OTP
  (and, with the shipped change, passkeys) at every login.
- Browser flow: `webauthn-authenticator` flipped `DISABLED → ALTERNATIVE` in
  `Browser - Conditional 2FA` (passkey as a second factor); the condition
  executions are left intact so credential-less users are routed to the
  required-action enrollment instead of being locked out.
- **Stricter mode (documented toggle)**: set both `auth-otp-form` and
  `webauthn-authenticator` to `REQUIRED` inside the 2FA subflow to demand
  TOTP **and** a passkey at every login (dual-factor 2FA) once all users are
  enrolled.

### 6. Secrets — Vault pattern, no literals
No real credentials in this directory. The Deployment references Secret
`keycloak-secrets` (namespace `evdr-system`); `keycloak-secrets.example.yaml`
shows the expected shape with `CHANGE-ME` placeholders. Provisioning:

```bash
# Static secrets (kv-v2, per evdr-service-template.hcl policy):
vault kv put secret/evdr/services/keycloak/admin   KC_BOOTSTRAP_ADMIN_USERNAME=... KC_BOOTSTRAP_ADMIN_PASSWORD=-
vault kv put secret/evdr/services/keycloak/db      KC_DB_USERNAME=keycloak KC_DB_PASSWORD=-
kubectl -n evdr-system create secret generic keycloak-secrets \
  --from-literal=KC_BOOTSTRAP_ADMIN_USERNAME="$(vault kv get -field=KC_BOOTSTRAP_ADMIN_USERNAME secret/evdr/services/keycloak/admin)" \
  --from-literal=KC_BOOTSTRAP_ADMIN_PASSWORD="$(vault kv get -field=KC_BOOTSTRAP_ADMIN_PASSWORD secret/evdr/services/keycloak/admin)" \
  --from-literal=KC_DB_USERNAME="$(vault kv get -field=KC_DB_USERNAME secret/evdr/services/keycloak/db)" \
  --from-literal=KC_DB_PASSWORD="$(vault kv get -field=KC_DB_PASSWORD secret/evdr/services/keycloak/db)"

# Preferred at scale: Vault Agent sidecar injector (enabled in the Vault
# chart, `src/infra/vault/helm-values.yaml`) with a Kubernetes-auth role
# bound to the keycloak ServiceAccount and the rendered evdr-service-keycloak
# policy (evdr-service-template.hcl). Commented annotations are included in
# the Deployment for that path. Dynamic DB creds land at database/creds/keycloak
# once the database engine is wired to Postgres (Phase 1 data layer).
```

### 7. Enterprise AD/LDAP federation (FR-4.1) — post-import step
```bash
# Exec into the Keycloak pod after import, or via the admin API:
# Realm Settings → User Federation → Add LDAP provider:
#   Vendor: Active Directory | Connection URL: ldaps://ad.evdr.internal:636
#   Bind DN / Bind credentials: from Vault (secret/evdr/services/keycloak/ldap)
#   Users DN: ou=Users,dc=evdr,dc=internal | Import users: ON
# Enable after the directory endpoints are added to the egress allow-list
# (src/infra/network/egress-allowlist.md, "Enterprise IdP endpoints", Phase 1).
```

## Apply order (orchestrator / later wave)
1. Data layer (workstream B) — Postgres 16 + Redis 7 in `storage`.
2. `kubectl -n evdr-system create secret generic keycloak-secrets ...` (from Vault, §6).
3. `kubectl apply -f src/infra/k8s/identity/` — Certificate first (issuer is
   already Ready from Phase 0), then Deployment/Service, then IngressRoute.

## Validation (this workstream, offline)
Every YAML in this directory passed `kubectl apply --dry-run=client
--validate=false -f <file>` (offline discovery served from a local cache —
the lab is unreachable, so server-side validation is deferred to the apply
wave) and a `python3` YAML parse. The realm JSON additionally parses as JSON
and was built from a real Keycloak 26.5 realm export (default clients, scopes,
and flows preserved verbatim; see `keycloak-realm-bootstrap.yaml`).

## Requirement traceability
| Requirement | Where |
|---|---|
| FR-4.1 SSO SAML2/OIDC + AD/LDAP federation | realm bootstrap, §4/§7 |
| FR-4.2 MFA TOTP + WebAuthn | realm bootstrap §5 |
| TR-6.1 Keycloak: SAML2, OIDC, LDAP, 2FA, realm-per-tenant | this directory |
| TR-6.2 Nextcloud ← Keycloak OIDC | `nextcloud` client + `src/infra/k8s/nextcloud/` |
| TR-1.5 / SR-1.2 automatic TLS | certificate + IngressRoute |
| SR-3.1 network isolation | NetworkPolicy + `src/infra/network/` baseline |

## Left-outs / follow-ups
- **HA mode** (`replicas: 3`, `KC_CACHE=ispn` + JGroups discovery) deferred to
  Tier 1+; Tier 0 runs a single replica (`KC_CACHE=local`).
- **Keycloak Authentication Policies** (KC 26) for hard "2FA always, no
  exceptions" — can replace the conditional-flow approach once operator
  hardening is configured; see §5.
- **End-to-end TLS** (Keycloak HTTPS on 8443 + CA-bundle trust in clients)
  tracked under SR-1.2 internal-trust extension.
- **Vault dynamic DB credentials** for Keycloak — blocked on the database
  engine connection (Phase 1 data layer, workstream B).
- **Realm import verification** (login flow smoke test) — needs the lab;
  part of the orchestrator's apply-wave verification.
