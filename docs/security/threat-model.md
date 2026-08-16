# EVDR Threat Model

> **Status:** v0.1 — Phase 0 baseline, **approved for Phase 0** (sign-off record and the independent-review caveat in §8)
> **Requirement:** SR-4.3 — documented threat model covering internal users, external parties, administrators, operators, and leak scenarios
> **Review cadence:** updated and re-approved at every phase boundary (see §8); material architecture changes trigger an interim review
> **Scope:** the EVDR platform across all four deployment tiers (0 internal, 1 shared SaaS, 2 dedicated, 3 on-prem) at Phase 0–2 architecture depth

---

## 1. Methodology

We model threats per trust boundary using STRIDE as the enumeration aid, plus explicit **abuse cases** for the leak scenarios that define a VDR's reason to exist. Each threat entry records: actor, threat, affected assets, existing/planned mitigations (traced to FTRS requirement IDs), the phase in which the mitigation lands, and residual risk accepted.

Risk scale: **L**ikelihood and **I**mpact each rated Low/Medium/High; residual risk is what remains *after* the listed mitigations are verified, not merely implemented.

## 2. System Context and Trust Boundaries

```
                         ┌──────────────────────────────────────────────┐
   External guests ─────▶│ TB1: Public edge (Traefik, TLS termination)  │
   (untrusted internet)  └──────────────┬───────────────────────────────┘
                                        ▼
   Internal users ──────▶ TB2: Application layer (portal, viewer, policy engine)
   (authenticated via Keycloak, semi-trusted)
                                        ▼
                         TB3: Service layer (Room SPI, adapters, converters)
                                        ▼
                         TB4: Data layer (Ceph RGW, PostgreSQL, Redis, Nextcloud)
                                        ▼
   Operators ──────────▶ TB5: Infrastructure/control plane (K8s API, Vault, CI/CD, hosts)
   (privileged, heavily audited)
```

Data classifications handled here: see `docs/security/data-classification-and-retention.md`. Encryption/key architecture: see `CLAUDE.md` §12 and SR-1.x.

## 3. Actors

| Actor | Trust level | Access paths | Notes |
|---|---|---|---|
| **Internal user** (bank staff, deal manager) | Medium — authenticated, authorised per room | Portal via Keycloak SSO + MFA (FR-4.1/4.2) | Malicious-insider and compromised-account variants both in scope |
| **External party / guest** | Low — no account, expiring link + password/OTP (FR-4.3) | Branded room portal, secure viewer, File Drop | Primary leak-risk actor by design |
| **Tenant administrator** | Medium-high within one tenant | Tenant Admin Console (P2.5), room/policy admin | Cannot cross tenant boundary; no KEK access |
| **Platform operator** (SRE/DevOps) | High on infrastructure, **zero on document content** | K8s API, Vault, hosts, CI/CD | SR-1.4: document access cryptographically impossible without break-glass KEK access (P2) |
| **System administrator** (platform app-level) | High within app, audited | Admin console, support tooling | All actions privileged-logged (SR-2.3) |
| **External attacker** (no credentials) | None | TB1 only | DDoS, scanning, exploit of exposed services |
| **Supply-chain attacker** | None initially | CI dependencies, container base images, Helm charts | Addressed by SR-4.1/SR-4.4 pipeline controls |

## 4. Assets (protection targets)

| # | Asset | Classification baseline | Primary controls |
|---|---|---|---|
| A1 | Tenant documents (plaintext content) | Restricted | Envelope encryption (SR-1.1, P2), Room SPI mediation, view-first DRM (ADR-0001) |
| A2 | Document DEKs / tenant KEKs | Restricted | Vault KEK custody, per-tenant key-path ACLs (TR-1.4), break-glass model (SR-1.5) |
| A3 | Audit trail & evidence exports | Restricted | Append-only schema (SR-5.1, P2), SHA-256 integrity letters (SR-5.2) |
| A4 | Identity credentials & sessions (SSO, OTP, links) | Confidential | Keycloak realms, MFA, expiring links (FR-4.x) |
| A5 | Room metadata, permissions, NDA evidence | Confidential | RLS tenant isolation (SR-2.2, P2) |
| A6 | Platform secrets (DB creds, API keys) | Restricted | Vault dynamic secrets (TR-1.4); never in Helm values/ConfigMaps/env (repo rule) |
| A7 | Infrastructure control plane (K8s API, Vault root/unseal) | Restricted | TLS 1.2/1.3 (SR-1.2), NetworkPolicies, unseal ceremony (runbook) |

