// Package conformance provides the shared SPI conformance suite (TR-2.4).
//
// RunSuite exercises the full RoomSPI contract against any adapter: room
// lifecycle, grant lifecycle and error taxonomy, immutable versioning,
// view-scoped rendering, retention floors, sealing semantics and export
// integrity. The suite is deliberately adapter-agnostic — it talks only to
// spi.RoomSPI and the sentinel errors in spi — so the same tests gate every
// adapter (NextcloudAdapter today, NativeAdapter in Phase 2.5) in CI on
// every merge (AGENTS.md §9). A red suite blocks the merge.
//
// Each subtest runs against a fresh adapter instance and a fresh backend so
// cases cannot leak state into each other.
package conformance

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
)

// Harness is implemented by each adapter's test wiring. It constructs a
// fresh adapter backed by a fresh simulated backend and returns the tenant
// context the adapter is bound to. cleanup runs when the subtest completes.
type Harness interface {
	New(t *testing.T) (adapter spi.RoomSPI, tenant spi.TenantContext, cleanup func())
}

// RunSuite runs the full SPI conformance suite against the adapter produced
// by h. It must be wired into CI for every adapter (TR-2.4).
func RunSuite(t *testing.T, h Harness) {
	t.Helper()
	cases := []struct {
		name string
		run  func(t *testing.T, fx *fixture)
	}{
		{"CreateRoom/happy-path", caseCreateRoomHappyPath},
		{"CreateRoom/explicit-classification", caseCreateRoomClassification},
		{"CreateRoom/slug-collision", caseCreateRoomSlugCollision},
		{"CreateRoom/cross-tenant", caseCreateRoomCrossTenant},
		{"GrantAccess/permission-tiers", caseGrantAccessTiers},
		{"GrantAccess/guest-requires-expiry", caseGrantAccessGuestExpiry},
		{"GrantAccess/unknown-room", caseGrantAccessUnknownRoom},
		{"RevokeAccess/lifecycle", caseRevokeAccessLifecycle},
		{"RevokeAccess/unknown-room", caseRevokeAccessUnknownRoom},
		{"PutDocument/versions", casePutDocumentVersions},
		{"PutDocument/folder-hierarchy", casePutDocumentFolderHierarchy},
		{"PutDocument/unknown-room", casePutDocumentUnknownRoom},
		{"PutDocument/sealed", casePutDocumentSealed},
		{"Render/access-control", caseRenderAccessControl},
		{"Render/range-and-pages", caseRenderRange},
		{"Render/cancellation", caseRenderCancellation},
		{"Render/unknown-document", caseRenderUnknownDocument},
		{"ListVersions/ascending", caseListVersions},
		{"Retention/apply", caseRetentionApply},
		{"Retention/sealed", caseRetentionSealed},
		{"Seal/lifecycle", caseSealLifecycle},
		{"Export/integrity", caseExportIntegrity},
		{"Export/sealed-room", caseExportSealed},
		{"Tenancy/isolation", caseTenancyIsolation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fx := newFixture(t, h)
			tc.run(t, fx)
		})
	}
}

// fixture is the shared state one conformance subtest builds on.
type fixture struct {
	adapter spi.RoomSPI
	tenant  spi.TenantContext // the tenant the adapter is bound to
	other   spi.TenantContext // a tenant the adapter is NOT bound to

	viewer   spi.Actor // internal user granted access
	stranger spi.Actor // internal user with no grant
}

func newFixture(t *testing.T, h Harness) *fixture {
	t.Helper()
	adapter, tenant, cleanup := h.New(t)
	t.Cleanup(cleanup)
	return &fixture{
		adapter: adapter,
		tenant:  tenant,
		other: spi.TenantContext{
			TenantID: spi.TenantID(string(tenant.TenantID) + "-other"),
			Actor:    spi.Actor{Kind: spi.ActorInternalUser, ID: "other-operator"},
		},
		viewer: spi.Actor{Kind: spi.ActorInternalUser, ID: "viewer-1", DisplayName: "Viewer One"},
		stranger: spi.Actor{Kind: spi.ActorInternalUser, ID: "stranger-1", DisplayName: "Stranger"},
	}
}

// ctx returns a context bound to the operator of the fixture's tenant.
func (fx *fixture) ctx() context.Context { return context.Background() }

// opTenant returns a tenant context acting as the given actor.
func (fx *fixture) opTenant(actor spi.Actor) spi.TenantContext {
	return spi.TenantContext{TenantID: fx.tenant.TenantID, Actor: actor}
}

// mustCreateRoom creates a room, failing the test on error.
func (fx *fixture) mustCreateRoom(t *testing.T, slug string, mutate func(*spi.CreateRoomRequest)) spi.Room {
	t.Helper()
	req := spi.CreateRoomRequest{
		Name:           "Room " + slug,
		Slug:           slug,
		Classification: "",
		Retention:      spi.RetentionPolicy{Anchor: spi.AnchorUpload, MinRetentionDays: 30},
	}
	if mutate != nil {
		mutate(&req)
	}
	room, err := fx.adapter.CreateRoom(fx.ctx(), fx.tenant, req)
	if err != nil {
		t.Fatalf("CreateRoom(%q): %v", slug, err)
	}
	return room
}

