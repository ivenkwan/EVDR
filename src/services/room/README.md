# Room Service (Go) — REST over the frozen Room SPI

Wave 3 vertical slice (TR-10.1). A small HTTP JSON service that exposes
portal/admin room operations over the injected `spi.RoomSPI` (contract
v0.1.0, frozen). It is a thin, honest translation layer: **HTTP → service
layer → SPI**. No storage backend (Nextcloud, Ceph, PostgreSQL) is ever
touched directly (AGENTS.md §6).

## Layout

```
src/services/room/
├── cmd/roomd/main.go        # wiring: adapter selection, env, graceful shutdown
├── doc.go                   # package overview + design posture
├── server.go                # Server, Config, routes, middleware, helpers
├── auth.go                  # Authenticator seam + DevAuthenticator (dev only)
├── events.go                # EventSink seam + slog implementation (TR-7.1 later)
├── catalog.go               # per-tenant projection of SPI-confirmed records
├── dto.go                   # JSON wire types + conversions + request validation
├── errors.go                # SPI sentinel → HTTP status/code mapping
├── rooms.go                 # create/list/get/seal/retention/export handlers
├── grants.go                # grant create/revoke/list handlers
├── documents.go             # upload (multipart), list, get, versions handlers
├── public.go                # public room branding (portal surface)
└── integration_test.go      # httptest suite over the memadapter-backed service
```

## Running

```bash
cd src/services/room/cmd/roomd
EVDR_ROOM_SERVICE_DEV_SECRET=change-me go run .          # listens on :8080

EVDR_ROOM_ADDR=:8080 \
EVDR_ROOM_SERVICE_DEV_SECRET=<secret> \                 # required — never defaulted
EVDR_ROOM_ADAPTER=mem \                                  # mem (dev) | nextcloud
EVDR_ROOM_SERVICE_DEV_TENANT=dev \
EVDR_ROOM_MAX_UPLOAD_BYTES=104857600 \
go run .
```

Nextcloud adapter selection (`EVDR_ROOM_ADAPTER=nextcloud`) additionally
reads `EVDR_NEXTCLOUD_BASE_URL`, `EVDR_NEXTCLOUD_USERNAME`,
`EVDR_NEXTCLOUD_APP_PASSWORD` (see `nextcloud.Config`). The adapter binds one
tenant at construction — the dev principal's tenant must equal it or every
SPI call fails with the SPI's own sentinels (see "Wrong-tenant behaviour").

### Tests

```bash
go test ./src/services/room/... -race -count=1   # integration suite (httptest + memadapter)
go test ./src/spi/... -count=1                   # SPI regression gate (never regress the contract)
```

The test suite covers: auth (401/403), room lifecycle (create/list/get,
slug collision, validation), **wrong-tenant probes surfacing the SPI's own
errors**, multipart upload + immutable versioning (FR-2.2), upload size
limits and validation (FR-2.1), grant lifecycle + SPI `invalid_grant`
semantics (FR-4.4), seal freezing mutations (FR-1.6), retention floors
(FR-5.5 hook), streaming tar export (FR-1.7/SR-5.2), public branding leak
check (FR-1.1), and event emission through the EventSink seam.

## HTTP API

All endpoints are under `/api/v1`. Errors are JSON envelopes with a stable
machine-readable `code`:

```json
{ "code": "room_not_found", "message": "spi: room not found", "requestId": "…" }
```

| Method + path | SPI call | Notes |
|---|---|---|
| `GET /healthz` | — | open liveness |
| `POST /api/v1/rooms` | `CreateRoom` | name/slug required; branding, classification, retention optional |
| `GET /api/v1/rooms` | — (projection) | room list, creation order |
| `GET /api/v1/rooms/{roomID}` | — (projection) | room record |
| `POST /api/v1/rooms/{roomID}/seal` | `SealRoom` | legal hold (FR-1.6); idempotent |
| `POST /api/v1/rooms/{roomID}/retention` | `ApplyRetention` | policy floors enforced by the SPI |
| `POST /api/v1/rooms/{roomID}/export` | `ExportRoom` | **streams** `application/x-tar` (FR-1.7/SR-5.2) |
| `GET /api/v1/rooms/{roomID}/grants` | — (projection) | active grants |
| `POST /api/v1/rooms/{roomID}/grants` | `GrantAccess` | guest grants must carry an expiry (SPI-enforced) |
| `DELETE /api/v1/rooms/{roomID}/grants/{grantID}` | `RevokeAccess` | idempotent-safe (FR-4.4) |
| `GET /api/v1/rooms/{roomID}/documents` | — (projection) | document list with current version (FR-2.2) |
| `POST /api/v1/rooms/{roomID}/documents` | `PutDocument` | `multipart/form-data`: `file`, optional `name`, `folderPath` (FR-2.3), `classification` |
| `GET /api/v1/rooms/{roomID}/documents/{documentID}` | — (projection) | document record |
| `GET /api/v1/rooms/{roomID}/documents/{documentID}/versions` | `ListVersions` | **live SPI read** — immutable versions ascending (FR-2.2) |
| `GET /api/v1/public/rooms/{roomID}/branding` | — (projection) | portal branding only (FR-1.1); no management data |

