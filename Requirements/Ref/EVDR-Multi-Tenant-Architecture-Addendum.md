# EVDR — Multi-Tenant Architecture & Stack Addendum

> Version 1.1 → **1.2 (storage revised)** | August 2026 | Companion to `Requirements/EVDR-Technology-Stack-Recommendation.md` v1.0
>
> Delivery decision (locked 2026-08-16): **hybrid commercialization** — an operator-run multi-tenant SaaS from Hong Kong for smaller partners and business units, **plus** dedicated and on-prem deployments for sovereignty-sensitive customers. One codebase, four deployment tiers.
>
> **Storage revision (Aug 2026):** MinIO replaced by **Ceph RGW (via Rook)**; Garage reserved for single-box on-prem appliances; CubeFS designated for the future mainland-China cell. Envelope encryption (per-document DEK under tenant KEK in Vault) is now the **primary** tenant-key control, with SSE-KMS as the secondary at-rest layer. Full analysis and sources: `Requirements/EVDR-Object-Storage-Alternatives-Analysis.md`.

---

## 1. Why This Addendum Exists

The v1.0 stack recommendation describes an excellent **single-tenant deployment**. The moment the platform serves more than one organization — multiple business units, partner banks, then paying customers — three things break unless they are designed in from the start:

1. **Nextcloud has no native multi-tenancy.** No `tenant_id` in its schema, no per-tenant encryption keys, and a user/group model that is global to an instance. It cannot safely host untrusted tenants side by side.
2. **The data layer has no tenant boundaries.** Postgres, the object store, OpenSearch, ClickHouse, and NATS as specced in v1.0 are all single-tenant by construction.
3. **There is no control plane.** Nothing provisions, meters, bills, suspends, or erases a tenant.

This addendum overlays a tenancy model on the v1.0 stack with **one structural change** (how Nextcloud fits — Section 3) and a set of per-layer deltas (Section 5). Everything else in v1.0 stands.

### What is a "tenant"?

A tenant is an **organization boundary**: a business unit, a partner bank, a customer company. Rooms, documents, users, policies, branding, encryption keys, audit trails, and quotas are all tenant-scoped. A human (e.g., an auditor) may act in multiple tenants but receives **separate principals and separate sessions** — identity correlation happens only in the control plane's metadata, never in the data plane. This is the isolation posture bank buyers and regulators expect.

---

## 2. Tenancy & Deployment Tiers

| Tier | Model | Who it serves | Isolation | Operator |
|---|---|---|---|---|
| **Tier 0 — Internal** | Single-tenant install; business units modeled as tenants | Internal bank use (first build) | Logical (`tenant_id`), one organization trusted | Us |
| **Tier 1 — Shared SaaS** | Multi-tenant cell, HK region | Smaller partners, FIs, professional firms | Logical + per-tenant keys + RLS | Us |
| **Tier 2 — Dedicated** | Single-tenant cell per customer, our HK cloud/colo | Sovereignty-sensitive banks; customer-managed keys | Full stack per tenant | Us |
| **Tier 3 — On-prem** | Customer-operated, licensed | Regulated enterprises; China-market candidates | Physical; air-gap capable | Customer |

All four tiers ship from the **same artifact set**: one umbrella Helm chart with per-tier values files. Tier 0 is deliberately run with tenancy enabled ("tenant-ready, single-tenant deployed") so the internal build becomes the proving ground for the commercial control plane — HSBC's business units are tenants #1–N.

---

## 3. The Core Structural Change: Nextcloud Behind a Storage SPI

### The problem

v1.0 uses Nextcloud as the repository and sharing core. For Tier 0/2/3 this remains correct. For a shared multi-tenant SaaS cell it is not viable:

- No tenant isolation in the data model or key management; one compromised tenant's PHP process boundary is every tenant's boundary.
- Instance-per-tenant at SaaS scale (hundreds of small tenants) becomes a fleet-management problem: upgrades, resource efficiency, and per-tenant ops cost all deteriorate.
- Per-tenant branding, watermark policy, and lifecycle (instant erasure) fight the product's architecture at every step.

### The decision: Storage & Sharing SPI with two adapters

Define a Go interface boundary — the **Room SPI** — that everything above the storage layer programs against:

