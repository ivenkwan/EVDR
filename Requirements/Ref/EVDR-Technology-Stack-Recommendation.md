# EVDR — Recommended Technology Stack

> Version 1.0 | August 2026 | For HKT CTO Review

---

## 1. Stack Philosophy

The stack is designed around four principles drawn directly from the requirements:

1. **Self-hosted and sovereign** — every component must run on-prem or in a HKT-controlled private cloud. No SaaS dependencies for core data paths.
2. **Layered, not monolithic** — Nextcloud handles storage/sharing; custom services handle security, policy, AI, and analytics. This keeps upgrades decoupled.
3. **Open-source first** — maximise auditability and avoid vendor lock-in, consistent with the compliance-grade positioning and long-term Hong Kong/China market vision.
4. **Pragmatic sequencing** — choose technologies that a 3-person team can operationalise in Phase 0–2, without requiring specialised hires until Phase 3+.

---

## 2. Architecture Overview

```
┌──────────────────────────────────────────────────────────┐
│                    Reverse Proxy / API Gateway            │
│                   (Traefik / Nginx + OAuth2 Proxy)       │
├──────────┬──────────┬───────────┬───────────┬────────────┤
│ External │  Admin    │  Secure   │  Policy   │  API /     │
│ Portal   │  Console  │  Viewer   │  Engine   │  MCP       │
│ (Next.js)│ (Next.js) │ (Custom)  │ (Go)      │  Gateway   │
├──────────┴──────────┴───────────┴───────────┴────────────┤
│              Event Bus / Message Queue (NATS)              │
├──────────────────────────────────────────────────────────┤
│           Nextcloud (Repository & Sharing Core)           │
│     + Collabora/ONLYOFFICE (in-browser editing)           │
├──────────────────────────────────────────────────────────┤
│        PostgreSQL  │  Redis  │  MinIO/S3  │  OpenSearch  │
└──────────────────────────────────────────────────────────┘
│         Container Orchestration (Kubernetes)              │
│              Infrastructure as Code (Terraform)           │
│              CI/CD (GitLab CI / GitHub Actions)            │
```

---

## 3. Detailed Stack by Layer

### 3.1 Infrastructure & Orchestration

| Component | Technology | Rationale |
|---|---|---|
| Container runtime | Docker + Kubernetes (K3s for single-node, full K8s for scale) | Your requirements documents already specify Docker/Kubernetes. K3s gives you a lightweight path for initial deployment that scales to full K8s without re-architecture. |
| IaC | Terraform + Helm charts | Terraform for infrastructure provisioning (VMs, networks, storage classes). Helm for deploying each service into K8s. Both are widely known and cloud-agnostic — important for the future Hong Kong/China multi-region vision. |
| CI/CD | GitLab CI (self-hosted) or GitHub Actions | Self-hosted GitLab Runner keeps build pipelines sovereign. SAST (Semgrep/Trivy), DAST (OWASP ZAP), dependency scanning, and SBOM generation from day one as specified in Phase 0. |
| Secrets management | HashiCorp Vault (or SOPS for a lighter start) | Vault for production: dynamic secrets, encryption-as-a-service, PKI. SOPS + age for a quicker bootstrap if Vault feels heavy for a 3-person team. |
| Reverse proxy / API gateway | Traefik | Native Kubernetes integration, automatic TLS via Let's Encrypt or internal CA, built-in circuit breaking and rate limiting. Alternatives: Nginx+Lua (more manual config), Kong (heavier). |

### 3.2 Repository & Sharing Core

| Component | Technology | Rationale |
|---|---|---|
| Document storage & sharing | **Nextcloud** (official Docker image) | Explicitly chosen in all requirement documents. Provides encryption, File Drop, Secure View, sharing primitives, LDAP/SSO, and federated sharing. PHP-based, well-documented, large community. |
| Object storage backend | MinIO (S3-compatible) | Nextcloud supports S3 as primary storage. MinIO gives you S3-compatible object storage fully self-hosted, with encryption, versioning, and lifecycle policies. Avoids tying storage to a specific cloud provider. |
| Database | PostgreSQL 16 | Nextcloud's recommended database. Also serves as the backing store for the custom policy engine, audit pipeline, and viewer telemetry. A single Postgres cluster (with read replicas at scale) avoids operational complexity of multiple databases. |
| Caching & sessions | Redis 7 | Nextcloud requires Redis for file locking and caching. Also used for session management, rate limiting, and as a message broker for smaller workloads. |

