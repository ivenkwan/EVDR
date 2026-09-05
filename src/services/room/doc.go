// Package room implements the EVDR Room Service (Wave 3, TR-10.1): a small
// HTTP JSON service that exposes portal/admin room operations over the frozen
// Room SPI (src/spi, contract v0.1.0 — TR-2.1). Every storage operation is
// delegated to the injected spi.RoomSPI adapter; this package never talks to
// Nextcloud, S3, or any backend directly (AGENTS.md §6 hard rule).
//
// # Tenancy
//
// The service mirrors the SPI's tenant-bound posture: the authenticated
// principal (resolved by the injected Authenticator from the request — never
// from client-supplied parameters, SR-2.2) is the only source of tenant
// identity. The SPI adapter enforces tenant scope on every call and returns
// its own sentinels (not-found for room/document-scoped mismatches, access
// denied on create) so cross-tenant probes are indistinguishable from plain
// misses. Wrong-tenant requests therefore surface as the SPI's errors —
// mapped to HTTP by this package — never as "exists but denied".
//
// Because the frozen SPI is an action contract with no enumeration methods
// (no ListRooms/ListDocuments/ListGrants/GetRoom), the service keeps a
// per-tenant projection ("catalog") of the records its own SPI calls
// returned: room records, upload receipts, grant records and seal receipts.
// The catalog is written only from successful SPI responses — there are no
// fabricated payloads — and is scoped by the principal's tenant. Version
// history and every mutation re-validate against the SPI live. Limitations
// of the projection (single-writer per cell, in-memory until the control
// plane metadata store lands) are documented in the package README.
//
// # Auth seam
//
// Authenticator is the seam where production Keycloak OIDC/JWT validation
// will plug in (identity integration is a later wave, TR-6.x). The shipped
// DevAuthenticator validates a shared secret (Authorization: Bearer, read
// from EVDR_ROOM_SERVICE_DEV_SECRET — never hardcoded, never logged) and
// maps every authenticated request to one configured dev principal. It is
// for development and integration tests only.
//
// # Audit seam
//
// Every decision is emitted through the injected EventSink as a structured
// event carrying tenant, actor, action, target and result. The shipped
// implementation logs via log/slog; NATS JetStream wiring is a later wave
// (TR-7.1) and plugs in behind the same interface.
package room