// mustPutDocument uploads a document, failing the test on error.
func (fx *fixture) mustPutDocument(t *testing.T, roomID spi.RoomID, name, folder, content string, mutate func(*spi.PutDocumentRequest)) spi.Document {
	t.Helper()
	req := spi.PutDocumentRequest{
		Name:        name,
		FolderPath:  folder,
		ContentType: "text/plain",
		Content:     strings.NewReader(content),
		SizeBytes:   int64(len(content)),
	}
	if mutate != nil {
		mutate(&req)
	}
	doc, err := fx.adapter.PutDocument(fx.ctx(), fx.tenant, roomID, req)
	if err != nil {
		t.Fatalf("PutDocument(%q): %v", name, err)
	}
	return doc
}

// mustGrant grants access, failing the test on error.
func (fx *fixture) mustGrant(t *testing.T, roomID spi.RoomID, subject string, kind spi.ActorKind, tier spi.PermissionTier, constraints spi.GrantConstraints) spi.AccessGrant {
	t.Helper()
	grant, err := fx.adapter.GrantAccess(fx.ctx(), fx.tenant, roomID, spi.AccessGrant{
		Subject:     subject,
		ActorKind:   kind,
		Tier:        tier,
		Constraints: constraints,
	})
	if err != nil {
		t.Fatalf("GrantAccess(%s on %s): %v", subject, roomID, err)
	}
	return grant
}

// render opens a render stream as actor, returning the error (nil on success).
func (fx *fixture) render(t *testing.T, actor spi.Actor, docID spi.DocumentID, req spi.RenderRequest) (spi.RenderStream, error) {
	t.Helper()
	return fx.adapter.GetRenderStream(fx.ctx(), fx.opTenant(actor), docID, req)
}

// openRender opens a stream as actor and fails the test on error.
func (fx *fixture) openRender(t *testing.T, actor spi.Actor, docID spi.DocumentID, first, last int) spi.RenderStream {
	t.Helper()
	stream, err := fx.render(t, actor, docID, spi.RenderRequest{
		FirstPage: first,
		LastPage:  last,
		Watermark: spi.WatermarkSpec{ViewerIdentity: actor.ID, Density: "normal", Opacity: 0.5},
	})
	if err != nil {
		t.Fatalf("GetRenderStream: %v", err)
	}
	return stream
}

// renderNumbers collects the page numbers from a stream until exhaustion.
func renderNumbers(t *testing.T, s spi.RenderStream) []int {
	t.Helper()
	defer s.Close()
	var nums []int
	for s.Next() {
		nums = append(nums, s.Page().Number)
	}
	if err := s.Err(); err != nil {
		t.Fatalf("render stream error: %v", err)
	}
	return nums
}

// ---- cases ----

func caseCreateRoomHappyPath(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "acme-ipo", nil)

	if room.ID == "" {
		t.Error("room.ID must be adapter-assigned and non-empty")
	}
	if room.Name != "Room acme-ipo" || room.Slug != "acme-ipo" {
		t.Errorf("room identity fields: got %q/%q", room.Name, room.Slug)
	}
	if room.State != spi.RoomActive {
		t.Errorf("new room state = %q, want %q", room.State, spi.RoomActive)
	}
	if room.Classification != spi.DefaultClassification {
		t.Errorf("zero-value classification must resolve to %q, got %q", spi.DefaultClassification, room.Classification)
	}
	if room.Retention.MinRetentionDays != 30 {
		t.Errorf("retention floor not persisted: %+v", room.Retention)
	}
	if room.CreatedAt.IsZero() {
		t.Error("CreatedAt must be set")
	}
	if room.CreatedBy.ID != fx.tenant.Actor.ID {
		t.Errorf("CreatedBy = %+v, want operator", room.CreatedBy)
	}
}

func caseCreateRoomClassification(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "classified", func(req *spi.CreateRoomRequest) {
		req.Classification = spi.ClassificationRestricted
	})
	if room.Classification != spi.ClassificationRestricted {
		t.Errorf("explicit classification = %q, want RESTRICTED", room.Classification)
	}
}

func caseCreateRoomSlugCollision(t *testing.T, fx *fixture) {
	fx.mustCreateRoom(t, "dup", nil)
	_, err := fx.adapter.CreateRoom(fx.ctx(), fx.tenant, spi.CreateRoomRequest{Name: "Dup", Slug: "dup"})
	if !errors.Is(err, spi.ErrRoomExists) {
		t.Fatalf("duplicate slug error = %v, want ErrRoomExists", err)
	}
}

