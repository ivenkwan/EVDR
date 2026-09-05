package room

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ivenkwan/evdr/src/spi"
)

// requireRoom resolves the principal's projected room record and answers 404
// room_not_found (the SPI's own code for a missing/wrong-tenant room) when it
// does not exist. Every room-scoped handler starts here.
func (s *Server) requireRoom(w http.ResponseWriter, r *http.Request, p Principal, action string, roomID spi.RoomID) (spi.Room, bool) {
	room, found := s.catalog.Room(p.TenantID, roomID)
	if !found {
		herr := mapSPIError(spi.ErrRoomNotFound)
		s.emit(r, p, action, string(roomID), herr.Code, nil)
		writeError(w, r, herr)
		return spi.Room{}, false
	}
	return room, true
}

// handleCreateRoom provisions a room through the SPI (spi.CreateRoom). The
// SPI enforces tenant scope; a principal whose tenant does not match the
// bound adapter surfaces as spi.ErrAccessDenied → 403. The returned room is
// recorded in the projection so list/get can answer (the SPI has no room
// read-back).
func (s *Server) handleCreateRoom(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	var body WireCreateRoomRequest
	if err := readJSON(w, r, &body, 64<<10); err != nil {
		writeError(w, r, err)
		return
	}
	req, herr := body.toSpiCreateRequest()
	if herr != nil {
		writeError(w, r, herr)
		return
	}
	tc := spi.TenantContext{TenantID: p.TenantID, Actor: p.Actor}
	room, err := s.spi.CreateRoom(r.Context(), tc, req)
	if err != nil {
		herr := mapSPIError(err)
		s.emit(r, p, "room.create", "", herr.Code, map[string]string{"slug": req.Slug})
		writeError(w, r, herr)
		return
	}
	s.catalog.RecordRoom(p.TenantID, room)
	s.emit(r, p, "room.create", string(room.ID), "ok", map[string]string{"slug": room.Slug})
	writeJSON(w, http.StatusCreated, wireRoom(room))
}

// handleListRooms lists the principal's tenant rooms from the projection.
// The frozen SPI has no enumeration method, so this answers from records the
// service itself created via the SPI (see catalog.go).
func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	rooms := s.catalog.Rooms(p.TenantID)
	out := make([]WireRoom, 0, len(rooms))
	for _, rm := range rooms {
		out = append(out, wireRoom(rm))
	}
	s.emit(r, p, "room.list", "-", "ok", map[string]string{"count": itoa(len(out))})
	writeJSON(w, http.StatusOK, map[string]any{"rooms": out})
}

// handleGetRoom returns the projected room record.
func (s *Server) handleGetRoom(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	room, found := s.requireRoom(w, r, p, "room.get", roomID)
	if !found {
		return
	}
	s.emit(r, p, "room.get", string(roomID), "ok", nil)
	writeJSON(w, http.StatusOK, wireRoom(room))
}

// handleSealRoom places a legal hold through the SPI (spi.SealRoom, FR-1.6)
// and reflects the sealed state in the projection.
func (s *Server) handleSealRoom(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	room, found := s.requireRoom(w, r, p, "room.seal", roomID)
	if !found {
		return
	}
	var body WireSealRequest
	if err := readJSON(w, r, &body, 64<<10); err != nil {
		writeError(w, r, err)
		return
	}
	tc := spi.TenantContext{TenantID: p.TenantID, Actor: p.Actor}
	receipt, err := s.spi.SealRoom(r.Context(), tc, roomID, body.toSpiSeal())
	if err != nil {
		herr := mapSPIError(err)
		s.emit(r, p, "room.seal", string(roomID), herr.Code, nil)
		writeError(w, r, herr)
		return
	}
	room.State = spi.RoomSealed
	s.catalog.UpdateRoomState(p.TenantID, room)
	s.catalog.RecordSeal(p.TenantID, receipt)
	s.emit(r, p, "room.seal", string(roomID), "ok", nil)
	writeJSON(w, http.StatusOK, wireSealReceipt(receipt))
}

// handleRetention applies a retention policy through the SPI
// (spi.ApplyRetention, FR-5.5 hook). Policy floors are the SPI's defence in
// depth; fine-grained validation belongs to the policy engine (later wave).
func (s *Server) handleRetention(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	room, found := s.requireRoom(w, r, p, "room.retention", roomID)
	if !found {
		return
	}
	var body WireRetention
	if err := readJSON(w, r, &body, 64<<10); err != nil {
		writeError(w, r, err)
		return
	}
	policy, herr := body.toSpiRetention()
	if herr != nil {
		writeError(w, r, herr)
		return
	}
	tc := spi.TenantContext{TenantID: p.TenantID, Actor: p.Actor}
	if err := s.spi.ApplyRetention(r.Context(), tc, roomID, policy); err != nil {
		herr := mapSPIError(err)
		s.emit(r, p, "room.retention", string(roomID), herr.Code, nil)
		writeError(w, r, herr)
		return
	}
	room.Retention = policy
	s.catalog.UpdateRoomState(p.TenantID, room)
	s.emit(r, p, "room.retention", string(roomID), "ok", nil)
	writeJSON(w, http.StatusOK, wireRoom(room))
}

// handleExportRoom streams the full-room export package (spi.ExportRoom,
// FR-1.7/SR-5.2). The response body is the raw archive (application/x-tar),
// not JSON.
func (s *Server) handleExportRoom(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	if _, found := s.requireRoom(w, r, p, "room.export", roomID); !found {
		return
	}
	var body WireExportRequest
	if err := readJSON(w, r, &body, 64<<10); err != nil {
		writeError(w, r, err)
		return
	}
	tc := spi.TenantContext{TenantID: p.TenantID, Actor: p.Actor}
	pkg, err := s.spi.ExportRoom(r.Context(), tc, roomID, body.toSpiExport())
	if err != nil {
		herr := mapSPIError(err)
		s.emit(r, p, "room.export", string(roomID), herr.Code, nil)
		writeError(w, r, herr)
		return
	}
	defer pkg.Content.Close()
	s.emit(r, p, "room.export", string(roomID), "ok", map[string]string{"entries": itoa(len(pkg.Manifest.Entries))})
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", `attachment; filename="evdr-room-`+string(roomID)+`-export.tar"`)
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, pkg.Content); err != nil && r.Context().Err() == nil {
		s.logger.Error("export stream failed", "request_id", requestID(r), "room_id", string(roomID), "error", err)
	}
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
