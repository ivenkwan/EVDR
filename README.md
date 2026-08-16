# EVDR — Enterprise Virtual Data Room

A compliance-grade, self-hosted secure document exchange platform for regulated enterprises.

---

## Business Objectives

EVDR addresses a structural gap in the market: most virtual data room and secure file exchange solutions are either US/Europe-centric SaaS products that lack data sovereignty, or closed platforms with limited extensibility. For regulated financial institutions in Hong Kong and mainland China — and for any enterprise that requires data to remain under its own control — there is no commercially available solution that combines VDR-grade document protection with AI-assisted compliance automation and multi-tenant commercial deployment.

**For internal use (HSBC first build):** Replace fragmented document exchange workflows — email attachments, manual FTP drops, ad-hoc portal links — with a single governed platform where every external document exchange is branded, watermarked, access-controlled, and immutably audited. Reduce compliance risk and audit preparation cost while improving the experience for external counterparties.

**For commercialisation (HK and Asia-Pacific):** Offer the platform as a multi-tenant SaaS from Hong Kong infrastructure, with dedicated and on-prem deployment options for sovereignty-sensitive customers. Target mid-market banks, professional services firms, and regulated enterprises that are underserved by US/EU-centric vendors and overcharged by legacy VDR platforms. Differentiate on self-hosted sovereignty, SIEM-grade audit integration, AI-assisted content intelligence, and agentic-AI-native extensibility.

**For mainland China:** Leverage the geopolitical wedge — "your data never leaves China" enforced by architecture, not policy — with a domestic cell built on CNCF-graduated storage (CubeFS), in-cell AI models (Qwen 2.5), and PIPL compliance by construction. No US/EU vendor can credibly make this claim.

### Strategic Positioning

EVDR is not a generic file-sharing portal. It is not a Dropbox alternative or a collaboration workspace. It is purpose-built for one workflow: **exchanging sensitive documents with external parties under governance controls that satisfy bank regulators**.

The product matches or exceeds the user-facing features that make SaaS VDRs commercially effective (branded rooms, dynamic watermarking, NDA gating, engagement analytics) while differentiating in areas those vendors do not address: self-hosted data sovereignty, per-tenant encryption with operator-insider protection, AI-assisted PII redaction and classification, and programmatic extensibility via MCP and REST APIs.

---

## Target Markets

| Market | Deployment | Data Residency | Commercial Model |
|---|---|---|---|
| HSBC internal (first build) | Tier 0 — self-hosted | HK on-prem / private cloud | Internal cost centre |
| HK / Asia-Pacific partners | Tier 1 — shared SaaS | HK cell only | Subscription + usage metering |
| Sovereignty-sensitive banks | Tier 2 — dedicated cell | HK cell, customer-managed keys | Enterprise license + SLA |
| Mainland China enterprises | Tier 3 — on-prem / mainland cell | Mainland China cell | Licensed, air-gap capable |

---

## Technology Stack

### Infrastructure

| Component | Technology |
|---|---|
| Container orchestration | Docker + Kubernetes (K3s → full K8s) |
| Infrastructure as Code | Terraform + Helm charts |
| CI/CD | GitLab CI (self-hosted runner) — SAST, DAST, dependency scan, SBOM |
| Secrets management | HashiCorp Vault — dynamic secrets, PKI, per-tenant key paths |
| Reverse proxy / API gateway | Traefik — automatic TLS, rate limiting, per-tenant resolution |
| Monitoring | Prometheus + Grafana + Loki |
| SIEM forwarding | Fluent Bit → tenant-configured destinations |

### Application Layer

| Component | Technology |
|---|---|
| External portal | Next.js 15 (App Router, TypeScript, SSR) |
| Admin console | Next.js 15 (separate app, tenant-scoped + operator-scoped) |
| UI components | shadcn/ui + Tailwind CSS — owned components, rapid per-room theming |
| Secure viewer | Custom service on PDF.js — page-by-page streaming, never full documents |
| Watermarking | Server-rendered via pdf-lib / qpdf — baked into output, not client overlays |
| Document conversion | LibreOffice headless — Office → PDF for uniform viewer rendering |
| Policy engine | Go microservice + embedded OPA (Rego) — global baselines + tenant overlays |
| NDA / consent gate | Next.js middleware — e-signature integration (DocuSign / HelloSign / OpenSign) |

### Data & Storage