func caseCreateRoomCrossTenant(t *testing.T, fx *fixture) {
	_, err := fx.adapter.CreateRoom(fx.ctx(), fx.other, spi.CreateRoomRequest{Name: "X", Slug: "x"})
	if !errors.Is(err, spi.ErrAccessDenied) {
		t.Fatalf("cross-tenant CreateRoom error = %v, want ErrAccessDenied", err)
	}
}

func caseGrantAccessTiers(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "tiers", nil)
	doc := fx.mustPutDocument(t, room.ID, "a.txt", "", "page one\npage two\npage three", nil)

	tiers := []spi.PermissionTier{
		spi.TierViewOnly,
		spi.TierDownloadAllowed,
		spi.TierPrintAllowed,
		spi.TierEditAllowed,
	}
	for _, tier := range tiers {
		t.Run(tier.String(), func(t *testing.T) {
			subject := "user-" + tier.String()
			grant := fx.mustGrant(t, room.ID, subject, spi.ActorInternalUser, tier, spi.GrantConstraints{})
			if grant.ID == "" {
				t.Error("grant.ID must be adapter-assigned and non-empty")
			}
			if grant.Subject != subject || grant.Tier != tier {
				t.Errorf("grant fields = %+v", grant)
			}
			if grant.CreatedBy.ID != fx.tenant.Actor.ID {
				t.Errorf("grant CreatedBy = %+v, want operator", grant.CreatedBy)
			}
			if grant.CreatedAt.IsZero() {
				t.Error("grant CreatedAt must be set")
			}
			// Any grant tier covers the view-scoped render.
			actor := spi.Actor{Kind: spi.ActorInternalUser, ID: subject}
			stream, err := fx.render(t, actor, doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
			if err != nil {
				t.Fatalf("render at tier %s: %v", tier, err)
			}
			if nums := renderNumbers(t, stream); len(nums) == 0 {
				t.Error("render at granted tier produced no pages")
			}
		})
	}
}

func caseGrantAccessGuestExpiry(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "guests", nil)

	_, err := fx.adapter.GrantAccess(fx.ctx(), fx.tenant, room.ID, spi.AccessGrant{
		Subject:   "guest@example.com",
		ActorKind: spi.ActorGuest,
		Tier:      spi.TierViewOnly,
	})
	if !errors.Is(err, spi.ErrInvalidGrant) {
		t.Fatalf("guest without expiry error = %v, want ErrInvalidGrant", err)
	}

	expiry := time.Now().Add(24 * time.Hour).UTC()
	grant := fx.mustGrant(t, room.ID, "guest@example.com", spi.ActorGuest, spi.TierViewOnly,
		spi.GrantConstraints{NotAfter: expiry})
	if grant.Constraints.NotAfter.IsZero() {
		t.Error("guest grant must carry the requested expiry")
	}
	if grant.ActorKind != spi.ActorGuest || grant.Subject != "guest@example.com" {
		t.Errorf("guest grant fields = %+v", grant)
	}
}

func caseGrantAccessUnknownRoom(t *testing.T, fx *fixture) {
	_, err := fx.adapter.GrantAccess(fx.ctx(), fx.tenant, spi.RoomID("no-such-room"),
		spi.AccessGrant{Subject: "u", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly})
	if !errors.Is(err, spi.ErrRoomNotFound) {
		t.Fatalf("unknown room error = %v, want ErrRoomNotFound", err)
	}
	// Cross-tenant must be indistinguishable from not-found (no existence oracle).
	_, err = fx.adapter.GrantAccess(fx.ctx(), fx.other, spi.RoomID("no-such-room"),
		spi.AccessGrant{Subject: "u", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly})
	if !errors.Is(err, spi.ErrRoomNotFound) {
		t.Fatalf("cross-tenant unknown room error = %v, want ErrRoomNotFound", err)
	}
}

func caseRevokeAccessLifecycle(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "revoke", nil)
	doc := fx.mustPutDocument(t, room.ID, "a.txt", "", "page one\npage two\npage three", nil)
	grant := fx.mustGrant(t, room.ID, fx.viewer.ID, spi.ActorInternalUser, spi.TierViewOnly, spi.GrantConstraints{})

	// Unknown grant → ErrGrantNotFound.
	err := fx.adapter.RevokeAccess(fx.ctx(), fx.tenant, room.ID, spi.GrantID("no-such-grant"))
	if !errors.Is(err, spi.ErrGrantNotFound) {
		t.Fatalf("unknown grant error = %v, want ErrGrantNotFound", err)
	}

	// Viewer can render before revocation.
	stream, err := fx.render(t, fx.viewer, doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if err != nil {
		t.Fatalf("render before revoke: %v", err)
	}
	stream.Close()

	// Revoke → immediate effect: no new stream may open.
	if err := fx.adapter.RevokeAccess(fx.ctx(), fx.tenant, room.ID, grant.ID); err != nil {
		t.Fatalf("RevokeAccess: %v", err)
	}
	_, err = fx.render(t, fx.viewer, doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, spi.ErrAccessDenied) {
		t.Fatalf("render after revoke error = %v, want ErrAccessDenied", err)
	}

	// Idempotent-safe: re-revoking returns nil.
	if err := fx.adapter.RevokeAccess(fx.ctx(), fx.tenant, room.ID, grant.ID); err != nil {
		t.Fatalf("re-revoke: %v", err)
	}

	// Revoking the last grant does not close the room.
	doc2 := fx.mustPutDocument(t, room.ID, "b.txt", "", "still open", nil)
	if doc2.ID == "" {
		t.Error("room must remain writable after last grant revocation")
	}
}

