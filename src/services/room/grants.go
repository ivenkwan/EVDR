package room

import (
	"net/http"

	"github.com/ivenkwan/evdr/src/spi"
)

// ---------------------------------------------------------------------------
// Grants: grant / revoke / list (FR-4.4 grant lifecycle via the SPI)
// ---------------------------------------------------------------------------

// handleListGrants lists the room's active grants from the projection (the
// SPI has no grant enumeration).
func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	if _, found := s.requireRoom(w, r, p, "grant.list", roomID); !found {
		return
	}
	grants := s.catalog.Grants(p.TenantID, roomID)
	out := make([]WireGrant, 0, len(grants))
	for _, g := range grants {
		out = append(out, wireGrant(g))
	}
	s.emit(r, p, "grant.list", string(roomID), "ok", map[string]string{"count": itoa(len(out))})
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}

// handleCreateGrant authorises a subject through the SPI (spi.GrantAccess).
// Contract semantics the SPI enforces — e.g. guest grants must carry an
// expiry (spi.ErrInvalidGrant) — surface as the SPI's own errors. The grant
// the SPI confirms is recorded in the projection.
func (s *Server) handleCreateGrant(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	if _, found := s.requireRoom(w, r, p, "grant.create", roomID); !found {
		return
	}
	var body WireGrantRequest
	if err := readJSON(w, r, &body, 64<<10); err != nil {
		writeError(w, r, err)
		return
	}
	spiGrant, herr := body.toSpiGrant()
	if herr != nil {
		writeError(w, r, herr)
		return
	}
	tc := spi.TenantContext{TenantID: p.TenantID, Actor: p.Actor}
	created, err := s.spi.GrantAccess(r.Context(), tc, roomID, spiGrant)
	if err != nil {
		herr := mapSPIError(err)
		s.emit(r, p, "grant.create", string(roomID), herr.Code, map[string]string{"subject": spiGrant.Subject})
		writeError(w, r, herr)
		return
	}
	s.catalog.RecordGrant(p.TenantID, roomID, created)
	s.emit(r, p, "grant.create", string(created.ID), "ok", map[string]string{
		"room_id": string(roomID), "subject": created.Subject, "tier": created.Tier.String(),
	})
	writeJSON(w, http.StatusCreated, wireGrant(created))
}

// handleRevokeGrant withdraws a grant immediately through the SPI
// (spi.RevokeAccess, FR-4.4). Idempotent-safe: re-revoking an already
// revoked grant returns 204. Unknown grants surface the SPI's
// ErrGrantNotFound → 404.
func (s *Server) handleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	p, ok := s.principal(w, r)
	if !ok {
		return
	}
	roomID := spi.RoomID(r.PathValue("roomID"))
	grantID := spi.GrantID(r.PathValue("grantID"))
	if _, found := s.requireRoom(w, r, p, "grant.revoke", roomID); !found {
		return
	}
	tc := spi.TenantContext{TenantID: p.TenantID, Actor: p.Actor}
	if err := s.spi.RevokeAccess(r.Context(), tc, roomID, grantID); err != nil {
		herr := mapSPIError(err)
		s.emit(r, p, "grant.revoke", string(grantID), herr.Code, map[string]string{"room_id": string(roomID)})
		writeError(w, r, herr)
		return
	}
	s.catalog.RevokeGrant(p.TenantID, roomID, grantID)
	s.emit(r, p, "grant.revoke", string(grantID), "ok", map[string]string{"room_id": string(roomID)})
	w.WriteHeader(http.StatusNoContent)
}
