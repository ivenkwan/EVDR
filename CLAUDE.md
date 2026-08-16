# EVDR — Enterprise Virtual Data Room

> **Owner:** Iven Kwan | **Stack Tier:** Enterprise | **Mode:** Agentic Coding
> This file is the persistent memory layer for Claude Code. Read it fully before touching any code.

---

## 1. Project Overview

A **compliance-grade, self-hosted secure document exchange platform** (Virtual Data Room) that enables regulated enterprises — starting with HSBC internal use, then commercialising for Hong Kong and mainland China — to exchange documents safely with external entities under full governance, immutable audit, AI-assisted content intelligence, and optional privacy-preserving analytics.

**Core value proposition:**
- Self-hosted, data-sovereign — no SaaS dependencies for core data paths; "your data never leaves your infrastructure"
- Secure external collaboration rooms with branded portals, dynamic watermarking, and view-only enforcement
- Multi-tenant SaaS + dedicated + on-prem deployment from one codebase (four tiers)
- Compliance automation (PII redaction, classification, retention, SIEM-grade audit) over and above standard VDR features
- Agentic-AI-native extensibility via MCP server and REST API

**Primary users:** Compliance officers, deal managers, internal bank staff, external auditors/regulators/partners, platform operators
**Persona:** Users who need to share sensitive documents with external parties under strict access controls and audit requirements

---

## 2. Tech Stack

```
Infrastructure:     Docker + Kubernetes (K3s → full K8s) · Terraform + Helm · GitLab CI (self-hosted)
                    HashiCorp Vault · Traefik (reverse proxy / API gateway)
Repository Core:    Nextcloud (Tiers 0/2/3) · Ceph RGW via Rook (all tiers, primary)
                    PostgreSQL 16 · Redis 7
Room Abstraction:   Room SPI (Go interface) · NextcloudAdapter · NativeAdapter (S3+PG)
Frontend:           Next.js 15 (App Router) · TypeScript · shadcn/ui · Tailwind CSS
                    Zustand · TanStack Query
Secure Viewer:      PDF.js · pdf-lib · LibreOffice headless (Office→PDF conversion)
Policy Engine:      Go microservice · embedded OPA (Rego)
Identity:           Keycloak (realm-per-tenant SSO, SAML 2.0, OIDC, 2FA)
Event Bus:          NATS JetStream (evdr.{cell}.{tenant}.>)
Audit & Analytics:  PostgreSQL (append-only RLS) · ClickHouse · Fluent Bit → SIEM
Search & OCR:        OpenSearch (index-per-tenant) · Tesseract OCR
PII Detection:       Presidio (Microsoft, Python)
AI Services:        FastAPI + LLM (Qwen 2.5 / Llama 3 via vLLM) · BGE-M3 embeddings
Office Editing:     Collabora Online (CODE)
Monitoring:         Prometheus + Grafana + Loki
API & MCP:           FastAPI MCP server · Next.js API routes · Go webhook emitter
Control Plane:      Tenant Provisioner · Metering & Billing (Stripe) · License Server (Ed25519)
Clean Room (future): PrivacyGo Data Clean Room · MP-SPDZ
```

**Package managers:** `pnpm` for Node (NOT npm/yarn) | `pip` with `--break-system-packages` for Python (or `uv` if adopted)
**NEVER use:** npm or yarn for Node dependencies

---

## 3. Architecture Layers