func caseRevokeAccessUnknownRoom(t *testing.T, fx *fixture) {
	err := fx.adapter.RevokeAccess(fx.ctx(), fx.tenant, spi.RoomID("no-such-room"), spi.GrantID("g"))
	if !errors.Is(err, spi.ErrRoomNotFound) {
		t.Fatalf("unknown room error = %v, want ErrRoomNotFound", err)
	}
}

func casePutDocumentVersions(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "versions", nil)

	v1 := fx.mustPutDocument(t, room.ID, "report.pdf", "", "first draft\npage two\npage three", nil)
	if v1.ID == "" {
		t.Fatal("document ID must be non-empty")
	}
	if v1.RoomID != room.ID || v1.Name != "report.pdf" || v1.FolderPath != "" {
		t.Errorf("document fields = %+v", v1)
	}
	if v1.Classification != spi.DefaultClassification {
		t.Errorf("document classification must inherit room floor %q, got %q", spi.DefaultClassification, v1.Classification)
	}
	if v1.SizeBytes != int64(len("first draft\npage two\npage three")) {
		t.Errorf("size = %d", v1.SizeBytes)
	}
	if v1.UploadedAt.IsZero() {
		t.Error("UploadedAt must be set")
	}

	// Same name+folder → new immutable version, never an overwrite.
	v2 := fx.mustPutDocument(t, room.ID, "report.pdf", "", "second draft\npage two\npage three", nil)
	if v2.CurrentVersion == v1.CurrentVersion {
		t.Error("current version must advance on re-upload")
	}

	versions, err := fx.adapter.ListVersions(fx.ctx(), fx.tenant, v1.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("version count = %d, want 2", len(versions))
	}
	if versions[0].Number != 1 || versions[1].Number != 2 {
		t.Errorf("version numbers = %d,%d want 1,2", versions[0].Number, versions[1].Number)
	}
	if versions[0].SHA256 == "" || versions[1].SHA256 == "" {
		t.Error("versions must carry SHA-256 digests")
	}
	if versions[0].SHA256 == versions[1].SHA256 {
		t.Error("distinct contents must have distinct digests")
	}
	if versions[1].DocumentID != v1.ID {
		t.Errorf("version DocumentID = %s, want %s", versions[1].DocumentID, v1.ID)
	}

	// The render of the current version reflects the newest content.
	stream := fx.openRender(t, fx.tenant.Actor, v1.ID, 1, 2)
	nums := renderNumbers(t, stream)
	if len(nums) != 2 || nums[0] != 1 || nums[1] != 2 {
		t.Errorf("render pages = %v, want [1 2]", nums)
	}
}

func casePutDocumentFolderHierarchy(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "folders", nil)
	doc := fx.mustPutDocument(t, room.ID, "deal.pdf", "legal/nda", "one\ntwo\nthree", nil)
	if doc.FolderPath != "legal/nda" {
		t.Errorf("FolderPath = %q, want legal/nda", doc.FolderPath)
	}
	stream := fx.openRender(t, fx.tenant.Actor, doc.ID, 1, 3)
	if nums := renderNumbers(t, stream); len(nums) != 3 {
		t.Errorf("render pages = %v, want 3 pages", nums)
	}
}

func casePutDocumentUnknownRoom(t *testing.T, fx *fixture) {
	_, err := fx.adapter.PutDocument(fx.ctx(), fx.tenant, spi.RoomID("no-such-room"), spi.PutDocumentRequest{
		Name: "a.txt", Content: strings.NewReader("x"),
	})
	if !errors.Is(err, spi.ErrRoomNotFound) {
		t.Fatalf("unknown room error = %v, want ErrRoomNotFound", err)
	}
	_, err = fx.adapter.PutDocument(fx.ctx(), fx.other, spi.RoomID("no-such-room"), spi.PutDocumentRequest{
		Name: "a.txt", Content: strings.NewReader("x"),
	})
	if !errors.Is(err, spi.ErrRoomNotFound) {
		t.Fatalf("cross-tenant unknown room error = %v, want ErrRoomNotFound", err)
	}
}

func casePutDocumentSealed(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "seal-put", nil)
	fx.mustPutDocument(t, room.ID, "a.txt", "", "one\ntwo\nthree", nil)
	if _, err := fx.adapter.SealRoom(fx.ctx(), fx.tenant, room.ID, spi.SealRequest{Reason: "litigation hold"}); err != nil {
		t.Fatalf("SealRoom: %v", err)
	}
	_, err := fx.adapter.PutDocument(fx.ctx(), fx.tenant, room.ID, spi.PutDocumentRequest{
		Name: "b.txt", Content: strings.NewReader("x"),
	})
	if !errors.Is(err, spi.ErrRoomSealed) {
		t.Fatalf("put on sealed room error = %v, want ErrRoomSealed", err)
	}
}

