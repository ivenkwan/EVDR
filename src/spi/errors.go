package spi

import "errors"

// Sentinel errors for the Room SPI contract. Callers match with errors.Is.
// Adapters must translate backend failures into these sentinels (wrapping is
// encouraged) so upstream services never depend on backend-specific errors.
var (
	// ErrRoomNotFound — the room does not exist in the tenant's scope.
	ErrRoomNotFound = errors.New("spi: room not found")
	// ErrRoomExists — CreateRoom slug collision within the tenant.
	ErrRoomExists = errors.New("spi: room already exists")
	// ErrDocumentNotFound — the document does not exist in the room.
	ErrDocumentNotFound = errors.New("spi: document not found")
	// ErrGrantNotFound — the access grant does not exist.
	ErrGrantNotFound = errors.New("spi: grant not found")
	// ErrAccessDenied — the actor holds no grant covering the operation.
	ErrAccessDenied = errors.New("spi: access denied")
	// ErrRoomSealed — the room is under legal hold; mutation rejected (FR-1.6).
	ErrRoomSealed = errors.New("spi: room is sealed")
	// ErrRoomClosed — the room is closed; only reads, export, and seal apply.
	ErrRoomClosed = errors.New("spi: room is closed")
	// ErrInvalidGrant — grant violates contract rules (e.g. guest grant
	// without expiry).
	ErrInvalidGrant = errors.New("spi: invalid grant")
	// ErrRetentionViolation — policy would breach a stored floor or hold.
	ErrRetentionViolation = errors.New("spi: retention policy violation")
	// ErrQuotaExceeded — tenant storage/quota ceiling reached (SR-3.2).
	ErrQuotaExceeded = errors.New("spi: quota exceeded")
	// ErrUnsupported — the adapter does not implement this operation.
	// Conformance requires both reference adapters to support all methods;
	// this exists for future constrained backends (TR-2.7).
	ErrUnsupported = errors.New("spi: operation unsupported by adapter")
)
