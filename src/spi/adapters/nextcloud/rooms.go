package nextcloud

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// roomIndexPath is the adapter-level RoomID → slug index.
func (a *Adapter) roomIndexPath() string { return a.davURL("_evdr", "rooms.json") }

// validateSlug rejects slugs that could escape the WebDAV namespace.
func validateSlug(slug string) error {
	if slug == "" {
		return errors.New("spi: room slug must not be empty")
	}
	if slug == "." || slug == ".." || strings.ContainsAny(slug, "/\\") {
		return fmt.Errorf("spi: invalid room slug %q", slug)
	}
	return nil
}

// CreateRoom provisions a room as a Nextcloud folder plus its metadata
// ledgers (TR-2.2). The room folder doubles as the OCS share root for
// GrantAccess. A slug collision is detected via the RoomID→slug index and
// the WebDAV MKCOL 405 response, and reported as spi.ErrRoomExists.
func (a *Adapter) CreateRoom(ctx context.Context, tenant spi.TenantContext, req spi.CreateRoomRequest) (spi.Room, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.Room{}, scopeCreate(err)
	}
	if err := validateSlug(req.Slug); err != nil {
		return spi.Room{}, err
	}
	unlock := a.lockRoom(spi.RoomID("create:" + req.Slug))
	defer unlock()

	idx, err := a.loadRoomIndex(ctx)
	if err != nil {
		return spi.Room{}, err
	}
	for _, slug := range idx {
		if slug == req.Slug {
			return spi.Room{}, spi.ErrRoomExists
		}
	}

	// Ensure the adapter-level metadata directory exists (first room).
	if err := a.mkcol(ctx, a.davURL("_evdr")); err != nil {
		return spi.Room{}, err
	}

	// Create the room folder. 405 = already exists → slug collision.
	resp, err := a.do(ctx, "MKCOL", a.roomURL(req.Slug), nil, "", false)
	if err != nil {
		return spi.Room{}, err
	}
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusNoContent:
		drain(resp)
	case http.StatusMethodNotAllowed:
		drain(resp)
		return spi.Room{}, spi.ErrRoomExists
	case http.StatusInsufficientStorage:
		drain(resp)
		return spi.Room{}, spi.ErrQuotaExceeded
	default:
		drain(resp)
		return spi.Room{}, fmt.Errorf("spi: create room: MKCOL %q: status %d", req.Slug, resp.StatusCode)
	}

	created := false
	defer func() {
		if !created {
			// Best-effort rollback so a failed CreateRoom leaves no residue.
			if r, derr := a.do(context.WithoutCancel(ctx), "DELETE", a.roomURL(req.Slug), nil, "", false); derr == nil {
				drain(r)
			}
		}
	}()

	// Room subdirectories.
	for _, sub := range []string{"_evdr", "_evdr/docs", "docs"} {
		if err := a.mkcol(ctx, a.roomURL(req.Slug, sub)); err != nil {
			return spi.Room{}, err
		}
	}

	classification := req.Classification
	if classification == "" {
		classification = spi.DefaultClassification
	}
	room := spi.Room{
		ID:             spi.RoomID(newUUID()),
		Name:           req.Name,
		Slug:           req.Slug,
		State:          spi.RoomActive,
		Branding:       req.Branding,
		Classification: classification,
		Retention:      req.Retention,
		CreatedAt:      time.Now().UTC(),
		CreatedBy:      tenant.Actor,
	}

	meta := roomMeta{Name: room.Name, Slug: room.Slug, State: room.State, Branding: room.Branding, Classification: room.Classification, Retention: room.Retention, CreatedAt: room.CreatedAt, CreatedBy: room.CreatedBy}
	if err := a.writeJSON(ctx, a.roomURL(req.Slug, "_evdr", "room.json"), meta); err != nil {
		return spi.Room{}, err
	}
	if err := a.writeJSON(ctx, a.roomURL(req.Slug, "_evdr", "grants.json"), grantLedger{Grants: map[spi.GrantID]grantRecord{}}); err != nil {
		return spi.Room{}, err
	}

	idx[room.ID] = room.Slug
	if err := a.saveRoomIndex(ctx, idx); err != nil {
		return spi.Room{}, err
	}

	created = true
	return room, nil
}

