package spi

import (
	"io"
	"time"
)

// Identity types. These are opaque to the SPI; format is owned by the
// control plane. String newtypes keep the contract free of external
// dependencies and force explicit conversion at system boundaries.

// TenantID identifies a tenant. Set from authenticated context only (SR-2.2).
type TenantID string

// RoomID identifies a room within a tenant.
type RoomID string

// DocumentID identifies a document within a room.
type DocumentID string

// VersionID identifies one immutable version of a document.
type VersionID string

// GrantID identifies an access grant.
type GrantID string

// SealID identifies a room seal / legal-hold action.
type SealID string

// ActorKind distinguishes the classes of principals the platform serves.
type ActorKind string

const (
	ActorInternalUser ActorKind = "internal_user" // SSO + MFA (FR-4.1/4.2)
	ActorGuest        ActorKind = "guest"         // expiring link / OTP (FR-4.3)
	ActorService      ActorKind = "service"       // platform service account
)

// Actor is the authenticated principal performing an operation, as resolved
// by the calling service's auth layer. Never constructed from client input.
type Actor struct {
	Kind        ActorKind
	ID          string // principal ID from the IdP / link token subject
	DisplayName string
}

// TenantContext carries the server-established tenant and actor for one call.
// Adapters use it for storage scoping and for the tenant_id carried by every
// downstream record and event (CLAUDE.md multi-tenancy rules).
type TenantContext struct {
	TenantID TenantID
	Actor    Actor
}

// Classification labels follow the four-level taxonomy defined in
// docs/security/data-classification-and-retention.md (FR-5.4, NFR-7.5).
type Classification string

const (
	ClassificationPublic       Classification = "PUBLIC"       // C0
	ClassificationInternal     Classification = "INTERNAL"     // C1
	ClassificationConfidential Classification = "CONFIDENTIAL" // C2 — platform default
	ClassificationRestricted   Classification = "RESTRICTED"   // C3
)

// DefaultClassification is applied to uploads that carry no explicit label.
const DefaultClassification = ClassificationConfidential

// PermissionTier is a room-level access tier (FR-1.2). Tiers are ordered:
// each tier includes the capabilities of the tiers below it.
type PermissionTier int

const (
	TierViewOnly PermissionTier = iota + 1
	TierDownloadAllowed
	TierPrintAllowed
	TierEditAllowed
)

func (t PermissionTier) String() string {
	switch t {
	case TierViewOnly:
		return "view_only"
	case TierDownloadAllowed:
		return "download_allowed"
	case TierPrintAllowed:
		return "print_allowed"
	case TierEditAllowed:
		return "edit_allowed"
	default:
		return "unknown"
	}
}

// RoomState is the lifecycle state of a room.
type RoomState string

const (
	RoomActive   RoomState = "active"
	RoomSealed   RoomState = "sealed" // legal hold — frozen (FR-1.6)
	RoomClosed   RoomState = "closed" // retention clock anchor
	RoomArchived RoomState = "archived"
)

// RoomBranding carries per-room presentation settings (FR-1.1). Asset
// references are storage keys resolvable through the adapter, never raw
// content, so portal rendering does not bypass the SPI.
type RoomBranding struct {
	LogoObjectKey string
	Theme         map[string]string // CSS custom property overrides
	AboutPage     string            // markdown
}

// Room is the aggregate record for a secure exchange room.
type Room struct {
	ID             RoomID
	Name           string
	Slug           string // portal URL slug, unique per tenant
	State          RoomState
	Branding       RoomBranding
	Classification Classification // floor for all documents in the room
	Retention      RetentionPolicy
	CreatedAt      time.Time
	CreatedBy      Actor
	ClosedAt       *time.Time // set when State transitions to RoomClosed
}

// CreateRoomRequest is the input to CreateRoom.
type CreateRoomRequest struct {
	Name           string
	Slug           string
	Branding       RoomBranding
	Classification Classification  // zero value resolves to DefaultClassification
	Retention      RetentionPolicy // must satisfy platform floors (validated by policy engine upstream)
}

// GrantConstraints bound where and when a grant is usable (FR-4.4).
// Zero values mean "not constrained". Expiry is always required for guests.
type GrantConstraints struct {
	NotBefore time.Time
	NotAfter  time.Time // mandatory for ActorGuest grants
	CIDRs     []string  // allowed client networks, e.g. "203.0.113.0/24"
	Domains   []string  // allowed email domains for link redemption
}

// AccessGrant is a room access authorisation at a permission tier.
type AccessGrant struct {
	ID          GrantID // assigned by the adapter on GrantAccess
	Subject     string  // email for guests, principal ID for internal users
	ActorKind   ActorKind
	Tier        PermissionTier
	Constraints GrantConstraints
	CreatedAt   time.Time
	CreatedBy   Actor
}