## 5. Threat Register

> Phase column = when the mitigation lands. Threats whose mitigation is post-P0 are **open risks** until that phase gate.

### 5.1 External party / guest threats

| ID | Threat (STRIDE) | L | I | Mitigations → requirement | Phase | Residual |
|---|---|---|---|---|---|---|
| T-01 | Guest shares expiring link/OTP onward (Information Disclosure) | H | M | Link expiry, optional password/OTP (FR-4.3); IP/domain-restricted grants (FR-4.4, P2); watermark attributes sessions regardless (FR-3.2) | P1–P2 | Medium — accepted; attribution is the control (ADR-0001) |
| T-02 | Guest photographs/screenshots rendered pages (ID) | H | H | Server-rendered identity watermark on every page (FR-3.2); blur-on-focus-loss + shortcut interception (FR-3.5, P2); page-at-a-time streaming limits bulk capture (FR-3.1) | P1–P2 | **Medium-High — explicitly accepted** per ADR-0001; deterrence+attribution, not prevention |
| T-03 | Guest scripts sequential page scraping to reconstruct a document (ID) | M | H | Page-streaming + per-tenant edge rate limits (SR-3.2, P1); anomaly detection on viewing patterns (FR-7.7, P4); session-bound watermarks make reconstructed copies attributable | P1–P4 | Medium |
| T-04 | Guest strips client-side protections / watermarks (Tampering) | M | M | No client-side-only watermarking — watermark is baked into rendered pixels server-side (TR-4.2); viewer JS controls are UX, not security boundary | P1 | Low |
| T-05 | Malicious upload via File Drop (malware, polyglot files) | M | H | Virus/malware scan before room acceptance (FR-6.3, P2); size/type validation (FR-6.2); conversion pipeline normalises Office→PDF, sanitising active content (FR-3.4) | P1–P2 | Low-Medium |
| T-06 | OTP/password brute force on guest links (Spoofing) | M | M | Rate limiting at edge (TR-1.5/SR-3.2); OTP single-use + short TTL; lockout + alerting | P1 | Low |

### 5.2 Internal user threats

| ID | Threat (STRIDE) | L | I | Mitigations → requirement | Phase | Residual |
|---|---|---|---|---|---|---|
| T-07 | Compromised internal credentials (phishing, reuse) (Spoofing) | M | H | SAML/OIDC federation to enterprise IdP (FR-4.1); MFA TOTP + WebAuthn (FR-4.2); RBAC least privilege (FR-4.5, P2); anomaly alerts (FR-7.7, P4) | P1–P2 | Medium |
| T-08 | Malicious insider bulk-exfiltrates rooms they can access (ID) | M | H | View-first default denies download path (ADR-0001); download tier is explicit grant, fully audited (FR-1.2/FR-7.1); mass-download anomaly detection (FR-7.7, P4); watermark attribution | P1–P4 | Medium — insider with legitimate download grant is residual by design |
| T-09 | Insider over-shares: grants external access too broadly (Elevation of Privilege) | M | M | Policy engine baselines non-overridable (FR-5.2, P2); grant expiry mandatory; bulk templates reviewed (FR-1.5); audit of every grant (SR-2.3) | P2 | Low-Medium |
| T-10 | Insider tampers with or deletes audit evidence of their actions (Tampering/Repudiation) | L | H | Append-only audit schema, no UPDATE/DELETE (SR-5.1, P2); SIEM forwarding off-platform (SR-5.3, P2); NDA/export evidence durable (FR-5.1) | P2 | Low |

### 5.3 Administrator threats (tenant admin / system admin)

| ID | Threat (STRIDE) | L | I | Mitigations → requirement | Phase | Residual |
|---|---|---|---|---|---|---|
| T-11 | Admin abuses privilege to access rooms beyond need-to-know (ID/EoP) | M | H | Privileged action logging with justification (SR-2.3, P1); RBAC roles with Auditor separation (FR-4.5, P2); break-glass only for content-level access (SR-1.5, P2.5) | P1–P2.5 | Medium until P2.5 |
| T-12 | Tenant admin attempts cross-tenant access (EoP/ID) | L | H | Realm-per-tenant identity (TR-6.5); RLS on every business table (SR-2.2, P2); bucket policy isolation (SR-2.1, P2); consoles scoped per-tenant (FR-11.2) | P2–P2.5 | Low |
| T-13 | Admin disables watermark/audit policy for a room (Tampering) | M | H | Global baselines (mandatory audit, watermarking, retention floors) enforced by policy engine with **no tenant override** (FR-5.2/TR-5.2, P2); policy decisions logged (TR-5.2) | P2 | Low |