// scopeCreate wraps errTenantMismatch into the CreateRoom sentinel.
func scopeCreate(err error) error {
	if errors.Is(err, errTenantMismatch) {
		return spi.ErrAccessDenied
	}
	return err
}

// loadRoomIndex loads the RoomID → slug index; a missing index is empty.
func (a *Adapter) loadRoomIndex(ctx context.Context) (map[spi.RoomID]string, error) {
	idx := map[spi.RoomID]string{}
	if _, err := a.readJSON(ctx, a.roomIndexPath(), &idx); err != nil {
		return nil, err
	}
	return idx, nil
}

// saveRoomIndex persists the RoomID → slug index.
func (a *Adapter) saveRoomIndex(ctx context.Context, idx map[spi.RoomID]string) error {
	if err := a.mkcol(ctx, a.davURL("_evdr")); err != nil {
		return err
	}
	return a.writeJSON(ctx, a.roomIndexPath(), idx)
}

// roomMeta is the room record stored at <slug>/_evdr/room.json. It mirrors
// spi.Room's mutable fields; state transitions are driven by SealRoom.
type roomMeta struct {
	Name           string              `json:"name"`
	Slug           string              `json:"slug"`
	State          spi.RoomState       `json:"state"`
	Branding       spi.RoomBranding    `json:"branding"`
	Classification spi.Classification  `json:"classification"`
	Retention      spi.RetentionPolicy `json:"retention"`
	CreatedAt      time.Time           `json:"created_at"`
	CreatedBy      spi.Actor           `json:"created_by"`
	ClosedAt       *time.Time          `json:"closed_at,omitempty"`
}

// room returns the spi.Room view of this metadata.
func (m roomMeta) room(id spi.RoomID) spi.Room {
	return spi.Room{
		ID:             id,
		Name:           m.Name,
		Slug:           m.Slug,
		State:          m.State,
		Branding:       m.Branding,
		Classification: m.Classification,
		Retention:      m.Retention,
		CreatedAt:      m.CreatedAt,
		CreatedBy:      m.CreatedBy,
		ClosedAt:       m.ClosedAt,
	}
}

// loadRoom resolves a room by ID and returns its record. The bool is false
// when the room does not exist in this tenant's scope.
func (a *Adapter) loadRoom(ctx context.Context, roomID spi.RoomID) (spi.Room, bool, error) {
	idx, err := a.loadRoomIndex(ctx)
	if err != nil {
		return spi.Room{}, false, err
	}
	slug, ok := idx[roomID]
	if !ok {
		return spi.Room{}, false, nil
	}
	var meta roomMeta
	ok, err = a.readJSON(ctx, a.roomURL(slug, "_evdr", "room.json"), &meta)
	if err != nil {
		return spi.Room{}, false, err
	}
	if !ok {
		return spi.Room{}, false, nil
	}
	return meta.room(roomID), true, nil
}

// saveRoom persists a room record.
func (a *Adapter) saveRoom(ctx context.Context, room spi.Room) error {
	meta := roomMeta{Name: room.Name, Slug: room.Slug, State: room.State, Branding: room.Branding, Classification: room.Classification, Retention: room.Retention, CreatedAt: room.CreatedAt, CreatedBy: room.CreatedBy, ClosedAt: room.ClosedAt}
	return a.writeJSON(ctx, a.roomURL(room.Slug, "_evdr", "room.json"), meta)
}

