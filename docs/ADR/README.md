# Architecture Decision Records

This directory contains the Architecture Decision Records (ADRs) for EVDR. An ADR captures a significant architectural decision: the context that forced it, the decision itself, the alternatives that were rejected, and the consequences the team accepts.

## When to write an ADR

Write an ADR when a decision:

- is expensive or disruptive to reverse (protocols, storage engines, tenancy models, crypto design),
- changes a security or compliance posture (touch any SR-* requirement),
- selects between credible alternatives where reasonable engineers would disagree,
- freezes or changes a cross-team contract (e.g. the Room SPI).

Do **not** write ADRs for routine implementation choices that follow existing conventions in `CLAUDE.md`.

## Format and lifecycle

- One file per decision: `NNNN-short-kebab-title.md`, numbered sequentially, never reused.
- Use the template below (MADR-style, trimmed for this project).
- Status lifecycle: `Proposed` → `Accepted` → (`Deprecated` | `Superseded by ADR-NNNN`).
- An `Accepted` ADR is **immutable in substance**. Corrections of fact are allowed; changing the decision requires a new ADR that supersedes it.
- ADRs touching security controls (any SR-* mapping) require security review sign-off before moving to `Accepted`.
- Every ADR must trace to the FTRS: list the FR/TR/SR/NFR IDs it implements or constrains.

## Template

```markdown
# ADR-NNNN: Title

- Status: Proposed | Accepted | Deprecated | Superseded by ADR-NNNN
- Date: YYYY-MM-DD
- Deciders: <names/roles>
- FTRS traceability: <IDs>

## Context
What is the issue we are facing? Include constraints, forces, and requirements.

## Decision
What is the change we are making? State it plainly in one or two sentences, then detail.

## Alternatives considered
For each alternative: what it is, why it was rejected.

## Consequences
Positive and negative. What becomes easier, what becomes harder, what must now be
enforced (and where: CI, policy engine, review), what is explicitly out of scope.
```

## Index

| ADR | Title | Status | Date |
|---|---|---|---|
| [0001](0001-drm-strategy-view-first.md) | DRM strategy: view-first default, controlled export, bounded PPAD R&D | Accepted | 2026-08-16 |
| [0002](0002-ci-platform-gitlab.md) | CI platform: GitLab per TR-1.3, self-hosted CE lab for Phase 0, GitHub code home | Proposed | 2026-08-16 |
