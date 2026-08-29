# Room SPI — Contract v0.1 (FROZEN for Phase 1)

The Room SPI is the single storage-abstraction contract for EVDR (TR-2.1). Every upstream service — portal, secure viewer, policy engine, AI services — programs against `RoomSPI`. No service touches Nextcloud, Ceph RGW/S3, PostgreSQL, or any storage backend directly (hard rule, `AGENTS.md` §6).

| Adapter | Tiers | Backend | Phase |
|---|---|---|---|
| NextcloudAdapter (TR-2.2) | 0, 2, 3 | Nextcloud OCS/WebDAV | P1 |
| NativeAdapter (TR-2.3) | 1 | Ceph RGW (S3) + PostgreSQL metadata + Redis | P2.5 |

Both adapters must pass the shared **SPI conformance suite** (TR-2.4, at `src/spi/conformance/`) in CI on every merge. A red suite blocks the merge.

## Files

| File | Contents |
|---|---|
| `interface.go` | `RoomSPI` interface, `ContractVersion` |
| `types.go` | All request/response/value types, enums, `RenderStream` |
| `errors.go` | Sentinel errors (match with `errors.Is`) |
| `doc.go` | Contract rules |
| `conformance/conformance.go` | **Shared conformance suite** (`RunSuite`) — runs the full contract against any adapter (TR-2.4) |
| `conformance/memadapter/` | In-memory reference adapter — self-verifies the suite and serves as a template for new adapters (test double, not production) |
| `adapters/nextcloud/` | **NextcloudAdapter** — `RoomSPI` over Nextcloud OCS/WebDAV (TR-2.2) |

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

---

## NextcloudAdapter design (`adapters/nextcloud`)

The adapter wraps Nextcloud's **OCS sharing API** (grant lifecycle) and **WebDAV file API** (rooms, documents, versions, export) behind `RoomSPI` (TR-2.2).

### Tenant binding

TR-2.2 deploys **one Nextcloud instance per cell/tenant**, so the adapter is bound to exactly one tenant at construction (`Config.TenantID`). A `TenantContext` that does not match is rejected:

- `CreateRoom` → `ErrAccessDenied`
- room-scoped operations (`GrantAccess`, `RevokeAccess`, `PutDocument`, `ApplyRetention`, `SealRoom`, `ExportRoom`) → `ErrRoomNotFound`
- document-scoped operations (`GetRenderStream`, `ListVersions`) → `ErrDocumentNotFound`

This makes cross-tenant probes indistinguishable from plain misses (no existence oracle).

### Storage layout (WebDAV)

Everything lives under the service account's namespace `/remote.php/dav/files/<service-user>/evdr/`:

```
_evdr/rooms.json                       RoomID → slug index
<slug>/                                room folder (OCS share root)
  docs/<folderPath>/<name>             current version of a document (live pointer)
  _evdr/room.json                      room record (state, retention, branding)
  _evdr/grants.json                    grant ledger (incl. OCS share ids)
  _evdr/seal.json                      seal receipt, present once sealed
  _evdr/docs.json                      normalized name+folder → document id index
  _evdr/docs/<docID>.json              per-document record (immutable version list)
  _evdr/versions/<docID>/v<N>          immutable version content (WebDAV COPY)
```

**Versioning:** every `PutDocument` PUTs the streamed content to the live path (SHA-256 computed while streaming, never buffering the payload) and then WebDAV-COPYs it to an immutable archive `v<N>`. All reads (`GetRenderStream`, `ListVersions`, `ExportRoom`) use the archives, so exports are consistent snapshots. Document ids are self-describing `<roomID>@<uuid>`; version ids are `<docID>@<number>`.

### Method → backend mapping

