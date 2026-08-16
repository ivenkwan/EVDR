// Package spi defines the Room SPI — the storage-abstraction contract that
// every EVDR upstream service (portal, secure viewer, policy engine, AI
// services) programs against, and that storage adapters implement.
//
// Contract version: v0.1 (frozen for Phase 1 implementation).
//
// Contract rules:
//
//  1. Upstream services MUST NOT access Nextcloud, Ceph RGW/S3, or any
//     storage backend directly. All document and room operations go through
//     RoomSPI.
//  2. Tenant identity is NEVER taken from client-supplied parameters.
//     TenantContext is constructed by the calling service's authentication
//     layer from verified JWT/session claims (SR-2.2).
//  3. The interface is frozen at v0.1 for Phase 1. Any change after freeze
//     requires an ADR, a contract version bump, simultaneous updates to both
//     adapters, and a green conformance suite (TR-2.4) on every merge.
//  4. GetRenderStream returns rendered pages one at a time. Implementations
//     MUST NOT expose a full-document stream through this method
//     (FR-3.1/TR-4.1). Bulk export exists only as ExportRoom, which is an
//     audited, policy-gated operation.
//
// This package is listed in CLAUDE.md §13 as a protected path.
package spi