| Component | Technology |
|---|---|
| Repository (Tiers 0/2/3) | Nextcloud (hardened, Docker) |
| Repository (Tier 1 SaaS) | Native adapter on Ceph RGW (no Nextcloud) |
| Object storage | Ceph RGW via Rook — S3 API, versioning, lifecycle, SSE-KMS with Vault |
| Object storage (appliance) | Garage (single-binary, 1 GB RAM) for single-box Tier 3 |
| Object storage (mainland) | CubeFS — CNCF-graduated, JD.com lineage |
| Database | PostgreSQL 16 — all services, RLS-enforced, append-only audit |
| Caching | Redis 7 — file locking, sessions, rate limiting |
| Full-text search | OpenSearch — index-per-tenant with ILM |
| OCR | Tesseract — scanned PDFs and images |

### Identity & Events

| Component | Technology |
|---|---|
| Identity provider | Keycloak — realm-per-tenant SSO, SAML 2.0, OIDC, 2FA |
| Event bus | NATS JetStream — `evdr.{cell}.{tenant}.>` namespaces |
| Analytics store | ClickHouse — tenant_id-partitioned, per-tenant query quotas |

### AI & Intelligence

| Component | Technology |
|---|---|
| PII detection / redaction | Presidio (Microsoft, Python) — 100+ data types |
| AI classification, summarisation, translation | FastAPI + self-hosted Qwen 2.5 / Llama 3 via vLLM |
| Embeddings | BGE-M3 — Chinese, English, cross-lingual retrieval |
| Office editing | Collabora Online (CODE) — in-browser DOCX/XLSX/PPTX |

### Integration & Control Plane

| Component | Technology |
|---|---|
| REST API | Next.js API routes + Go policy service |
| MCP server | FastAPI — agentic AI orchestration |
| Webhooks | Go emitter on NATS — tenant-configured endpoints |
| Email add-ins | Outlook Add-in + Gmail Extension |
| Tenant provisioning | Idempotent engine — Keycloak, RLS, Ceph, Vault, NATS, OpenSearch |
| Metering & billing | NATS → ClickHouse → Stripe (SaaS); CSV/API for enterprise invoicing |
| License server (on-prem) | Ed25519 signed licenses, offline activation, air-gap capable |

---

## Architecture Design

### Layered Platform

EVDR is a layered platform centred on a secure document repository and viewer, with separate services for policy, logging, search, AI, and multi-tenant management. Layers communicate through a shared event bus (NATS JetStream) and a storage abstraction (Room SPI).

```
┌──────────────────────────────────────────────────────────────────┐
│  Edge:  Traefik (TLS, tenant DNS resolution, rate limiting)       │
├──────────────────────────────────────────────────────────────────┤
│  Application                                                    │
│    External Portal  ·  Admin Console  ·  Secure Viewer           │
│    Policy Engine   ·  MCP Server                                 │
├──────────────────────────────────────────────────────────────────┤
│  Services                                                       │
│    Room Service / SPI  ·  Watermarking  ·  Document Conversion   │
│    NDA / Consent Gate  ·  Webhook Emitter                       │
├──────────────────────────────────────────────────────────────────┤
│  Identity:  Keycloak (realm-per-tenant SSO)                    │
├──────────────────────────────────────────────────────────────────┤
│  Data:  PostgreSQL (RLS)  ·  Redis  ·  Ceph RGW                 │
│         OpenSearch  ·  Nextcloud (Tiers 0/2/3 only)              │
├──────────────────────────────────────────────────────────────────┤
│  Events:  NATS → ClickHouse (telemetry)  →  Fluent Bit → SIEM     │
├──────────────────────────────────────────────────────────────────┤
│  AI:  Presidio (PII)  ·  FastAPI + Qwen 2.5 / Llama 3 via vLLM  │
├──────────────────────────────────────────────────────────────────┤
│  Control Plane:  Provisioner  ·  Metering  ·  License Server      │
└──────────────────────────────────────────────────────────────────┘
         Kubernetes (K3s → full K8s)  ·  Terraform  ·  Helm
```

### Room SPI — Storage Abstraction

The most important architectural decision: all upstream services program against the Room SPI, never against storage directly. Two adapters implement it, allowing the same codebase to serve all four deployment tiers from a single artifact set.

| Adapter | Deployment Tiers | Storage Backend |
|---|---|---|
| NextcloudAdapter | 0, 2, 3 | Nextcloud OCS/WebDAV APIs |
| NativeAdapter | 1 (shared SaaS) | Ceph RGW (S3) + PostgreSQL metadata |

A shared conformance test suite runs against both adapters on every merge — this is what makes the storage backend swappable and what contained the MinIO-to-Ceph migration as a values-file change, not an architecture rewrite.

### Multi-Tenant Isolation

Tenant isolation is enforced at every layer — no component relies solely on application-level filtering:

| Layer | Isolation Mechanism |
|---|---|
| Identity | Keycloak realm per tenant — separate principals, separate sessions |
| Database | PostgreSQL Row-Level Security (`tenant_id` on every table) |
| Object storage | Per-tenant RGW user + bucket + Vault KEK (envelope encryption) |
| Events | NATS subject namespace `evdr.{cell}.{tenant}.>` with per-tenant stream limits |
| Search | OpenSearch index-per-tenant with ILM for clean erasure |
| Analytics | ClickHouse partitioned by `tenant_id` with per-tenant query quotas |
| Network | Shared VPC with egress controls (Tier 1); dedicated VPC per customer (Tier 2); customer network (Tier 3) |

### Encryption Model

Two layers, architecturally ordered:

1. **Primary — Application-layer envelope encryption:** Every document gets a per-document DEK wrapped by a tenant KEK held in Vault. Tenant isolation survives even full storage-cluster compromise — platform operators cannot read tenant documents without audited break-glass access to the KEK.
2. **Secondary — Storage-layer SSE:** Ceph SSE-KMS with Vault KMS backend, tenant-scoped keys. Bucket-level isolation and at-rest encryption.

This ordering makes the storage backend swappable (Ceph, Garage, CubeFS) without changing the tenancy story — the Room SPI + envelope encryption absorb backend differences.

### Cell Architecture for Data Residency

Tenant data never leaves its designated cell. Cells are fully self-contained stacks stamped from the same Helm chart:

| Cell | Region | Storage | AI | Use Case |
|---|---|---|---|---|
| HK Cell | Hong Kong | Ceph RGW | Qwen 2.5 vLLM | Tier 1 SaaS + Tier 2 dedicated |
| Mainland Cell | China (future) | CubeFS | Qwen 2.5 vLLM | Tier 3 on-prem / mainland licensed |
| Customer cell | Customer premises | Ceph / Garage / CubeFS | Per-tenant config | Tier 3 air-gap capable |

No cross-cell storage or database connectivity exists at the network layer. The global control plane holds metadata only (tenant name, tier, quotas, billing) — never documents.

### Control Plane

The global control plane manages tenant lifecycle but never touches tenant documents:

- **Tenant Provisioner** — idempotent onboarding (< 15 minutes target): Keycloak realm, RLS role, storage bucket, Vault KEK, NATS streams, search index, DNS, branding, quotas
- **Metering & Billing** — usage events from NATS → ClickHouse → Stripe (SaaS) or enterprise invoicing
- **License Server** — Ed25519 signed licenses for on-prem deployments, offline activation, air-gap capable

---

## Build Phases

| Phase | Weeks | Deliverable | Gate |
|---|---|---|---|
| 0 — Foundation | 1–3 | Threat model, IaC baseline, CI/CD security, Room SPI contract | Foundation Gate |
| 1 — Core Exchange | 4–9 | Branded rooms, secure viewer, watermarking, File Drop, guest access | — |
| 2 — Governance | 10–15 | Policy engine, RBAC/ABAC, audit trail, NDA gating, encryption, pen test | **MVP Gate** |
| 2.5 — Commercialisation | 16–20 | NativeAdapter, tenant provisioning, billing, console split (parallel with P3) | — |
| 3 — Intelligence | 16–22 | OCR, PII redaction, AI classification/translation, Office editing, MCP API | — |
| 4 — Analytics | 23–26 | Engagement analytics, leak detection, CRM integrations, billing GA | **Commercial Launch Gate** |
| 5 — Clean Room | 27+ | PrivacyGo / MP-SPDZ evaluation, mainland cell assessment | Optional |

---

## Documentation

| Document | Description |
|---|---|
| [Functional & Technical Requirement Specifications](Requirements/EVDR-Functional-and-Technical-Requirement-Specifications.md) | Master spec — 190 requirements (FR/TR/NFR/SR) with traceability |
| [Technology Stack Recommendation](Requirements/Ref/EVDR-Technology-Stack-Recommendation.md) | v1.0 — single-tenant stack decisions with rationale |
| [Multi-Tenant Architecture Addendum](Requirements/Ref/EVDR-Multi-Tenant-Architecture-Addendum.md) | v1.2 — tenancy model, control plane, cell architecture |
| [Object Storage Alternatives Analysis](Requirements/Ref/EVDR-Object-Storage-Alternatives-Analysis.md) | v1.2 — MinIO retirement, Ceph RGW decision |
| [Build Plan & Phase Checklist](Todo.md) | Working checklist with FR/TR/SR mappings and gate criteria |

---

## License

See [LICENSE](LICENSE) file.