| SPI method | Backend calls |
|---|---|
| `CreateRoom` | MKCOL room folder (+ `_evdr`, `docs`, `_evdr/docs`), PUT `room.json`/`grants.json`, update `rooms.json` index. Best-effort DELETE rollback on failure. |
| `GrantAccess` | Append to `grants.json` + OCS `POST /ocs/v2.php/apps/files_sharing/api/v1/shares` |
| `RevokeAccess` | Mark revoked in `grants.json` + OCS `DELETE .../shares/{id}` (OCS 404 = already gone, still nil) |
| `PutDocument` | MKCOL parents, WebDAV PUT live path, WebDAV COPY → `_evdr/versions/<docID>/v<N>`, update doc record + index |
| `GetRenderStream` | Access check → WebDAV GET the immutable version archive → delegate rendering to the configured `Renderer` |
| `ListVersions` | Access check → read doc record |
| `ApplyRetention` | Update `room.json` retention after floor check |
| `SealRoom` | Write `seal.json` (idempotent), set `room.state = sealed` |
| `ExportRoom` | Two-pass: hash every archive entry via WebDAV GET, then stream a tar (archive + `manifest.json` + `INTEGRITY_LETTER.txt`) through an `io.Pipe` |

### OCS share mapping (grant tiers)

Nextcloud OCS permission bits: 1=read, 2=update, 4=create, 8=delete, 16=share.

| SPI tier | OCS share type | OCS permissions |
|---|---|---|
| `TierViewOnly` | 0 (user) / 3 (public link for guests) | 1 (read) |
| `TierDownloadAllowed` | 0 | 1 (read) |
| `TierPrintAllowed` | 0 | 1 (read) |
| `TierEditAllowed` | 0 | 15 (read+update+create+delete, **no share**) |

**Documented limitation:** OCS has no download/print permission bit, so the three read tiers map identically. Download/print enforcement happens in the viewer pipeline (the SPI only ever transports rendered pages, FR-3.1) and in the upstream policy engine — not in share bits. Guest grants map to expiring public links: `expireDate` mirrors `Constraints.NotAfter` (FR-4.3 — guests have no account).

### Rendering (GetRenderStream)

Rendering — Office→PDF conversion, page rasterisation, server-side watermark baking — is a viewer-pipeline concern (TR-4.x), **not** a storage-adapter concern. The adapter:

1. enforces the actor's grant (creator or active, time-valid grant on the room; revoked/expired/not-yet-valid grants → `ErrAccessDenied`),
2. streams the immutable version archive over WebDAV,
3. hands it to the configured `Renderer` and returns the resulting `RenderStream` wrapped so `Next()` unblocks on context cancellation and `Err()` surfaces it.

With no `Renderer` configured, `GetRenderStream` returns `ErrUnsupported` with a wiring hint. Tests inject a fake renderer; production wires the real viewer pipeline at construction. Grant constraints on CIDRs/domains are enforced upstream by the policy engine (it owns client network context).

### Access control on reads

`GetRenderStream`, `ListVersions` and `ExportRoom` all require the actor to be the room creator or hold an active, time-valid grant. This is adapter-level defence in depth; the policy engine still performs the fine-grained decisions upstream (SPI README §"what the SPI deliberately does not contain").

### Sealing semantics (documented interpretations)

- **Granting access on a sealed room is forbidden** (`ErrRoomSealed`, explicit in the contract).
- **Revoking access on a sealed room is permitted.** Legal hold freezes documents, versions, and metadata — it must never prevent withdrawing access to them. This is a deliberate, documented interpretation; the conformance suite enforces it.
- Re-sealing returns the stored receipt (idempotent).
- `SealReceipt.FrozenObjects` = documents + versions frozen at seal time.

### Retention

`ApplyRetention` rejects any policy whose `MinRetentionDays` is below the currently stored floor with `ErrRetentionViolation` — defence in depth behind the policy engine, which validates floors upstream. The initial policy is set at `CreateRoom`.

### Export integrity

`ExportRoom` is the only bulk-read operation (FR-1.7, SR-5.2). The package is a streamed tar with:

