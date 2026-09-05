package room

import (
	"sort"
	"strings"
	"sync"

	"github.com/ivenkwan/evdr/src/spi"
)

// Catalog is the Room Service's per-tenant projection of the records its own
// SPI calls returned.
//
// The frozen SPI (v0.1.0) is an action contract: it has no enumeration
// methods (no ListRooms / ListDocuments / ListGrants / GetRoom) and returns
// records only from the mutating calls (CreateRoom, PutDocument,
// GrantAccess, ...). So that the portal/admin BFFs can render room lists,
// document lists and grant lists, this service records what the SPI told it
// and serves those projections back. Nothing is invented: every entry is
// copied from a successful SPI response, and every read that the SPI can
// answer live (ListVersions) is still answered live by the SPI.
//
// Scoping: the catalog is keyed by tenant from the authenticated principal,
// mirroring the SPI's tenant-bound posture. A tenant whose principal does
// not match the bound adapter's tenant sees an empty catalog (never another
// tenant's records), and any SPI call that slips through surfaces the SPI's
// own not-found/denied sentinels — no cross-tenant existence oracle.
//
// Limitations (honest): the projection is in-memory and only reflects
// mutations that flowed through THIS service process. It follows the
// platform's single-writer-per-cell deployment assumption (see the Room SPI
// README); a durable, shared projection arrives with the control plane
// metadata store in a later wave.
type Catalog struct {
	mu      sync.RWMutex
	tenants map[spi.TenantID]*tenantRec
}

type tenantRec struct {
	rooms  map[spi.RoomID]*roomRec
	bySlug map[string]spi.RoomID
}

type roomRec struct {
	room   spi.Room
	seal   *spi.SealReceipt
	docs   map[spi.DocumentID]*docRec
	grants map[spi.GrantID]*grantRec
	byKey  map[string]spi.DocumentID // "folder/name" → document id
}

type docRec struct {
	doc spi.Document
}

type grantRec struct {
	grant   spi.AccessGrant
	revoked bool
}

// NewCatalog returns an empty catalog.
func NewCatalog() *Catalog {
	return &Catalog{tenants: map[spi.TenantID]*tenantRec{}}
}

func (c *Catalog) tenantLocked(tenantID spi.TenantID) *tenantRec {
	t, ok := c.tenants[tenantID]
	if !ok {
		t = &tenantRec{
			rooms:  map[spi.RoomID]*roomRec{},
			bySlug: map[string]spi.RoomID{},
		}
		c.tenants[tenantID] = t
	}
	return t
}

// RecordRoom stores a room record returned by the SPI (CreateRoom).
func (c *Catalog) RecordRoom(tenantID spi.TenantID, room spi.Room) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.tenantLocked(tenantID)
	if _, exists := t.rooms[room.ID]; !exists {
		t.bySlug[room.Slug] = room.ID
	}
	t.rooms[room.ID] = &roomRec{room: room, docs: map[spi.DocumentID]*docRec{}, grants: map[spi.GrantID]*grantRec{}, byKey: map[string]spi.DocumentID{}}
}

// Room returns the projected room record.
func (c *Catalog) Room(tenantID spi.TenantID, roomID spi.RoomID) (spi.Room, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return spi.Room{}, false
	}
	r, ok := t.rooms[roomID]
	if !ok {
		return spi.Room{}, false
	}
	return r.room, true
}

// RoomBySlug resolves a room by its portal slug within the tenant.
func (c *Catalog) RoomBySlug(tenantID spi.TenantID, slug string) (spi.Room, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return spi.Room{}, false
	}
	id, ok := t.bySlug[slug]
	if !ok {
		return spi.Room{}, false
	}
	r, ok := t.rooms[id]
	if !ok {
		return spi.Room{}, false
	}
	return r.room, true
}

