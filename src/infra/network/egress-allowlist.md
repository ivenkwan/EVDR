# Egress Allow-List (SR-3.1)

Tier 1 runs in a shared VPC **with egress controls**: cluster nodes may reach only the destinations below. Everything else is dropped and logged. This document is the source of truth; enforcement mechanism depends on the environment (see §3).

## 1. Allowed destinations

| Destination | Ports | Purpose | When needed |
|---|---|---|---|
| `github.com`, `objects.githubusercontent.com` | 443 | K3s release binaries (checksum-verified installs) | Node bootstrap/upgrade only |
| `get.k3s.io` | 443 | (not used at runtime — installs go through the verified-binary path) | — |
| OCI registries: `registry-1.docker.io`, `quay.io`, `gcr.io`, `k8s.gcr.io`, `registry.k8s.io`, `ghcr.io` | 443 | Container images (Vault, Traefik, cert-manager, CI-scanned app images) | Always |
| Ubuntu archives (`archive.ubuntu.com`, `security.ubuntu.com`) or internal mirror | 80/443 | OS security patches (SR-4.4 patch SLA) | Always |
| Helm chart repos (`helm.releases.hashicorp.com`, `charts.jetstack.io`, `traefik.github.io`, `charts.helm.sh` CDNs) | 443 | Chart fetch at deploy time | Deploy windows; mirror to an internal registry for real cells |
| Enterprise IdP endpoints (AD/LDAP/Entra/Okta tenant URLs) | 389/636/443 | Keycloak federation (Phase 1, FR-4.1) | From Phase 1 |
| NTP (`pool.ntp.org` or internal time source) | 123/udp | Clock sync — certificate and audit-trail integrity | Always |
| DNS resolvers (VPC resolver / internal DNS) | 53 | Name resolution | Always |
| SIEM destinations (tenant-configured) | per tenant | Fluent Bit forwarding (SR-5.3) | From Phase 2 |

## 2. Explicitly denied (examples, not exhaustive)

- Arbitrary internet HTTP/HTTPS (no general web egress from cluster nodes).
- Outbound SMTP except via the approved relay (when alerting lands, TR-11.2).
- Any direct egress from pods: NetworkPolicies in `network-policies.yaml` keep pod egress in-cluster; only node-level bootstrap/patch flows leave the VPC.

## 3. Enforcement by environment

- **libvirt lab / Tier 0 on-prem:** host `nftables` ruleset applied by the rebuild runbook — forward chain default-drop for the cluster subnet with the destinations above allowed; DNS/NTP pinned to internal servers.
- **Public clouds (future `clouds/` modules):** the same list rendered as security-group egress rules + NAT gateway allow-listing inside the cloud module (see `src/infra/terraform/README.md` contract).
- **Tier 3 on-prem:** customer network enforces; we ship this list as the required-egress specification.

## 4. Change control

Adding a destination is a PR against this file with a requirement trace, reviewed by security. The allow-list is re-validated at each phase gate alongside the threat model (SR-4.3).