func caseRenderAccessControl(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "render-acl", nil)
	doc := fx.mustPutDocument(t, room.ID, "a.txt", "", "page one\npage two\npage three", nil)

	// Stranger (no grant, not the creator) → denied.
	_, err := fx.render(t, fx.stranger, doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, spi.ErrAccessDenied) {
		t.Fatalf("stranger render error = %v, want ErrAccessDenied", err)
	}

	// Granted viewer → allowed.
	fx.mustGrant(t, room.ID, fx.viewer.ID, spi.ActorInternalUser, spi.TierViewOnly, spi.GrantConstraints{})
	if _, err := fx.render(t, fx.viewer, doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1}); err != nil {
		t.Fatalf("granted render: %v", err)
	}

	// Expired grant (past NotAfter) → denied.
	expired := fx.mustGrant(t, room.ID, "expired-user", spi.ActorInternalUser, spi.TierViewOnly,
		spi.GrantConstraints{NotAfter: time.Now().Add(-time.Hour).UTC()})
	_ = expired
	_, err = fx.render(t, spi.Actor{Kind: spi.ActorInternalUser, ID: "expired-user"}, doc.ID,
		spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, spi.ErrAccessDenied) {
		t.Fatalf("expired-grant render error = %v, want ErrAccessDenied", err)
	}

	// Future NotBefore → not yet valid → denied.
	fx.mustGrant(t, room.ID, "future-user", spi.ActorInternalUser, spi.TierViewOnly,
		spi.GrantConstraints{NotBefore: time.Now().Add(time.Hour).UTC()})
	_, err = fx.render(t, spi.Actor{Kind: spi.ActorInternalUser, ID: "future-user"}, doc.ID,
		spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, spi.ErrAccessDenied) {
		t.Fatalf("future-NotBefore render error = %v, want ErrAccessDenied", err)
	}

	// Revoked grant → denied.
	revoked := fx.mustGrant(t, room.ID, "revoked-user", spi.ActorInternalUser, spi.TierViewOnly, spi.GrantConstraints{})
	if err := fx.adapter.RevokeAccess(fx.ctx(), fx.tenant, room.ID, revoked.ID); err != nil {
		t.Fatalf("RevokeAccess: %v", err)
	}
	_, err = fx.render(t, spi.Actor{Kind: spi.ActorInternalUser, ID: "revoked-user"}, doc.ID,
		spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, spi.ErrAccessDenied) {
		t.Fatalf("revoked-grant render error = %v, want ErrAccessDenied", err)
	}
}

func caseRenderRange(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "render-range", nil)
	doc := fx.mustPutDocument(t, room.ID, "a.txt", "", "page one\npage two\npage three", nil)
	fx.mustGrant(t, room.ID, fx.viewer.ID, spi.ActorInternalUser, spi.TierViewOnly, spi.GrantConstraints{})

	stream := fx.openRender(t, fx.viewer, doc.ID, 1, 2)
	nums := renderNumbers(t, stream)
	if len(nums) == 0 {
		t.Fatal("render produced no pages")
	}
	prev := 0
	for _, n := range nums {
		if n < 1 || n > 2 {
			t.Errorf("page number %d outside requested range [1,2]", n)
		}
		if n <= prev {
			t.Errorf("pages not strictly increasing: %v", nums)
		}
		prev = n
	}
}

func caseRenderCancellation(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "render-cancel", nil)
	doc := fx.mustPutDocument(t, room.ID, "a.txt", "", "page one\npage two\npage three", nil)
	fx.mustGrant(t, room.ID, fx.viewer.ID, spi.ActorInternalUser, spi.TierViewOnly, spi.GrantConstraints{})

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := fx.adapter.GetRenderStream(ctx, fx.opTenant(fx.viewer), doc.ID,
		spi.RenderRequest{FirstPage: 1, LastPage: 100})
	if err != nil {
		t.Fatalf("GetRenderStream: %v", err)
	}
	defer stream.Close()
	cancel()
	// Next must unblock (immediately false) and surface the cancellation.
	if stream.Next() {
		t.Error("Next returned true after cancellation")
	}
	if stream.Err() == nil {
		t.Error("stream.Err() must surface the cancellation")
	}
	if !errors.Is(stream.Err(), context.Canceled) {
		t.Errorf("stream.Err() = %v, want context.Canceled", stream.Err())
	}
}

