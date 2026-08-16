# ADR-0002: CI platform — GitLab CI per TR-1.3, self-hosted CE lab for Phase 0, GitHub remains code home

- Status: Proposed (pending security sign-off — touches SR-4.1 / SR-4.4)
- Date: 2026-08-16
- Deciders: Platform Engineering (Iven Kwan)
- FTRS traceability: TR-1.3, SR-4.1, SR-4.4

## Context

TR-1.3 mandates GitLab CI with a self-hosted runner for the security pipeline
(SAST, DAST, dependency scanning, SBOM, image scanning). The canonical code
repository, however, currently lives on GitHub (`github.com/ivenkwan/EVDR`).
Phase 0 cannot close its "CI/CD pipeline green" exit criterion until this
tension is resolved: the pipeline definition (`.gitlab-ci.yml`) is authored and
locally validated, but has never executed on a runner.

Constraints:

- The pipeline stages (Semgrep SAST, OWASP ZAP DAST, Trivy dependency + image
  scanning, Syft SBOM) are GitLab-CI-native; porting them to a second CI
  system would double maintenance of a security-critical surface.
- The lab workstation has no external GitLab instance available; any Phase 0
  proof must run self-contained.
- Credentials must never enter the repository — runner tokens, registry
  credentials, and kubeconfigs are supplied via masked CI variables only
  (see `docs/runbooks/gitlab-runner.md`).

## Decision

1. **GitLab CI remains the CI platform of record**, per TR-1.3. No port to
   GitHub Actions is made; `.gitlab-ci.yml` is the single pipeline definition.
2. **For Phase 0 gate evidence**, a self-hosted GitLab CE instance (Docker,
   lab-only) plus a locked `evdr-docker` runner (docker executor) executes the
   pipeline against the sample service. This proves the pipeline end-to-end
   without external dependencies.
3. **GitHub remains the canonical code home for now.** The lab GitLab holds a
   working mirror/clone for CI execution. The production hosting decision —
   migrate canonical to GitLab (self-hosted or SaaS), or keep GitHub with a
   push mirror to GitLab-for-CI — is deferred to Phase 1 entry and recorded by
   superseding or amending this ADR. Push mirroring keeps CI executing on
   exactly what lands on the default branch either way, so the pipeline
   semantics decided here survive that choice.

## Alternatives considered

- **Migrate canonical repo to GitLab.com SaaS now.** Rejected for Phase 0:
  requires external account/billing decisions and data-residency review for a
  compliance product's source hosting; not needed to prove the pipeline.
- **Deviate to GitHub Actions + self-hosted runner.** Rejected: contradicts
  TR-1.3, forks the security-pipeline definition onto a second platform, and
  GitHub-hosted runners conflict with the self-hosted posture required for
  later sovereign tiers.
- **Keep GitHub only, defer all CI execution.** Rejected: the Foundation Gate
  requires a *green pipeline against a sample service* as exit evidence;
  deferring blocks the gate.

## Consequences

- The lab GitLab CE instance is **not** a production service: no backup, no
  HA, ephemeral storage acceptable. It exists to produce Phase 0 gate evidence.
- All CI credentials flow through masked/protected CI variables; the pipeline
  itself contains no secrets (enforced by the Semgrep credential-literal rule
  in `ci/semgrep/rules.yml`).
- When the production hosting decision lands (Phase 1), the runner
  registration model in `docs/runbooks/gitlab-runner.md` carries over
  unchanged; only the GitLab base URL and project path change.
- The DAST stage targets the sample service deployed on the lab K3s cluster;
  runner DNS/hosts mapping for `hello.evdr.internal` is runner configuration,
  not repo content.
- Out of scope: GitLab hardening for production use (LDAP/SAML, backup,
  monitoring) — revisited when hosting is decided.