### 3.3 External Portal & Admin Console

| Component | Technology | Rationale |
|---|---|---|
| Frontend framework | **Next.js 15** (React, App Router) | TypeScript-based, SSR/SSG capable, excellent DX. The external portal and admin console are React SPAs with SSR for branded room pages. Next.js also gives you API routes for lightweight backend logic without needing a separate service for every endpoint. |
| UI component library | **shadcn/ui + Tailwind CSS** | shadcn/ui provides high-quality, accessible components you own (copy-paste, not dependency). Tailwind gives rapid theming per room (brand colors, logos) — directly supporting the "branded rooms" requirement. |
| State management | Zustand (lightweight) | For client-side state. Server state managed via TanStack Query (React Query) against the API gateway. |
| Document rendering (viewer) | **PDF.js + pdf-lib** for PDFs; **LibreOffice headless** → PDF conversion for Office formats | PDF.js for in-browser page-by-page rendering (the page-streaming viewer). pdf-lib for server-side watermark stamping. LibreOffice headless converts DOCX/XLSX/PPTX → PDF for uniform rendering in the secure viewer. |

### 3.4 Secure Viewer & Watermarking Service

| Component | Technology | Rationale |
|---|---|---|
| Page-streaming viewer | Custom service built on **PDF.js** (Mozilla) | The core enforcement point for view-only mode, watermarking, and analytics. Serves pages individually (not full documents), preventing download of the raw file. Open-source, actively maintained, well-understood rendering pipeline. |
| Server-side watermarking | **pdf-lib** (Node.js) or **qpdf** + **Ghostscript** for batch stamping | Bakes viewer identity, timestamp, and session tokens into rendered page images before sending to browser. Server-side rendering is critical — client-side overlays can be stripped. |
| Office → PDF conversion | **LibreOffice headless** (soffice --headless --convert-to pdf) | Converts DOCX, XLSX, PPTX to PDF on upload so the secure viewer only needs to handle one format. This is how most VDRs normalise document rendering. |
| Screenshot deterrence | Browser-side JS (visibilitychange, blur events, keyboard shortcut interception) + CSS mix-blend-mode overlays | As the requirements note, this is a deterrence layer with a hard ceiling. Paired with watermarking (which survives screenshots) as the actual forensic control. |

### 3.5 Policy & Compliance Engine

| Component | Technology | Rationale |
|---|---|---|
| Policy microservice | **Go** (Golang) | A dedicated ABAC/RBAC policy service sitting in front of Nextcloud's share API. Go is chosen for: compiled binary (no runtime dependency), excellent concurrency for middleware-style request handling, strong standard library for HTTP/TLS/crypto, and being a natural fit for security-oriented services. |
| Identity & SSO | Nextcloud native SAML/OIDC + Keycloak | Keycloak handles identity brokering: SAML 2.0, OIDC, LDAP/AD federation, 2FA/TOTP. Nextcloud connects to Keycloak as its SSO provider. External parties use lightweight OTP-based flows (no Keycloak account needed). |
| NDA / consent gating | Custom middleware (Node.js/Next.js API routes) | Lightweight middleware that intercepts first room access, presents NDA, persists signed acceptance (timestamp, IP, identity, document version) to the audit store. For e-signature integration, embed **DocuSign** or **HelloSign** (or open-source **OpenSign**) via iframe/webhook. |

### 3.6 Audit & Telemetry Pipeline

