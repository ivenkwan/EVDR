package nextcloud

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// versionRecord is one immutable version of a document.
type versionRecord struct {
	ID        spi.VersionID `json:"id"`
	Number    int           `json:"number"`
	Name      string        `json:"name"` // document name at upload time
	SHA256    string        `json:"sha256"`
	SizeBytes int64         `json:"size_bytes"`
	CreatedAt time.Time     `json:"created_at"`
	CreatedBy spi.Actor     `json:"created_by"`
}

// docRecord is the per-document metadata record.
type docRecord struct {
	ID             spi.DocumentID    `json:"id"`
	RoomID         spi.RoomID        `json:"room_id"`
	Name           string            `json:"name"`
	FolderPath     string            `json:"folder_path"`
	ContentType    string            `json:"content_type"`
	Classification spi.Classification `json:"classification"`
	SizeBytes      int64             `json:"size_bytes"`
	UploadedAt     time.Time         `json:"uploaded_at"`
	UploadedBy     spi.Actor         `json:"uploaded_by"`
	Versions       []versionRecord   `json:"versions"`
}

// CurrentVersion returns the newest version record. Versions are appended in
// upload order, so the last one is the current one.
func (d *docRecord) CurrentVersion() spi.VersionID {
	if len(d.Versions) == 0 {
		return ""
	}
	return d.Versions[len(d.Versions)-1].ID
}

// docKey normalises the name+folder identity of a document within a room.
// Same key = same document = new immutable version (FR-2.2).
func docKey(folderPath, name string) string {
	folder := strings.Trim(folderPath, "/")
	if folder == "" {
		return name
	}
	return folder + "/" + name
}

// splitDocID parses "<roomID>@<uuid>" document ids back into their parts.
func splitDocID(id spi.DocumentID) (spi.RoomID, string, bool) {
	s := string(id)
	i := strings.Index(s, "@")
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return spi.RoomID(s[:i]), s[i+1:], true
}

// versionURL is the immutable archive URL of one version.
func (a *Adapter) versionURL(slug, docID string, number int) string {
	return a.roomURL(slug, "_evdr", "versions", docID, fmt.Sprintf("v%d", number))
}

// validateDocName rejects names that could widen their WebDAV path.
func validateDocName(name string) error {
	if name == "" {
		return errors.New("spi: document name must not be empty")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("spi: document name must not contain path separators: %q", name)
	}
	return nil
}

// validateFolderPath rejects ".." traversal in folder paths. The empty
// folder (room root) is valid.
func validateFolderPath(folder string) error {
	folder = strings.Trim(folder, "/")
	if folder == "" {
		return nil
	}
	for _, seg := range strings.Split(folder, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("spi: invalid document folder path %q", folder)
		}
	}
	return nil
}