```
┌──────────────────────────────────────────────────────────────┐
│  Edge: Traefik (TLS, tenant resolution, rate limiting)       │
├──────────────────────────────────────────────────────────────┤
│  Application: External Portal · Admin Console · Secure       │
│                 Viewer · Policy Engine · MCP Server           │
├──────────────────────────────────────────────────────────────┤
│  Service: Room Service/SPI · Watermarking · Document Convert │
│            NDA/Consent Gate · Webhook Emitter                │
├──────────────────────────────────────────────────────────────┤
│  Identity: Keycloak (realm-per-tenant SSO)                   │
├──────────────────────────────────────────────────────────────┤
│  Data: PostgreSQL (RLS + tenant_id) · Redis · Ceph RGW       │
│        OpenSearch · Nextcloud (Tiers 0/2/3 only)             │
├──────────────────────────────────────────────────────────────┤
│  Events: NATS JetStream → ClickHouse (telemetry)              │
│          → Fluent Bit → SIEM → Grafana Alerting               │
├──────────────────────────────────────────────────────────────┤
│  AI: Presidio (PII) · FastAPI + LLM (Qwen 2.5 / Llama 3)    │
├──────────────────────────────────────────────────────────────┤
│  Control Plane: Tenant Provisioner · Metering · License      │
└──────────────────────────────────────────────────────────────┘
│  Container Orchestration: Kubernetes (K3s → full K8s)        │
│  Infrastructure as Code: Terraform + Helm                    │
```

### Room SPI — Critical Abstraction

All upstream services program against the Room SPI, never against storage directly. Two adapters:

| Adapter | Used in | Implementation |
|---|---|---|
| NextcloudAdapter | Tier 0, 2, 3 | Wraps Nextcloud OCS/WebDAV APIs |
| NativeAdapter | Tier 1 (shared SaaS) | S3 (Ceph RGW) + PostgreSQL metadata + Redis |

Both adapters must pass the shared SPI conformance test suite in CI on every merge.

### Deployment Tiers

| Tier | Model | Storage | Identity | Operator |
|---|---|---|---|---|
| 0 — Internal | Single-tenant (tenant-ready) | Nextcloud + Ceph RGW | Keycloak (single realm) | Us |
| 1 — Shared SaaS | Multi-tenant cell, HK | Ceph RGW (NativeAdapter) | Realm per tenant | Us |
| 2 — Dedicated | Single-tenant cell, HK | Ceph RGW + BYOK | Realm (or customer IdP) | Us |
| 3 — On-prem | Customer-operated | Ceph RGW / Garage / CubeFS | Customer IdP | Customer |

---

## 4. Project File Structure

```
evdr/
├── CLAUDE.md                     ← you are here
├── README.md
├── Todo.md                       ← phase checklist with FR/TR/SR traceability
├── Requirements/
│   ├── EVDR-Functional-and-Technical-Requirement-Specifications.md  ← FTRS (master spec)
│   └── Ref/
│       ├── EVDR-Technology-Stack-Recommendation.md                 ← v1.0 stack decisions
│       ├── EVDR-Multi-Tenant-Architecture-Addendum.md               ← v1.2 tenancy overlay
│       ├── EVDR-Object-Storage-Alternatives-Analysis.md             ← v1.2 storage decision
│       ├── Core Secure Data Clean Room Requirement - Part 1.md
│       ├── Core Secure Data Clean Room Requirement - Part 2.md
│       ├── Secure Data Clean Room Platform  Market Landscape and Feature - Blueprint.md
│       ├── Secure Data Clean Room Platform  Market Landscape and Feature - Build Plan.md
│       ├── Competitive Analysis - Virtual Data Room - Digify.md
│       ├── Extract key features from digify and add our proposition.md
│       ├── Feature-Gap Analysis_ Digify vs. Self-Hosted Requirement.md
│       └── digify-feature-gap-analysis.md
├── docs/
│   ├── ADR/                       ← Architecture Decision Records
│   └── api/                       ← OpenAPI specs (future)
└── src/                           ← application code (future)
    ├── frontend/                  ← Next.js external portal + admin console
    ├── services/                  ← Go microservices (policy engine, webhook emitter, viewer)
    ├── spi/                       ← Room SPI interface + adapters
    ├── ai/                        ← FastAPI AI services (PII, classification, summarisation)
    ├── control-plane/             ← Tenant Provisioner, Metering, License Server
    └── infra/
        ├── helm/                  ← Umbrella chart + per-tier values
        └── terraform/             ← Cell stamping, networking, DNS
```