```
Room SPI (Go):  CreateRoom / GrantAccess / RevokeAccess / PutDocument / GetRenderStream
                ListVersions / ApplyRetention / ExportRoom / SealRoom (legal hold)
```

Two adapters implement it:

| Adapter | Used in | Implementation |
|---|---|---|
| **NextcloudAdapter** | Tier 0, 2, 3 | Wraps Nextcloud's OCS/WebDAV APIs; one Nextcloud instance per deployment. Keeps v1.0's Phase 0–2 plan fully intact. |
| **NativeAdapter** | Tier 1 (shared SaaS) | Purpose-built on any S3-compatible object store (Ceph RGW primary — see the storage alternatives analysis) + Postgres (room/grant/version metadata) + Redis. No Nextcloud in the shared cell. |

Everything above the SPI — portal, secure viewer, policy engine, audit pipeline, AI services, MCP server — is **tenant-aware and storage-agnostic**. This is the single most important architectural move for commercialization:

- The internal build (v1.0 plan) ships unchanged on Nextcloud.
- The SaaS cell gets a clean, cheap-per-tenant core with per-tenant keys and instant erasure.
- Adapter parity is enforced by a shared conformance test suite run in CI against both adapters.

**Alternative rejected:** instance-per-tenant Nextcloud everywhere (viable for dozens of tenants, uneconomic for hundreds; upgrade orchestration across a fleet is a full-time team). **Alternative deferred:** drop Nextcloud entirely (would discard v1.0's Phase 1 storage foundation and re-plan the internal build).

---

## 4. Reference Architecture — Shared SaaS Cell (Tier 1)

```
                ┌─────────────────────────────────────────────────────────┐
                │               GLOBAL CONTROL PLANE                     │
                │  Tenant Provisioner · Config & Feature Flags           │
                │  Metering & Billing · License Server (on-prem tiers)   │
                │  Platform Operator Console (cross-tenant, break-glass) │
                │  ── metadata only; documents never transit here ──     │
                └────────────┬────────────────────────────┬──────────────┘
                     provisions / meters │                │ signed licenses
        ┌────────────────────────────────┴───┐   ┌────────┴─────────────────┐
        │      SHARED SAAS CELL — HK         │   │  DEDICATED / ON-PREM     │
        │      (replicate per region)        │   │  Same chart, tenant=1,   │
        │                                    │   │  NextcloudAdapter        │
        │  Traefik edge: tenant resolution   │   └──────────────────────────┘
        │  ({tenant}.evdr.hk / custom CNAME) │
        │  ┌─────────┬─────────┬──────────┐  │
        │  │ Portal  │ Viewer  │ Policy   │  │   All tenant-aware;
        │  │ (Next)  │ Service │ Engine   │  │   rate-limited + quota'd
        │  └─────────┴─────────┴──────────┘  │   per tenant
        │  ┌──────────────────────────────┐  │
        │  │ Room Service (Go) + SPI       │  │
        │  │  ├─ NativeAdapter (S3→Ceph)  │  │
        │  │  └─ [NextcloudAdapter off]   │  │
        │  └──────────────────────────────┘  │
        │  NATS JetStream (evdr.{tenant}.>)  │
        │  Keycloak (realm per tenant)       │
        │  PostgreSQL (RLS) · Redis (ACL)    │
        │  Ceph RGW/Rook (bucket & key /tenant) │
        │  OpenSearch (index/tenant)         │
        │  ClickHouse (tenant_id partition)  │
        │  vLLM — Qwen (in-cell AI)          │
        └────────────────────────────────────┘
```

A **cell** is the full application stack deployed into one region/jurisdiction. Cells are stampable via Terraform + the umbrella Helm chart. Tenant data **never** leaves its cell (Section 8).

---

## 5. Layer-by-Layer Deltas from v1.0

### 5.1 Edge / ingress (Traefik)

- **Tenant resolution middleware** maps `{tenant}.evdr.hk` (or verified custom domain via CNAME + dns-01 TLS) → tenant context on every request; missing/unknown tenant = fail closed.
- **Per-tenant rate limits and quotas** at the edge (Traefik plugin or Envoy if limits get exotic) — the first noisy-neighbor control.

### 5.2 Identity — Keycloak, realm per tenant

- One Keycloak **realm per tenant**, created by the control plane via the Admin API. Guests authenticate per-realm with email-OTP/link flows as in v1.0 — a person in two tenants is two principals by design.
- Internal users of tenant organizations federate their own IdP (SAML/OIDC) into their realm; Tier 0 federates the bank's AD.
- Keycloak handles hundreds of realms comfortably at our target scale (tens–low hundreds of tenants). *Alternative noted:* Zitadel has org-native multi-tenancy and would remove realm automation, but switching identity vendors late is costly — stay with Keycloak unless realm automation proves painful.
- **Platform operators** get a separate administrative realm; every operator action on a tenant realm is break-glass, audited, and alertable to the tenant's admins.

### 5.3 PostgreSQL — tenant_id + Row-Level Security everywhere

- Every business table gains `tenant_id UUID NOT NULL`; every query path runs under a tenant-scoped session variable with **RLS policies** enforcing it. The tenant context is set by the Room Service / Policy Engine at authentication time — never by client-supplied parameters.
- PgBouncer in front, transaction pooling with `SET LOCAL app.tenant_id` per transaction.
- Quotas: per-tenant storage accounting and connection budget; noisy or regulated tenants can be **promoted to a dedicated Postgres cluster** (logical replication migration) without code change — this is the Tier 1→2 upgrade path.
- NextcloudAdapter deployments keep their own schema/instance (Nextcloud manages its DB); tenant-scoped tables (policy, audit, viewer telemetry) still follow the RLS model.

### 5.4 Object storage — Ceph RGW with per-tenant buckets and keys

> Revised Aug 2026: MinIO (and its KES) removed from the stack — the community edition was gutted in 2025 and the project archived in 2026. Full decision record and sources in `Requirements/EVDR-Object-Storage-Alternatives-Analysis.md`.

- Storage backend is **Ceph RGW operated via the Rook operator** (CNCF-graduated; foundation-governed under the Ceph Foundation/Linux Foundation). **RGW user + bucket per tenant**, provisioned via Rook ObjectBucketClaims by the Tenant Provisioner.
- **Primary tenant-key control is application-layer envelope encryption**: every document gets a per-document DEK wrapped by the tenant KEK held in Vault. Tenant isolation therefore holds even if the storage cluster is fully compromised — a stronger story for bank security teams than storage-layer SSE alone, and it makes the backend swappable (Ceph / Garage / CubeFS) without changing the tenancy model.
- **Secondary at-rest layer**: Ceph SSE-KMS with the **Vault KMS backend** (native RGW capability), using tenant-scoped keys. Bucket policy + TLS = storage-layer isolation; deleting the bucket and destroying the tenant KEK is a defensible cryptographic-erasure story.
- Operator access to a tenant KEK is break-glass, audited, time-boxed, and alerts the tenant — platform operators cannot read tenant documents even with full infrastructure access.
- Deployment profiles per tier: Rook-Ceph for Tiers 0–2; **Garage** single-binary profile for single-box on-prem appliances; **CubeFS** for the future mainland cell — all behind the same SPI (values-file flip, not a port).

### 5.5 Event bus — NATS JetStream namespaced per tenant

- Subject namespace `evdr.{cell}.{tenant}.>`; per-tenant stream limits (size/age) so one tenant's telemetry burst cannot evict another's.
- Metering taps the same subjects (Section 6).

### 5.6 Search — OpenSearch, index per tenant

- **Index-per-tenant** with rollover/ILM. Isolation is structural, and right-to-erasure is a single index delete (vs. delete-by-query on shared indexes).
- *Deferred alternative:* filtered aliases on shared indexes if we ever have thousands of micro-tenants — not expected at our price point.

### 5.7 Analytics — ClickHouse partitioned by tenant

- Audit and viewer-telemetry tables use `tenant_id` as the leading `ORDER BY`/`PARTITION BY` column; per-tenant query quotas and role-scoped dashboards. Tenant-facing analytics (the Digify-style engagement dashboards) query with a mandatory tenant predicate enforced in the query service, not the UI.

### 5.8 Policy engine — tenancy-aware PDP

- Keep the v1.0 Go policy service as the PDP, but embed **OPA** (Rego) with **per-tenant policy overlays**: global baseline policies (platform non-negotiables — audit, watermarking, retention floors) + tenant-configurable policies (IP allow-lists, download tiers, AI-processing consent). Every decision carries `tenant_id` and is logged.
- *Alternative:* AWS Cedar (simpler policy language) — evaluate during Phase 2.5; OPA is the default for ecosystem maturity.

### 5.9 AI services — per-tenant routing, consent, and metering

- **Per-tenant AI-processing switch.** Some bank tenants will contractually refuse AI processing of their documents; this is a tenant-level policy flag enforced in the pipeline *before* content reaches any model, with audit evidence of enforcement. Expect this to be a sales objection-killer if it's on the roadmap early.
- Model routing is per tenant and per cell: HK cell runs self-hosted **Qwen 2.5 via vLLM** (Chinese/English capability — already the v1.0 choice and exactly right for the market); a tenant may pin "no external AI" (self-hosted only) vs "external APIs allowed."
- Prompts, embeddings, and outputs are tenant-scoped and inherit the same retention/erasure rules as documents. AI usage is metered per tenant (tokens, pages OCR'd, documents summarized).

### 5.10 Secure viewer — tenant branding + conversion quotas

- Watermark templates, branding, and viewer policy are tenant-config; the viewer service is shared but every render is tenant-tagged.
- **LibreOffice conversion workers are the noisy-neighbor hotspot** (CPU-heavy, per-document). Conversion queue is per-tenant fair-shared with a worker pool that autoscales on queue depth; large conversions for one tenant cannot starve another.

### 5.11 Secrets — Vault with per-tenant key paths

- OSS Vault: strict path-prefix ACLs (`tenants/{id}/*`) for KEKs and tenant credentials; Vault Enterprise namespaces become worth it past ~50 tenants or when selling dedicated Vault isolation to Tier 2 buyers.

---

## 6. Control Plane (New Services)

v1.0 has no control plane; commercialization requires four components. All live in the global control plane and touch **metadata only**.

| Component | Responsibility | Notes |
|---|---|---|
| **Tenant Provisioner** | Idempotent onboarding: Keycloak realm, `tenant` record + RLS role, Ceph RGW user/bucket (ObjectBucketClaim) + Vault KEK, NATS subjects/streams, OpenSearch index, DNS/branding, quotas, feature flags | Target: new SaaS tenant fully onboarded in **< 15 minutes**, fully automated. Same engine drives Tier 2 dedicated stacks via Terraform/Helm. |
| **Tenant Config & Flags** | Per-tenant settings, branding assets, watermark presets, retention, AI consent, feature flags | DB-driven flags initially; self-hosted Flagsmith/Unleash if experimentation grows. |
| **Metering & Billing** | Usage events from NATS → ClickHouse rollups → rating → invoice | Stripe for SaaS card/SEPA billing (HK entity supported); usage-CSV/API export for enterprise invoicing on 30-day terms. Meter: seats, storage GB, pages viewed, conversions, AI tokens, e-sign events. |
| **License Server (Tier 3)** | On-prem entitlement: signed license files (Ed25519), node-locked or floating seats, offline activation, timed grace periods | Self-built verification in-product (public-key check, no phone-home required); Keygen-style server optional for renewals. Air-gap capable by design — mandatory for the China market. |

**Two consoles, strictly separated** (this split is the v1.0 Admin Console evolving, not a rewrite):

- **Tenant Admin Console** — the customer's view: their rooms, users, policies, analytics, audit export, billing. Tenant-scoped by construction.
- **Platform Operator Console** — our view: cell health, tenant lifecycle, quota management, break-glass access. Every action on tenant data/alerting is audited; tenant admins can be notified of operator break-glass events (a feature — transparency sells to regulators).

---

## 7. Isolation & Security Model Summary

| Dimension | Tier 1 (Shared SaaS) | Tier 2 (Dedicated) | Tier 3 (On-prem) |
|---|---|---|---|
| Compute | Shared cluster, per-tenant quotas & fair-share | Dedicated cluster per tenant | Customer hardware |
| Identity | Keycloak realm per tenant | Realm (or customer IdP direct) | Customer IdP; realm local |
| Data | RLS + bucket-per-tenant + per-tenant keys | Dedicated DB/buckets | Entirely customer-held |
| Keys | Tenant KEK in Vault; envelope encryption | Same, or **customer-managed keys** (BYOK) | Customer KMS/HSM |
| Network | Shared VPC, egress controls | Dedicated VPC, private links | Customer network |
| Operator document access | **Cryptographically impossible** without audited break-glass | Same or contractually none | None — we never touch it |

Additional controls: per-tenant SIEM forwarding (each tenant can have its own SIEM destination for its audit stream), per-tenant audit export packages (the v1.0 evidentiary export, scoped per tenant), pen-test reports issued per cell and per major release, and a SOC 2 Type II / ISO 27001 program scoped to the SaaS cell — bank procurement will ask for both before signing.

---

## 8. Data Residency & the Hong Kong / China Path

The geo-political wedge in the product thesis is sovereignty, so residency is an **architectural first-class citizen**, not a deployment afterthought:

1. **Cell architecture.** Every region runs a complete, independent stack (data plane). The HK cell serves HK/Asia-Pacific SaaS and dedicated tiers. Tenant documents, audit trails, indexes, and telemetry never cross cell boundaries — enforced at the network layer (no cross-cell storage/DB connectivity) and by policy.
2. **Global control plane holds metadata only** (tenant name, tier, quota, billing). For especially sensitive mainland tenants, even metadata can be pinned to the mainland cell — the control plane supports cell-local metadata for tenants that require it.
3. **Mainland China cell (later, gated on business case):** a separate stamp of the same chart — domestic Kubernetes (Aliyun ACK / Tencent TKE or customer-prem), Harbor-mirrored images (no pulls from foreign registries), in-cell Qwen models (already the default), local object storage, and a licensing/commercial entity structure that satisfies ICP and PIPL requirements. PIPL cross-border transfer rules are honored trivially because **nothing crosses**: each mainland tenant's full data footprint stays in the mainland cell.
4. **Selling point, engineered:** "Your data never leaves Hong Kong / never leaves your infrastructure" is enforced by architecture (no cross-cell data paths exist), attestable in audits, and differentiates against US/EU-centric SaaS VDRs whose multi-region story is a compliance afterthought.

---

## 9. Tenant Lifecycle Flows

- **Onboard (Tier 1):** sales/admin triggers Provisioner → realm, storage, keys, streams, index, DNS, branding, quotas, flags → tenant admin receives activation → first room creatable in minutes. All steps idempotent and re-runnable.
- **Tier upgrade (1 → 2):** Provisioner stamps a dedicated cell stack → logical replication migrates Postgres → object-level bucket migration (S3 copy) → cutover with DNS + brief maintenance window. The SPI and RLS model make this a data-copy exercise, not a re-architecture.
- **Suspend:** Policy Engine flips tenant to `suspended`; all sessions revoked at realm; edge blocks; audit retention preserved. Used for non-payment or incident response.
- **Offboard / erasure:** export package (documents + audit + evidence letter) if contractually provided → delete Keycloak realm, RGW tenant bucket, Vault KEK (cryptographic erasure), OpenSearch index, ClickHouse partitions, Vault paths → certificate of erasure generated into the (retained) control-plane record. Clean, provable, defensible.

---

## 10. Impact on the Build Plan

The v1.0 phase plan survives intact — with tenancy retrofitted cheaply because we start now:

| Phase | Change |
|---|---|
| **Phase 0 (Wks 1–3)** | Add: tenancy model in threat model; `tenant_id` conventions; hostname-based tenant resolution in the edge design; SPI interface contract drafted. |
| **Phase 1 (Wks 4–9)** | Build against the **Room SPI from day one** (NextcloudAdapter first). `tenant_id` in every schema and every event, RLS enabled even though Tier 0 has one tenant. Costs ~nothing now, saves a painful migration later. |
| **Phase 2 (Wks 10–15)** | Audit pipeline and policy engine built tenant-aware from the start. |
| ****Phase 2.5 (new, Wks 16–20, parallel with Phase 3)** | **Commercialization track:** NativeAdapter build + conformance suite vs NextcloudAdapter; Tenant Provisioner; Keycloak realm automation; metering pipeline; Tenant/Operator console split. **Pilot: onboard 2–3 internal business units as real tenants on the shared model.** |
| **Phase 3 (Wks 16–22)** | AI services launch with per-tenant routing, consent switch, and metering (retrofitting consent later is far harder). |
| **Phase 4 (Wks 23–26)** | Per-tenant analytics dashboards; billing GA (Stripe); license server for first Tier 3 pilots. |
| **Phase 5 (Wks 27+)** | Unchanged (PrivacyGo/MP-SPDZ clean-room layer) — note that MPC parties map naturally onto tenants, so the tenancy model composes with it. |

**Commercial launch gate criteria** (end of Phase 4): NativeAdapter conformance parity, pen test of the shared cell passed, SOC 2 Type I complete (Type II in progress), tenant onboarding < 15 min demonstrated ≥ 10 times, billing cycle executed end-to-end, break-glass transparency features demonstrated to at least two prospect security teams.

---

## 11. Stack Delta Summary (v1.0 → v1.1)

| Layer | v1.0 | v1.1 delta |
|---|---|---|
| Repository core | Nextcloud | Nextcloud **behind Room SPI**; NativeAdapter (S3→Ceph + Postgres) added for shared SaaS |
| Identity | Keycloak SSO | Keycloak **realm-per-tenant** + control-plane realm automation; operator break-glass model |
| Database | PostgreSQL 16 | + `tenant_id` + **RLS** + PgBouncer tenant sessions + promotion path to dedicated cluster |
| Object storage | MinIO | **Replaced with Ceph RGW via Rook** (MinIO archived 2026) — bucket-per-tenant, Vault KEK envelope encryption (primary) + SSE-KMS/Vault (secondary); Garage for appliance on-prem; CubeFS for mainland cell |
| Policy engine | Custom Go service | + **OPA** embedded; global baseline + tenant policy overlays; AI-consent switch |
| Event bus | NATS JetStream | + per-tenant subject namespaces & stream limits |
| Search | OpenSearch | + **index-per-tenant** with ILM |
| Analytics DB | ClickHouse | + `tenant_id` partitioning, per-tenant quotas |
| Secrets | Vault | + per-tenant key paths/ACLs; BYOK for Tier 2 |
| AI services | FastAPI + Qwen/vLLM | + per-tenant model routing, **AI-processing consent flag**, token metering |
| Admin console | Admin Console | Split: **Tenant Admin** vs **Platform Operator** consoles |
| New services | — | Tenant Provisioner, Tenant Config/Flags, Metering & Billing, **License Server** |
| IaC | Terraform + Helm | + **cell stamping** model (HK cell, later mainland cell); same chart, four tier value files |

---

## 12. Risks & Mitigations

| Risk | Mitigation |
|---|---|
| SPI adapter drift (feature works on Nextcloud, not Native) | Shared conformance suite in CI blocking merge on both adapters; feature flags gate adapter-specific paths |
| Keycloak realm sprawl / upgrade pain | Realm lifecycle fully automated from day one; realm count reviewed quarterly; Zitadel re-evaluated at >100 tenants |
| RLS performance at scale | Tenant predicate in indexes; partitioning on hot tables; dedicated-cluster promotion path for heavy tenants |
| LibreOffice conversion cost & noisy neighbors | Per-tenant fair-share queues, autoscaling worker pools, conversion quotas in commercial terms |
| Insider-risk concerns from bank buyers | Envelope encryption makes operator document access cryptographically impossible; audited break-glass with tenant alerting as a *feature* |
| SOC 2 / ISO 27001 timeline vs sales cycle | Start the compliance program at Phase 2.5, not at launch; Type I by launch, Type II (6-month observation) immediately after |
| Mainland cell complexity (ICP, PIPL, entity structure) | Gated on business case; architecture already cell-clean; commercial/legal workstream runs separately from engineering |
| On-prem license circumvention | Accept as cost of doing business; signed licenses + grace periods + support entitlement tied to license status rather than technical lock |

---

## 13. Open Decisions for Next Review

1. Custom domains (CNAME + dns-01) at commercial launch or Phase 4+? (Affects edge TLS automation priority.)
2. Vault Enterprise namespaces vs OSS path-prefix ACLs — decide at ~50 tenants or first BYOK deal.
3. OPA vs Cedar for the policy overlay language — spike during Phase 2.5.
4. Self-built license verification vs Keygen-style server for Tier 3 — spike during Phase 2.5.
5. Mainland China cell trigger criteria (committed pipeline? entity readiness?) — business decision with a technical readiness checklist attached.