// PutDocument streams a document into the room over WebDAV (FR-2.2, FR-2.5).
// Content is streamed without buffering; the SHA-256 is computed while the
// body is written. After the PUT, the new content is archived to the
// immutable version path via WebDAV COPY, so every version has a stable
// archive that ExportRoom and GetRenderStream read. The live
// docs/<folderPath>/<name> file is only ever the "current pointer".
func (a *Adapter) PutDocument(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, doc spi.PutDocumentRequest) (spi.Document, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.Document{}, scopeRoom(err)
	}
	if err := validateDocName(doc.Name); err != nil {
		return spi.Document{}, err
	}
	if err := validateFolderPath(doc.FolderPath); err != nil {
		return spi.Document{}, err
	}
	unlock := a.lockRoom(roomID)
	defer unlock()

	room, ok, err := a.loadRoom(ctx, roomID)
	if err != nil {
		return spi.Document{}, err
	}
	if !ok {
		return spi.Document{}, spi.ErrRoomNotFound
	}
	if room.State == spi.RoomSealed {
		return spi.Document{}, spi.ErrRoomSealed
	}

	index, err := a.loadDocIndex(ctx, room.Slug)
	if err != nil {
		return spi.Document{}, err
	}
	key := docKey(doc.FolderPath, doc.Name)

	rec, isNew, err := func() (docRecord, bool, error) {
		if docID, ok := index[key]; ok {
			r, ok, err := a.loadDocRecord(ctx, room.Slug, docID)
			if err != nil {
				return docRecord{}, false, err
			}
			if !ok {
				return docRecord{}, false, fmt.Errorf("spi: doc index points at missing record %s", docID)
			}
			return r, false, nil
		}
		return docRecord{
			ID:         spi.DocumentID(string(roomID) + "@" + newUUID()),
			RoomID:     roomID,
			Name:       doc.Name,
			FolderPath: strings.Trim(doc.FolderPath, "/"),
		}, true, nil
	}()
	if err != nil {
		return spi.Document{}, err
	}

	// Ensure parent collections exist (docs/ plus any nested folders).
	folderParts := splitFolderParts(doc.FolderPath)
	for i := range folderParts {
		segs := append([]string{room.Slug, "docs"}, folderParts[:i+1]...)
		if err := a.mkcol(ctx, a.roomURL(segs...)); err != nil {
			return spi.Document{}, err
		}
	}

	// PUT the content, hashing while streaming (no whole-payload buffering).
	liveURL := a.roomURL(append([]string{room.Slug, "docs"}, append(folderParts, doc.Name)...)...)
	hasher := sha256.New()
	counter := &countingReader{r: io.TeeReader(doc.Content, hasher)}
	contentType := doc.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	resp, err := a.do(ctx, http.MethodPut, liveURL, counter, contentType, false)
	if err != nil {
		return spi.Document{}, err
	}
	switch resp.StatusCode {
	case http.StatusCreated, http.StatusNoContent, http.StatusOK:
		drain(resp)
	case http.StatusInsufficientStorage:
		drain(resp)
		return spi.Document{}, spi.ErrQuotaExceeded
	case http.StatusConflict:
		drain(resp)
		return spi.Document{}, fmt.Errorf("spi: put document: WebDAV conflict (missing parent folder?)")
	default:
		drain(resp)
		return spi.Document{}, fmt.Errorf("spi: put document: status %d", resp.StatusCode)
	}
	size := counter.n
	hash := hex.EncodeToString(hasher.Sum(nil))

	// Archive the new content as the immutable next version.
	number := len(rec.Versions) + 1
	// Ensure the archive collections exist (real Nextcloud requires the
	// COPY destination parent to exist).
	for _, segs := range [][]string{
		{room.Slug, "_evdr", "versions"},
		{room.Slug, "_evdr", "versions", string(rec.ID)},
	} {
		if err := a.mkcol(ctx, a.roomURL(segs...)); err != nil {
			return spi.Document{}, err
		}
	}
	dest := a.versionURL(room.Slug, string(rec.ID), number)
	copyResp, err := a.doWithHeader(ctx, "COPY", liveURL, "Destination", dest)
	if err != nil {
		return spi.Document{}, err
	}
	switch copyResp.StatusCode {
	case http.StatusCreated, http.StatusNoContent:
		drain(copyResp)
	case http.StatusInsufficientStorage:
		drain(copyResp)
		return spi.Document{}, spi.ErrQuotaExceeded
	default:
		drain(copyResp)
		return spi.Document{}, fmt.Errorf("spi: put document: archive COPY v%d: status %d", number, copyResp.StatusCode)
	}

	classification := doc.Classification
	if classification == "" {
		classification = room.Classification
		if classification == "" {
			classification = spi.DefaultClassification
		}
	}
	rec.ContentType = contentType
	rec.Classification = classification
	rec.SizeBytes = size
	rec.UploadedAt = time.Now().UTC()
	rec.UploadedBy = tenant.Actor
	ver := versionRecord{
		ID:        spi.VersionID(fmt.Sprintf("%s@%d", rec.ID, number)),
		Number:    number,
		Name:      doc.Name,
		SHA256:    hash,
		SizeBytes: size,
		CreatedAt: time.Now().UTC(),
		CreatedBy: tenant.Actor,
	}
	rec.Versions = append(rec.Versions, ver)
	if err := a.saveDocRecord(ctx, room.Slug, rec); err != nil {
		return spi.Document{}, err
	}
	if isNew {
		index[key] = rec.ID
		if err := a.saveDocIndex(ctx, room.Slug, index); err != nil {
			return spi.Document{}, err
		}
	}

	return spi.Document{
		ID:             rec.ID,
		RoomID:         rec.RoomID,
		Name:           rec.Name,
		FolderPath:     rec.FolderPath,
		ContentType:    rec.ContentType,
		Classification: rec.Classification,
		CurrentVersion: ver.ID,
		SizeBytes:      size,
		UploadedAt:     rec.UploadedAt,
		UploadedBy:     rec.UploadedBy,
	}, nil
}