---

## 5. Key Commands

> Infrastructure and run commands will be defined as the codebase is built. Interim:

```bash
# Infrastructure (Phase 0)
terraform plan -var-file=hk-cell.tfvars          # plan cell stamp
terraform apply -var-file=hk-cell.tfvars          # stamp cell
helm upgrade --install evdr ./infra/helm -f ./infra/helm/values-tier1.yaml  # deploy services

# Backend (Go)
cd src/services/policy
go build ./...                                    # compile
go test ./... -v -race                            # run tests

# Frontend (Next.js)
cd src/frontend
pnpm install
pnpm dev                    # dev server (port 3000)
pnpm build && pnpm start    # production build
pnpm lint                   # ESLint + Prettier
pnpm typecheck              # tsc --noEmit

# AI Services (Python)
cd src/ai
pip install -r requirements.txt --break-system-packages
uvicorn main:app --reload --port 8001
pytest tests/ -v

# SPI Conformance (critical — run on every merge)
cd src/spi
go test ./conformance/... -v    # must pass against BOTH adapters

# CI
# GitLab CI with self-hosted runner; SAST (Semgrep), DAST (OWASP ZAP), dependency scanning, SBOM
```

---

## 6. Hard Rules — NEVER Violate

### Security & Data Safety
- **NEVER bypass the Room SPI** — all document access must go through the SPI interface, never directly to Nextcloud APIs or Ceph RGW/S3.
- **NEVER set tenant_id from client-supplied parameters** — always set from service-layer authentication context.
- **NEVER allow cross-tenant data access** at any layer — RLS, bucket policy, realm boundaries, NATS namespaces, OpenSearch indexes, ClickHouse partitions all enforce isolation.
- **NEVER expose raw connection strings, Vault secrets, or KMS keys** in logs, responses, or error messages.
- **NEVER store raw user prompts in plaintext** without PII stripping for AI services.

### Multi-Tenancy Rules
- **Every business table** must include `tenant_id UUID NOT NULL`.
- **Every query path** must enforce tenant scope via PostgreSQL RLS — no service-level filtering as sole isolation.
- **Every audit event** must carry `tenant_id` — no silent data paths.
- **Every policy decision** must be logged with `tenant_id`, actor, action, and result.

### Document Protection
- **NEVER serve full documents to the viewer** — always page-by-page streaming via PDF.js.
- **NEVER use client-side-only watermarking** — server-side rendering is mandatory (client-side overlays are strippable).
- **NEVER skip the NDA/consent gate** on first room access — it is a non-overridable platform baseline.

### Code Conventions
- **NEVER use `print()` for logging** — use structured logging appropriate to the language (`logrus` for Go, `loguru` or `structlog` for Python, `console` with severity levels for TypeScript).
- **NEVER commit `.env` files** — use `.env.example` with placeholder values only.
- **NEVER modify Nextcloud core** — all custom logic lives in adjacent services (SPI, policy engine, viewer, AI services) to keep Nextcloud upgrades decoupled.

---

## 7. Coding Conventions

### Go / Backend Services
- Go microservices for: Policy Engine, Webhook Emitter, Viewer Service backend, Room Service/SPI
- **Error handling**: explicit error returns with context; never swallow errors
- **File naming**: `snake_case` for Go files, `kebab-case` for frontend and config files
- **Interfaces first**: define the Room SPI and other interfaces before implementations
- **Concurrency**: use Go channels and `sync` primitives; avoid unnecessary mutex contention

### TypeScript / Frontend
- Next.js 15 App Router — **Server Components by default**, add `"use client"` only for interactive components
- shadcn/ui + Tailwind CSS — owned components (copy-paste, not dependency); rapid per-room theming via CSS custom properties
- **All data fetching** via TanStack Query against API gateway — never `fetch()` directly in components
- Zod schemas for all API response validation

