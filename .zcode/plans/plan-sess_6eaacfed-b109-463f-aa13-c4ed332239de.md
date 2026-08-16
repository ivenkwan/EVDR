## Task 1 — Fix companion-document paths in FTRS

Edit `Z:\GITHUB\EVDR\Requirements\EVDR-Functional-and-Technical-Requirement-Specifications.md` (lines 6–8), changing the three companion references from `Requirements/EVDR-*.md` to `Requirements/Ref/EVDR-*.md`:
- `Requirements/Ref/EVDR-Technology-Stack-Recommendation.md`
- `Requirements/Ref/EVDR-Multi-Tenant-Architecture-Addendum.md`
- `Requirements/Ref/EVDR-Object-Storage-Alternatives-Analysis.md`

## Task 2 — Create `Z:\GITHUB\EVDR\Todo.md` (repo root) — detailed build-activity phase plan

A checkbox-driven working document synthesized from FTRS Section 11 deliverables, expanded with the full FR/TR/SR requirement mappings from Sections 4–6. Structure:

**Header block**
- Purpose, how to use the checklist, source-of-truth note (FTRS v1.0)
- Milestones & gates overview table: MVP gate (end of Phase 2, wk 15), Commercial Launch gate (end of Phase 4, wk 26, per Section 13's 8 criteria), Phase 2.5 running parallel with Phase 3

**Per phase (0, 1, 2, 2.5, 3, 4, 5) — same template:**
- **Objective** — one paragraph
- **Window** — week range from Section 11
- **Entry criteria** — verifiable preconditions (e.g., Phase 1 requires frozen Room SPI contract + green CI/CD from Phase 0; Phase 2.5 requires stable NextcloudAdapter conformance suite)
- **Build activities** — grouped by workstream (Infra / Storage / Identity / Frontend / Viewer / Policy / Events / AI / Control-plane), each as a `- [ ]` checkbox annotated with its FR/TR/SR ID (e.g., "Deploy hardened Nextcloud — TR-2.11–2.13", "Server-side dynamic watermarking — FR-3.2, TR-4.2")
- **Exit criteria** — verifiable completion conditions forming the gate to the next phase (e.g., Phase 0 exit: threat model signed off, IaC baseline reproducible, SPI contract frozen, CI/CD with SAST/DAST/SBOM green)

Activity coverage per phase (from the FTRS tables):
- **P0 (wk 1–3):** threat model, data classification model, Terraform+K3s+Vault+Traefik IaC, GitLab CI security pipeline, Room SPI contract draft, DRM strategy decision
- **P1 (wk 4–9):** Nextcloud/Ceph/PG/Redis deployment, Room SPI + NextcloudAdapter + conformance suite, branded-room portal, secure viewer + watermarking + LibreOffice conversion, Keycloak SSO + guest OTP links, File Drop, NATS + observability stack
- **P2 (wk 10–15):** Go/OPA policy engine, RBAC/ABAC, immutable audit log + SIEM pipeline, NDA/e-sign gate, export package + integrity letter, envelope encryption + SSE-KMS, PG RLS, viewer hardening, AV scanning, break-glass model, first pen test
- **P2.5 (wk 16–20, parallel):** NativeAdapter + parity, Tenant Provisioner (<15 min), realm automation, Admin/Operator consoles, metering, cell stamping Helm chart, suspension/offboarding + cryptographic erasure, 2–3 BU pilot
- **P3 (wk 16–22):** OpenSearch + Tesseract OCR, Presidio PII + redaction, AI classification/summarisation/translation (vLLM/BGE-M3), AI consent switch, Collabora editing (pending Collabora-vs-ONLYOFFICE decision), email add-ins, REST API + MCP server, classification labels, per-tenant SIEM, key rotation/BYOK
- **P4 (wk 23–26):** page telemetry + dashboards/heatmaps, leak detection, anomaly alerts, webhook emitter + CRM/Zapier, Stripe billing GA + Ed25519 license server, SOC 2 Type I, multi-GB files, custom domains, tier upgrade automation, Garage appliance option, Q&A workflow
- **P5 (wk 27+):** PrivacyGo TEE + MP-SPDZ prototypes, governed query interface, clean-room control separation, CubeFS/mainland cell evaluation

**Tail sections:**
- Cross-phase open-decisions tracker (Section 14's 8 decisions with due milestones)
- Commercial Launch Gate checklist (Section 13's 8 criteria as verifiable checkboxes)

Both edits use the Edit/Write tools only; no shell commands needed.