Room-scoped ops whose room is unknown (or belongs to another tenant) answer
`404 room_not_found`; create with a mismatched tenant answers
`403 access_denied` — the SPI's own error mapping, never "exists but denied".

`GET /render`-style endpoints are deliberately absent: viewing/rendering
(`GetRenderStream`, watermarking) is owned by the viewer pipeline service
(TR-4.x) in a parallel workstream.

### HTTP error codes (stable)

| Code | HTTP | SPI sentinel |
|---|---|---|
| `unauthenticated` | 401 | auth layer |
| `access_denied` | 403 | `ErrAccessDenied` (incl. wrong-tenant create) |
| `bad_request` / `invalid_grant` | 400 | validation / `ErrInvalidGrant` |
| `payload_too_large` | 413 | upload ceiling |
| `room_not_found` / `document_not_found` / `grant_not_found` | 404 | SPI not-found sentinels (incl. wrong-tenant scoped ops) |
| `room_exists` | 409 | `ErrRoomExists` |
| `room_sealed` / `room_closed` / `retention_violation` | 409 | SPI sentinels |
| `quota_exceeded` | 507 | `ErrQuotaExceeded` |
| `unsupported` | 501 | `ErrUnsupported` |

## Auth model — dev only

Requests authenticate with `Authorization: Bearer <shared secret>`
(`EVDR_ROOM_SERVICE_DEV_SECRET`, compared constant-time). The dev
implementation maps **every** authenticated request to ONE configured
principal (user + tenant from environment) — client-supplied identity is
ignored by construction, and `tenant_id` is never read from request
parameters (SR-2.2, AGENTS.md hard rule).

> **Dev-only caveat:** there is no per-user identity yet — every request acts
> as the same internal user. Production replaces `DevAuthenticator` with
> Keycloak OIDC/JWT validation behind the same `Authenticator` interface
> (identity integration is a later wave, TR-6.x). The frontend BFFs hold the
> secret server-side; it must never be exposed to browsers or logs.

## Projection catalog — why it exists and its limits

The frozen SPI has **no enumeration or read-back methods** (no
ListRooms/ListDocuments/ListGrants/GetRoom). Rooms, documents and grants are
returned only by mutating calls. So that list pages can render, the service
keeps a per-tenant **projection** of what the SPI confirmed on calls this
service made (create receipts, upload receipts, grant records, seal
receipts). Nothing is fabricated; version history and every mutation are
still answered live by the SPI. Wrong-tenant lookups hit an empty catalog →
the same not-found codes the SPI would return.

Honest limitations:
- The projection is **in-memory** (dev slice). It matches the platform's
  single-writer-per-cell deployment assumption and the memadapter's own
  volatility; a durable projection arrives with the control-plane metadata
  store (later wave).
- Records created by other processes/services are not visible to this
  instance until a matching SPI mutation flows through it.

## Audit seam

Every decision (success or failure) is emitted via the injected `EventSink`
as `{tenant_id, actor, action, target, result, …}` — e.g. `room.create`,
`doc.upload`, `doc.versions`, `grant.revoke`, `branding.public`. The shipped
`SlogEventSink` writes structured logs (slog); NATS JetStream →
ClickHouse/append-only-PostgreSQL wiring (TR-7.1) plugs in behind the same
interface without code changes here.

## Operational notes

- Uploads are capped by `MaxUploadBytes` (default 100 MiB,
  `EVDR_ROOM_MAX_UPLOAD_BYTES`); parts spool to disk beyond 8 MiB. Size/type
  allow-list checks also run client-side in the portal/admin BFFs (FR-2.1).
- Malware scanning and MIME sniffing are upstream validation (SPI contract
  language) and land with the document pipeline wave.
- Structured logging only (slog, JSON), no `print()`; requests carry an
  `X-Request-ID` echoed in responses, logs and events.
