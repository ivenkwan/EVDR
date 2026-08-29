// Package memadapter provides an in-memory reference implementation of the
// Room SPI.
//
// It exists for two reasons: (1) to self-verify the conformance suite in
// src/spi/conformance — if the suite passes against two structurally
// different backends (in-memory here, Nextcloud OCS/WebDAV there), the suite
// is adapter-agnostic; (2) as a compact template for future adapters
// (NativeAdapter in Phase 2.5) showing every contract rule in one place.
//
// It is a test double and a reference, NOT a production backend.
package memadapter

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// Adapter is an in-memory Room SPI implementation.
type Adapter struct {
	mu       sync.Mutex
	tenantID spi.TenantID
	rooms    map[spi.RoomID]*roomState
	bySlug   map[string]spi.RoomID
}

type roomState struct {
	room   spi.Room
	grants map[spi.GrantID]grantRec
	docs   map[spi.DocumentID]*docRec
	byKey  map[string]spi.DocumentID
	seal   *spi.SealReceipt
}

type grantRec struct {
	grant   spi.AccessGrant
	revoked bool
}

type versionRec struct {
	ver spi.Version
}

type docRec struct {
	id         spi.DocumentID
	roomID     spi.RoomID
	name       string
	folderPath string
	content    map[int][]byte // version number → content (immutable)
	versions   []spi.Version  // ordered by number
}

// New returns a fresh in-memory adapter bound to tenantID.
func New(tenantID spi.TenantID) *Adapter {
	return &Adapter{
		tenantID: tenantID,
		rooms:    map[spi.RoomID]*roomState{},
		bySlug:   map[string]spi.RoomID{},
	}
}

func (a *Adapter) checkTenant(tenant spi.TenantContext) error {
	if tenant.TenantID != a.tenantID {
		return errTenantMismatch
	}
	return nil
}

var errTenantMismatch = errors.New("memadapter: tenant mismatch")

func scopeRoom(err error) error {
	if errors.Is(err, errTenantMismatch) {
		return spi.ErrRoomNotFound
	}
	return err
}

func scopeDoc(err error) error {
	if errors.Is(err, errTenantMismatch) {
		return spi.ErrDocumentNotFound
	}
	return err
}

func scopeCreate(err error) error {
	if errors.Is(err, errTenantMismatch) {
		return spi.ErrAccessDenied
	}
	return err
}