func caseRenderUnknownDocument(t *testing.T, fx *fixture) {
	_, err := fx.adapter.GetRenderStream(fx.ctx(), fx.tenant, spi.DocumentID("nope"), spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, spi.ErrDocumentNotFound) {
		t.Fatalf("unknown document error = %v, want ErrDocumentNotFound", err)
	}
	_, err = fx.adapter.GetRenderStream(fx.ctx(), fx.other, spi.DocumentID("nope"), spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, spi.ErrDocumentNotFound) {
		t.Fatalf("cross-tenant unknown document error = %v, want ErrDocumentNotFound", err)
	}
}

func caseListVersions(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "list-versions", nil)
	doc := fx.mustPutDocument(t, room.ID, "a.txt", "", "one", nil)
	fx.mustPutDocument(t, room.ID, "a.txt", "", "two", nil)

	versions, err := fx.adapter.ListVersions(fx.ctx(), fx.tenant, doc.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 || versions[0].Number != 1 || versions[1].Number != 2 {
		t.Fatalf("versions = %+v, want [1 2]", versions)
	}

	_, err = fx.adapter.ListVersions(fx.ctx(), fx.tenant, spi.DocumentID("nope"))
	if !errors.Is(err, spi.ErrDocumentNotFound) {
		t.Fatalf("unknown document error = %v, want ErrDocumentNotFound", err)
	}
	_, err = fx.adapter.ListVersions(fx.ctx(), fx.other, spi.DocumentID("nope"))
	if !errors.Is(err, spi.ErrDocumentNotFound) {
		t.Fatalf("cross-tenant unknown document error = %v, want ErrDocumentNotFound", err)
	}
}

func caseRetentionApply(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "retention", nil) // floor 30 days

	// Extending the floor is fine.
	if err := fx.adapter.ApplyRetention(fx.ctx(), fx.tenant, room.ID,
		spi.RetentionPolicy{Anchor: spi.AnchorRoomClosure, MinRetentionDays: 90, AutoPurge: true}); err != nil {
		t.Fatalf("ApplyRetention(extend): %v", err)
	}

	// Shortening below the stored floor is a contract violation.
	err := fx.adapter.ApplyRetention(fx.ctx(), fx.tenant, room.ID,
		spi.RetentionPolicy{Anchor: spi.AnchorRoomClosure, MinRetentionDays: 10})
	if !errors.Is(err, spi.ErrRetentionViolation) {
		t.Fatalf("ApplyRetention(shorten) error = %v, want ErrRetentionViolation", err)
	}

	// Unknown room / cross-tenant → not found.
	err = fx.adapter.ApplyRetention(fx.ctx(), fx.tenant, spi.RoomID("nope"),
		spi.RetentionPolicy{MinRetentionDays: 90})
	if !errors.Is(err, spi.ErrRoomNotFound) {
		t.Fatalf("unknown room error = %v, want ErrRoomNotFound", err)
	}
	err = fx.adapter.ApplyRetention(fx.ctx(), fx.other, spi.RoomID("nope"),
		spi.RetentionPolicy{MinRetentionDays: 90})
	if !errors.Is(err, spi.ErrRoomNotFound) {
		t.Fatalf("cross-tenant unknown room error = %v, want ErrRoomNotFound", err)
	}
}

func caseRetentionSealed(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "retention-seal", nil)
	if _, err := fx.adapter.SealRoom(fx.ctx(), fx.tenant, room.ID, spi.SealRequest{Reason: "hold"}); err != nil {
		t.Fatalf("SealRoom: %v", err)
	}
	err := fx.adapter.ApplyRetention(fx.ctx(), fx.tenant, room.ID,
		spi.RetentionPolicy{MinRetentionDays: 120})
	if !errors.Is(err, spi.ErrRoomSealed) {
		t.Fatalf("ApplyRetention on sealed room error = %v, want ErrRoomSealed", err)
	}
}

