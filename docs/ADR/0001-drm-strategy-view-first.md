# ADR-0001: DRM Strategy — View-First Default, Controlled Export, Bounded PPAD R&D

- Status: Accepted
- Date: 2026-08-16
- Deciders: Platform Lead, Security Lead, Product Owner
- FTRS traceability: FR-3.1–FR-3.7, FR-1.7, TR-4.1–TR-4.6, SR-5.2, NFR-1.6

## Context

EVDR exchanges documents with external parties who are, by design, not fully trusted. A recurring demand in VDR procurement is "DRM" — the ability to keep control of a document *after* it has been viewed or downloaded (post-download protection). The FTRS frames this as FR-3.7 (PPAD: post-download protection via a DRM wrapper or rights-enforced container), prioritised Low and scheduled as a Phase 2 stretch item.

The engineering reality that drives this decision:

1. **Browser-based viewing cannot prevent capture.** Any content rendered on a screen can be photographed or screenshotted. Client-side controls (disable right-click, block print) are deterrents, not controls, and are bypassable by a determined recipient.
2. **Post-download enforcement requires a controlling agent on the recipient's device.** File-format DRM (password/expiry PDF wrappers) is trivially strip-able; rights-management containers (e.g. Microsoft Purview/AIP-class IRM) require recipient-side software installation and identity federation, which conflicts with EVDR's zero-friction guest model (NFR-5.1: link to first document in < 3 clicks, < 60 seconds, no account creation).
3. EVDR's actual defensible value is **deterrence + attribution + evidence**, not absolute prevention: server-rendered watermarking with viewer identity (FR-3.2), page-streaming so the full document never reaches the browser (FR-3.1), and an immutable audit trail that makes leaks attributable and prosecutable.

The Phase 0 build plan requires this strategy to be recorded explicitly so that Phases 1–2 build the right controls and sales/engineering stop promising hard post-download DRM.

## Decision

EVDR adopts a **view-first default** document protection strategy:

1. **Default posture: view-only in the browser.** Documents are served as individually rendered, server-watermarked pages (FR-3.1, FR-3.2, TR-4.1, TR-4.2). Download, print, and copy are blocked by default at the room policy level (FR-3.3), with watermark identity, timestamp, IP/domain, and session ID baked into every rendered page.
2. **Controlled export model.** Download is an explicit, auditable privilege granted per room/folder/file tier (FR-1.2), never a silent default. Authorised exports are watermarked, logged, and — for full-room export — packaged with a SHA-256 integrity letter (FR-1.7, SR-5.2). Post-download protection for exports is contractual (NDA gate, FR-5.1) plus evidentiary (audit trail), not cryptographic.
3. **PPAD is a bounded R&D track, not a committed feature.** A single feasibility spike at Phase 2 (stretch) evaluates a rights-enforced container/wrapper for the narrow case of authorised exports to counterparties who accept a viewer agent. The spike has a fixed time-box, produces a go/no-go ADR, and must not alter the view-first architecture. No sales or roadmap commitment is made for PPAD beyond this spike.

## Alternatives considered

- **Full recipient-side DRM (IRM container, e.g. Azure RMS class):** Strongest theoretical post-download control. Rejected: requires recipient software install and federated identity, destroying the no-account guest model; licence cost and operational complexity per external party; poor fit for ad-hoc counterparties (auditors, regulators, deal counterparties).
- **File-format DRM wrappers (expiring/password PDFs, securedownload products):** Rejected as a primary control: wrappers are strip-able with commodity tools, creating a false sense of security and a compliance liability if marketed as a control. May be revisited inside the PPAD spike only as an export-time *deterrent* layered on watermarking.
- **No export at all (hard view-only platform):** Rejected: legitimate deal workflows require controlled export (e.g. regulator submission packages); refusing export pushes users to shadow-IT channels with zero audit.

## Consequences

- Phase 1 builds page-streaming, server watermarking, and view-only enforcement as **the** protection storey; engineering effort is not diluted on recipient-side agents.
- Marketing and security questionnaires must describe post-download protection as *watermark attribution + audit evidence + contractual NDA*, never as cryptographic enforcement. This wording is part of the compliance storey and must be reviewed when this ADR changes.
- Room permission tiers (view/download/print/edit, FR-1.2) become load-bearing security policy and are enforced in the Policy Engine (Phase 2), not just the UI.
- The PPAD spike (Phase 2, stretch) must produce either a superseding ADR or an explicit no-go record; silence is not an acceptable outcome.
- Screenshot-friction features (FR-3.5) and anomaly detection (FR-7.7) are prioritised as the realistic leak-deterrence layer.
