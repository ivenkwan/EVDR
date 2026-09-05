package room

import (
	"fmt"
	"strings"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// This file defines the service's JSON wire types and their conversions to
// and from the SPI value types. Wire types are stable API surface: they are
// what the portal/admin BFFs Zod-validate against. Time fields marshal as
// RFC 3339; optional constraints marshal as null when unset.

// WireActor is the JSON form of spi.Actor.
type WireActor struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// WireBranding is the JSON form of spi.RoomBranding (FR-1.1). LogoObjectKey
// is a storage key resolvable only through the SPI/adapter; no direct asset
// URL is implied.
type WireBranding struct {
	LogoObjectKey string            `json:"logoObjectKey,omitempty"`
	Theme         map[string]string `json:"theme,omitempty"`
	AboutPage     string            `json:"aboutPage,omitempty"`
}

// WireRetention is the JSON form of spi.RetentionPolicy.
type WireRetention struct {
	Anchor           string `json:"anchor"`
	MinRetentionDays int    `json:"minRetentionDays"`
	MaxRetentionDays int    `json:"maxRetentionDays"`
	AutoPurge        bool   `json:"autoPurge"`
}

// WireRoom is the JSON form of spi.Room.
type WireRoom struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Slug           string        `json:"slug"`
	State          string        `json:"state"`
	Branding       WireBranding  `json:"branding"`
	Classification string        `json:"classification"`
	Retention      WireRetention `json:"retention"`
	CreatedAt      time.Time     `json:"createdAt"`
	CreatedBy      WireActor     `json:"createdBy"`
	ClosedAt       *time.Time    `json:"closedAt,omitempty"`
}

// WireCreateRoomRequest is the JSON body of POST /api/v1/rooms.
type WireCreateRoomRequest struct {
	Name           string         `json:"name"`
	Slug           string         `json:"slug"`
	Branding       *WireBranding  `json:"branding,omitempty"`
	Classification string         `json:"classification,omitempty"`
	Retention      *WireRetention `json:"retention,omitempty"`
}

// WireSealRequest is the JSON body of POST /api/v1/rooms/{id}/seal (FR-1.6).
type WireSealRequest struct {
	Reason       string `json:"reason,omitempty"`
	LegalHoldRef string `json:"legalHoldRef,omitempty"`
}

// WireSealReceipt is the JSON form of spi.SealReceipt.
type WireSealReceipt struct {
	ID            string    `json:"id"`
	RoomID        string    `json:"roomId"`
	SealedAt      time.Time `json:"sealedAt"`
	SealedBy      WireActor `json:"sealedBy"`
	FrozenObjects int       `json:"frozenObjects"`
}

// WireExportRequest is the JSON body of POST /api/v1/rooms/{id}/export.
// The response is the raw export archive stream, not JSON.
type WireExportRequest struct {
	IncludeAuditTrail bool `json:"includeAuditTrail"`
	IncludeVersions   bool `json:"includeVersions"`
}

// WireGrantRequest is the JSON body of POST /api/v1/rooms/{id}/grants.
type WireGrantRequest struct {
	Subject     string               `json:"subject"`
	ActorKind   string               `json:"actorKind"`
	Tier        string               `json:"tier,omitempty"`
	Constraints *WireGrantConstraint `json:"constraints,omitempty"`
}

// WireGrantConstraint is the JSON form of spi.GrantConstraints. Unset
// boundaries are null.
type WireGrantConstraint struct {
	NotBefore *time.Time `json:"notBefore,omitempty"`
	NotAfter  *time.Time `json:"notAfter,omitempty"`
	CIDRs     []string   `json:"cidrs,omitempty"`
	Domains   []string   `json:"domains,omitempty"`
}

// WireGrant is the JSON form of spi.AccessGrant.
type WireGrant struct {
	ID          string              `json:"id"`
	Subject     string              `json:"subject"`
	ActorKind   string              `json:"actorKind"`
	Tier        string              `json:"tier"`
	Constraints WireGrantConstraint `json:"constraints"`
	CreatedAt   time.Time           `json:"createdAt"`
	CreatedBy   WireActor           `json:"createdBy"`
}

// WireDocument is the JSON form of spi.Document.
type WireDocument struct {
	ID             string    `json:"id"`
	RoomID         string    `json:"roomId"`
	Name           string    `json:"name"`
	FolderPath     string    `json:"folderPath"`
	ContentType    string    `json:"contentType"`
	Classification string    `json:"classification"`
	CurrentVersion string    `json:"currentVersion"`
	SizeBytes      int64     `json:"sizeBytes"`
	UploadedAt     time.Time `json:"uploadedAt"`
	UploadedBy     WireActor `json:"uploadedBy"`
}

