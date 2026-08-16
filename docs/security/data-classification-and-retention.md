# EVDR Data Classification & Retention Policy Model

> **Status:** v0.1 — Phase 0 model definition, **pending approval** (Phase 0 exit criterion)
> **Requirement traceability:** FR-5.4 (classification labels with policy-driven access rules), FR-5.5 (retention/auto-purge per class), NFR-7.5 (HKMA alignment), FR-1.6 (legal hold), FR-11.8 (erasure), SR-5.x (evidence)
> **Scope:** the *model* — label taxonomy, handling rules, retention schedule, and how each lands in platform controls. Enforcement mechanics are built in Phase 2 (policy engine) and Phase 3 (labels on documents).

---

## 1. Purpose

Regulated enterprises (starting with HSBC internal use) must be able to prove that every document in EVDR carries a classification, that access and handling follow from that classification, and that retention and disposal are systematic rather than ad hoc. This document defines the model that the Policy Engine (P2), document labels (P3), and retention automation (P3) implement.

## 2. Classification Taxonomy

Four levels, aligned to common HK-regulated-institution schemes so tenants can map their internal taxonomies 1:1. The platform stores the **platform label**; tenant-specific label names are mapped as display aliases (P3 tenant config).

| Level | Label | Definition | Examples | Default handling profile |
|---|---|---|---|---|
| C0 | `PUBLIC` | Approved for public release | Marketing materials, published terms | No access restriction beyond room membership; watermark optional per room policy; download allowed |
| C1 | `INTERNAL` | Internal business information; low harm if leaked | Internal procedures, org announcements | Watermark on; download per room tier; no external guests without Room Owner approval |
| C2 | `CONFIDENTIAL` | Commercially sensitive; material harm if leaked | Deal documents, contracts, financial models, board packs | **Default for new uploads.** Watermark mandatory (non-overridable baseline, FR-5.2); view-first default (ADR-0001); download is explicit per-grant; external access requires NDA gate (FR-5.1) |
| C3 | `RESTRICTED` | Regulatory/legally privileged/PII-bearing; severe harm if leaked | Client PII, regulatory correspondence, legal advice, KYC files | C2 controls **plus**: view-only default with download requiring Room Owner + policy-engine allow (P2); export requires enhanced audit justification; retention floors cannot be lowered by tenant overlay (FR-5.2/FR-5.3) |

Rules:

- Labels attach to **documents** (and default-inherit from the room's classification setting); folders can set a default, individual documents may raise but never silently lower below the room floor.
- Label changes are audit events (who, from→to, when, why).
- C3 documents trigger the PII detection pipeline as a pre-release check when AI/redaction ships (FR-8.2/8.3, P3).
- Mixed-classification rooms are normal: the strictest applicable label governs each document, not the room average.

## 3. Retention Model

Retention is expressed per classification with **platform floors** (non-overridable baselines, FR-5.2) and **tenant overlays** that may only *extend* retention or add holds (FR-5.3).

| Class | Minimum retention (floor) | Default maximum | Disposal method | Notes |
|---|---|---|---|---|
| C0 PUBLIC | None (room lifetime) | Room closure + 30 days | Standard delete | — |
| C1 INTERNAL | Room lifetime | Room closure + 90 days | Standard delete | Tenant overlay may extend |
| C2 CONFIDENTIAL | Room closure + 6 months | 7 years from room closure | Cryptographic erasure (key destruction, FR-11.8) | Floor aligns with common HK record-keeping expectations for business records; tenant may extend |
| C3 RESTRICTED | 7 years from room closure (or per regulation binding the record) | Per regulation; else 7 years | Cryptographic erasure + **certificate of erasure** (SR-5.2) | Floor is a platform baseline; tenant overlays cannot shorten (FR-5.2) |

Model rules:

1. **Retention clock anchors:** room closure, document upload, or case closure — the anchor is part of the room's retention policy set at creation (P1 `ApplyRetention` in the Room SPI, TR-2.1).
2. **Legal hold overrides everything.** A sealed room / legal hold (FR-1.6) freezes documents *and metadata* regardless of schedule; purge jobs must check hold state before every destructive action.
3. **Auto-purge is evidence-producing, not silent.** Every purge emits an audit event with document ID, class, policy applied, and pre-purge SHA-256 (FR-5.5, SR-5.1).
4. **Audit records themselves** follow the audit retention schedule (see §4), independent of document class.
5. **Backups** age out on the same schedule as primary data; retention policy must account for snapshot lifecycle (restore drills, NFR-3.4).

## 4. Audit & Evidence Retention

| Record | Minimum retention | Rationale |
|---|---|---|
| Audit trail (append-only log, SR-5.1) | 7 years | SOC 2 / ISO 27001 evidence windows; HKMA supervisory expectations for regulated-institution records |
| NDA/e-signature acceptance evidence (FR-5.1) | Life of access grant + 7 years | Contract enforceability after access ends |
| Export packages + integrity letters (SR-5.2) | 7 years | Evidentiary value of the integrity letter depends on retention |
| Erasure certificates (FR-11.8) | Permanent (or ≥ 10 years) | Proof of disposal is itself a compliance record |
| Privileged-action logs (SR-2.3) | 7 years | Operator accountability window |

## 5. HKMA Alignment (NFR-7.5)

This model is designed to be defensible under HKMA supervisory expectations for regulated institutions (notably the Supervisory Policy Manual modules on technology risk and operational resilience, and record-keeping obligations under Hong Kong regulatory regimes). Specifically:

- **Four-level taxonomy** maps onto the classification schemes HKMA expects institutions to operate, with handling rules proportionate to sensitivity.
- **Retention floors + cryptographic erasure with certificates** give examiners a demonstrable systematic-disposal storey rather than ad-hoc deletion.
- **Immutable audit with 7-year retention** supports HKMA examination and investigation windows.
- **Data residency** (HK cell default; mainland cell evaluation in P5) keeps records within expected jurisdictions (NFR-7.1).

Before commercial launch (P4), this model must be reviewed against the specific obligations of each launch tenant type (e.g. SFC-licensed corporations' record-keeping rules where applicable). That review is tracked as a launch-gate dependency, not a Phase 0 blocker.

## 6. Mapping to Platform Controls (build traceability)

| Model element | Enforcing control | Phase |
|---|---|---|
| Room retention policy at creation | Room SPI `ApplyRetention` (contract v0.1) | P1 contract → P3 automation |
| Watermark mandatory at C2+ | Policy engine global baseline, no tenant override (FR-5.2) | P2 |
| NDA gate for external access | NDA/e-sign gate (FR-5.1, TR-5.3) | P2 |
| Download/print/edit tiers | Room permission tiers (FR-1.2) + policy decisions logged | P1 tiers → P2 enforcement |
| Legal hold | `SealRoom` (Room SPI) + retention freeze (FR-1.6) | P1 contract → P2 semantics |
| Auto-purge with evidence | Retention/auto-purge schedules (FR-5.5) on ClickHouse/PG audit | P3 |
| Document labels | Classification labels on documents (FR-5.4) | P3 |
| Erasure certificates | Tenant offboarding crypto-erasure (FR-11.8) | P2.5 |

## 7. Approval

| Version | Date | Change | Approver |
|---|---|---|---|
| 0.1 | 2026-08-16 | Phase 0 model baseline | *(pending)* |