### 5.4 Operator / infrastructure threats

| ID | Threat (STRIDE) | L | I | Mitigations → requirement | Phase | Residual |
|---|---|---|---|---|---|---|
| T-14 | Operator reads tenant documents via storage/DB/snapshot access (ID) | M | **Critical** | Envelope encryption: content unreadable without tenant KEK in Vault (SR-1.1/SR-1.4, P2); Ceph SSE-KMS secondary layer (SR-1.3, P2); until P2 lands, operator access is contractual + logged only — **flagged as top open risk for P0–P1** | P2 | High→Low at P2 |
| T-15 | Operator misuses Vault KEK (EoP/ID) | L | Critical | Per-tenant key-path ACLs (TR-1.4, P0); break-glass: time-boxed, multi-person approval, tenant-admin alert (SR-1.5, P2.5); Vault audit log to SIEM | P0–P2.5 | Medium until P2.5 |
| T-16 | Vault unseal keys / root token compromise (ID/EoP) | L | Critical | Shamir 5-of-3 split custody, root token revoked post-bootstrap (runbook); Vault TLS + NetworkPolicy isolation (P0); audit device enabled before any secret stored | P0 | Low-Medium |
| T-17 | Host/hypervisor-level compromise (EoP/ID) | L | H | K3s hardening (secrets-encryption, restricted admission, CIS-aligned config — `src/infra/k3s`); TLS on all internal hops (SR-1.2); disk encryption at VM layer; patching SLA (SR-4.4) | P0 | Medium |
| T-18 | K8s API abuse (forged identities, escalation) (EoP) | M | H | RBAC least privilege, NodeRestriction, audit logging to Loki; NetworkPolicies default-deny (P0); no cloud metadata exposure (self-hosted) | P0 | Low-Medium |
| T-19 | Snapshot/backup exfiltration (ID) | M | H | Backups encrypted; etcd snapshots access-controlled; restore drills (NFR-3.4); envelope encryption means DB/object dumps lack keys (P2) | P0–P2 | Medium→Low |
| T-20 | DoS against edge or cluster (Denial of Service) | M | M | Traefik rate limiting (TR-1.5); per-tenant quotas (SR-3.2, P1); K3s/etcd resource limits; cell isolation limits blast radius (SR-3.3) | P0–P1 | Medium |

### 5.5 Supply chain / platform threats

| ID | Threat (STRIDE) | L | I | Mitigations → requirement | Phase | Residual |
|---|---|---|---|---|---|---|
| T-21 | Vulnerable dependency or base image shipped (Tampering/ID) | H | H | Dependency scanning + Trivy image scan in CI with patch SLA (SR-4.1/SR-4.4, P0); SBOM per build (TR-1.3); scheduled re-scans of published images | P0 | Medium |
| T-22 | CI pipeline poisoning (malicious MR, runner hijack) (Tampering/EoP) | M | Critical | Protected default branch + review; self-hosted runner isolation (TR-1.3); secrets only via Vault/env, never literals (repo rule); Semgrep SAST on every pipeline | P0 | Medium |
| T-23 | Malicious/compromised Helm chart or container image upstream (Tampering) | M | H | Pinned chart/image versions in `src/infra`; private registry mirror plan; image verification at admission (post-P0 hardening, tracked T-21 follow-up) | P0+ | Medium |
| T-24 | Log/monitoring pipeline as data-leak path (document content in logs) (ID) | M | M | Structured logging conventions forbid content logging (repo rule); Loki/Prom access scoped; SIEM forwarding of audit only (TR-7.x) | P1 | Low |

### 5.6 Cross-tenant threats (SaaS cell)

| ID | Threat (STRIDE) | L | I | Mitigations → requirement | Phase | Residual |
|---|---|---|---|---|---|---|
| T-25 | Cross-tenant data access via any layer (ID/EoP) | L | Critical | Six-layer isolation: RLS, bucket policy, realm, NATS namespace, index scope, ClickHouse partition (SR-2.1, P2); tenant context set server-side only (SR-2.2) | P2 | Low |
| T-26 | Noisy neighbour degrades co-tenants (DoS) | M | M | Edge rate limits/quotas (SR-3.2, P1); fair-share conversion queue (TR-4.5, P2.5); cell-level capacity monitoring | P1–P2.5 | Low-Medium |

## 6. Leak Scenarios (abuse-case deep dives)

These are the scenarios a VDR exists to answer. Each ends with the evidence storey EVDR must produce.

