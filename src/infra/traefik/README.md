# Traefik Edge / API Gateway (TR-1.5)

Traefik is the single public edge: TLS termination, routing, rate limiting, and (from Phase 1) per-tenant resolution middleware. The embedded K3s Traefik is disabled — this directory is the only ingress controller definition.

## Files

| Path | Purpose |
|---|---|
| `helm-values.yaml` | Traefik deployment: entrypoints, HTTPS redirect, JSON logs, Prometheus metrics, default TLS option binding |
| `crds/tls-option.yaml` | Cluster default TLS policy — TLS 1.2 floor, strong suites, strict SNI (SR-1.2) |
| `crds/default-certificate.yaml` | Default serving certificate from Vault PKI + default TLSStore |
| `crds/middlewares.yaml` | Baseline edge middlewares: rate limit, retry, security headers |

## Apply order

1. cert-manager + bootstrap CA + Vault + `bootstrap-vault.sh` + `cluster-issuer-vault.yaml` must all be live (see the Phase 0 runbook).
2. `helm upgrade --install traefik traefik/traefik -n traefik --create-namespace -f src/infra/traefik/helm-values.yaml`
3. `kubectl apply -f src/infra/traefik/crds/`

## Requirement mapping notes

- **Automatic TLS (TR-1.5):** every Ingress/IngressRoute gets a cert-manager Certificate from `vault-pki-issuer`; HTTP→HTTPS redirect is unconditional at the `web` entrypoint.
- **TLS policy (SR-1.2):** `crds/tls-option.yaml` is bound as the default for `websecure`; TLS 1.0/1.1 and weak suites are refused at handshake.
- **Rate limiting (TR-1.5 / groundwork for SR-3.2):** `edge-rate-limit` is the cluster-wide baseline. Per-tenant limits arrive in Phase 1 as generated IngressRoute middlewares keyed by tenant resolution.
- **Circuit breaking (TR-1.5 wording):** Traefik v3 has no circuit-breaker middleware (removed after v1.7). The delivered equivalents are: retry middleware (`edge-retry`), Kubernetes readiness gates, and rate limiting. If a true breaker is needed later, it lands in the policy-engine sidecar or a service-mesh decision — tracked as an architecture note, not silently dropped.
- **Tenant resolution middleware (TR-1.5):** Phase 1 deliverable; it will key off host/slug and attach tenant context headers verified downstream.