### Python / AI Services
- FastAPI microservices for: PII detection (Presidio), document AI (classification, summarisation, translation), MCP server
- **Async by default**: all FastAPI routes use `async def`
- Pydantic v2 for schemas — explicit types, never `Any`
- AI processing consent flag must be checked before any content reaches an LLM — log the check result

### Infrastructure
- **Terraform + Helm only** — no manual infrastructure changes in production
- **One umbrella Helm chart** with `values-tier0.yaml`, `values-tier1.yaml`, `values-tier2.yaml`, `values-tier3.yaml`
- All secrets via Vault — never in Helm values, ConfigMaps, or environment variables directly

---

## 8. Database Rules

```sql
-- Every business table MUST have:
id          UUID PRIMARY KEY DEFAULT gen_random_uuid()
created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
tenant_id   UUID NOT NULL REFERENCES tenants(id)

-- RLS policy MUST exist for every user-data table
ALTER TABLE <table> ENABLE ROW LEVEL SECURITY;
```

- **Audit tables are append-only** — no UPDATE/DELETE privileges; enforce at schema level
- **PostgreSQL RLS** is the primary tenant isolation mechanism — never rely solely on application-level filtering
- **Migrations**: tracked and versioned; never edit existing migration files
- **Indexes**: all foreign keys and `tenant_id` columns must be indexed
- **Nextcloud manages its own database** — do not modify Nextcloud schema directly; custom tenant-scoped tables live alongside

---

## 9. Testing Approach

```
Unit tests:          Pure functions, policy evaluation, watermark logic, SPI contract tests
Integration tests:   API routes with test DB, viewer rendering pipeline, NATS event flow
SPI Conformance:     Shared suite run against BOTH adapters on every merge — BLOCKS merge if either fails
E2E tests:           Full upload → convert → watermark → view → audit pipeline (slow, marked @e2e)
Penetration tests:   First at Phase 2 exit; annual thereafter; critical findings block gate
```

- **Always mock LLM calls** in unit/integration tests
- **SPI conformance is a merge gate** — if a change breaks one adapter, the merge is blocked
- Test database is separate from any production or staging instance

---

## 10. Multi-Tenancy & Access Control

- All access is scoped by `tenant_id` from JWT/session claims, enforced by PostgreSQL RLS
- **Keycloak realm per tenant** — a person in multiple tenants gets separate principals and sessions
- **Envelope encryption is primary tenant-key control**: per-document DEK wrapped by tenant KEK in Vault — isolation survives storage-cluster compromise
- **Ceph SSE-KMS with Vault KMS backend** is the secondary at-rest layer
- Operator break-glass access to tenant KEK: time-boxed, audit-logged, multi-person approval, alerts tenant admins
- Per-tenant AI-processing consent switch enforced before content reaches any model

---

## 11. Observability & Audit

- Every viewer page view, policy decision, file access, NDA signing, export, and admin action emits to NATS
- NATS → ClickHouse (telemetry/analytics) + PostgreSQL append-only (audit of record) + Fluent Bit (SIEM forwarding)
- Prometheus + Grafana for infrastructure monitoring; Loki for application logs
- Grafana Alerting → Slack/Email/Teams webhooks for anomaly detection
- Audit events are immutable by schema — no UPDATE/DELETE on audit tables

---

## 12. Encryption & Key Management

```
Primary:   Envelope encryption — per-document DEK, wrapped by tenant KEK in Vault
Secondary: Ceph SSE-KMS — Vault KMS backend, tenant-scoped keys
Transit:   TLS 1.2/1.3 on all connections (service mesh or Vault-issued certs)
Operator:  Break-glass only — cryptographically impossible without KEK access
BYOK:      Supported for Tier 2 dedicated customers
Erasure:   Destroy tenant KEK + RGW bucket → cryptographic erasure → certificate of erasure
```