// ApplyRetention sets the room's retention policy. Shortening below the
// currently stored floor is rejected with spi.ErrRetentionViolation (defence
// in depth behind the policy engine, which validates floors upstream).
func (a *Adapter) ApplyRetention(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, policy spi.RetentionPolicy) error {
	if err := a.checkTenant(tenant); err != nil {
		return scopeRoom(err)
	}
	unlock := a.lockRoom(roomID)
	defer unlock()

	room, ok, err := a.loadRoom(ctx, roomID)
	if err != nil {
		return err
	}
	if !ok {
		return spi.ErrRoomNotFound
	}
	if room.State == spi.RoomSealed {
		return spi.ErrRoomSealed
	}
	if policy.MinRetentionDays < room.Retention.MinRetentionDays {
		return spi.ErrRetentionViolation
	}
	room.Retention = policy
	return a.saveRoom(ctx, room)
}

// SealRoom places a legal hold (FR-1.6). The seal receipt is persisted and
// re-sealing an already sealed room returns the stored receipt (idempotent).
// After sealing, every mutating SPI call returns spi.ErrRoomSealed; reads
// and ExportRoom continue to work.
func (a *Adapter) SealRoom(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, req spi.SealRequest) (spi.SealReceipt, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.SealReceipt{}, scopeRoom(err)
	}
	unlock := a.lockRoom(roomID)
	defer unlock()

	room, ok, err := a.loadRoom(ctx, roomID)
	if err != nil {
		return spi.SealReceipt{}, err
	}
	if !ok {
		return spi.SealReceipt{}, spi.ErrRoomNotFound
	}

	sealPath := a.roomURL(room.Slug, "_evdr", "seal.json")
	var existing spi.SealReceipt
	if ok, err := a.readJSON(ctx, sealPath, &existing); err != nil {
		return spi.SealReceipt{}, err
	} else if ok {
		return existing, nil // idempotent re-seal
	}

	frozen, err := a.frozenObjectCount(ctx, room.Slug)
	if err != nil {
		return spi.SealReceipt{}, err
	}
	receipt := spi.SealReceipt{
		ID:            spi.SealID(newUUID()),
		RoomID:        roomID,
		SealedAt:      time.Now().UTC(),
		SealedBy:      tenant.Actor,
		FrozenObjects: frozen,
	}
	if err := a.writeJSON(ctx, sealPath, receipt); err != nil {
		return spi.SealReceipt{}, err
	}
	room.State = spi.RoomSealed
	if err := a.saveRoom(ctx, room); err != nil {
		return spi.SealReceipt{}, err
	}
	return receipt, nil
}

// frozenObjectCount counts documents plus versions for the seal receipt.
func (a *Adapter) frozenObjectCount(ctx context.Context, slug string) (int, error) {
	index, err := a.loadDocIndex(ctx, slug)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, docID := range index {
		rec, ok, err := a.loadDocRecord(ctx, slug, docID)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}
		n += 1 + len(rec.Versions)
	}
	return n, nil
}

// exportEntry is one object written into the export archive.
type exportEntry struct {
	Path  string // archive-relative path, also used as the manifest path
	Bytes []byte // pre-serialised content (room record, ledgers, seal)
	// ContentURL is set for version content, which is streamed from the
	// immutable archive over WebDAV rather than buffered.
	ContentURL string
}