func caseSealLifecycle(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "seal", nil)
	doc := fx.mustPutDocument(t, room.ID, "a.txt", "", "one", nil)
	fx.mustPutDocument(t, room.ID, "a.txt", "", "two", nil) // 1 doc, 2 versions
	grant := fx.mustGrant(t, room.ID, fx.viewer.ID, spi.ActorInternalUser, spi.TierViewOnly, spi.GrantConstraints{})

	receipt, err := fx.adapter.SealRoom(fx.ctx(), fx.tenant, room.ID, spi.SealRequest{Reason: "litigation", LegalHoldRef: "case-42"})
	if err != nil {
		t.Fatalf("SealRoom: %v", err)
	}
	if receipt.ID == "" || receipt.RoomID != room.ID || receipt.SealedAt.IsZero() {
		t.Errorf("receipt = %+v", receipt)
	}
	if receipt.FrozenObjects != 3 {
		t.Errorf("FrozenObjects = %d, want 3 (1 document + 2 versions)", receipt.FrozenObjects)
	}
	if receipt.SealedBy.ID != fx.tenant.Actor.ID {
		t.Errorf("SealedBy = %+v, want operator", receipt.SealedBy)
	}

	// Idempotent: re-seal returns the stored receipt.
	again, err := fx.adapter.SealRoom(fx.ctx(), fx.tenant, room.ID, spi.SealRequest{Reason: "again"})
	if err != nil {
		t.Fatalf("re-seal: %v", err)
	}
	if again.ID != receipt.ID || !again.SealedAt.Equal(receipt.SealedAt) {
		t.Errorf("re-seal receipt = %+v, want stored %+v", again, receipt)
	}

	// All mutations → ErrRoomSealed.
	if _, err := fx.adapter.GrantAccess(fx.ctx(), fx.tenant, room.ID,
		spi.AccessGrant{Subject: "late-user", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly}); !errors.Is(err, spi.ErrRoomSealed) {
		t.Errorf("GrantAccess on sealed room = %v, want ErrRoomSealed", err)
	}
	if _, err := fx.adapter.PutDocument(fx.ctx(), fx.tenant, room.ID,
		spi.PutDocumentRequest{Name: "b.txt", Content: strings.NewReader("x")}); !errors.Is(err, spi.ErrRoomSealed) {
		t.Errorf("PutDocument on sealed room = %v, want ErrRoomSealed", err)
	}
	if err := fx.adapter.ApplyRetention(fx.ctx(), fx.tenant, room.ID,
		spi.RetentionPolicy{MinRetentionDays: 90}); !errors.Is(err, spi.ErrRoomSealed) {
		t.Errorf("ApplyRetention on sealed room = %v, want ErrRoomSealed", err)
	}

	// Reads remain available.
	if versions, err := fx.adapter.ListVersions(fx.ctx(), fx.tenant, doc.ID); err != nil || len(versions) != 2 {
		t.Errorf("ListVersions on sealed room = %v, %v", versions, err)
	}
	if _, err := fx.render(t, fx.tenant.Actor, doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1}); err != nil {
		t.Errorf("render on sealed room: %v", err)
	}
	if _, err := fx.adapter.ExportRoom(fx.ctx(), fx.tenant, room.ID, spi.ExportOptions{}); err != nil {
		t.Errorf("export on sealed room: %v", err)
	}

	// Deliberate contract interpretation: revoking access on a sealed room is
	// permitted — legal hold freezes documents/metadata, never access
	// withdrawal. After revocation the viewer can no longer open streams.
	if err := fx.adapter.RevokeAccess(fx.ctx(), fx.tenant, room.ID, grant.ID); err != nil {
		t.Errorf("RevokeAccess on sealed room must be allowed (security-preserving), got %v", err)
	}
	if _, err := fx.render(t, fx.viewer, doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1}); !errors.Is(err, spi.ErrAccessDenied) {
		t.Errorf("render by revoked viewer on sealed room = %v, want ErrAccessDenied", err)
	}
}

func caseExportIntegrity(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "export", nil)
	fx.mustPutDocument(t, room.ID, "a.txt", "", "version one\npage two\npage three", nil)
	fx.mustPutDocument(t, room.ID, "a.txt", "", "version two\npage two\npage three", nil)
	fx.mustGrant(t, room.ID, fx.viewer.ID, spi.ActorInternalUser, spi.TierViewOnly, spi.GrantConstraints{})

	pkg, err := fx.adapter.ExportRoom(fx.ctx(), fx.tenant, room.ID, spi.ExportOptions{
		IncludeVersions:   true,
		IncludeAuditTrail: true,
	})
	if err != nil {
		t.Fatalf("ExportRoom: %v", err)
	}
	defer pkg.Content.Close()
	if pkg.RoomID != room.ID {
		t.Errorf("package RoomID = %s", pkg.RoomID)
	}
	if len(pkg.IntegrityLetter) == 0 {
		t.Error("integrity letter must be present")
	}
	if len(pkg.Manifest.Entries) == 0 {
		t.Fatal("manifest must not be empty")
	}

	// Every hashed tar entry must match its manifest digest (SR-5.2).
	verifyExportArchive(t, pkg)

	// IncludeVersions=false must export the current version only.
	pkg2, err := fx.adapter.ExportRoom(fx.ctx(), fx.tenant, room.ID, spi.ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRoom(current only): %v", err)
	}
	defer pkg2.Content.Close()
	docEntries := 0
	for _, e := range pkg2.Manifest.Entries {
		if strings.HasPrefix(e.Path, "docs/") {
			docEntries++
		}
	}
	if docEntries != 1 {
		t.Errorf("current-only export has %d document entries, want 1", docEntries)
	}
	verifyExportArchive(t, pkg2)

	// Export is access-gated like other reads.
	if _, err := fx.adapter.ExportRoom(fx.ctx(), fx.opTenant(fx.stranger), room.ID, spi.ExportOptions{}); !errors.Is(err, spi.ErrAccessDenied) {
		t.Errorf("stranger export error = %v, want ErrAccessDenied", err)
	}
}