- every object bound to a **SHA-256 digest + size** in `manifest.json` and in the returned `ExportManifest`,
- a human-readable `INTEGRITY_LETTER.txt` embedding all digests so a third party can verify the package offline,
- exports of sealed rooms permitted (eDiscovery) and reflecting the frozen state; `IncludeAuditTrail` adds `audit/grants.json` and `audit/seal.json` (the adapter's native audit state — the platform's activity audit trail lives in the append-only PostgreSQL store, merged upstream by the exporter service).

### Concurrency and known limitations

- Safe for concurrent use within one process: per-room mutexes serialise ledger read-modify-write.
- **Cross-process writes are not serialised** (Nextcloud offers no compare-and-swap on plain files). The deployment model is one Room Service process per cell, matching the one-instance-per-tenant topology. This is the single-writer assumption.
- All OCS/WebDAV calls use HTTP Basic auth with a service-account app password; TLS is enforced by the deployment (never disable certificate verification).
- `ExportRoom` reads every included version twice (hash pass + tar pass). Versions are immutable, so the snapshot is consistent; the cost is acceptable for an audited, policy-gated bulk operation.

---

## SPI conformance suite (`conformance/`) and CI

`conformance.RunSuite(t, harness)` executes the **full contract** — room lifecycle, grant lifecycle and error taxonomy, immutable versioning, view-scoped rendering (incl. cancellation), retention floors, sealing, export integrity, and tenant isolation — against **any** adapter. It speaks only `spi.RoomSPI` and the sentinels, never a backend API.

- Each subtest runs against a **fresh adapter instance and fresh backend** (no cross-case state).
- A `Harness` is a few lines of test wiring per adapter: construct the adapter (with its simulated backend), return it plus its bound tenant. See `adapters/nextcloud/nextcloud_test.go` (`ncHarness`) and `conformance/conformance_test.go` (`memHarness`).
- The in-memory reference adapter (`conformance/memadapter/`) runs the same suite, proving it is adapter-agnostic and guarding against Nextcloud-specific assumptions leaking into the tests.
- Unit tests in `adapters/nextcloud/` cover the backend mapping itself (request shapes, OCS forms, WebDAV methods, sentinel translation) with `net/http/httptest` mock servers — no real Nextcloud needed.

### Running locally

```bash
# From the repo root (Go 1.25+; this repo's toolchain at ~/.local/go/bin):
go build ./...
go test ./... -race
go vet ./...

# Just the SPI packages, verbosely:
go test ./src/spi/... -v
```

### Wiring into CI

The suite is the executable definition of TR-2.4: run it against **every** adapter on **every** merge and block the merge on red:

```yaml
# .gitlab-ci.yml — add to the existing pipeline (self-hosted runner)
spi-conformance:
  stage: test
  script:
    - go build ./...
    - go test ./src/spi/... -race -v
    - go vet ./...
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
```

When NativeAdapter lands (Phase 2.5), it adds its own harness and the same command covers both adapters automatically — the suite is shared by construction. A change that breaks one adapter turns CI red and blocks the merge (AGENTS.md §9: "SPI conformance is a merge gate").

## Versioning policy

- `ContractVersion` follows SemVer. **v0.1 is frozen** for Phase 1 implementation.
- Post-freeze changes require: an ADR, a version bump, simultaneous updates to both adapters, and a green conformance suite — in the same merge.
- Additive changes (new method/field) = minor bump. Semantics changes = major bump and an explicit migration note for stored data.
- This path is protected (`AGENTS.md` §13): PRs touching `src/spi/interface.go` require platform-lead review.

## What the SPI deliberately does not contain

- **Policy decisions** (watermark mandatory? download allowed?) — the Phase 2 policy engine decides upstream; the SPI executes.
- **Audit emission** — `tenant_id`-carrying audit events are emitted by upstream services/policy engine onto NATS (TR-7.x), not by adapters. Adapters must not create silent data paths, and must not be relied on as the audit source.
- **Rendering/conversion** — Office→PDF and page rasterisation are viewer-pipeline services (TR-4.x); the SPI transports already-rendered pages via the adapter's injected `Renderer`.