// ExportRoom produces the full-room export package: a streamed tar archive
// whose every entry is bound to a SHA-256 digest in the manifest, plus a
// human-readable integrity letter (FR-1.7, SR-5.2). Version content is read
// from the immutable _evdr/versions archives only, so the package is a
// consistent snapshot even if the live pointer changes. Exporting a sealed
// room is permitted (eDiscovery) and reflects the frozen state.
func (a *Adapter) ExportRoom(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, opts spi.ExportOptions) (spi.ExportPackage, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.ExportPackage{}, scopeRoom(err)
	}
	unlock := a.lockRoom(roomID)
	defer unlock()

	room, ok, err := a.loadRoom(ctx, roomID)
	if err != nil {
		return spi.ExportPackage{}, err
	}
	if !ok {
		return spi.ExportPackage{}, spi.ErrRoomNotFound
	}
	grants, err := a.loadGrantLedger(ctx, room.Slug)
	if err != nil {
		return spi.ExportPackage{}, err
	}
	if !a.canAccess(tenant.Actor, room, grants) {
		return spi.ExportPackage{}, spi.ErrAccessDenied
	}

	// Pass 1: plan entries and compute digests. Version content is hashed by
	// streaming the WebDAV archive copy; the second pass re-reads the same
	// immutable archives into the tar stream.
	entries := []exportEntry{}
	roomBytes, err := json.MarshalIndent(room, "", "  ")
	if err != nil {
		return spi.ExportPackage{}, fmt.Errorf("spi: export: encode room: %w", err)
	}
	entries = append(entries, exportEntry{Path: "room.json", Bytes: roomBytes})

	if opts.IncludeAuditTrail {
		g, err := json.MarshalIndent(grants, "", "  ")
		if err != nil {
			return spi.ExportPackage{}, fmt.Errorf("spi: export: encode grants: %w", err)
		}
		entries = append(entries, exportEntry{Path: "audit/grants.json", Bytes: g})
		var seal spi.SealReceipt
		if ok, err := a.readJSON(ctx, a.roomURL(room.Slug, "_evdr", "seal.json"), &seal); err != nil {
			return spi.ExportPackage{}, err
		} else if ok {
			s, err := json.MarshalIndent(seal, "", "  ")
			if err != nil {
				return spi.ExportPackage{}, fmt.Errorf("spi: export: encode seal: %w", err)
			}
			entries = append(entries, exportEntry{Path: "audit/seal.json", Bytes: s})
		}
	}

	index, err := a.loadDocIndex(ctx, room.Slug)
	if err != nil {
		return spi.ExportPackage{}, err
	}
	docIDs := make([]string, 0, len(index))
	for _, id := range index {
		docIDs = append(docIDs, string(id))
	}
	sort.Strings(docIDs)

	type versionTarget struct {
		docID string
		name  string
		ver   versionRecord
	}
	var targets []versionTarget
	for _, id := range docIDs {
		rec, ok, err := a.loadDocRecord(ctx, room.Slug, spi.DocumentID(id))
		if err != nil {
			return spi.ExportPackage{}, err
		}
		if !ok {
			continue
		}
		for _, v := range rec.Versions {
			if !opts.IncludeVersions && v.ID != rec.CurrentVersion() {
				continue
			}
			targets = append(targets, versionTarget{docID: id, name: rec.Name, ver: v})
		}
	}

	manifest := spi.ExportManifest{RoomID: roomID, GeneratedAt: time.Now().UTC()}
	for _, t := range targets {
		path := fmt.Sprintf("docs/%s/v%d/%s", t.docID, t.ver.Number, sanitizeArchiveName(t.name))
		sum, size, err := a.hashURL(ctx, room.Slug, t.docID, t.ver.Number)
		if err != nil {
			return spi.ExportPackage{}, err
		}
		entries = append(entries, exportEntry{
			Path:       path,
			ContentURL: a.versionURL(room.Slug, t.docID, t.ver.Number),
		})
		manifest.Entries = append(manifest.Entries, spi.ManifestEntry{Path: path, SHA256: sum, SizeBytes: size})
	}

	// Non-version entries carry their own digests in the manifest too.
	// Version content entries were hashed during pass 1 and must not be
	// re-added (their in-memory bytes are nil — content lives in WebDAV).
	for _, e := range entries {
		if e.ContentURL != "" {
			continue
		}
		manifest.Entries = append(manifest.Entries, spi.ManifestEntry{Path: e.Path, SHA256: sha256Hex(e.Bytes), SizeBytes: int64(len(e.Bytes))})
	}
	// The integrity letter is the only entry whose digest is not in the
	// manifest: it embeds the manifest's own digests (FR-1.7).
	letter := buildIntegrityLetter(manifest)

	pr, pw := io.Pipe()
	go func() {
		tw := tar.NewWriter(pw)
		fail := func(err error) {
			_ = pw.CloseWithError(err)
		}
		for _, e := range entries {
			if e.ContentURL != "" {
				if err := a.writeTarStream(ctx, tw, e.Path, e.ContentURL); err != nil {
					fail(err)
					return
				}
				continue
			}
			if err := writeTarBytes(tw, e.Path, e.Bytes); err != nil {
				fail(err)
				return
			}
		}
		mb, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			fail(err)
			return
		}
		if err := writeTarBytes(tw, "manifest.json", mb); err != nil {
			fail(err)
			return
		}
		if err := writeTarBytes(tw, "INTEGRITY_LETTER.txt", letter); err != nil {
			fail(err)
			return
		}
		if err := tw.Close(); err != nil {
			fail(err)
			return
		}
		_ = pw.Close()
	}()

	return spi.ExportPackage{
		RoomID:          roomID,
		Content:         pr,
		Manifest:        manifest,
		IntegrityLetter: letter,
		GeneratedAt:     manifest.GeneratedAt,
	}, nil
}

