# ADR-0003: NDA / consent gate — recorded acceptance baseline with an e-signature provider seam

- Status: Proposed
- Date: 2026-09-05
- Deciders: Iven Kwan (owner); security review required before `Accepted` (ADRs touching SR-* controls)
- FTRS traceability: FR-5.1, TR-5.3, NFR-4.1, NFR-5.1, SR-5.1 (audit), SR-5.2 (integrity letter)

## Context

FR-5.1/TR-5.3 require an NDA / consent gate that blocks the first room or file
access until the external party accepts, and retains durable evidence
(timestamp, IP, identity, document version) in the audit store. The gate is a
**non-overridable platform baseline** (AGENTS.md §6 — never skipped).

The open question is the acceptance mechanism. TR-5.3 names three e-signature
integrations (DocuSign / HelloSign / OpenSign iframe+webhook) or — per the
Phase 2 entry criteria — a recorded-acceptance fallback. Forces:

- **Data sovereignty (NFR-7.1, core value proposition):** no SaaS dependency
  on core data paths; tenant data never leaves its cell. An NDA acceptance
  records the signer's identity evidence and the document version it bound to —
  sending that to a US/EU SaaS vendor is a data-residency event for every
  tenant, including mainland-China ones where cross-border transfer of
  identity data is PIPL-sensitive.
- **Latency of onboarding (NFR-5.1):** guest from invitation link to first
  document in < 3 clicks / < 60 seconds. Redirecting to a third-party signing
  flow adds hops and failure modes.
- **Legal weight:** a true e-signature (qualified/advanced under HK Electronic
  Transactions Ordinance, mainland E-signature Law, or eIDAS where relevant)
  may be required for some document types; an NDA between counterparties is
  typically a commercial acceptance where recorded acceptance is defensible,
  but "typically" is not "always" — tenants differ.
- **Operational cost:** running a second self-hosted app (OpenSign) is another
  hardened service to patch and audit; commercial per-envelope fees scale with
  room count.

## Decision

Ship the gate with **platform-native recorded acceptance as the baseline**, and
define an **e-signature provider interface** so tenants can upgrade the
mechanism per transaction without changing the gate logic:

1. The gate middleware (Next.js API, per TR-5.3) presents the NDA text for the
   room **version** currently in force, records explicit acceptance
   (checkbox + typed full name), and persists an immutable evidence record —
   `tenant_id`, room/document version, NDA content hash, principal identity,
   timestamp, IP, user agent — to the append-only audit store (SR-5.1). No
   access path proceeds without a record for the current NDA version; a new
   NDA version invalidates prior acceptances.
2. Acceptance capture goes through an `E-SignProvider` interface. The
   **built-in provider is `RecordedAcceptance`** (default, all tiers).
   A second provider, **OpenSign self-hosted** (iframe + webhook), is the
   documented upgrade path when a tenant requires a third-party-visible
   electronic signature; DocuSign/HelloSign are permitted only as an explicit
   per-tenant opt-in where the tenant's residency rules allow the vendor, and
   never in the Tier 3 on-prem default.
3. All providers return the same evidence record shape to the audit store; the
   provider type is recorded per acceptance. The integrity letter (FR-1.7,
   SR-5.2) includes the acceptance evidence hash.

Rationale: the compliance control that must hold everywhere is *the gate and
its durable evidence*, not the signature vendor. Recorded acceptance keeps the
sovereignty story intact, has zero per-acceptance cost, and meets the
3-click/60-second budget. E-signature remains available where a tenant
actually needs it, behind the same seam — no rework of the gate when the
provider changes.

## Alternatives considered

- **DocuSign / HelloSign (commercial SaaS iframe+webhook):** fastest to
  integrate and strongest brand recognition for signers. Rejected as the
  default: contradicts the data-sovereignty posture (signer identity data
  leaves the cell), adds per-envelope cost at room scale, and is unavailable
  in some mainland deployment contexts. Kept as per-tenant opt-in via the seam.
- **OpenSign self-hosted as the default:** sovereignty-compatible and
  closer to TR-5.3's letter. Rejected as the *default* for now: it adds a
  second security-sensitive self-hosted service to operate and audit before
  the MVP gate, and the gate's evidence requirements do not need it. Chosen as
  the documented upgrade path instead (see Consequences — decision is
  reversible without rework because of the seam).
- **No seam, hard-wire recorded acceptance:** simplest, but reverses into
  rework the day a tenant demands a qualified e-signature. Rejected.

## Consequences

**Positive:** the non-overridable gate ships in Phase 2 without vendor
dependency; evidence records stay in the audit store (NFR-4.1 append-only);
sovereignty and PIPL/跨境 concerns are default-satisfied; onboarding stays
inside NFR-5.1.

**Negative / to enforce:** recorded acceptance is a weaker legal artifact than
a vendor e-signature for dispute scenarios — the room contract/NDA template
must state acceptance of recorded evidence, and legal counsel review is
required before commercial launch (Phase 4 gate dependency, same owner as the
Phase 0 classification caveat). Tenant-configurable provider selection must be
policy-governed (per-tenant AI-consent-style switch) so a tenant's residency
rules are enforced in the policy engine, not by UI.

**Out of scope (now):** OpenSign deployment; provider metering events for
e-sign (FR-11.4 metering dimension); per-tenant provider policy wiring — all
land with the Phase 2 gate build, using this ADR's seam.

## Security review

Pending — required before `Accepted` (SR-5.1/SR-5.2 evidence chain).