// doWithHeader issues one authenticated request with an extra header.
func (a *Adapter) doWithHeader(ctx context.Context, method, url, header, value string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("spi: %s %s: %w", method, url, err)
	}
	req.Header.Set("Authorization", a.basicAuth())
	req.Header.Set(header, value)
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("spi: %s %s: %w", method, url, err)
	}
	return resp, nil
}

// splitFolderParts returns the folder path segments (empty for root).
func splitFolderParts(folder string) []string {
	folder = strings.Trim(folder, "/")
	if folder == "" {
		return nil
	}
	return strings.Split(folder, "/")
}

// loadDocIndex reads the room's name+folder → document id index.
func (a *Adapter) loadDocIndex(ctx context.Context, slug string) (map[string]spi.DocumentID, error) {
	index := map[string]spi.DocumentID{}
	if _, err := a.readJSON(ctx, a.roomURL(slug, "_evdr", "docs.json"), &index); err != nil {
		return nil, err
	}
	return index, nil
}

// saveDocIndex persists the room's document index.
func (a *Adapter) saveDocIndex(ctx context.Context, slug string, index map[string]spi.DocumentID) error {
	return a.writeJSON(ctx, a.roomURL(slug, "_evdr", "docs.json"), index)
}

// loadDocRecord reads one document record.
func (a *Adapter) loadDocRecord(ctx context.Context, slug string, docID spi.DocumentID) (docRecord, bool, error) {
	var rec docRecord
	ok, err := a.readJSON(ctx, a.roomURL(slug, "_evdr", "docs", string(docID)+".json"), &rec)
	if err != nil {
		return rec, false, err
	}
	if !ok {
		return rec, false, nil
	}
	return rec, true, nil
}

// saveDocRecord persists one document record.
func (a *Adapter) saveDocRecord(ctx context.Context, slug string, rec docRecord) error {
	return a.writeJSON(ctx, a.roomURL(slug, "_evdr", "docs", string(rec.ID)+".json"), rec)
}

// resolveDoc locates a document's room and record by its self-describing id.
func (a *Adapter) resolveDoc(ctx context.Context, docID spi.DocumentID) (spi.Room, docRecord, bool, error) {
	roomID, _, ok := splitDocID(docID)
	if !ok {
		return spi.Room{}, docRecord{}, false, nil
	}
	room, ok, err := a.loadRoom(ctx, roomID)
	if err != nil || !ok {
		return spi.Room{}, docRecord{}, false, err
	}
	rec, ok, err := a.loadDocRecord(ctx, room.Slug, docID)
	if err != nil || !ok {
		return spi.Room{}, docRecord{}, false, err
	}
	return room, rec, true, nil
}

// resolveVersion returns the requested version record; the empty VersionID
// means the current version.
func (rec docRecord) resolveVersion(id spi.VersionID) (versionRecord, bool) {
	if id == "" {
		return rec.Versions[len(rec.Versions)-1], true
	}
	for _, v := range rec.Versions {
		if v.ID == id {
			return v, true
		}
	}
	return versionRecord{}, false
}