| Component | Technology | Rationale |
|---|---|---|
| Event bus | **NATS JetStream** | Lightweight, high-throughput message bus for event streaming between services (viewer → audit → analytics → webhooks). JetStream adds persistent streaming, replay, and at-least-once delivery. Lighter than Kafka for a platform of this scale. |
| Immutable audit store | PostgreSQL (append-only table) + **ClickHouse** for analytics queries | Audit events land in an append-only Postgres table (immutable, tamper-evident). ClickHouse (or TimescaleDB as an alternative) provides fast analytical queries over large event volumes for the Phase 4 analytics dashboard. |
| SIEM forwarding | Fluentd / Fluent Bit → syslog or Elastic Agent | Ships structured audit events to your existing SIEM infrastructure (Splunk, QRadar, or open-source Wazuh/Suricata). Fluent Bit is lightweight enough to run as a sidecar in each pod. |
| Log aggregation | **Loki** (Grafana stack) | For application logs (not audit events). Paired with Grafana for dashboards and alerting. Loki is cost-effective for log volumes that don't need full-text indexing. |

### 3.7 Search & AI Services

| Component | Technology | Rationale |
|---|---|---|
| Full-text search & OCR | **OpenSearch** (Elasticsearch fork) + Tesseract OCR | OpenSearch indexes all uploaded documents. Tesseract extracts text from scanned PDFs/images before indexing. OpenSearch is fully open-source (no license concerns like Elasticsearch's change), self-hostable, and proven at scale. |
| PII detection & redaction | **Presidio** (Microsoft, open-source) + custom rule engine | Presidio provides named-entity recognition for PII (names, emails, IDs, phone numbers, etc.) with configurable detection rules and anonymisation/redaction capabilities. Runs as a Python microservice. |
| AI classification & summarisation | **Python** microservice + LLM API (self-hosted Llama 3 or cloud API) | A FastAPI/Flask service that calls an LLM for document classification, summarisation, and translation. For full sovereignty, run **Llama 3** (or **Qwen 2.5** for Chinese language support critical to the HK/China market) via **vLLM** or **Ollama** on GPU nodes. For speed of delivery, start with a commercial API (OpenAI, Anthropic) and migrate to self-hosted later. |
| Document embeddings | **text-embedding-3-small** (or self-hosted BGE/M3 embeddings) | For semantic search over document contents. BGE-M3 (BAAI) supports Chinese, English, and cross-lingual retrieval — important for the Hong Kong market. |

### 3.8 Analytics & Monitoring

| Component | Technology | Rationale |
|---|---|---|
| Viewer telemetry | Custom instrumentation in PDF.js viewer → NATS → ClickHouse | Page-enter/page-exit events, dwell times, scroll depth. ClickHouse handles time-series aggregation efficiently. |
| Analytics dashboard | **Grafana** (or custom React dashboard via Apache ECharts) | Grafana plugs directly into ClickHouse/Postgres for dashboards. For a more productised look, Apache ECharts (used extensively in Chinese enterprise software) provides rich heatmap and engagement visualisations. |
| APM & infrastructure monitoring | **Prometheus + Grafana** | Standard Kubernetes monitoring stack. Node exporter, cAdvisor, and custom service metrics. |
| Alerting | Grafana Alerting + webhook → Slack/email/Teams | Alert on anomalies (mass downloads, unusual hours, abnormal sharing patterns). |

### 3.9 In-Browser Office Editing

| Component | Technology | Rationale |
|---|---|---|
| Office editing | **Collabora Online** (CODE) | Open-source LibreOffice-based editing in the browser. Nextcloud has a mature Collabora app. Supports DOCX, XLSX, PPTX with real-time collaboration and version history. Self-hosted, no Microsoft dependency. |
| Alternative | ONLYOFFICE Document Server | If Collabora's compatibility is insufficient for specific Excel/PowerPoint features, ONLYOFFICE is the alternative. Both integrate with Nextcloud natively. Pick one; don't run both. |

### 3.10 API & Integration Layer

| Component | Technology | Rationale |
|---|---|---|
| REST API | Next.js API routes + Go policy service | The portal's Next.js backend exposes CRUD APIs for rooms, documents, permissions, and audit queries. The Go policy service exposes decision APIs. |
| MCP server | **Python (FastAPI)** implementing the MCP protocol | For agentic AI orchestration. This lets internal AI agents create rooms, upload documents, query audit logs, and trigger workflows programmatically — matching Papermark's agent-native design and the requirements' emphasis on MCP-friendly extensibility. |
| Webhooks | Custom webhook emitter service (Go) listening on NATS | Emits events (room opened, document viewed, NDA signed, export generated) to configurable webhook URLs. Enables Zapier/Make integration and CRM connectors (Salesforce, HubSpot) in Phase 4. |
| Email add-ins | **React-based Outlook Add-in** + **Gmail Extension** | For Phase 3: replace email attachments with governed secure links. Standard web extension and Office Add-in APIs. |

### 3.11 Future: Clean Room Analytics Layer (Phase 5)

| Component | Technology | Rationale |
|---|---|---|
| Privacy-preserving computation | **PrivacyGo Data Clean Room** (TEE-based) | Open-source, supports Jupyter-based analysis, multi-party collaboration without raw data exposure. Relevant to the China/TikTok engineering ecosystem, which aids future China-market expansion. |
| Secure MPC | **MP-SPDZ** | For secure multi-party computation workflows where TEEs aren't sufficient. Requires cryptography-specialised engineering — plan as a separate team track. |
| Query governance | Custom policy layer on top of clean-room engine | Ensures only aggregated, policy-approved outputs leave the clean room. |

---

## 4. Summary Table

| Layer | Primary Tech | Language(s) |
|---|---|---|
| Infrastructure | K3s/Kubernetes, Terraform, Vault, Traefik | HCL, Bash |
| Repository | Nextcloud, PostgreSQL, Redis, MinIO | PHP (Nextcloud), SQL |
| External Portal & Admin | Next.js, shadcn/ui, Tailwind CSS | TypeScript |
| Secure Viewer | PDF.js, pdf-lib, LibreOffice headless | TypeScript, JS |
| Policy Engine | Custom Go microservice | Go |
| Identity | Keycloak (SAML/OIDC/2FA) | Java |
| Event Bus | NATS JetStream | Go |
| Audit Store | PostgreSQL (append-only) + ClickHouse | SQL |
| Search & OCR | OpenSearch + Tesseract | Java, C++ |
| PII Detection | Presidio | Python |
| AI Services | FastAPI + LLM (Llama 3 / Qwen 2.5 via vLLM) | Python |
| Office Editing | Collabora Online | C++ (LO core) |
| Monitoring | Prometheus + Grafana + Loki | Go |
| API & MCP | FastAPI MCP server, Next.js API routes | Python, TypeScript |
| Log Shipping | Fluent Bit | C |
| CI/CD | GitLab CI + Trivy + Semgrep + OWASP ZAP | YAML |
| Clean Room (future) | PrivacyGo, MP-SPDZ | Python, C++ |

---

## 5. Team Skill Requirements

Given a 3-person engineering team:

| Role | Primary languages/skills needed |
|---|---|
| Platform/Backend Engineer | Go, PostgreSQL, Kubernetes, Terraform, NATS |
| Frontend/Product Engineer | TypeScript, React/Next.js, Tailwind, PDF.js |
| Infrastructure/Security Engineer | Docker/K8s, Vault, CI/CD security, Traefik, monitoring stack |

**Phase 3 additions (AI/ML):** One engineer with Python + LLM/ML experience (can be the platform engineer upskilled, or a part-time specialist).

**Phase 5 additions (Clean Room):** Cryptography or privacy-engineering specialist — this is a different skill profile and should be planned as a separate hire or workstream.

---

## 6. Deployment Topology (Initial — Phase 0-2)

```
┌─────────────────────────────────────────┐
│  K3s Cluster (3 nodes minimum for HA)   │
│                                          │
│  ┌─────────┐  ┌──────────┐  ┌────────┐ │
│  │ Traefik │→ │ Nextcloud│  │ Portal │ │
│  │ (ingress)│  │ (app+db) │  │ (Next) │ │
│  └─────────┘  └──────────┘  └────────┘ │
│  ┌─────────┐  ┌──────────┐  ┌────────┐ │
│  │  Redis  │  │  Policy  │  │  Vault │ │
│  └─────────┘  │  (Go)    │  └────────┘ │
│  ┌─────────┐  └──────────┘  ┌────────┐ │
│  │  MinIO  │  ┌──────────┐  │  NATS  │ │
│  └─────────┘  │ Viewer   │  └────────┘ │
│               │ Service  │             │
│               └──────────┘             │
│  ┌─────────┐  ┌──────────┐            │
│  │ Postgres│  │ Keycloak │            │
│  │ (primary│  │ (SSO)    │            │
│  │ +replica│  └──────────┘            │
│  └─────────┘                          │
└─────────────────────────────────────────┘
```

This topology fits on 3-5 VMs (8 vCPU, 32GB RAM each) for Phase 0-2. Scale horizontally by adding worker nodes for the viewer service (most compute-intensive) and read replicas for Postgres/ClickHouse in later phases.

---

## 7. Key Decisions & Alternatives Considered

| Decision | Chosen | Alternatives considered | Why this choice |
|---|---|---|---|
| Frontend framework | Next.js | Remix, Vue/Nuxt, SvelteKit | Largest ecosystem, TypeScript-first, API routes eliminate need for a separate BFF for simple endpoints |
| Policy engine language | Go | Python, Rust, Java | Go compiles to a single binary (easy containerisation), excellent for concurrent middleware, strong crypto stdlib. Python for AI services where ecosystem matters more than raw performance. |
| Event bus | NATS JetStream | Kafka, RabbitMQ, Redis Streams | Kafka is overkill for this scale. RabbitMQ adds operational weight. NATS is ~20MB binary, JetStream adds persistence, and it's Go-native (fits the policy service). |
| Analytics DB | ClickHouse | TimescaleDB, Druid | ClickHouse is columnar, purpose-built for time-series/analytics, and significantly faster than TimescaleDB for aggregation queries on large event volumes. |
| LLM for AI features | Qwen 2.5 / Llama 3 via vLLM | OpenAI API, Anthropic API, local Ollama | Start with cloud API for speed; migrate to self-hosted for sovereignty. Qwen 2.5 specifically for Chinese language support critical to HK/China market expansion. |
| VDR core | Nextcloud | Papermark, custom build | Nextcloud provides the broadest foundation (encryption, sharing, File Drop, LDAP, SSO). Papermark is an alternative worth watching but less mature for enterprise governance. Custom build from scratch would add 6+ months. |

---

## 8. Total Stack Dependency Count

| Category | Number of core services |
|---|---|
| Infrastructure | 4 (K3s, Terraform, Vault, Traefik) |
| Data layer | 4 (PostgreSQL, Redis, MinIO, NATS) |
| Application services | 6 (Nextcloud, Portal, Admin, Policy Engine, Viewer, Keycloak) |
| AI/Search | 3 (OpenSearch, Presidio, LLM service) |
| Monitoring | 3 (Prometheus, Grafana, Loki) |
| CI/CD | 1 pipeline (GitLab CI with multiple scanners) |
| **Total** | **~21 services** |

This is a significant but manageable number for a platform of this ambition. Kubernetes (K3s) handles the operational complexity of running 21 services — deployment, scaling, health checking, and secret injection are all declarative.

---

## 9. Recommended First Steps

1. **Lock the infrastructure stack (Week 1):** Terraform + K3s + Vault + Traefik + GitLab CI. Get a repeatable cluster deployment working before writing any application code.
2. **Deploy Nextcloud and prove the data path (Week 2):** PostgreSQL + Redis + MinIO + Nextcloud. Upload a file, share it, verify encryption. This validates the entire storage foundation.
3. **Scaffold the portal and viewer (Week 3-4):** Next.js external portal with one branded room. Custom viewer with basic page-streaming and server-rendered watermarking. This is the earliest moment you can demo the core user experience.
4. **Layer the policy engine (Week 5-6):** Go microservice intercepting Nextcloud API calls, enforcing time-bound and IP-restricted access. Keycloak for SSO.
5. **Wire the audit pipeline (Week 7-9):** NATS → Postgres append-only → Fluent Bit → SIEM. Every viewer action, policy decision, and file access logged immutably.

By end of Week 9, you have a working prototype that demonstrates the core value proposition: secure, branded, watermarked document exchange with governed access and immutable audit — the foundation to build everything else on top of.
