package room

import (
	"net/http"

	"github.com/ivenkwan/evdr/src/spi"
)

// handlePublicBranding serves the external portal's branded room surface
// (FR-1.1). It returns ONLY public-safe presentation metadata — room name,
// slug, state and branding (logo object key, theme overrides, about page).
// No documents, no grants, no retention, no creator/audit data: the external
// guest never sees management surface. Room state is included so the portal
// can honestly tell a guest whether a room is active.
//
// The endpoint still authenticates (service-to-service): the portal BFF
// holds the shared dev secret today, production will authenticate the portal
// as a service principal (TR-6.x).
func (s *Server) handlePublicBranding(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	room, found := s.catalog.Room(p.TenantID, roomID)
	if !found {
		herr := mapSPIError(spi.ErrRoomNotFound)
		s.emit(r, p, "branding.public", string(roomID), herr.Code, nil)
		writeError(w, r, herr)
		return
	}
	s.emit(r, p, "branding.public", string(roomID), "ok", nil)
	writeJSON(w, http.StatusOK, wirePublicRoom(room))
}
