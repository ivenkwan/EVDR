# hello-service — Phase 0 pipeline validation workload

The smallest real service that proves the CI/CD security gates work end to end (Phase 0 exit criterion: *"CI/CD pipeline green against a sample service with SAST, DAST, dependency scan, and SBOM stages all reporting"*).

- **SAST:** Semgrep scans this service on every pipeline (`ci/semgrep/rules.yml`).
- **Dependency scan:** Trivy fs scans its `go.mod` (zero external deps by design — the baseline must be clean before real services arrive).
- **Build:** multi-stage Dockerfile → distroless `nonroot` image via Kaniko, pushed to the GitLab registry.
- **Image scan:** Trivy fails the pipeline on CRITICAL findings (SR-4.4).
- **SBOM:** Syft emits CycloneDX + SPDX artifacts, retained 90 days as evidence.
- **DAST:** OWASP ZAP baseline runs against the staging deployment at `https://hello.staging.evdr.internal`.

## Endpoints

| Path | Purpose |
|---|---|
| `/healthz` | liveness probe |
| `/readyz` | readiness probe |
| `/version` | build version (injected via ldflags) — proves the deployed image is the scanned build |

## Conventions demonstrated here (copy these, not the functionality)

- Structured logging via the standard library `log/slog` (production Go services use `logrus` per `CLAUDE.md`; this sample deliberately has zero dependencies so the pipeline baseline stays hermetic).
- `http.Server` with explicit timeouts; graceful shutdown on SIGTERM.
- Security headers set in-service — the edge adds its own, services do not rely on the proxy.
- Kubernetes: non-root UID 65532, read-only root filesystem, dropped capabilities, no service-account token, resource requests/limits, liveness/readiness probes.