---

## 13. DO NOT MODIFY (Stable / Protected)

> This section will be populated as code is written. Initial protected paths:

| Path | Reason |
|---|---|
| `src/spi/interface.go` | Room SPI contract — changes break both adapters and block CI |
| Nextcloud database schema | Managed by Nextcloud — never modify directly |
| Vault tenant key paths | Production secrets — changes need security review |

---

## 14. Deployment

```
Dev:     docker compose -f infra/docker-compose.dev.yml up
Staging: GitLab CI → build images → deploy to K8s staging namespace
Prod:    Manual approval gate → K8s prod namespace rolling update
Cell:    Terraform stamp + Helm deploy with per-tier values file

Required secrets (Vault / K8s secrets):
  VAULT_ADDR, VAULT_TOKEN
  POSTGRES_URL (postgresql+asyncpg://...)
  REDIS_URL
  CEPH_RGW_ACCESS_KEY, CEPH_RGW_SECRET_KEY
  KEYCLOAK_ADMIN_PASSWORD
  NATS_SERVER_URL
  ANTHROPIC_API_KEY (or self-hosted LLM endpoint)
  STRIPE_SECRET_KEY (billing)
  JWT_SECRET_KEY
  TENANT_MASTER_ENCRYPTION_KEY
```

---

## 15. Context Pointers

- `@Requirements/EVDR-Functional-and-Technical-Requirement-Specifications.md` — master spec (FR/TR/SR/NFR)
- `@Requirements/Ref/EVDR-Technology-Stack-Recommendation.md` — v1.0 technology stack with rationale
- `@Requirements/Ref/EVDR-Multi-Tenant-Architecture-Addendum.md` — v1.2 multi-tenancy, control plane, cell model
- `@Requirements/Ref/EVDR-Object-Storage-Alternatives-Analysis.md` — v1.2 Ceph RGW decision, MinIO retirement
- `@Todo.md` — phase checklist with FR/TR/SR traceability and gate criteria
- `@docs/ADR/` — Architecture Decision Records

---

## 16. Common Tasks

**"Add a new storage adapter (e.g., for CubeFS mainland cell)":**
1. Create `src/spi/adapters/cubefs_adapter.go` implementing the Room SPI interface
2. Ensure it passes the shared SPI conformance suite
3. Add a `values-mainland.yaml` Helm values file selecting the CubeFS adapter
4. Add integration tests in `src/spi/conformance/`
5. Update `docs/ADR/` with the decision record

**"Add a new policy rule (global baseline):**
1. Define the OPA Rego rule in the policy engine's baseline policy bundle
2. Add a conformance test verifying the rule is enforced and non-overridable
3. Add audit event verification for the policy decision
4. Update the FTRS with the new requirement ID if it's a new control

**"Add a new tenant-facing feature (e.g., Q&A workflow):"
1. Define FR/TR IDs in the FTRS
2. Add checklist items to `Todo.md` under the relevant phase
3. Implement backend in `src/services/`
4. Implement frontend in `src/frontend/`
5. Ensure tenant scoping (RLS, NATS namespace, audit events) is included
6. Add API route versioned under `/api/v1/`

**"Onboard a new AI model tenant routing rule":**
1. Add tenant config in the AI service's routing table
2. Verify AI consent flag is checked before routing
3. Meter the model usage event to NATS for billing
4. Test both "no external AI" and "external APIs allowed" modes

---

## 17. Session Notes

When compacting, always preserve:
- The Room SPI contract state (frozen v0.1 at Phase 1 start, subsequent versions)
- Current phase and gate status (check `Todo.md` for active phase)
- Adapter conformance status (NextcloudAdapter + NativeAdapter CI status)
- Active open decisions (see `Todo.md` Open Decisions Tracker)
- Any security review or pen-test findings and their remediation status