// PutDocumentRequest is the input to PutDocument. Content is streamed;
// implementations must not require the whole payload in memory (FR-2.5).
type PutDocumentRequest struct {
	Name           string
	FolderPath     string // slash-separated path within the room (FR-2.3); "" = root
	ContentType    string // MIME type as sniffed/validated by the caller
	Content        io.Reader
	SizeBytes      int64
	Classification Classification // zero value inherits the room floor
	Metadata       map[string]string
}

// Document is the record of an uploaded document. CurrentVersion always
// points at the newest immutable version (FR-2.2).
type Document struct {
	ID             DocumentID
	RoomID         RoomID
	Name           string
	FolderPath     string
	ContentType    string
	Classification Classification
	CurrentVersion VersionID
	SizeBytes      int64
	UploadedAt     time.Time
	UploadedBy     Actor
}

// Version is one immutable revision of a document.
type Version struct {
	ID         VersionID
	DocumentID DocumentID
	Number     int // monotonically increasing per document, from 1
	SHA256     string
	SizeBytes  int64
	CreatedAt  time.Time
	CreatedBy  Actor
}

// RenderRequest asks for a rendered page range of one document version.
// Pages are 1-indexed and inclusive. The viewer requests pages individually
// or in small ranges; implementations may cap RangeSize.
type RenderRequest struct {
	Version   VersionID // zero value = current version
	FirstPage int
	LastPage  int
	Watermark WatermarkSpec // applied server-side before delivery (FR-3.2/TR-4.2)
}

// WatermarkSpec is the server-rendered watermark baked into rendered output
// (FR-3.2, FR-3.6, TR-4.6). Token values are resolved by the caller from the
// authenticated session — adapters render what they are given.
type WatermarkSpec struct {
	ViewerIdentity string
	Timestamp      time.Time
	ClientIP       string
	SessionID      string
	Density        string  // room preset: sparse|normal|dense
	Opacity        float64 // 0.0–1.0
	Rotation       float64 // degrees
}

// Page is a single rendered page. Content is typically image/webp or
// application/pdf for a single-page PDF; ContentType is authoritative.
type Page struct {
	Number      int
	ContentType string
	Content     io.Reader
}

// RenderStream is a forward-only stream of rendered pages. It exists so the
// viewer never holds a whole document (FR-3.1). Callers must Close it.
type RenderStream interface {
	// Next advances the stream. It returns false at exhaustion or error.
	Next() bool
	// Page returns the current page; valid only after Next returned true.
	Page() Page
	// Err returns the first error encountered, if any.
	Err() error
	// Close releases underlying resources. Safe to call multiple times.
	Close() error
}

// RetentionAnchor is the event that starts a retention clock.
type RetentionAnchor string

const (
	AnchorUpload      RetentionAnchor = "upload"
	AnchorRoomClosure RetentionAnchor = "room_closure"
)

// RetentionPolicy governs room/document retention (FR-5.5, model defined in
// docs/security/data-classification-and-retention.md). Platform floors are
// enforced upstream by the policy engine; adapters persist and honour the
// resolved policy.
type RetentionPolicy struct {
	Anchor           RetentionAnchor
	MinRetentionDays int  // floor; purge before this is a contract violation
	MaxRetentionDays int  // 0 = no maximum
	AutoPurge        bool // purge at MaxRetentionDays (evidence-emitting, FR-5.5)
}

// ExportManifest is the machine-readable inventory of an export package.
type ExportManifest struct {
	RoomID      RoomID
	Entries     []ManifestEntry
	GeneratedAt time.Time
}

// ManifestEntry is one exported object with its integrity digest.
type ManifestEntry struct {
	Path      string // archive-relative path
	SHA256    string
	SizeBytes int64
}

// ExportOptions parameterises ExportRoom (FR-1.7).
type ExportOptions struct {
	IncludeAuditTrail bool // include room activity log in the package
	IncludeVersions   bool // include all versions, not just current
}

// ExportPackage is an audited, integrity-protected full-room export
// (FR-1.7, SR-5.2). Content is a streamed archive (e.g. tar); the integrity
// letter binds every entry's SHA-256 so a third party can verify the
// package has not been altered.
type ExportPackage struct {
	RoomID          RoomID
	Content         io.ReadCloser
	Manifest        ExportManifest
	IntegrityLetter []byte // human-readable letter embedding the manifest digests
	GeneratedAt     time.Time
}

// SealRequest initiates a legal hold / room seal (FR-1.6).
type SealRequest struct {
	Reason       string
	LegalHoldRef string // external matter/case reference
}

// SealReceipt confirms a seal. A sealed room rejects all mutations with
// ErrRoomSealed until the seal is lifted by a future contract operation.
type SealReceipt struct {
	ID            SealID
	RoomID        RoomID
	SealedAt      time.Time
	SealedBy      Actor
	FrozenObjects int // documents + versions frozen at seal time
}