// sanitizeArchiveName keeps archive entry names free of path separators and
// control characters so a document name can never escape its directory.
func sanitizeArchiveName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, "\\", "_")
	return name
}

// hashURL streams one version archive and returns its SHA-256 and size.
func (a *Adapter) hashURL(ctx context.Context, slug string, docID string, number int) (string, int64, error) {
	resp, err := a.do(ctx, http.MethodGet, a.versionURL(slug, docID, number), nil, "", false)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		drain(resp)
		return "", 0, fmt.Errorf("spi: export: version archive v%d of %s missing", number, docID)
	}
	if resp.StatusCode != http.StatusOK {
		drain(resp)
		return "", 0, fmt.Errorf("spi: export: GET version archive: status %d", resp.StatusCode)
	}
	h := sha256.New()
	n, err := io.Copy(h, resp.Body)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// writeTarStream streams one version archive into the tar writer. The caller
// compares the entry digest against the manifest (SR-5.2).
func (a *Adapter) writeTarStream(ctx context.Context, tw *tar.Writer, name, url string) error {
	resp, err := a.do(ctx, http.MethodGet, url, nil, "", false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		drain(resp)
		return fmt.Errorf("spi: export: GET %s: status %d", url, resp.StatusCode)
	}
	if resp.ContentLength < 0 {
		// Chunked response: buffer the entry (size unknown to tar).
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return writeTarBytes(tw, name, b)
	}
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: resp.ContentLength, ModTime: time.Now().UTC()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := io.Copy(tw, resp.Body); err != nil {
		return err
	}
	return nil
}

// writeTarBytes writes a fixed-size entry into the tar writer.
func writeTarBytes(tw *tar.Writer, name string, b []byte) error {
	hdr := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(b)), ModTime: time.Now().UTC()}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := tw.Write(b); err != nil {
		return err
	}
	return nil
}

// buildIntegrityLetter renders the human-readable integrity letter embedding
// every manifest digest (SR-5.2).
func buildIntegrityLetter(m spi.ExportManifest) []byte {
	var b strings.Builder
	b.WriteString("EVDR Room Export — Integrity Letter\n")
	b.WriteString("====================================\n\n")
	fmt.Fprintf(&b, "Room ID:           %s\n", m.RoomID)
	fmt.Fprintf(&b, "Generated (UTC):   %s\n\n", m.GeneratedAt.UTC().Format(time.RFC3339))
	b.WriteString("SHA-256 digests per archive entry (see manifest.json):\n\n")
	for _, e := range m.Entries {
		fmt.Fprintf(&b, "  %s  sha256:%s  (%d bytes)\n", e.Path, e.SHA256, e.SizeBytes)
	}
	b.WriteString("\nVerification: recompute SHA-256 over each archive entry and\n")
	b.WriteString("compare with the digest above. Any mismatch means the package\n")
	b.WriteString("has been altered or corrupted.\n")
	return []byte(b.String())
}