**L-1 — External guest leaks a document to press/competitor.**
Path: photographs watermarked pages (T-02) or reconstructs via scraping (T-03). *Response storey:* watermark tokens (viewer identity, timestamp, IP/domain, session ID) identify the leaking session; audit trail shows exactly what was rendered, when, to whom; NDA acceptance evidence (FR-5.1) binds the recipient. Residual: leak itself is not preventable; attribution must survive watermark cropping attempts — watermark density/rotation presets (FR-1.3/FR-3.6) are the mitigation knob.

**L-2 — Authorised download leaks onward.**
Path: user with download tier exports file (T-08). *Response storey:* download is logged with actor/timestamp/IP; room export packages carry SHA-256 integrity letters (SR-5.2); Phase 4 adds recipient-specific export signatures (FR-9.3). PPAD R&D (ADR-0001) is the only planned cryptographic post-download control and is explicitly uncommitted.

**L-3 — Operator or infrastructure provider dumps storage.**
Path: Ceph/Postgres/backup exfiltration (T-14, T-19). *Response storey:* pre-P2 this is detected/deterred by privileged-action logging (SR-2.3) and access process; post-P2 envelope encryption makes plaintext recovery impossible without break-glass KEK access, which is multi-party, time-boxed, and alerts the tenant (SR-1.4/1.5). **This is the single largest open risk during P0–P1 and is why envelope encryption gates the MVP.**

**L-4 — Attacker gains a valid session (credential theft, link forwarding).**
Path: T-01/T-07. *Response storey:* MFA for internal users; session-scoped watermarking makes even authenticated leaks attributable; anomaly detection flags unusual-hours/mass access (FR-7.7, P4); time/IP/domain-restricted grants shrink the window (FR-4.4, P2).

**L-5 — Evidence destruction after a leak.**
Path: insider/admin attempts to purge audit rows or SIEM copy (T-10). *Response storey:* append-only Postgres schema (no UPDATE/DELETE grants), plus off-platform SIEM forwarding; room export integrity letter lets a third party verify the trail hasn't been rewritten.

## 7. Assumptions and Out of Scope (Phase 0)

- Physical/datacentre security of hosts is provided by the environment owner; EVDR assumes locked-down VM provisioning (hardening owned via Terraform/K3s baseline, not facilities).
- Enterprise IdP (AD/LDAP) security is inherited, not re-modelled; IdP compromise appears only as T-07.
- Endpoint security of viewers' devices is out of scope (drives ADR-0001's deterrence posture).
- DDoS beyond edge rate limiting (upstream/scrubbing) is an environment decision, not Phase 0 scope.
- Nation-state actors with prolonged host access: partially mitigated by encryption-at-rest design; full counter-APT programme out of scope for MVP.

## 8. Maintenance

- Re-reviewed at each phase gate (P1: viewer/SPI attack surface; P2: policy engine, audit, envelope encryption; P2.5: tenancy/control plane; P3: AI data paths; P4: analytics/webhooks).
- New threats from penetration tests (first at P2 exit, SR-4.2) and incidents are appended with IDs; entries are not deleted, only re-scored.
- Owner: Infrastructure/Security Engineer; approver: Security Lead. Sign-off of this v0.1 is a Phase 0 exit criterion.

### Sign-off record (v0.1)

- **Approved:** 2026-08-17, for the Phase 0 Foundation Gate.
- **Approval mechanism:** the Phase 0 build is single-operator — the project owner holds all Section 12 roles, including the Security Lead approver role. Approval was recorded by the autonomous build agent acting under the owner's standing directive; there is no second human in the loop to co-sign.
- **Review performed before approval:** full-document check against SR-4.3's required coverage — actors (§3), trust boundaries (§2), threat register spanning internal users, external parties, administrators, operators, supply chain, and cross-tenant (§5.1–§5.6), and the defining leak scenarios (§6). Coverage is complete; residual risks T-02 (Medium-High, screenshot photography) and T-14 (High until P2 envelope encryption) are explicitly accepted with named phase gates.
- **Caveat (binding):** this satisfies the Phase 0 gate for the lab build only. SR-4.3's intent is an *independent* security review; before any production deployment or real tenant data, this document must be re-reviewed and signed by a Security Lead who is not the build operator. Tracked under Todo.md cross-phase recurring activities.

| Version | Date | Change | Approver |
|---|---|---|---|
| 0.1 | 2026-08-16 | Phase 0 baseline | Project owner, 2026-08-17 (solo-operator self-approval — see record above) |