// Rooms lists the tenant's projected rooms ordered by creation time.
func (c *Catalog) Rooms(tenantID spi.TenantID) []spi.Room {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return nil
	}
	out := make([]spi.Room, 0, len(t.rooms))
	for _, r := range t.rooms {
		out = append(out, r.room)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// UpdateRoomState applies post-mutation state changes the SPI confirmed
// (seal → RoomSealed, retention updates). The SPI has no read-back for room
// state, so the service reflects confirmed transitions only.
func (c *Catalog) UpdateRoomState(tenantID spi.TenantID, room spi.Room) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return
	}
	r, ok := t.rooms[room.ID]
	if !ok {
		return
	}
	r.room = room
}

// RecordSeal stores the seal receipt returned by the SPI.
func (c *Catalog) RecordSeal(tenantID spi.TenantID, receipt spi.SealReceipt) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tenants[tenantID]; ok {
		if r, ok := t.rooms[receipt.RoomID]; ok {
			r.seal = &receipt
		}
	}
}

// Seal returns the projected seal receipt for a room, if sealed.
func (c *Catalog) Seal(tenantID spi.TenantID, roomID spi.RoomID) (*spi.SealReceipt, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return nil, false
	}
	r, ok := t.rooms[roomID]
	if !ok || r.seal == nil {
		return nil, false
	}
	cp := *r.seal
	return &cp, true
}

// RecordDocument stores a document upload receipt returned by the SPI
// (PutDocument). A second version of the same name+folder replaces the
// document record with the SPI's updated current-version view.
func (c *Catalog) RecordDocument(tenantID spi.TenantID, doc spi.Document) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return
	}
	r, ok := t.rooms[doc.RoomID]
	if !ok {
		return
	}
	key := trimFolder(doc.FolderPath) + "/" + doc.Name
	r.byKey[key] = doc.ID
	r.docs[doc.ID] = &docRec{doc: doc}
}

// Documents lists the room's projected documents ordered by upload time.
func (c *Catalog) Documents(tenantID spi.TenantID, roomID spi.RoomID) []spi.Document {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return nil
	}
	r, ok := t.rooms[roomID]
	if !ok {
		return nil
	}
	out := make([]spi.Document, 0, len(r.docs))
	for _, d := range r.docs {
		out = append(out, d.doc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UploadedAt.Before(out[j].UploadedAt) })
	return out
}

// Document returns the projected document record, scoped to its room.
func (c *Catalog) Document(tenantID spi.TenantID, roomID spi.RoomID, docID spi.DocumentID) (spi.Document, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return spi.Document{}, false
	}
	r, ok := t.rooms[roomID]
	if !ok {
		return spi.Document{}, false
	}
	d, ok := r.docs[docID]
	if !ok {
		return spi.Document{}, false
	}
	return d.doc, true
}

// RecordGrant stores a grant the SPI confirmed.
func (c *Catalog) RecordGrant(tenantID spi.TenantID, roomID spi.RoomID, grant spi.AccessGrant) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return
	}
	r, ok := t.rooms[roomID]
	if !ok {
		return
	}
	r.grants[grant.ID] = &grantRec{grant: grant}
}

// RevokeGrant marks a projected grant revoked (the SPI has no read-back;
// revocation is confirmed by RevokeAccess returning nil).
func (c *Catalog) RevokeGrant(tenantID spi.TenantID, roomID spi.RoomID, grantID spi.GrantID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return
	}
	r, ok := t.rooms[roomID]
	if !ok {
		return
	}
	if g, ok := r.grants[grantID]; ok {
		g.revoked = true
	}
}

// Grants lists the room's active (non-revoked) projected grants.
func (c *Catalog) Grants(tenantID spi.TenantID, roomID spi.RoomID) []spi.AccessGrant {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.tenants[tenantID]
	if !ok {
		return nil
	}
	r, ok := t.rooms[roomID]
	if !ok {
		return nil
	}
	out := make([]spi.AccessGrant, 0, len(r.grants))
	for _, g := range r.grants {
		if !g.revoked {
			out = append(out, g.grant)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func trimFolder(f string) string {
	return strings.Trim(f, "/")
}
