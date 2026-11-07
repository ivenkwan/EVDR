package room

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ivenkwan/evdr/src/spi"
)

// ---------------------------------------------------------------------------
// Documents: upload / list / get / versions (FR-2.1 upload, FR-2.2 version
// tracking, FR-2.3 folder hierarchy — all via the SPI)
// ---------------------------------------------------------------------------

// requireDocument resolves the principal's projected document record scoped
// to its room and answers 404 document_not_found when missing. Document
// scoping mirrors the SPI: an id that exists in another room (or tenant) is
// indistinguishable from a miss.
func (s *Server) requireDocument(w http.ResponseWriter, r *http.Request, p Principal, action string, roomID spi.RoomID, docID spi.DocumentID) (spi.Document, bool) {
	doc, found := s.catalog.Document(p.TenantID, roomID, docID)
	if !found {
		herr := mapSPIError(spi.ErrDocumentNotFound)
		s.emit(r, p, action, string(docID), herr.Code, map[string]string{"room_id": string(roomID)})
		s.writeError(w, r, herr)
		return spi.Document{}, false
	}
	return doc, true
}

// handleListDocuments lists the room's documents from the projection.
func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	if _, found := s.requireRoom(w, r, p, "doc.list", roomID); !found {
		return
	}
	docs := s.catalog.Documents(p.TenantID, roomID)
	out := make([]WireDocument, 0, len(docs))
	for _, d := range docs {
		out = append(out, wireDocument(d))
	}
	s.emit(r, p, "doc.list", string(roomID), "ok", map[string]string{"count": itoa(len(out))})
	writeJSON(w, http.StatusOK, map[string]any{"documents": out})
}

// handleGetDocument returns the projected document record.
func (s *Server) handleGetDocument(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	docID := spi.DocumentID(r.PathValue("documentID"))
	doc, found := s.requireDocument(w, r, p, "doc.get", roomID, docID)
	if !found {
		return
	}
	s.emit(r, p, "doc.get", string(docID), "ok", nil)
	writeJSON(w, http.StatusOK, wireDocument(doc))
}

// handleUploadDocument accepts a multipart/form-data upload for a room:
//
//	file:       the document content (required; filename and content-type
//	           come from the part)
//	name:       optional explicit document name (defaults to the file name)
//	folderPath: optional slash-separated folder within the room (FR-2.3)
//	classification: optional classification label (inherits the room floor
//	           when absent — resolved by the SPI)
//
// The body is capped at the server's MaxUploadBytes (FR-2.1 upstream size
// validation; the client additionally validates extension + size). Content
// streams to the SPI (spi.PutDocument): uploading the same name+folder
// creates the next immutable version instead of overwriting (FR-2.2).
func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	room, found := s.requireRoom(w, r, p, "doc.upload", roomID)
	if !found {
		return
	}
	if r.Header.Get("Content-Type") == "" || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		herr := badRequest("upload must be multipart/form-data")
		s.emit(r, p, "doc.upload", string(roomID), herr.Code, nil)
		s.writeError(w, r, herr)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.maxUploadBytes)
	// 8 MiB stays in memory; larger parts spool to disk. The authoritative
	// ceiling above is enforced while parsing.
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		herr := &httpError{Status: http.StatusBadRequest, Code: codeBadRequest, Err: err}
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			herr = httpErr(http.StatusRequestEntityTooLarge, codePayloadTooLarge, err)
		}
		s.emit(r, p, "doc.upload", string(roomID), herr.Code, nil)
		s.writeError(w, r, herr)
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck // best-effort temp cleanup

	name := strings.TrimSpace(r.FormValue("name"))
	folderPath := strings.TrimSpace(r.FormValue("folderPath"))
	classification := strings.ToUpper(strings.TrimSpace(r.FormValue("classification")))
	if !validClassifications[classification] {
		herr := badRequest(fmt.Sprintf("classification must be one of PUBLIC, INTERNAL, CONFIDENTIAL, RESTRICTED (got %q)", r.FormValue("classification")))
		s.writeError(w, r, herr)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		herr := badRequest("a single 'file' part is required: " + err.Error())
		s.emit(r, p, "doc.upload", string(roomID), herr.Code, nil)
		s.writeError(w, r, herr)
		return
	}
	defer file.Close()
	if header == nil || header.Filename == "" {
		herr := badRequest("file part must carry a filename")
		s.writeError(w, r, herr)
		return
	}
	if name == "" {
		name = header.Filename
	}
	if err := validateDocumentName(name); err != nil {
		herr := badRequest(err.Error())
		s.emit(r, p, "doc.upload", string(roomID), herr.Code, map[string]string{"name": name})
		s.writeError(w, r, herr)
		return
	}
	if !validFolderPath(folderPath) {
		herr := badRequest("folderPath must not contain '\\' and must be slash-separated")
		s.writeError(w, r, herr)
		return
	}
	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	tc := spi.TenantContext{TenantID: p.TenantID, Actor: p.Actor}
	doc, err := s.spi.PutDocument(r.Context(), tc, roomID, spi.PutDocumentRequest{
		Name:           name,
		FolderPath:     folderPath,
		ContentType:    contentType,
		Content:        file,
		SizeBytes:      header.Size,
		Classification: spi.Classification(classification),
		Metadata:       map[string]string{"room_name": room.Name},
	})
	if err != nil {
		herr := mapSPIError(err)
		s.emit(r, p, "doc.upload", string(roomID), herr.Code, map[string]string{"name": name})
		s.writeError(w, r, herr)
		return
	}
	s.catalog.RecordDocument(p.TenantID, doc)
	s.emit(r, p, "doc.upload", string(doc.ID), "ok", map[string]string{
		"room_id": string(roomID), "name": doc.Name,
	})
	writeJSON(w, http.StatusCreated, wireDocument(doc))
}

// handleListVersions lists every immutable version of one document through
// the SPI (spi.ListVersions, FR-2.2), ordered ascending. This is a live SPI
// read — never served from the projection.
func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	docID := spi.DocumentID(r.PathValue("documentID"))
	if _, found := s.requireRoom(w, r, p, "doc.versions", roomID); !found {
		return
	}
	if _, found := s.requireDocument(w, r, p, "doc.versions", roomID, docID); !found {
		return
	}
	tc := spi.TenantContext{TenantID: p.TenantID, Actor: p.Actor}
	versions, err := s.spi.ListVersions(r.Context(), tc, docID)
	if err != nil {
		herr := mapSPIError(err)
		s.emit(r, p, "doc.versions", string(docID), herr.Code, nil)
		s.writeError(w, r, herr)
		return
	}
	out := make([]WireVersion, 0, len(versions))
	for _, v := range versions {
		out = append(out, wireVersion(v))
	}
	s.emit(r, p, "doc.versions", string(docID), "ok", map[string]string{"count": itoa(len(out))})
	writeJSON(w, http.StatusOK, map[string]any{"versions": out})
}

// validateDocumentName rejects names that would escape their folder or be
// ambiguous in the adapter's key space (the SPI adapters apply the same
// rule; this is the friendly early check).
func validateDocumentName(name string) error {
	if name == "" {
		return errors.New("document name is required")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("document name %q must not contain path separators", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("document name %q must not start with '.'", name)
	}
	return nil
}

// validFolderPath allows empty (room root) or a slash-separated relative
// path without backslashes or leading/trailing slashes.
func validFolderPath(f string) bool {
	if f == "" {
		return true
	}
	if strings.ContainsAny(f, "\\") || strings.HasPrefix(f, "/") || strings.HasSuffix(f, "/") {
		return false
	}
	for _, seg := range strings.Split(f, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}
