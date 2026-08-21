# Room SPI — Contract v0.1 (FROZEN for Phase 1)

The Room SPI is the single storage-abstraction contract for EVDR (TR-2.1). Every upstream service — portal, secure viewer, policy engine, AI services — programs against `RoomSPI`. No service touches Nextcloud, Ceph RGW/S3, PostgreSQL, or any storage backend directly (hard rule, `AGENTS.md` §6).

| Adapter | Tiers | Backend | Phase |
|---|---|---|---|
| NextcloudAdapter (TR-2.2) | 0, 2, 3 | Nextcloud OCS/WebDAV | P1 |
| NativeAdapter (TR-2.3) | 1 | Ceph RGW (S3) + PostgreSQL metadata + Redis | P2.5 |

Both adapters must pass the shared **SPI conformance suite** (TR-2.4, built in P1 at `src/spi/conformance/`) in CI on every merge. A red suite blocks the merge.

## Files

| File | Contents |
|---|---|
| `interface.go` | `RoomSPI` interface, `ContractVersion` |
| `types.go` | All request/response/value types, enums, `RenderStream` |
| `errors.go` | Sentinel errors (match with `errors.Is`) |
| `doc.go` | Contract rules |

## Semantics and invariants

### Tenancy (non-negotiable)

- `TenantContext` is constructed **only** by the calling service's auth layer from verified JWT/session claims. Tenant identity is never read from client-supplied parameters (SR-2.2; `AGENTS.md` hard rule).
- Adapters scope every storage operation to `TenantContext.TenantID`. A call whose context tenant does not match the resource's tenant must behave as *not found* (`ErrRoomNotFound`/`ErrDocumentNotFound`), never as *exists but denied*, to avoid cross-tenant existence oracles.

### Method contracts

| Method | Key invariants |
|---|---|
| `CreateRoom` | Atomic provisioning of storage + metadata + retention record. Slug collision → `ErrRoomExists`. Zero-value classification resolves to `DefaultClassification` (CONFIDENTIAL). |
| `GrantAccess` | Guest grants (`ActorGuest`) **must** carry `Constraints.NotAfter`; violation → `ErrInvalidGrant`. Returns adapter-assigned `GrantID`. Sealed room → `ErrRoomSealed`. |
| `RevokeAccess` | Takes effect immediately (FR-4.4) — an in-flight render may complete, but no new stream may open. Idempotent-safe: re-revoking returns nil; unknown grant → `ErrGrantNotFound`. |
| `PutDocument` | Streaming; implementations must not buffer whole payloads in memory (multi-GB files, FR-2.5). Same `Name`+`FolderPath` creates a new immutable version, never overwrites. Sealed room → `ErrRoomSealed`. |
| `GetRenderStream` | **View-scoped only.** Returns rendered pages one at a time with the watermark baked in server-side; never a whole-document payload (FR-3.1, TR-4.1). Download semantics live in the policy engine upstream, not here. |
| `ListVersions` | All versions, ascending `Number`. Unknown document → `ErrDocumentNotFound`. |
| `ApplyRetention` | Adapters reject a policy that shortens below the stored floor → `ErrRetentionViolation` (defence in depth behind the policy engine). Sealed room → `ErrRoomSealed`. |
| `ExportRoom` | The **only** bulk-read operation. Package = streamed archive + per-entry SHA-256 manifest + integrity letter (FR-1.7, SR-5.2). Permitted on sealed rooms (eDiscovery); must reflect frozen state. Intended to be policy-gated and audited upstream. |
| `SealRoom` | Legal hold (FR-1.6): room, documents, versions, and metadata become immutable; all mutations → `ErrRoomSealed`; reads and export continue. Idempotent: re-sealing returns the existing `SealReceipt`. |

### Error taxonomy

Adapters translate backend failures into the sentinels in `errors.go` (wrapping with `%w` is encouraged). Upstream services must not switch on backend-specific error types — that would re-couple them to the storage implementation.

### Concurrency and context

- Implementations must be safe for concurrent use.
- Every method takes `context.Context` first and must honour cancellation and deadlines; streams must unblock `Next()` on context cancellation and surface it via `Err()`.

## Versioning policy

- `ContractVersion` follows SemVer. **v0.1 is frozen** for Phase 1 implementation.
- Post-freeze changes require: an ADR, a version bump, simultaneous updates to both adapters, and a green conformance suite — in the same merge.
- Additive changes (new method/field) = minor bump. Semantics changes = major bump and an explicit migration note for stored data.
- This path is protected (`AGENTS.md` §13): PRs touching `src/spi/interface.go` require platform-lead review.

## What the SPI deliberately does not contain

- **Policy decisions** (watermark mandatory? download allowed?) — the Phase 2 policy engine decides upstream; the SPI executes.
- **Audit emission** — `tenant_id`-carrying audit events are emitted by upstream services/policy engine onto NATS (TR-7.x), not by adapters. Adapters must not create silent data paths, and must not be relied on as the audit source.
- **Rendering/conversion** — Office→PDF and page rasterisation are viewer-pipeline services (TR-4.x); the SPI transports already-rendered pages.