// CreateRoom provisions a room in memory. See the Nextcloud adapter for the
// contract rationale; semantics are identical.
func (a *Adapter) CreateRoom(ctx context.Context, tenant spi.TenantContext, req spi.CreateRoomRequest) (spi.Room, error) {
	select {
	case <-ctx.Done():
		return spi.Room{}, ctx.Err()
	default:
	}
	if err := a.checkTenant(tenant); err != nil {
		return spi.Room{}, scopeCreate(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.bySlug[req.Slug]; exists {
		return spi.Room{}, spi.ErrRoomExists
	}
	class := req.Classification
	if class == "" {
		class = spi.DefaultClassification
	}
	room := spi.Room{
		ID:             spi.RoomID(uuid()),
		Name:           req.Name,
		Slug:           req.Slug,
		State:          spi.RoomActive,
		Branding:       req.Branding,
		Classification: class,
		Retention:      req.Retention,
		CreatedAt:      time.Now().UTC(),
		CreatedBy:      tenant.Actor,
	}
	a.rooms[room.ID] = &roomState{room: room, grants: map[spi.GrantID]grantRec{}, docs: map[spi.DocumentID]*docRec{}, byKey: map[string]spi.DocumentID{}}
	a.bySlug[room.Slug] = room.ID
	return room, nil
}

// GrantAccess records a grant. Guest grants must carry NotAfter.
func (a *Adapter) GrantAccess(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, grant spi.AccessGrant) (spi.AccessGrant, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.AccessGrant{}, scopeRoom(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.rooms[roomID]
	if !ok {
		return spi.AccessGrant{}, spi.ErrRoomNotFound
	}
	if state.room.State == spi.RoomSealed {
		return spi.AccessGrant{}, spi.ErrRoomSealed
	}
	if grant.ActorKind == spi.ActorGuest && grant.Constraints.NotAfter.IsZero() {
		return spi.AccessGrant{}, spi.ErrInvalidGrant
	}
	if !grant.Constraints.NotAfter.IsZero() && !grant.Constraints.NotBefore.IsZero() &&
		grant.Constraints.NotBefore.After(grant.Constraints.NotAfter) {
		return spi.AccessGrant{}, spi.ErrInvalidGrant
	}
	rec := spi.AccessGrant{
		ID:          spi.GrantID(uuid()),
		Subject:     grant.Subject,
		ActorKind:   grant.ActorKind,
		Tier:        grant.Tier,
		Constraints: grant.Constraints,
		CreatedAt:   time.Now().UTC(),
		CreatedBy:   tenant.Actor,
	}
	state.grants[rec.ID] = grantRec{grant: rec}
	return rec, nil
}

// RevokeAccess withdraws a grant. Idempotent-safe; permitted on sealed
// rooms (security-preserving — see nextcloud adapter docs).
func (a *Adapter) RevokeAccess(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, grantID spi.GrantID) error {
	if err := a.checkTenant(tenant); err != nil {
		return scopeRoom(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.rooms[roomID]
	if !ok {
		return spi.ErrRoomNotFound
	}
	rec, ok := state.grants[grantID]
	if !ok {
		return spi.ErrGrantNotFound
	}
	if rec.revoked {
		return nil
	}
	rec.revoked = true
	state.grants[grantID] = rec
	return nil
}

// PutDocument stores a new immutable version per upload.
func (a *Adapter) PutDocument(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, doc spi.PutDocumentRequest) (spi.Document, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.Document{}, scopeRoom(err)
	}
	if doc.Name == "" || strings.ContainsAny(doc.Name, "/\\") {
		return spi.Document{}, fmt.Errorf("memadapter: invalid document name %q", doc.Name)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.rooms[roomID]
	if !ok {
		return spi.Document{}, spi.ErrRoomNotFound
	}
	if state.room.State == spi.RoomSealed {
		return spi.Document{}, spi.ErrRoomSealed
	}
	content, err := io.ReadAll(doc.Content)
	if err != nil {
		return spi.Document{}, err
	}

	key := strings.Trim(doc.FolderPath, "/") + "/" + doc.Name
	docID, exists := state.byKey[key]
	var rec *docRec
	if !exists {
		rec = &docRec{
			id:         spi.DocumentID(string(roomID) + "@" + uuid()),
			roomID:     roomID,
			name:       doc.Name,
			folderPath: strings.Trim(doc.FolderPath, "/"),
			content:    map[int][]byte{},
		}
		state.docs[rec.id] = rec
		state.byKey[key] = rec.id
	} else {
		rec = state.docs[docID]
	}
	num := len(rec.versions) + 1
	class := doc.Classification
	if class == "" {
		class = state.room.Classification
		if class == "" {
			class = spi.DefaultClassification
		}
	}
	sum := sha256.Sum256(content)
	ver := spi.Version{
		ID:         spi.VersionID(fmt.Sprintf("%s@%d", rec.id, num)),
		DocumentID: rec.id,
		Number:     num,
		SHA256:     hex.EncodeToString(sum[:]),
		SizeBytes:  int64(len(content)),
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  tenant.Actor,
	}
	rec.content[num] = append([]byte(nil), content...)
	rec.versions = append(rec.versions, ver)

	return spi.Document{
		ID:             rec.id,
		RoomID:         rec.roomID,
		Name:           rec.name,
		FolderPath:     rec.folderPath,
		ContentType:    contentType(doc.ContentType),
		Classification: class,
		CurrentVersion: ver.ID,
		SizeBytes:      ver.SizeBytes,
		UploadedAt:     ver.CreatedAt,
		UploadedBy:     tenant.Actor,
	}, nil
}

func contentType(c string) string {
	if c == "" {
		return "application/octet-stream"
	}
	return c
}

// GetRenderStream renders a document version view-scoped, one page per line
// of content, honouring the page range and context cancellation.
func (a *Adapter) GetRenderStream(ctx context.Context, tenant spi.TenantContext, docID spi.DocumentID, req spi.RenderRequest) (spi.RenderStream, error) {
	if err := a.checkTenant(tenant); err != nil {
		return nil, scopeDoc(err)
	}
	a.mu.Lock()
	rec, err := a.resolveDocLocked(docID)
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	state := a.rooms[rec.roomID]
	if !a.canAccessLocked(tenant.Actor, state) {
		a.mu.Unlock()
		return nil, spi.ErrAccessDenied
	}
	ver, ok := resolveVersionLocked(rec, req.Version)
	if !ok {
		a.mu.Unlock()
		return nil, spi.ErrDocumentNotFound
	}
	content := append([]byte(nil), rec.content[ver.Number]...)
	a.mu.Unlock()

	return renderLines(ctx, content, req), nil
}

func (a *Adapter) resolveDocLocked(docID spi.DocumentID) (*docRec, error) {
	for _, state := range a.rooms {
		if rec, ok := state.docs[docID]; ok {
			return rec, nil
		}
	}
	return nil, spi.ErrDocumentNotFound
}

func resolveVersionLocked(rec *docRec, id spi.VersionID) (spi.Version, bool) {
	if id == "" {
		if len(rec.versions) == 0 {
			return spi.Version{}, false
		}
		return rec.versions[len(rec.versions)-1], true
	}
	for _, v := range rec.versions {
		if v.ID == id {
			return v, true
		}
	}
	return spi.Version{}, false
}

// canAccessLocked mirrors the Nextcloud adapter's rule: creator or an
// active, time-valid grant.
func (a *Adapter) canAccessLocked(actor spi.Actor, state *roomState) bool {
	if actor.ID != "" && state.room.CreatedBy.ID == actor.ID {
		return true
	}
	now := time.Now()
	for _, g := range state.grants {
		if g.revoked || g.grant.Subject != actor.ID {
			continue
		}
		if !g.grant.Constraints.NotBefore.IsZero() && now.Before(g.grant.Constraints.NotBefore) {
			continue
		}
		if !g.grant.Constraints.NotAfter.IsZero() && !now.Before(g.grant.Constraints.NotAfter) {
			continue
		}
		return true
	}
	return false
}

// renderLines produces one page per line of content.
func renderLines(ctx context.Context, content []byte, req spi.RenderRequest) spi.RenderStream {
	text := strings.TrimRight(string(content), "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	var pages []spi.Page
	for i, ln := range lines {
		n := i + 1
		if n < req.FirstPage {
			continue
		}
		if req.LastPage > 0 && n > req.LastPage {
			break
		}
		pages = append(pages, spi.Page{Number: n, ContentType: "text/plain", Content: strings.NewReader(ln)})
	}
	return &pageStream{ctx: ctx, pages: pages}
}

// pageStream is a forward-only page stream honouring context cancellation.
type pageStream struct {
	ctx   context.Context
	pages []spi.Page
	i     int
	err   error
}

func (s *pageStream) Next() bool {
	if err := s.ctx.Err(); err != nil {
		s.err = err
		return false
	}
	if s.i >= len(s.pages) {
		return false
	}
	s.i++
	return true
}

func (s *pageStream) Page() spi.Page { return s.pages[s.i-1] }

func (s *pageStream) Err() error { return s.err }

func (s *pageStream) Close() error { return nil }

// ListVersions returns versions ascending by number.
func (a *Adapter) ListVersions(ctx context.Context, tenant spi.TenantContext, docID spi.DocumentID) ([]spi.Version, error) {
	if err := a.checkTenant(tenant); err != nil {
		return nil, scopeDoc(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	rec, err := a.resolveDocLocked(docID)
	if err != nil {
		return nil, err
	}
	if !a.canAccessLocked(tenant.Actor, a.rooms[rec.roomID]) {
		return nil, spi.ErrAccessDenied
	}
	out := make([]spi.Version, len(rec.versions))
	copy(out, rec.versions)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

// ApplyRetention rejects floors that would shorten below the stored one.
func (a *Adapter) ApplyRetention(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, policy spi.RetentionPolicy) error {
	if err := a.checkTenant(tenant); err != nil {
		return scopeRoom(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.rooms[roomID]
	if !ok {
		return spi.ErrRoomNotFound
	}
	if state.room.State == spi.RoomSealed {
		return spi.ErrRoomSealed
	}
	if policy.MinRetentionDays < state.room.Retention.MinRetentionDays {
		return spi.ErrRetentionViolation
	}
	state.room.Retention = policy
	return nil
}

// SealRoom freezes the room; re-seal returns the stored receipt.
func (a *Adapter) SealRoom(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, req spi.SealRequest) (spi.SealReceipt, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.SealReceipt{}, scopeRoom(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.rooms[roomID]
	if !ok {
		return spi.SealReceipt{}, spi.ErrRoomNotFound
	}
	if state.seal != nil {
		return *state.seal, nil
	}
	frozen := 0
	for _, rec := range state.docs {
		frozen += 1 + len(rec.versions)
	}
	receipt := spi.SealReceipt{
		ID:            spi.SealID(uuid()),
		RoomID:        roomID,
		SealedAt:      time.Now().UTC(),
		SealedBy:      tenant.Actor,
		FrozenObjects: frozen,
	}
	state.seal = &receipt
	state.room.State = spi.RoomSealed
	return receipt, nil
}

// ExportRoom builds a tar archive with a per-entry SHA-256 manifest.
func (a *Adapter) ExportRoom(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, opts spi.ExportOptions) (spi.ExportPackage, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.ExportPackage{}, scopeRoom(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state, ok := a.rooms[roomID]
	if !ok {
		return spi.ExportPackage{}, spi.ErrRoomNotFound
	}
	if !a.canAccessLocked(tenant.Actor, state) {
		return spi.ExportPackage{}, spi.ErrAccessDenied
	}

	roomBytes, _ := json.MarshalIndent(state.room, "", "  ")
	entries := []exportEntry{{Path: "room.json", Bytes: roomBytes}}
	if opts.IncludeAuditTrail {
		g, _ := json.MarshalIndent(grantsView(state.grants), "", "  ")
		entries = append(entries, exportEntry{Path: "audit/grants.json", Bytes: g})
		if state.seal != nil {
			s, _ := json.MarshalIndent(state.seal, "", "  ")
			entries = append(entries, exportEntry{Path: "audit/seal.json", Bytes: s})
		}
	}

	manifest := spi.ExportManifest{RoomID: roomID, GeneratedAt: time.Now().UTC()}
	docIDs := make([]string, 0, len(state.docs))
	for id := range state.docs {
		docIDs = append(docIDs, string(id))
	}
	sort.Strings(docIDs)
	for _, id := range docIDs {
		rec := state.docs[spi.DocumentID(id)]
		for _, v := range rec.versions {
			if !opts.IncludeVersions && v.ID != rec.versions[len(rec.versions)-1].ID {
				continue
			}
			content := rec.content[v.Number]
			sum := sha256.Sum256(content)
			path := fmt.Sprintf("docs/%s/v%d/%s", rec.id, v.Number, sanitizeName(rec.name))
			entries = append(entries, exportEntry{Path: path, Bytes: append([]byte(nil), content...)})
			manifest.Entries = append(manifest.Entries, spi.ManifestEntry{
				Path: path, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(content)),
			})
		}
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Path, "docs/") {
			continue // version entries were added to the manifest above
		}
		sum := sha256.Sum256(e.Bytes)
		manifest.Entries = append(manifest.Entries, spi.ManifestEntry{
			Path: e.Path, SHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(e.Bytes)),
		})
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		if err := writeTarBytes(tw, e.Path, e.Bytes); err != nil {
			return spi.ExportPackage{}, err
		}
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	_ = writeTarBytes(tw, "manifest.json", mb)
	_ = writeTarBytes(tw, "INTEGRITY_LETTER.txt", integrityLetter(manifest))
	_ = tw.Close()

	return spi.ExportPackage{
		RoomID:          roomID,
		Content:         io.NopCloser(bytes.NewReader(buf.Bytes())),
		Manifest:        manifest,
		IntegrityLetter: integrityLetter(manifest),
		GeneratedAt:     manifest.GeneratedAt,
	}, nil
}

type exportEntry struct {
	Path  string
	Bytes []byte
}

func grantsView(grants map[spi.GrantID]grantRec) map[spi.GrantID]spi.AccessGrant {
	out := map[spi.GrantID]spi.AccessGrant{}
	for id, g := range grants {
		if !g.revoked {
			out[id] = g.grant
		}
	}
	return out
}

func writeTarBytes(tw *tar.Writer, name string, b []byte) error {
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(b)), ModTime: time.Now().UTC()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(b)
	return err
}

func integrityLetter(m spi.ExportManifest) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "EVDR Room Export — Integrity Letter\nRoom ID: %s\nGenerated: %s\n\n", m.RoomID, m.GeneratedAt.UTC().Format(time.RFC3339))
	for _, e := range m.Entries {
		fmt.Fprintf(&b, "  %s  sha256:%s  (%d bytes)\n", e.Path, e.SHA256, e.SizeBytes)
	}
	return []byte(b.String())
}

func sanitizeName(name string) string {
	return strings.NewReplacer("/", "_", "\\", "_").Replace(name)
}

func uuid() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("memadapter: crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Compile-time assertion.
var _ spi.RoomSPI = (*Adapter)(nil)
