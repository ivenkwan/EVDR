package nextcloud

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// grantRecord is one ledger entry: the SPI grant plus the OCS share it maps
// to and the revocation flag.
type grantRecord struct {
	Grant      spi.AccessGrant `json:"grant"`
	OCSShareID int             `json:"ocs_share_id,omitempty"`
	Revoked    bool            `json:"revoked,omitempty"`
}

// grantLedger is the room's access ledger at <slug>/_evdr/grants.json.
type grantLedger struct {
	Grants map[spi.GrantID]grantRecord `json:"grants"`
}

// permissionBits maps a SPI permission tier onto Nextcloud OCS share
// permission bits (1=read, 2=update, 4=create, 8=delete, 16=share).
//
// Documented limitation: OCS has no download/print permission bit, so
// TierViewOnly, TierDownloadAllowed and TierPrintAllowed all map to read (1).
// Download and print enforcement happens in the viewer pipeline — the SPI
// only ever transports rendered pages (FR-3.1) — and in the upstream policy
// engine, not in the share bits.
func permissionBits(tier spi.PermissionTier) int {
	switch tier {
	case spi.TierEditAllowed:
		return 1 | 2 | 4 | 8 // read + update + create + delete (no share)
	default:
		return 1 // read
	}
}

// GrantAccess authorises a subject on a room at a permission tier. The grant
// is recorded in the room ledger and materialised as an OCS share: a user
// share (shareType 0) for internal users and services, a public-link share
// (shareType 3) with expiry for guests (FR-4.3 — guests have no account).
func (a *Adapter) GrantAccess(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, grant spi.AccessGrant) (spi.AccessGrant, error) {
	if err := a.checkTenant(tenant); err != nil {
		return spi.AccessGrant{}, scopeRoom(err)
	}
	unlock := a.lockRoom(roomID)
	defer unlock()

	room, ok, err := a.loadRoom(ctx, roomID)
	if err != nil {
		return spi.AccessGrant{}, err
	}
	if !ok {
		return spi.AccessGrant{}, spi.ErrRoomNotFound
	}
	if room.State == spi.RoomSealed {
		return spi.AccessGrant{}, spi.ErrRoomSealed
	}
	if grant.ActorKind == spi.ActorGuest && grant.Constraints.NotAfter.IsZero() {
		return spi.AccessGrant{}, spi.ErrInvalidGrant
	}
	if !grant.Constraints.NotAfter.IsZero() && !grant.Constraints.NotBefore.IsZero() &&
		grant.Constraints.NotBefore.After(grant.Constraints.NotAfter) {
		return spi.AccessGrant{}, spi.ErrInvalidGrant
	}

	ledger, err := a.loadGrantLedger(ctx, room.Slug)
	if err != nil {
		return spi.AccessGrant{}, err
	}

	// Materialise the OCS share.
	form := url.Values{}
	form.Set("path", "/evdr/"+room.Slug)
	form.Set("permissions", strconv.Itoa(permissionBits(grant.Tier)))
	switch grant.ActorKind {
	case spi.ActorGuest:
		// Expiring public link (FR-4.3); expiry mirrors NotAfter so the
		// share dies with the grant.
		form.Set("shareType", "3")
		form.Set("name", grant.Subject)
		if !grant.Constraints.NotAfter.IsZero() {
			form.Set("expireDate", grant.Constraints.NotAfter.UTC().Format("2006-01-02"))
		}
	default:
		form.Set("shareType", "0")
		form.Set("shareWith", grant.Subject)
	}

	resp, err := a.do(ctx, http.MethodPost, a.ocsSharesURL(), strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", true)
	if err != nil {
		return spi.AccessGrant{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		drain(resp)
		return spi.AccessGrant{}, fmt.Errorf("spi: grant access: OCS create share: status %d", resp.StatusCode)
	}
	ocs, err := parseOCS(resp.Body)
	if err != nil {
		return spi.AccessGrant{}, fmt.Errorf("spi: grant access: %w", err)
	}
	switch ocs.Meta.StatusCode {
	case 100:
		// ok
	case 404:
		return spi.AccessGrant{}, spi.ErrRoomNotFound
	case 507:
		return spi.AccessGrant{}, spi.ErrQuotaExceeded
	default:
		return spi.AccessGrant{}, fmt.Errorf("spi: grant access: OCS error %d: %s", ocs.Meta.StatusCode, ocs.Meta.Message)
	}

	rec := grantRecord{
		Grant: spi.AccessGrant{
			ID:          spi.GrantID(newUUID()),
			Subject:     grant.Subject,
			ActorKind:   grant.ActorKind,
			Tier:        grant.Tier,
			Constraints: grant.Constraints,
			CreatedAt:   time.Now().UTC(),
			CreatedBy:   tenant.Actor,
		},
		OCSShareID: ocs.Data.ID,
	}
	ledger.Grants[rec.Grant.ID] = rec
	if err := a.saveGrantLedger(ctx, room.Slug, ledger); err != nil {
		return spi.AccessGrant{}, err
	}
	return rec.Grant, nil
}

// RevokeAccess withdraws a grant immediately (FR-4.4): the OCS share is
// deleted and the ledger entry marked revoked, so no new render stream can
// open. Revoking an unknown grant returns spi.ErrGrantNotFound; revoking an
// already-revoked grant returns nil (idempotent-safe). Unlike other
// mutations, revocation is deliberately permitted on sealed rooms — legal
// hold freezes documents and metadata, it must never prevent withdrawing
// access to them.
func (a *Adapter) RevokeAccess(ctx context.Context, tenant spi.TenantContext, roomID spi.RoomID, grantID spi.GrantID) error {
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

	ledger, err := a.loadGrantLedger(ctx, room.Slug)
	if err != nil {
		return err
	}
	rec, ok := ledger.Grants[grantID]
	if !ok {
		return spi.ErrGrantNotFound
	}
	if rec.Revoked {
		return nil // idempotent-safe
	}

	if rec.OCSShareID != 0 {
		shareURL := a.ocsSharesURL() + "/" + strconv.Itoa(rec.OCSShareID)
		resp, err := a.do(ctx, http.MethodDelete, shareURL, nil, "", true)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			drain(resp)
			return fmt.Errorf("spi: revoke access: OCS delete share: status %d", resp.StatusCode)
		}
		ocs, err := parseOCS(resp.Body)
		if err != nil {
			return fmt.Errorf("spi: revoke access: %w", err)
		}
		switch ocs.Meta.StatusCode {
		case 100:
			// ok
		case 404:
			// Share already gone — treat as revoked (idempotent-safe).
		default:
			return fmt.Errorf("spi: revoke access: OCS error %d: %s", ocs.Meta.StatusCode, ocs.Meta.Message)
		}
	}

	rec.Revoked = true
	ledger.Grants[grantID] = rec
	return a.saveGrantLedger(ctx, room.Slug, ledger)
}

// loadGrantLedger reads the room's grant ledger; a missing ledger is empty.
func (a *Adapter) loadGrantLedger(ctx context.Context, slug string) (grantLedger, error) {
	ledger := grantLedger{Grants: map[spi.GrantID]grantRecord{}}
	if _, err := a.readJSON(ctx, a.roomURL(slug, "_evdr", "grants.json"), &ledger); err != nil {
		return ledger, err
	}
	if ledger.Grants == nil {
		ledger.Grants = map[spi.GrantID]grantRecord{}
	}
	return ledger, nil
}

// saveGrantLedger persists the room's grant ledger.
func (a *Adapter) saveGrantLedger(ctx context.Context, slug string, ledger grantLedger) error {
	if ledger.Grants == nil {
		ledger.Grants = map[spi.GrantID]grantRecord{}
	}
	return a.writeJSON(ctx, a.roomURL(slug, "_evdr", "grants.json"), ledger)
}

// canAccess decides whether an actor may open a render stream, list versions
// or export a room: the room creator always can (platform operators manage
// their rooms), otherwise an active, time-valid grant on the room is
// required. Constraints on CIDRs/domains are enforced upstream by the policy
// engine, which owns client network context (SPI README §semantics).
func (a *Adapter) canAccess(actor spi.Actor, room spi.Room, ledger grantLedger) bool {
	if actor.ID != "" && room.CreatedBy.ID == actor.ID {
		return true
	}
	now := time.Now()
	for _, g := range ledger.Grants {
		if g.Revoked || g.Grant.Subject != actor.ID {
			continue
		}
		if !g.Grant.Constraints.NotBefore.IsZero() && now.Before(g.Grant.Constraints.NotBefore) {
			continue
		}
		if !g.Grant.Constraints.NotAfter.IsZero() && !now.Before(g.Grant.Constraints.NotAfter) {
			continue
		}
		return true
	}
	return false
}

// compile-time assertion that Adapter implements the frozen RoomSPI contract.
var _ spi.RoomSPI = (*Adapter)(nil)