// GetRenderStream opens a view-scoped, forward-only stream of rendered pages
// for one document version (FR-3.1). The actor must hold an active grant on
// the document's room (or be the room creator); otherwise spi.ErrAccessDenied
// is returned and no content is fetched. Rendering is delegated to the
// configured Renderer (viewer pipeline); the adapter transports already
// rendered pages and never exposes a whole-document payload through this
// method.
func (a *Adapter) GetRenderStream(ctx context.Context, tenant spi.TenantContext, docID spi.DocumentID, req spi.RenderRequest) (spi.RenderStream, error) {
	if err := a.checkTenant(tenant); err != nil {
		return nil, scopeDoc(err)
	}
	unlock := a.lockRoom(roomIDOf(docID))
	defer unlock()

	room, rec, ok, err := a.resolveDoc(ctx, docID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, spi.ErrDocumentNotFound
	}
	grants, err := a.loadGrantLedger(ctx, room.Slug)
	if err != nil {
		return nil, err
	}
	if !a.canAccess(tenant.Actor, room, grants) {
		return nil, spi.ErrAccessDenied
	}
	ver, ok := rec.resolveVersion(req.Version)
	if !ok {
		// Unknown version id: the document (as referenced) does not exist.
		return nil, spi.ErrDocumentNotFound
	}
	if a.cfg.Renderer == nil {
		return nil, fmt.Errorf("%w: GetRenderStream: no Renderer configured; wire the viewer pipeline's renderer at adapter construction", spi.ErrUnsupported)
	}

	contentURL := a.versionURL(room.Slug, string(rec.ID), ver.Number)
	resp, err := a.do(ctx, http.MethodGet, contentURL, nil, "", false)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		drain(resp)
		return nil, spi.ErrDocumentNotFound
	}
	if resp.StatusCode != http.StatusOK {
		drain(resp)
		return nil, fmt.Errorf("spi: render stream: GET version %s: status %d", ver.ID, resp.StatusCode)
	}

	stream, err := a.cfg.Renderer.Render(ctx, resp.Body, rec.ContentType, req)
	if err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	// The renderer owns resp.Body from here (it must close it via stream
	// Close); the wrapper guarantees the cancellation contract.
	return &ctxRenderStream{ctx: ctx, RenderStream: stream}, nil
}

// roomIDOf extracts the room id from a self-describing document id. For a
// malformed id the empty RoomID is fine — resolveDoc reports not-found.
func roomIDOf(docID spi.DocumentID) spi.RoomID {
	roomID, _, _ := splitDocID(docID)
	return roomID
}

// ListVersions returns all immutable versions of a document ordered by
// Version.Number ascending (FR-2.2). An unknown document returns
// spi.ErrDocumentNotFound. Read access is gated like GetRenderStream.
func (a *Adapter) ListVersions(ctx context.Context, tenant spi.TenantContext, docID spi.DocumentID) ([]spi.Version, error) {
	if err := a.checkTenant(tenant); err != nil {
		return nil, scopeDoc(err)
	}
	unlock := a.lockRoom(roomIDOf(docID))
	defer unlock()

	room, rec, ok, err := a.resolveDoc(ctx, docID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, spi.ErrDocumentNotFound
	}
	grants, err := a.loadGrantLedger(ctx, room.Slug)
	if err != nil {
		return nil, err
	}
	if !a.canAccess(tenant.Actor, room, grants) {
		return nil, spi.ErrAccessDenied
	}

	versions := make([]spi.Version, 0, len(rec.Versions))
	for _, v := range rec.Versions {
		versions = append(versions, spi.Version{
			ID:         v.ID,
			DocumentID: rec.ID,
			Number:     v.Number,
			SHA256:     v.SHA256,
			SizeBytes:  v.SizeBytes,
			CreatedAt:  v.CreatedAt,
			CreatedBy:  v.CreatedBy,
		})
	}
	sort.SliceStable(versions, func(i, j int) bool { return versions[i].Number < versions[j].Number })
	return versions, nil
}
