# EVDR — Build Plan & Phase Checklist

> **Working document** | Derived from `Requirements/EVDR-Functional-and-Technical-Requirement-Specifications.md` v1.0 (Section 11 Build Phase Plan, expanded with full FR/TR/SR mappings from Sections 4–6).
>
> **How to use:** tick `- [ ]` checkboxes as activities complete. A phase is done only when every **Exit criterion** is verifiably met — exit criteria are the gates; activities are the means. Requirement IDs in parentheses trace each activity back to the FTRS.

---

## Milestones & Gates Overview

| Milestone | End of | Week | Definition |
|---|---|---|---|
| **Foundation Gate** | Phase 0 | 3 | Threat model, IaC baseline, CI/CD security, Room SPI contract frozen |
| **MVP Gate** ✦ | Phase 2 | 15 | Compliance-grade core exchange: viewer, watermarking, policy engine, audit, NDA, export, encryption |
| **Commercial Launch Gate** ✦ | Phase 4 | 26 | Section 13 criteria (8 gates) all verifiable — see [Launch Gate Checklist](#commercial-launch-gate-checklist) |
| Optional R&D exit | Phase 5 | 27+ | Clean-room prototypes evaluated; mainland cell readiness assessed |

- **Phase 2.5 runs in parallel with Phase 3** (different workstreams: control-plane vs product intelligence) — do not serialize them.
- Phases count: **6 sequential + 1 parallel track (2.5) + 1 optional track (5)**.

---

## Phase 0 — Foundation and Control Plane (Weeks 1–3)

**Objective:** Establish the security-first foundation before any product code: threat model, data classification model, reproducible infrastructure-as-code, CI/CD with security scanning from day one, and the Room SPI storage contract that Phase 1 implements.

> **Status note (2026-08-16):** All Phase 0 *code and document artifacts* are authored and locally verified (Go builds/vets/tests, Terraform fmt+validate clean, YAML/shell parse-checked, Semgrep rules self-tested, sample-service image built and response-checked). Items left unticked below require either human sign-off or execution on provisioned hosts (K3s bootstrap, Vault init/unseal, runner registration, green pipeline run, tear-down/rebuild drill). See `docs/runbooks/phase-0-foundation-rebuild.md` for the execution sequence.

### Entry Criteria

- [ ] Core team onboarded: Platform/Backend Engineer, Frontend/Product Engineer, Infrastructure/Security Engineer (Section 12)
- [ ] Git repository + GitLab project created with protected default branch
- [ ] Target infrastructure environment (VMs/hosts for K3s) provisioned and accessible
- [ ] ADR (architecture decision record) process agreed

### Build Activities

**Governance & Security**

- [x] Author threat model covering internal users, external parties, administrators, operators, and leak scenarios (SR-4.3) — `docs/security/threat-model.md` v0.1, sign-off pending
- [x] Define data classification and retention policy model (FR-5.4, NFR-7.5) — `docs/security/data-classification-and-retention.md`, approval pending
- [x] Record DRM strategy decision: view-first default, controlled export model — bounded R&D track for PPAD (FR-3.7) — `docs/ADR/0001-drm-strategy-view-first.md` (Accepted)

**Infrastructure & IaC**

- [x] Terraform IaC baseline: VMs, networks, storage classes; cloud-agnostic (TR-1.2) — `src/infra/terraform/` module contract + libvirt reference impl; fmt/validate clean
- [ ] Bootstrap K3s cluster — Docker + Kubernetes runtime (TR-1.1)
- [ ] Deploy HashiCorp Vault: dynamic secrets, encryption-as-a-service, PKI (TR-1.4)
- [ ] Deploy Traefik reverse proxy / API gateway with automatic TLS (TR-1.5)
- [ ] Enforce TLS 1.2/1.3 on all connections incl. internal service-to-service (SR-1.2)
- [ ] Network isolation baseline: shared VPC with egress controls (SR-3.1)

**CI/CD Security Pipeline**

- [ ] GitLab CI with self-hosted runner (TR-1.3)
- [ ] SAST — Semgrep (TR-1.3, SR-4.1)
- [ ] DAST — OWASP ZAP (TR-1.3, SR-4.1)
- [ ] Dependency scanning (TR-1.3, SR-4.1)
- [ ] SBOM generation (TR-1.3, SR-4.1)
- [ ] Trivy container image scanning in CI with vulnerability alerting and patch SLA (SR-4.4)

**Contracts**

- [x] Draft Room SPI interface contract: `CreateRoom`, `GrantAccess`, `RevokeAccess`, `PutDocument`, `GetRenderStream`, `ListVersions`, `ApplyRetention`, `ExportRoom`, `SealRoom` (TR-2.1) — `src/spi/` v0.1.0, builds + vets clean

### Exit Criteria

- [ ] Threat model reviewed and signed off by security; scheduled for per-phase update (SR-4.3)
- [ ] IaC baseline reproducible — full tear-down/rebuild of K3s + Vault + Traefik from code via documented runbook
- [ ] CI/CD pipeline green against a sample service with SAST, DAST, dependency scan, and SBOM stages all reporting
- [x] Room SPI contract v0.1 frozen for Phase 1 implementation — `ContractVersion = "0.1.0"` in `src/spi/interface.go`; change policy in `src/spi/README.md`
- [x] DRM strategy decision recorded as ADR — `docs/ADR/0001-drm-strategy-view-first.md`
- [ ] Data classification and retention model approved

---

## Phase 1 — Core Secure Exchange and Branded Rooms (Weeks 4–9) `[MVP starts]`

**Objective:** Deliver the core product loop: internal users create branded rooms, upload documents through the Room SPI, and external guests view them through a page-streaming watermarked viewer via expiring no-account links — all on hardened self-hosted infrastructure with full observability.

### Entry Criteria

- [ ] All Phase 0 exit criteria met
- [ ] Room SPI contract frozen
- [ ] CI/CD deploy-on-green operational
- [ ] Infra baseline (K3s, Vault, Traefik) reproducible

### Build Activities

**Data & Storage Layer**

- [ ] Deploy Ceph RGW via Rook operator — S3 API, SigV2+V4, versioning, lifecycle, bucket notifications (TR-2.5)
- [ ] Deploy PostgreSQL 16 (TR-2.11)
- [ ] Deploy Redis 7 (TR-2.12)
- [ ] Deploy hardened Nextcloud: TLS, Postgres backend, Redis file locking, backup strategy, network-separated from external-facing services (TR-2.13)

**Room SPI & Adapter**

- [ ] Implement Room SPI as Go interface (TR-2.1)
- [ ] Implement NextcloudAdapter wrapping OCS/WebDAV APIs (TR-2.2)
- [ ] Build SPI conformance suite; run against adapter in CI on every merge (TR-2.4)

**Identity & Access**

- [ ] Deploy Keycloak; federate SAML 2.0 / OIDC with enterprise AD/LDAP for internal users (FR-4.1, TR-6.1)
- [ ] Connect Nextcloud to Keycloak as SSO provider (TR-6.2)
- [ ] MFA for internal users: TOTP + WebAuthn/FIDO2 (FR-4.2)
- [ ] External guest access: expiring secure links with password or one-time OTP — no account creation (FR-4.3, TR-6.3)

**External Portal (Frontend)**

- [ ] Next.js 15 external portal — App Router, TypeScript, SSR for branded room pages (TR-3.1)
- [ ] shadcn/ui + Tailwind CSS owned-component baseline for rapid per-room theming (TR-3.2)
- [ ] Zustand client state + TanStack Query server state against API gateway (TR-3.3)
- [ ] Separate external portal vs admin console apps with distinct navigation and access models (FR-4.7, TR-3.4)
- [ ] Branded rooms: custom logo, colour theme, metadata, About page, counterparty-specific branding (FR-1.1)

**Rooms & Documents**

- [ ] Room-level permission tiers: view-only / download-allowed / print-allowed / edit-allowed (FR-1.2)
- [ ] Room watermark policy presets: density, opacity, rotation, token selection (viewer identity, timestamp, IP/domain, session ID) (FR-1.3, FR-3.6, TR-4.6)
- [ ] Document upload with progress indication and size validation — PDF, DOCX, XLSX, PPTX, images, scanned PDFs (FR-2.1)
- [ ] Automatic indexing and version tracking on upload (FR-2.2)
- [ ] Folder hierarchy within rooms (FR-2.3)
- [ ] Drag-and-drop upload with auto-indexing (FR-2.4)
- [ ] Upload-only File Drop links for external submission without room visibility (FR-6.1)

**Secure Viewer**

- [ ] Page-streaming viewer on PDF.js — serve rendered pages individually, never full documents (FR-3.1, TR-4.1)
- [ ] Server-rendered dynamic watermarking baked into output before browser delivery (FR-3.2, TR-4.2)
- [ ] View-only enforcement: block download, print, copy/paste (FR-3.3)
- [ ] Office-to-PDF conversion pipeline — LibreOffice headless (FR-3.4, TR-4.3)

**Events & Observability**

- [ ] NATS JetStream event bus with subject namespace `evdr.{cell}.{tenant}.>` (TR-7.1)
- [ ] Loki application log aggregation (TR-7.5)
- [ ] Prometheus + Grafana infrastructure monitoring — Node exporter, cAdvisor, service metrics (TR-11.1)
- [ ] Grafana Alerting → Slack/Email/Teams webhooks (TR-11.2)
- [ ] Per-tenant rate limits and quotas at Traefik edge — first noisy-neighbour control (SR-3.2)
- [ ] Privileged action logging: actor identity, action, target, timestamp, IP, justification (SR-2.3)

**API**

- [ ] Next.js API routes for portal CRUD baseline (TR-10.1)

### Exit Criteria

- [ ] End-to-end flow demonstrable: internal user creates branded room → uploads DOCX → auto-converts to PDF → external guest opens expiring OTP link → views server-watermarked pages one at a time with no download path
- [ ] SPI conformance suite green against NextcloudAdapter in CI
- [ ] Nextcloud backup restore tested
- [ ] Keycloak SSO + MFA working for internal users; guest OTP access requires no account
- [ ] Monitoring dashboards live; all services shipping logs to Loki; alert routing verified
- [ ] NFR spot-checks pass: first-page render < 500ms for < 50-page documents (NFR-1.1); Office-to-PDF < 30s for < 100-page documents (NFR-1.5); watermark stamping < 300ms/page (NFR-1.6)

---

## Phase 2 — Governance, Audit, NDA, Evidentiary Export (Weeks 10–15) `[MVP ends]`

**Objective:** Turn the exchange into a compliance-grade platform: a policy engine with non-overridable baselines, RBAC/ABAC with revocation, an immutable audit trail piped to SIEM, NDA gating before first access, evidentiary export with integrity proof, and envelope encryption that locks operators out of tenant documents. First penetration test closes the phase.

### Entry Criteria

- [ ] Phase 1 exit criteria met — core exchange loop stable
- [ ] NDA / e-signature approach selected (DocuSign / HelloSign / OpenSign iframe+webhook, or recorded-acceptance fallback) (TR-5.3)
- [ ] Internal SIEM destination(s) identified for pipeline verification

### Build Activities

**Policy Engine**

- [ ] Go policy microservice as Policy Decision Point in front of Room SPI (TR-5.1)
- [ ] Embedded OPA (Rego) global baseline policies — mandatory audit, watermarking, retention floors — no tenant override (TR-5.2, FR-5.2)
- [ ] Tenant-configurable policy overlays: IP allow-lists, download tiers, AI-processing consent, custom retention (FR-5.3)
- [ ] Every policy decision carries `tenant_id` and is logged (TR-5.2)

**Access Control**

- [ ] Time-bound and IP/domain-restricted grants with instant revocation (FR-4.4)
- [ ] RBAC roles — Room Owner, Room Contributor, Room Viewer, Auditor, System Administrator — with folder-/file-level granularity (FR-4.5)
- [ ] ABAC conditions: time-of-day, device posture, geo-location (FR-4.6)
- [ ] Room expiration and auto-revocation schedules (FR-1.4)
- [ ] Bulk permission templates for recurring exchange scenarios (FR-1.5)
- [ ] Legal-hold / room seal — freeze documents and metadata for eDiscovery (FR-1.6)

**Audit & Compliance**

- [ ] Immutable append-only audit log in PostgreSQL — no UPDATE/DELETE privileges on audit schema (FR-7.1, TR-7.2, SR-5.1)
- [ ] All viewer actions, policy decisions, file accesses, and admin operations emit `tenant_id` events to NATS — no silent data paths (TR-7.6)
- [ ] ClickHouse analytics store — `tenant_id` leading partition key, per-tenant query quotas (TR-7.3)
- [ ] Fluent Bit SIEM forwarding pipeline → syslog / Elastic Agent (FR-7.2, TR-7.4, SR-5.3)
- [ ] Per-tenant audit export packages in ISO 27001 / SOC 2 evidence format (FR-7.3)
- [ ] NDA / e-signature gate before first room or file access with durable evidence retention — timestamp, IP, identity, document version (FR-5.1, TR-5.3)
- [ ] Full room export package: documents + activity logs + SHA-256-backed integrity letter (FR-1.7, TR-5.4, SR-5.2)

**Encryption & Tenant Isolation**

- [ ] Envelope encryption: per-document DEK wrapped by tenant KEK held in Vault (SR-1.1, TR-2.8)
- [ ] Operator document access cryptographically impossible without audited break-glass KEK access (SR-1.4)
- [ ] Ceph SSE-KMS with Vault KMS backend, tenant-scoped keys (SR-1.3, TR-2.9)
- [ ] PostgreSQL RLS: `tenant_id UUID NOT NULL` on every business table; tenant-scoped session variable set server-side at authentication, never from client parameters (SR-2.2, TR-2.11)
- [ ] Tenant isolation verified across RLS, bucket policy, realm boundaries, stream namespaces, index scoping, partition predicates (SR-2.1)
- [ ] Break-glass operator model: time-boxed, audit-logged, alertable to tenant admins, multi-person approval (SR-1.5)

**Viewer Hardening & Inbound Exchange**

- [ ] Blur-on-focus-loss and keyboard-shortcut interception — screenshot-friction deterrence (FR-3.5, TR-4.4)
- [ ] File Drop links: configurable expiration, password protection, upload-size limits (FR-6.2)
- [ ] Virus/malware scanning on uploaded files before room acceptance (FR-6.3)

**Security Validation**

- [ ] First penetration test; remediate critical findings before gate (SR-4.2)

### Exit Criteria — ✦ MVP GATE

- [ ] Policy engine enforces non-overridable baselines on every access path
- [ ] Audit trail demonstrably append-only — attempted UPDATE/DELETE fails; every view, download, print, policy decision, and admin action is present
- [ ] NDA gate blocks first access until acceptance and retains signed evidence
- [ ] Room export regenerates with a verifiable SHA-256 integrity letter
- [ ] Envelope encryption live: documents unreadable without tenant KEK; break-glass model documented and alerting
- [ ] RLS verified — cross-tenant query returns zero rows
- [ ] SIEM forwarding verified end-to-end to at least one destination
- [ ] Penetration test complete with zero open critical findings
- [ ] NFR checks: 50 concurrent uploading users per tenant without degradation (NFR-1.2); API P95 < 200ms CRUD, < 1s complex audit queries (NFR-1.4)

---

## Phase 2.5 — Commercialisation Track (Weeks 16–20, parallel with Phase 3)

**Objective:** Build the multi-tenant control plane: the NativeAdapter for the shared SaaS cell, automated tenant provisioning in under 15 minutes, split tenant-admin and platform-operator consoles, usage metering, cell stamping, and the suspension/offboarding lifecycle including cryptographic erasure — validated by a pilot with 2–3 internal business units.

### Entry Criteria

- [ ] MVP gate (Phase 2) passed — control plane depends on governance/audit foundation
- [ ] NextcloudAdapter + SPI conformance suite stable enough to certify a second adapter
- [ ] Tier 1 shared-cell infrastructure plan approved (network, Ceph capacity, VPC layout)

### Build Activities

**Storage**

- [ ] Build NativeAdapter: S3-compatible object store + PostgreSQL metadata + Redis — no Nextcloud in the shared cell (TR-2.3)
- [ ] NativeAdapter passes the shared SPI conformance suite in CI — parity with NextcloudAdapter (TR-2.4)
- [ ] Per-tenant RGW user + bucket provisioned via Rook ObjectBucketClaims (TR-2.10)

**Tenant Provisioning**

- [ ] Tenant Provisioner — idempotent onboarding engine: Keycloak realm, Postgres RLS role, RGW user/bucket, Vault KEK + key-path ACL, NATS subjects/streams, OpenSearch index + ILM, DNS/branding, quotas, feature flags (TR-12.1, FR-11.1)
- [ ] Keycloak realm automation via Admin API — hundreds of realms supported (TR-6.5)
- [ ] Separate platform-operator realm; every operator action on a tenant realm is break-glass, audited, alertable (TR-6.4)
- [ ] Tenant Config & feature flags: per-tenant settings, branding assets, watermark presets, retention policies, AI consent flags (TR-12.2)

**Consoles**

- [ ] Tenant Admin Console — rooms, users, policies, analytics, audit export, billing, scoped per-tenant (FR-11.2)
- [ ] Platform Operator Console — cell health, tenant lifecycle, quota management, break-glass access (FR-11.3)

**Metering & Cell Operations**

- [ ] Metering pipeline: seats, storage GB, pages viewed, conversions, AI tokens, e-sign events — NATS → ClickHouse rollups → rating (FR-11.4, TR-12.3)
- [ ] Cell stamping: umbrella Helm chart with per-tier values — one artifact set stamps Tier 0–3 cells (TR-1.6)
- [ ] Cell isolation at network layer — no cross-cell storage or database connectivity (SR-3.3)
- [ ] Conversion queue: per-tenant fair-shared worker pool with autoscaling (TR-4.5)
- [ ] Tenant suspension: revoke all sessions, block edge access, preserve audit retention (FR-11.7)
- [ ] Tenant offboarding with cryptographic erasure: export package → delete realm, bucket, KEK, index, partitions, Vault paths → certificate of erasure (FR-11.8)
- [ ] Pilot: onboard 2–3 internal business units as real tenants (FR-11.1)

**Decision Spikes (Open decisions due this phase)**

- [ ] OPA vs AWS Cedar for policy overlay language — spike (#3)
- [ ] Self-built license verification vs Keygen-style server — spike (#4)
- [ ] Vault Enterprise namespaces vs OSS path-prefix ACLs — decide at ~50 tenants or first BYOK deal (#2)

### Exit Criteria

- [ ] Tenant onboarded end-to-end in < 15 minutes, demonstrated repeatedly
- [ ] NativeAdapter and NextcloudAdapter both pass the identical SPI conformance suite in CI
- [ ] Suspension and offboarding demonstrated on a pilot tenant with erasure certificate issued
- [ ] Operator break-glass access triggers multi-person approval, audit entry, and tenant-admin alert
- [ ] Metering events flowing for all metered dimensions; pilot tenants actively using the platform

---

## Phase 3 — Intelligence, Editing, Workflow Integration (Weeks 16–22)

**Objective:** Add the differentiators: full-text search with OCR across the tenant corpus, PII detection and redaction before external release, AI classification/summarisation/translation under a per-tenant consent switch, in-browser Office editing, and the REST/MCP integration surface for agentic and workflow automation.

### Entry Criteria

- [ ] MVP gate (Phase 2) passed — search and AI build on the stable upload + audit pipeline
- [ ] AI model approach confirmed: commercial API for speed first, self-hosted Qwen 2.5 via vLLM as sovereignty target (TR-8.4)
- [ ] Collabora vs ONLYOFFICE evaluation scheduled — decision due at phase start (#6)

### Build Activities

**Search & OCR**

- [ ] OpenSearch full-text indexing — index-per-tenant with ILM for isolation and clean erasure (TR-8.1)
- [ ] Tesseract OCR for scanned PDFs and images before indexing; EN + ZH language packs (TR-8.2, FR-8.1, NFR-6.3)

**PII Detection & AI Services**

- [ ] Presidio PII detection across 100+ data types as pre-release check (TR-8.3, FR-8.2)
- [ ] Redaction pipeline for PII masking with audit evidence of redactions applied (FR-8.3)
- [ ] FastAPI AI microservice + LLM: document classification and auto-routing into folders/categories (FR-8.4, TR-8.4)
- [ ] AI summarisation of uploaded documents for room-level overview (FR-8.5)
- [ ] AI translation for cross-border exchange — Chinese/English primary (FR-8.6)
- [ ] BGE-M3 embeddings for semantic and cross-lingual retrieval (TR-8.5)
- [ ] Per-tenant AI-processing consent switch enforced before content reaches any model, with audit evidence of enforcement (FR-8.7)
- [ ] Per-tenant AI model routing: pin "no external AI" vs "external APIs allowed" (TR-8.6)
- [ ] AI usage metering: token counts, pages OCR'd, documents summarised (TR-8.7)

**Office Editing**

- [ ] Collabora Online (CODE) in-browser editing with version history and Nextcloud integration (FR-2.6, TR-9.1)
- [ ] ONLYOFFICE fallback evaluation only if Collabora compatibility proves insufficient — run one, not both (TR-9.2)

**Integration & API Surface**

- [ ] REST API: room CRUD, document management, permission administration, audit queries (FR-10.1, TR-10.1)
- [ ] MCP server (FastAPI) for agentic AI orchestration — programmatic rooms, document processing, audit queries (FR-10.2, TR-10.2)
- [ ] Outlook Add-in + Gmail Extension — replace attachments with governed secure links (FR-10.4, TR-10.4)

**Governance Extensions**

- [ ] Data classification labels on documents with policy-driven access rules (FR-5.4)
- [ ] Configurable retention and auto-purge schedules per document class (FR-5.5)
- [ ] Per-tenant SIEM forwarding — tenant-configurable destinations (FR-7.4)
- [ ] Key lifecycle: automatic KEK rotation schedule, DEK rotation on re-upload, BYOK for Tier 2 (SR-1.6)

### Exit Criteria

- [ ] Full-text search < 2s across a tenant corpus up to 1M documents (NFR-1.3)
- [ ] PII detection flags sample documents; redaction pipeline produces audit evidence
- [ ] AI-consent-OFF tenant: provable from audit trail that zero document content reached any model
- [ ] Office editing round-trips DOCX/XLSX/PPTX with version history through the repository
- [ ] REST API + MCP server documented and exercised by an external script/agent
- [ ] Classification labels drive access decisions through the policy engine
- [ ] Per-tenant SIEM forwarding live for at least one tenant-configured destination

---

## Phase 4 — Analytics, Leak Detection, Business Integrations (Weeks 23–26)

**Objective:** Reach the commercial launch gate: page-level engagement analytics and dashboards, leak-detection and anomaly alerting, outbound webhooks and CRM integrations, billing GA with Stripe, the offline-capable license server for Tier 3, and SOC 2 Type I completion.

### Entry Criteria

- [ ] Phase 3 exit criteria met
- [ ] Phase 2.5 complete — tenant, metering, and cell-stamping foundation in place
- [ ] SOC 2 Type I audit window booked
- [ ] Stripe account and billing model ratified

### Build Activities

**Analytics & Telemetry**

- [ ] Viewer telemetry pipeline: PDF.js instrumentation → NATS → ClickHouse — page-enter/exit events, dwell times, scroll depth (FR-7.5, TR-11.3)
- [ ] Per-tenant analytics dashboards: room- and document-level engagement via Apache ECharts (FR-9.1, TR-11.4)
- [ ] Per-external-party interest summaries: documents viewed, time spent, download activity (FR-9.2)
- [ ] Room heatmaps and real-time open/view notifications (FR-7.6)

**Leak Detection & Alerting**

- [ ] Leak-detection measures: canary tokens, invisible fingerprinting, recipient-specific export signatures (FR-9.3)
- [ ] Anomaly detection alerts: mass downloads, unusual viewing hours, abnormal sharing patterns (FR-7.7)

**Integrations**

- [ ] Webhook emitter (Go) listening on NATS → tenant-configured webhook URLs (FR-10.3, TR-10.3)
- [ ] CRM integrations: Salesforce, HubSpot via API and webhook (FR-9.4)
- [ ] Zapier/Make-style no-code webhook compatibility (FR-9.5)

**Commercialisation GA**

- [ ] Billing GA: Stripe for card/SEPA; usage-CSV/API export for enterprise invoicing (FR-11.5)
- [ ] License server for Tier 3: Ed25519 signed license files, node-locked or floating seats, offline activation, timed grace periods, air-gap capable (FR-11.6, TR-12.4)
- [ ] Per-tenant custom domains: CNAME + dns-01 TLS automation (FR-11.10)
- [ ] Tenant tier upgrade automation (Tier 1 → 2): cell stamping + data migration with brief maintenance window (FR-11.9)
- [ ] Garage single-binary storage for single-box Tier 3 appliances (TR-2.6)

**Platform Hardening**

- [ ] Large-file handling — multi-GB range (FR-2.5)
- [ ] Room Q&A workflow: threading, ownership assignment, status tracking (FR-1.8)
- [ ] SOC 2 Type I complete; Type II observation period in progress (NFR-7.2, FR-7.8)

### Exit Criteria — ✦ COMMERCIAL LAUNCH GATE

- [ ] All eight Section 13 gate criteria verifiable — see [Launch Gate Checklist](#commercial-launch-gate-checklist)
- [ ] Scale validation: 500+ concurrent users per tenant, 5,000+ per SaaS cell (NFR-2.1); 99.9% cell uptime trajectory (NFR-3.1)
- [ ] Backup/restore and DR targets demonstrated: RTO < 4h, RPO < 1h, audit RPO < 15 min (NFR-3.2–3.3)

---

## Phase 5 — Optional Privacy-Preserving Analytics (Weeks 27+)

**Objective:** Bounded R&D beyond the core product: evaluate TEE-based and MPC-based clean-room analytics for collaborative analysis without raw-record exposure, and assess the Mainland China cell build (CubeFS, domestic Kubernetes, PIPL compliance).

### Entry Criteria

- [ ] Commercial launch shipped; business case for clean-room analytics approved
- [ ] Mainland China cell trigger criteria evaluated against business decision (#5)

### Build Activities

- [ ] PrivacyGo Data Clean Room evaluation — TEE-based collaboration (FR-12.1)
- [ ] MP-SPDZ secure multi-party computation prototype (FR-12.1)
- [ ] Governed query interface — only aggregated or policy-approved outputs released (FR-12.2)
- [ ] Control-model separation: document rooms vs analytic clean rooms (FR-12.3)
- [ ] Mainland China cell evaluation: CubeFS storage, domestic K8s (Aliyun ACK / Tencent TKE), Harbor-mirrored images, PIPL-by-construction (TR-2.7, NFR-7.4)
- [ ] Re-evaluate Garage v3 if versioning + SSE-KMS land (#8, Phase 4 review carry-forward)

### Exit Criteria

- [ ] TEE and MPC prototypes evaluated with a documented go/no-go recommendation
- [ ] Governed query interface provably releases only aggregated outputs
- [ ] Mainland cell technical-readiness checklist complete; trigger recommendation recorded

---

## Cross-Phase Recurring Activities

Run continuously; do not defer to phase boundaries:

- [ ] Threat model updated per phase (SR-4.3)
- [ ] Annual penetration tests after the first (Phase 2); remediation SLA for criticals (SR-4.2)
- [ ] Container base-image vulnerability alerting and patch SLA (SR-4.4)
- [ ] Postgres + Ceph daily automated snapshots; restore drills (NFR-3.4)
- [ ] Per-tenant quota and rate-limit review as tenant count grows
- [ ] Revisit Zitadel-vs-Keycloak if realm automation proves painful at scale — review at > 100 tenants (#7)

---

## Open Decisions Tracker

| # | Decision | Status | Due | Blocker Risk If Slipped |
|---|---|---|---|---|
| 1 | Custom domains (CNAME + dns-01) at launch vs Phase 4+ | Open | Phase 3 review | Launch-gate scope (FR-11.10) |
| 2 | Vault Enterprise namespaces vs OSS path-prefix ACLs | Open | Phase 2.5 (~50 tenants / first BYOK) | Key-path ACL rework |
| 3 | OPA vs AWS Cedar for policy overlays | Open | Phase 2.5 spike | Policy engine rewrite |
| 4 | Self-built license verification vs Keygen-style server | Open | Phase 2.5 spike | Tier 3 licensing (FR-11.6) |
| 5 | Mainland China cell trigger criteria | Open | Business decision | Phase 5 evaluation sequencing |
| 6 | Collabora vs ONLYOFFICE for production | Open | Phase 3 start | Editing workstream (TR-9.1–9.2) |
| 7 | Zitadel vs Keycloak at realm scale | Deferred | Review at > 100 tenants | Identity migration cost |
| 8 | Garage v3 for appliance tier (versioning + SSE-KMS) | Deferred | Phase 4 review | Tier 3 appliance storage (TR-2.6) |

---

## Commercial Launch Gate Checklist

All eight must be verifiable before external commercial launch (FTRS Section 13):

- [ ] 1. NativeAdapter conformance parity with NextcloudAdapter verified by CI suite
- [ ] 2. Penetration test of the shared SaaS cell passed with no critical findings
- [ ] 3. SOC 2 Type I audit complete; Type II observation period in progress
- [ ] 4. Tenant onboarding demonstrated in < 15 minutes across ≥ 10 test tenants
- [ ] 5. Billing cycle executed end-to-end (Stripe + usage metering)
- [ ] 6. Break-glass transparency features demonstrated to ≥ 2 prospect security teams
- [ ] 7. Per-tenant SIEM forwarding operational for ≥ 3 tenant-configured destinations
- [ ] 8. AI consent switch enforced with audit evidence for ≥ 2 tenant configurations