// WireVersion is the JSON form of spi.Version (FR-2.2).
type WireVersion struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"documentId"`
	Number     int       `json:"number"`
	SHA256     string    `json:"sha256"`
	SizeBytes  int64     `json:"sizeBytes"`
	CreatedAt  time.Time `json:"createdAt"`
	CreatedBy  WireActor `json:"createdBy"`
}

// WirePublicRoom is the public room branding projection exposed to the
// external portal (FR-1.1). It deliberately carries NO documents, grants,
// retention, createdBy or audit data — the external guest never sees
// management surface.
type WirePublicRoom struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Slug     string       `json:"slug"`
	State    string       `json:"state"`
	Branding WireBranding `json:"branding"`
}

// ---------------------------------------------------------------------------
// SPI → wire conversions
// ---------------------------------------------------------------------------

func wireActor(a spi.Actor) WireActor {
	return WireActor{Kind: string(a.Kind), ID: a.ID, DisplayName: a.DisplayName}
}

func wireBranding(b spi.RoomBranding) WireBranding {
	return WireBranding{LogoObjectKey: b.LogoObjectKey, Theme: b.Theme, AboutPage: b.AboutPage}
}

func wireRetention(r spi.RetentionPolicy) WireRetention {
	return WireRetention{
		Anchor:           string(r.Anchor),
		MinRetentionDays: r.MinRetentionDays,
		MaxRetentionDays: r.MaxRetentionDays,
		AutoPurge:        r.AutoPurge,
	}
}

func wireRoom(r spi.Room) WireRoom {
	return WireRoom{
		ID:             string(r.ID),
		Name:           r.Name,
		Slug:           r.Slug,
		State:          string(r.State),
		Branding:       wireBranding(r.Branding),
		Classification: string(r.Classification),
		Retention:      wireRetention(r.Retention),
		CreatedAt:      r.CreatedAt,
		CreatedBy:      wireActor(r.CreatedBy),
		ClosedAt:       r.ClosedAt,
	}
}

func wireSealReceipt(s spi.SealReceipt) WireSealReceipt {
	return WireSealReceipt{
		ID:            string(s.ID),
		RoomID:        string(s.RoomID),
		SealedAt:      s.SealedAt,
		SealedBy:      wireActor(s.SealedBy),
		FrozenObjects: s.FrozenObjects,
	}
}

func wireGrant(g spi.AccessGrant) WireGrant {
	c := WireGrantConstraint{CIDRs: g.Constraints.CIDRs, Domains: g.Constraints.Domains}
	if !g.Constraints.NotBefore.IsZero() {
		t := g.Constraints.NotBefore
		c.NotBefore = &t
	}
	if !g.Constraints.NotAfter.IsZero() {
		t := g.Constraints.NotAfter
		c.NotAfter = &t
	}
	return WireGrant{
		ID:          string(g.ID),
		Subject:     g.Subject,
		ActorKind:   string(g.ActorKind),
		Tier:        g.Tier.String(),
		Constraints: c,
		CreatedAt:   g.CreatedAt,
		CreatedBy:   wireActor(g.CreatedBy),
	}
}

func wireDocument(d spi.Document) WireDocument {
	return WireDocument{
		ID:             string(d.ID),
		RoomID:         string(d.RoomID),
		Name:           d.Name,
		FolderPath:     d.FolderPath,
		ContentType:    d.ContentType,
		Classification: string(d.Classification),
		CurrentVersion: string(d.CurrentVersion),
		SizeBytes:      d.SizeBytes,
		UploadedAt:     d.UploadedAt,
		UploadedBy:     wireActor(d.UploadedBy),
	}
}

func wireVersion(v spi.Version) WireVersion {
	return WireVersion{
		ID:         string(v.ID),
		DocumentID: string(v.DocumentID),
		Number:     v.Number,
		SHA256:     v.SHA256,
		SizeBytes:  v.SizeBytes,
		CreatedAt:  v.CreatedAt,
		CreatedBy:  wireActor(v.CreatedBy),
	}
}

func wirePublicRoom(r spi.Room) WirePublicRoom {
	return WirePublicRoom{
		ID:       string(r.ID),
		Name:     r.Name,
		Slug:     r.Slug,
		State:    string(r.State),
		Branding: wireBranding(r.Branding),
	}
}

// ---------------------------------------------------------------------------
// Wire → SPI conversions and request validation
// ---------------------------------------------------------------------------

// validClassifications lists the SPI classification labels accepted at the
// API boundary. Empty means "inherit" and is resolved by the SPI.
var validClassifications = map[string]bool{
	"":                                     true,
	string(spi.ClassificationPublic):       true,
	string(spi.ClassificationInternal):     true,
	string(spi.ClassificationConfidential): true,
	string(spi.ClassificationRestricted):   true,
}

var validRetentionAnchors = map[string]bool{
	"":                            true, // zero policy — not anchored
	string(spi.AnchorUpload):      true,
	string(spi.AnchorRoomClosure): true,
}

// toSpiCreateRequest converts and validates a create-room body. Returned
// error is an *httpError already typed for the caller's convenience.
func (r *WireCreateRoomRequest) toSpiCreateRequest() (spi.CreateRoomRequest, *httpError) {
	if strings.TrimSpace(r.Name) == "" {
		return spi.CreateRoomRequest{}, badRequest("name is required")
	}
	slug := strings.TrimSpace(r.Slug)
	if slug == "" {
		return spi.CreateRoomRequest{}, badRequest("slug is required")
	}
	if len(slug) > 64 || !validSlug(slug) {
		return spi.CreateRoomRequest{}, badRequest("slug must be 1–64 chars of a-z0-9 and hyphens")
	}
	class := strings.ToUpper(strings.TrimSpace(r.Classification))
	if !validClassifications[class] {
		return spi.CreateRoomRequest{}, badRequest(fmt.Sprintf("classification must be one of PUBLIC, INTERNAL, CONFIDENTIAL, RESTRICTED (got %q)", r.Classification))
	}
	out := spi.CreateRoomRequest{
		Name:           strings.TrimSpace(r.Name),
		Slug:           slug,
		Classification: spi.Classification(class),
	}
	if r.Branding != nil {
		out.Branding = spi.RoomBranding{
			LogoObjectKey: r.Branding.LogoObjectKey,
			Theme:         r.Branding.Theme,
			AboutPage:     r.Branding.AboutPage,
		}
	}
	if r.Retention != nil {
		ret, err := r.Retention.toSpiRetention()
		if err != nil {
			return spi.CreateRoomRequest{}, err
		}
		out.Retention = ret
	}
	return out, nil
}

// toSpiRetention converts and validates a retention policy body.
func (r *WireRetention) toSpiRetention() (spi.RetentionPolicy, *httpError) {
	anchor := strings.TrimSpace(r.Anchor)
	if !validRetentionAnchors[anchor] {
		return spi.RetentionPolicy{}, badRequest(fmt.Sprintf("retention anchor must be one of upload, room_closure (got %q)", r.Anchor))
	}
	if r.MinRetentionDays < 0 {
		return spi.RetentionPolicy{}, badRequest("minRetentionDays must be >= 0")
	}
	if r.MaxRetentionDays != 0 && r.MaxRetentionDays < r.MinRetentionDays {
		return spi.RetentionPolicy{}, badRequest("maxRetentionDays must be 0 (unlimited) or >= minRetentionDays")
	}
	return spi.RetentionPolicy{
		Anchor:           spi.RetentionAnchor(anchor),
		MinRetentionDays: r.MinRetentionDays,
		MaxRetentionDays: r.MaxRetentionDays,
		AutoPurge:        r.AutoPurge,
	}, nil
}

func (r *WireSealRequest) toSpiSeal() spi.SealRequest {
	return spi.SealRequest{Reason: r.Reason, LegalHoldRef: r.LegalHoldRef}
}

func (r *WireExportRequest) toSpiExport() spi.ExportOptions {
	return spi.ExportOptions{IncludeAuditTrail: r.IncludeAuditTrail, IncludeVersions: r.IncludeVersions}
}

// toSpiGrant converts and validates a grant body. Constraint time semantics
// (guest expiry etc.) are enforced by the SPI — errors surface as the SPI's
// own sentinels mapped to HTTP.
func (r *WireGrantRequest) toSpiGrant() (spi.AccessGrant, *httpError) {
	subject := strings.TrimSpace(r.Subject)
	if subject == "" {
		return spi.AccessGrant{}, badRequest("subject is required")
	}
	kind, ok := parseActorKind(r.ActorKind)
	if !ok {
		return spi.AccessGrant{}, badRequest(fmt.Sprintf("actorKind must be one of internal_user, guest, service (got %q)", r.ActorKind))
	}
	tier := spi.TierViewOnly
	if strings.TrimSpace(r.Tier) != "" {
		parsed, ok := parseTier(r.Tier)
		if !ok {
			return spi.AccessGrant{}, badRequest(fmt.Sprintf("tier must be one of view_only, download_allowed, print_allowed, edit_allowed (got %q)", r.Tier))
		}
		tier = parsed
	}
	g := spi.AccessGrant{
		Subject:   subject,
		ActorKind: kind,
		Tier:      tier,
	}
	if r.Constraints != nil {
		g.Constraints = spi.GrantConstraints{
			NotBefore: derefTime(r.Constraints.NotBefore),
			NotAfter:  derefTime(r.Constraints.NotAfter),
			CIDRs:     r.Constraints.CIDRs,
			Domains:   r.Constraints.Domains,
		}
	}
	return g, nil
}

func parseActorKind(s string) (spi.ActorKind, bool) {
	switch strings.TrimSpace(s) {
	case string(spi.ActorInternalUser):
		return spi.ActorInternalUser, true
	case string(spi.ActorGuest):
		return spi.ActorGuest, true
	case string(spi.ActorService):
		return spi.ActorService, true
	default:
		return "", false
	}
}

func parseTier(s string) (spi.PermissionTier, bool) {
	switch strings.TrimSpace(s) {
	case "view_only":
		return spi.TierViewOnly, true
	case "download_allowed":
		return spi.TierDownloadAllowed, true
	case "print_allowed":
		return spi.TierPrintAllowed, true
	case "edit_allowed":
		return spi.TierEditAllowed, true
	default:
		return 0, false
	}
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func validSlug(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}
