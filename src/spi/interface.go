package spi

import "context"

// ContractVersion is the frozen Room SPI contract version. v0.1 is frozen for
// Phase 1 implementation; changes after freeze follow the process in doc.go.
const ContractVersion = "0.1.0"

// RoomSPI is the storage-abstraction contract for EVDR (TR-2.1). All upstream
// services program against this interface; storage backends are reached only
// through adapters (NextcloudAdapter for Tiers 0/2/3, NativeAdapter for
// Tier 1). Both adapters must pass the shared conformance suite in CI
// (TR-2.4).
//
// Implementations must be safe for concurrent use.
type RoomSPI interface {
	// CreateRoom provisions a new room within the tenant's scope: storage
	// location, metadata record, and initial retention policy. The returned
	// Room is the persisted record. Slug collision returns ErrRoomExists.
	CreateRoom(ctx context.Context, tenant TenantContext, req CreateRoomRequest) (Room, error)

	// GrantAccess authorises a subject on a room at a permission tier. The
	// returned grant carries the adapter-assigned GrantID. Grants to guests
	// must carry Constraints.NotAfter; a zero NotAfter for ActorGuest returns
	// ErrInvalidGrant. Granting on a sealed room returns ErrRoomSealed.
	GrantAccess(ctx context.Context, tenant TenantContext, room RoomID, grant AccessGrant) (AccessGrant, error)

	// RevokeAccess withdraws a grant immediately (FR-4.4). Revocation of an
	// unknown grant returns ErrGrantNotFound. Revoking the last grant does
	// not close the room. The operation is idempotent-safe: revoking an
	// already-revoked grant returns nil.
	RevokeAccess(ctx context.Context, tenant TenantContext, room RoomID, grant GrantID) error

	// PutDocument streams a new document (or a new version of an existing
	// name at the same FolderPath) into a room, assigning Version 1 (or
	// number N+1) immutably (FR-2.2). The caller is responsible for upstream
	// validation (size, type, malware scan); the adapter persists, indexes,
	// and versions. A sealed room returns ErrRoomSealed.
	PutDocument(ctx context.Context, tenant TenantContext, room RoomID, doc PutDocumentRequest) (Document, error)

	// GetRenderStream returns a forward-only stream of rendered pages for
	// one document version, with the watermark baked in server-side
	// (FR-3.1/3.2). It never returns a whole-document payload. Access is
	// denied (ErrAccessDenied) when the actor holds no grant covering the
	// document, and any download-tier semantics are enforced upstream by the
	// policy engine — this method is always view-scoped.
	GetRenderStream(ctx context.Context, tenant TenantContext, doc DocumentID, req RenderRequest) (RenderStream, error)

	// ListVersions returns all immutable versions of a document, ordered by
	// Version.Number ascending. An unknown document returns
	// ErrDocumentNotFound.
	ListVersions(ctx context.Context, tenant TenantContext, doc DocumentID) ([]Version, error)

	// ApplyRetention sets the room's retention policy (FR-5.5 hook; model in
	// docs/security/data-classification-and-retention.md). The policy engine
	// upstream validates floors; adapters must reject a policy that would
	// shorten below the currently stored floor with ErrRetentionViolation.
	// Applying retention to a sealed room returns ErrRoomSealed.
	ApplyRetention(ctx context.Context, tenant TenantContext, room RoomID, policy RetentionPolicy) error

	// ExportRoom produces the full-room export package with manifest and
	// SHA-256 integrity letter (FR-1.7, SR-5.2). This is the only bulk-read
	// operation in the contract; it is intended to be policy-gated and
	// audited upstream. Exporting a sealed room is permitted (eDiscovery)
	// and must include the frozen state.
	ExportRoom(ctx context.Context, tenant TenantContext, room RoomID, opts ExportOptions) (ExportPackage, error)

	// SealRoom places a legal hold: the room and all its documents, versions,
	// and metadata become immutable (FR-1.6). All subsequent mutating calls
	// return ErrRoomSealed. Reads and ExportRoom remain available. Sealing
	// an already-sealed room returns the existing SealReceipt.
	SealRoom(ctx context.Context, tenant TenantContext, room RoomID, req SealRequest) (SealReceipt, error)
}
