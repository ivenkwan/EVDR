# EVDR — Object Storage Alternatives Analysis (Replacing MinIO)

> Version 1.2 | August 2026 | Companion to `Requirements/EVDR-Technology-Stack-Recommendation.md` v1.0 and `Requirements/EVDR-Multi-Tenant-Architecture-Addendum.md` v1.1
>
> **Decision:** replace MinIO with **Ceph RGW (via Rook)** as the primary object storage for all tiers; **CubeFS** as the designated storage layer for the future mainland-China cell; **Garage** reserved for edge/on-prem appliances. Application-layer envelope encryption (per-document DEK wrapped by tenant KEK in Vault) becomes the **primary** tenant-key isolation mechanism — SSE becomes a secondary layer, not the primary one.

---

## 1. Why MinIO Is Off the Table

The v1.0/v1.1 architecture specified MinIO as the S3-compatible object store. That choice is no longer defensible:

- **The company hollowed out the community edition.** In May–June 2025 MinIO stripped the admin console from the free edition down to a bare object browser, moving management features into the paid AIStor product. This was widely read as a "trojan horse" update — features silently disappeared on upgrade.
- **The open-source project is dead.** In December 2025 the company announced the project was in maintenance mode; the GitHub repository was archived in February 2026 (LWN dates the full archival to early 2026; some trackers record April 2026). No new code, no fixes beyond advisories for the last release.
- **This hits our design directly.** v1.1 relied on KES (MinIO's key encryption service) for per-tenant KMS keys behind the paid wall's trajectory, and on MinIO's K8s operator. Building a commercial product on archived upstream with an adversarial vendor is a non-starter — especially a product whose buyers are banks asking "what happens if your vendor disappears?"
- **Forks are not an exit.** The main fork (Silo, maintained by Ruohang Feng) is explicitly bug/security-fixes-only with no feature roadmap. Acceptable as a short-term bridge for an existing MinIO deployment; not a foundation for a new build.

The product thesis is **vendor-stability sovereignty**. Our storage layer must embody it.

---

## 2. Candidates Evaluated

| Candidate | License | Governance | Maturity | Verdict for EVDR |
|---|---|---|---|---|
| **Ceph RGW** (via Rook) | LGPL 2.1 / Apache 2.0 mix | Ceph Foundation (Linux Foundation); IBM/Red Hat sustained development | ~15 years, S3 gateway since ~2012, massive production base | ✅ **Primary choice — all current tiers** |
| **CubeFS** | Apache 2.0 | CNCF **graduated** (March 2025); born at JD.com | 200+ production orgs: JD, OPPO, NetEase, Beike, Xiaomi, Shopee | ✅ **Designated for mainland-China cell** |
| **Garage** | AGPL v3 | Deuxfleurs non-profit, independent Forgejo hosting | Young (2020) but unusual rigor: S3 correctness/durability testing | ⚠️ Reserved: single-box on-prem appliances / edge |
| **SeaweedFS** | Apache 2.0 | Single primary maintainer (Chris Lu); active | Strong throughput & small-file story; SSE added mid-2025 but with early bugs (e.g., KMS multi-part corruption #7499) | ❌ Single-maintainer bus factor for a bank product |
| **Swift (OpenStack)** | Apache 2.0 | OpenStack Foundation | Mature, but an OpenStack-era deployment model misaligned with our K8s-native stack | ❌ Operational mismatch |
| **Apache Ozone** | Apache 2.0 | Apache Software Foundation | Solid Hadoop-lineage object store | ❌ Hadoop ecosystem gravity; weaker fit as a pure S3 backend for K8s microservices |
| **Silo (MinIO fork)** | AGPL v3 | Individual maintainer | Fixes-only; no roadmap | ❌ Bridge option only — nothing to bridge from |

---

## 3. Evaluation Against EVDR's Criteria

### 3.1 The criteria (from the multi-tenant addendum)

C1 S3 API fidelity (SigV4, versioning, lifecycle, multipart) · C2 Per-tenant encryption & key management compatible with Vault · C3 Kubernetes operability · C4 Governance & longevity (foundation/vendor-neutral stewardship) · C5 Scale range — must work at 3-node dedicated tier *and* large shared SaaS cell · C6 HK/China path · C7 Total cost of operations for a small platform team.

### 3.2 Matrix

| Criterion | Ceph RGW | CubeFS | Garage | SeaweedFS |
|---|---|---|---|---|
| C1 S3 fidelity | **Best-in-class** — v2+v4 signing, versioning, lifecycle, bucket notifications; runs the `ceph/s3-tests` suite | Good (S3 + POSIX + HDFS protocols) | Partial — **no object versioning**, SigV4 only | Good; passes much of s3-tests but gaps remain |
| C2 Keys/Vault | **SSE-KMS with Vault backend natively**; also SSE-S3/SSE-C | Own key management; no native Vault KMS story surfaced | **SSE-C only** — explicitly rejects SSE-KMS/S3 | SSE-S3/KMS/C since ~mid-2025; early bugs observed |
| C3 Kubernetes | **Rook operator** — CNCF-graduated, first-class K8s story | K8s-deployable; strong in CNCF ecosystem | Single Go/Rust binary, trivial to run, no operator | Simple binaries; community operators |
| C4 Governance | Ceph Foundation / Linux Foundation; kernel-merged tech; steady releases (20.2.x in 2026) | CNCF graduated — highest maturity level | Non-profit, independent infra; healthy but young | Individual-led; active but concentrated |
| C5 Scale range | PB-scale; needs ≥3 OSDs, meaningful RAM at mon/mgr | Web-scale proven (tens of billions of objects at OPPO) | Small-to-medium sweet spot; 1GB RAM minimum | Strong throughput, esp. small files |
| C6 HK/China | Neutral | **Born in China (JD.com), Chinese-language docs/community, domestic production references** | Neutral | Neutral; some China community |
| C7 Ops cost | Highest of the four — a real Ceph skill footprint | Moderate | Lowest | Low |

### 3.3 The decision logic

**Ceph RGW wins on the criteria that are non-negotiable for this product** — S3 API completeness (our versioning and retention features depend on it), native Vault-backed SSE-KMS (the per-tenant key story from v1.1 maps almost one-to-one), foundation governance, and a decade-plus durability track record. Its weakness is operational weight, which is real and addressed in §5.

**CubeFS is the mainland-cell answer waiting for the mainland cell.** CNCF-graduated, Apache-2.0, born at JD.com, with Chinese enterprise production references — for a mainland deployment, "domestically proven storage with CNCF governance" is precisely the procurement story. Keeping the same **Room SPI abstraction** (from the multi-tenant addendum) means the cell can run CubeFS without touching application code; the SPI insulates us again.

**Garage is deliberately *not* the primary.** It lacks object versioning (a baseline need for retention/legal-hold) and SSE-KMS. But it is the best fit for a future single-box on-prem appliance tier (Tier 3 lite), where a 1-GB-footprint single binary with real durability testing beats running mons and OSDs on one host.

**SeaweedFS loses on bus factor**, not on technology — a single-maintainer core dependency is the same class of risk we are fleeing in MinIO, and its SSE implementation is too young for bank audit.

---

## 4. What Changes in the Architecture (v1.1 → v1.2 deltas)

### 4.1 Encryption model — envelope encryption becomes primary

v1.1 leaned on MinIO SSE with tenant-scoped keys via KES. With Ceph, we keep server-side encryption but **demote it**: SSE-KMS (Vault backend) is the at-rest baseline, while the **application-layer envelope encryption** in the Room Service (per-document DEK, wrapped by the tenant KEK in Vault) becomes the *primary* tenant-key isolation control.

This ordering is actually stronger for the sales narrative: tenant isolation now holds **even if the storage cluster is fully compromised**, because operators still cannot read objects without the Vault-held KEK. It also makes the storage backend swappable (Garage's SSE-C-only stance, CubeFS's own KMS) without changing the tenancy story — the SPI absorbs backend differences.

### 4.2 Per-tenant isolation mapping

| v1.1 (MinIO) | v1.2 (Ceph RGW) |
|---|---|
| Bucket per tenant | **RGW user + bucket per tenant**, provisioned by the Tenant Provisioner via Rook's ObjectBucketClaims / `radosgw-admin` |
| Per-tenant KMS key via KES | Per-tenant Vault KEK (envelope, primary) + tenant-scoped SSE-KMS keys (secondary) |
| MinIO operator | **Rook operator** (CephObjectStore / ObjectBucketClaim) |
| Erasure-coding in MinIO | Ceph replication size 3 (or EC profile at scale) |

### 4.3 Deployment topology impact

- **Shared SaaS cell (Tier 1) & dedicated (Tier 2):** Rook-Ceph cluster per cell — 3+ mons, OSDs on dedicated storage nodes. Rook is CNCF-graduated with a well-documented RGW path (`CephObjectStore`), which keeps the K8s story intact.
- **Internal build (Tier 0):** same stack — and see §5 for how to keep Phase 0–2 honest about Ceph's weight.
- **On-prem (Tier 3):** default to Rook-Ceph where the customer has ≥3 nodes; Garage single-binary profile for appliance-style deployments. Both behind the same SPI, so the choice is a values-file flip, not a port.
- **Mainland cell (future):** CubeFS profile, same SPI.

### 4.4 Things v1.1 gets to keep unchanged

The Room SPI, Postgres RLS tenancy, Keycloak realms, NATS namespaces, OpenSearch per-tenant indexes, ClickHouse partitioning, control plane, metering, license server — all storage-agnostic already, which is exactly why this replacement is contained. Nextcloud itself also supports S3-compatible external storage, so the NextcloudAdapter deployments point at RGW.

---

## 5. Managing Ceph's Operational Cost (the honest section)

Ceph is the right call technically and the heaviest call operationally. Accept and mitigate:

1. **Run RGW-only roles mentally separate.** We consume Ceph as an S3 service. Avoid the temptation to also run CephFS/RBD until a use case forces it; every additional Ceph surface is more upgrade surface.
2. **Rook handles day-2.** Upgrades (mgrs → mons → OSDs), cluster CRDs, and ObjectBucketClaims are operator-driven. Budget a "storage guild" rotation within the platform team; Ceph skills are findable in HK (Red Hat ecosystem) unlike niche alternatives.
3. **Hardware floor:** 3 mons on separate nodes; RGW ≥2 replicas behind Traefik; OSDs sized for replication-3. For the internal Phase 0 build, a compact 3-node Rook cluster with 6 OSDs is a known workable pattern.
4. **Version pinning + staged upgrades** in the cell stamping pipeline (same discipline as the rest of the chart).
5. **Escape hatches stay open by design:** the SPI + S3 API mean a future migration (say, to CubeFS or Garage v3 with versioning) is a data-copy exercise, not a re-architecture. This document should be revisited if Garage adds versioning + SSE-KMS or if a Ceph Foundation issue emerges.

---

## 6. Recommendation Summary

| Deployment | Storage | Rationale |
|---|---|---|
| Tier 0 internal build | **Ceph RGW via Rook** | Foundation governance, full S3, Vault SSE-KMS; proves the commercial stack internally |
| Tier 1 shared SaaS | **Ceph RGW via Rook** | Same; PB headroom for cell growth |
| Tier 2 dedicated | **Ceph RGW via Rook** (customer-managed keys where required) | Mature audit story for bank procurement |
| Tier 3 on-prem | **Ceph RGW** (multi-node) or **Garage** (single-box appliance profile) | Match storage to customer hardware reality |
| Mainland China cell | **CubeFS** | CNCF-graduated, JD.com lineage, domestic production references, PIPL-friendly story |

**The unifying principle:** the storage layer is a pluggable S3 backend behind the Room SPI. We pick the most boring, best-governed option per market and never let storage choice leak into application architecture again — that discipline is what made this MinIO fire-drill a contained change instead of an existential one.

---

## Sources

- [LWN — A look at MinIO alternatives: Ceph and Garage](https://lwn.net/Articles/1077739/) (MinIO maintenance-mode Dec 2025, archival 2026, Silo fork status, Ceph/Garage deep comparison)
- [Reddit r/selfhosted — Avoid MinIO: developers introduce trojan horse update](https://www.reddit.com/r/selfhosted/comments/1kva3pw/avoid_minio_developers_introduce_trojan_horse/) (console stripping, community backlash)
- [Onidel — MinIO vs Ceph RGW vs SeaweedFS vs Garage in 2025](https://onidel.com/blog/minio-ceph-seaweedfs-garage-2025)
- [Rilavek — Self-Hosted S3 Storage in 2026: RustFS, SeaweedFS, Garage, Ceph](https://rilavek.com/resources/self-hosted-s3-compatible-object-storage-2026)
- [Rook docs — Object Storage (RGW)](https://rook.io/docs/rook/v1.15/Storage-Configuration/Object-Storage-RGW/object-storage/) (3-OSD minimum, vhost-style addressing)
- [OneUptime — How to Configure Ceph for Small (3-Node) Clusters](https://oneuptime.com/blog/post/2026-03-31-rook-configure-small-3-node-clusters/view)
- [Ceph docs — RGW encryption (SSE-S3/SSE-KMS)](https://docs.ceph.com/en/latest/radosgw/encryption/) and [Ceph RGW Vault integration](https://shanaceph.readthedocs.io/en/latest/radosgw/vault/)
- [Garage — Encryption cookbook (SSE-C only)](https://garagehq.deuxfleurs.fr/documentation/cookbook/encryption/) and [Garage S3 compatibility](https://garagehq.deuxfleurs.fr/documentation/reference-manual/s3-compatibility/) (no versioning; no SSE-KMS by design)
- [SeaweedFS — SSE-C/KMS discussions](https://github.com/seaweedfs/seaweedfs/discussions/5361) and [KMS bug #7499](https://github.com/seaweedfs/seaweedfs/issues/7499)
- [CNCF — CubeFS graduation announcement](https://www.prnewswire.com/news-releases/cloud-native-computing-foundation-announces-cubefs-graduation-302356177.html), [CNCF CubeFS project page](https://www.cncf.io/projects/cubefs/), [OPPO case study](https://www.cncf.io/case-studies/oppo/), [InfoQ graduation coverage](https://www.infoq.com/news/2025/03/cubefs-cncf-graduation/)
- [iomete — Evaluating S3-Compatible Object Storage (2026)](https://iomete.com/resources/blog/evaluating-s3-compatible-storage-for-lakehouse)