func caseExportSealed(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "export-sealed", nil)
	fx.mustPutDocument(t, room.ID, "a.txt", "", "one", nil)
	if _, err := fx.adapter.SealRoom(fx.ctx(), fx.tenant, room.ID, spi.SealRequest{Reason: "eDiscovery"}); err != nil {
		t.Fatalf("SealRoom: %v", err)
	}
	pkg, err := fx.adapter.ExportRoom(fx.ctx(), fx.tenant, room.ID, spi.ExportOptions{IncludeAuditTrail: true})
	if err != nil {
		t.Fatalf("export of sealed room must be permitted (eDiscovery): %v", err)
	}
	defer pkg.Content.Close()
	verifyExportArchive(t, pkg)
	// The frozen state is reflected in the package.
	sealSeen := false
	for _, e := range pkg.Manifest.Entries {
		if e.Path == "audit/seal.json" {
			sealSeen = true
		}
	}
	if !sealSeen {
		t.Error("sealed export must include the seal receipt (audit trail)")
	}
}

func caseTenancyIsolation(t *testing.T, fx *fixture) {
	room := fx.mustCreateRoom(t, "tenant", nil)
	doc := fx.mustPutDocument(t, room.ID, "a.txt", "", "one", nil)

	// Every room-scoped operation from another tenant behaves as not-found.
	if _, err := fx.adapter.GrantAccess(fx.ctx(), fx.other, room.ID,
		spi.AccessGrant{Subject: "u", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly}); !errors.Is(err, spi.ErrRoomNotFound) {
		t.Errorf("cross-tenant GrantAccess = %v, want ErrRoomNotFound", err)
	}
	if err := fx.adapter.RevokeAccess(fx.ctx(), fx.other, room.ID, spi.GrantID("g")); !errors.Is(err, spi.ErrRoomNotFound) {
		t.Errorf("cross-tenant RevokeAccess = %v, want ErrRoomNotFound", err)
	}
	if _, err := fx.adapter.PutDocument(fx.ctx(), fx.other, room.ID,
		spi.PutDocumentRequest{Name: "x.txt", Content: strings.NewReader("x")}); !errors.Is(err, spi.ErrRoomNotFound) {
		t.Errorf("cross-tenant PutDocument = %v, want ErrRoomNotFound", err)
	}
	if err := fx.adapter.ApplyRetention(fx.ctx(), fx.other, room.ID,
		spi.RetentionPolicy{MinRetentionDays: 90}); !errors.Is(err, spi.ErrRoomNotFound) {
		t.Errorf("cross-tenant ApplyRetention = %v, want ErrRoomNotFound", err)
	}
	if _, err := fx.adapter.SealRoom(fx.ctx(), fx.other, room.ID, spi.SealRequest{Reason: "x"}); !errors.Is(err, spi.ErrRoomNotFound) {
		t.Errorf("cross-tenant SealRoom = %v, want ErrRoomNotFound", err)
	}
	if _, err := fx.adapter.ExportRoom(fx.ctx(), fx.other, room.ID, spi.ExportOptions{}); !errors.Is(err, spi.ErrRoomNotFound) {
		t.Errorf("cross-tenant ExportRoom = %v, want ErrRoomNotFound", err)
	}

	// Document-scoped operations behave as document-not-found.
	if _, err := fx.adapter.GetRenderStream(fx.ctx(), fx.other, doc.ID,
		spi.RenderRequest{FirstPage: 1, LastPage: 1}); !errors.Is(err, spi.ErrDocumentNotFound) {
		t.Errorf("cross-tenant GetRenderStream = %v, want ErrDocumentNotFound", err)
	}
	if _, err := fx.adapter.ListVersions(fx.ctx(), fx.other, doc.ID); !errors.Is(err, spi.ErrDocumentNotFound) {
		t.Errorf("cross-tenant ListVersions = %v, want ErrDocumentNotFound", err)
	}
}

// verifyExportArchive walks the tar stream of an export package and asserts
// every hashed entry matches its manifest digest and size.
func verifyExportArchive(t *testing.T, pkg spi.ExportPackage) {
	t.Helper()
	digests := map[string]spi.ManifestEntry{}
	for _, e := range pkg.Manifest.Entries {
		digests[e.Path] = e
	}
	tr := tar.NewReader(pkg.Content)
	checked := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reading export archive: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		// Un-hashed metadata entries.
		if hdr.Name == "manifest.json" || hdr.Name == "INTEGRITY_LETTER.txt" {
			continue
		}
		entry, ok := digests[hdr.Name]
		if !ok {
			t.Fatalf("archive entry %q missing from manifest", hdr.Name)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("reading archive entry %q: %v", hdr.Name, err)
		}
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != entry.SHA256 {
			t.Errorf("archive entry %q sha256 = %s, manifest says %s", hdr.Name, got, entry.SHA256)
		}
		if int64(len(content)) != entry.SizeBytes {
			t.Errorf("archive entry %q size = %d, manifest says %d", hdr.Name, len(content), entry.SizeBytes)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("export archive contained no verifiable entries")
	}
	if fmt.Sprint(pkg.RoomID) == "" {
		t.Error("package RoomID must be set")
	}
}
