package server_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ivenkwan/evdr/src/services/viewer/server"
	"github.com/ivenkwan/evdr/src/spi"
	"github.com/ivenkwan/evdr/src/spi/conformance/memadapter"
)

// fixture wires a fresh in-memory adapter (bound to tenant-a) with a seeded
// room, document and grants, then mounts the viewer HTTP server over it.
type fixture struct {
	adapter *memadapter.Adapter
	srv     *httptest.Server
	tenant  spi.TenantContext

	room     spi.Room
	doc      spi.Document // three-line text document
	granted  spi.Actor    // internal user with a view grant
	stranger spi.Actor    // no grant
	guest    spi.Actor    // guest with expiry
	operator spi.Actor
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	tenant := spi.TenantContext{
		TenantID: "tenant-a",
		Actor:    spi.Actor{Kind: spi.ActorInternalUser, ID: "operator-1", DisplayName: "Operator"},
	}
	adapter := memadapter.New(tenant.TenantID)
	room, err := adapter.CreateRoom(context.Background(), tenant, spi.CreateRoomRequest{
		Name: "Deal room", Slug: "deal-a",
		Retention: spi.RetentionPolicy{Anchor: spi.AnchorUpload, MinRetentionDays: 30},
	})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	doc, err := adapter.PutDocument(context.Background(), tenant, room.ID, spi.PutDocumentRequest{
		Name:        "overview.txt",
		FolderPath:  "",
		ContentType: "text/plain",
		Content:     strings.NewReader("page one body\npage two body\npage three body\n"),
		SizeBytes:   45,
	})
	if err != nil {
		t.Fatalf("PutDocument: %v", err)
	}
	granted := spi.Actor{Kind: spi.ActorInternalUser, ID: "viewer-1", DisplayName: "Viewer One"}
	stranger := spi.Actor{Kind: spi.ActorInternalUser, ID: "stranger-1"}
	guest := spi.Actor{Kind: spi.ActorGuest, ID: "guest@example.com"}
	for _, g := range []struct {
		subject string
		kind    spi.ActorKind
	}{
		{granted.ID, spi.ActorInternalUser},
		{guest.ID, spi.ActorGuest},
	} {
		gr, err := adapter.GrantAccess(context.Background(), tenant, room.ID, spi.AccessGrant{
			Subject: g.subject, ActorKind: g.kind, Tier: spi.TierViewOnly,
			Constraints: spi.GrantConstraints{NotAfter: time.Now().Add(24 * time.Hour).UTC()},
		})
		if err != nil {
			t.Fatalf("GrantAccess(%s): %v", g.subject, err)
		}
		if g.kind == spi.ActorGuest && gr.ID == "" {
			t.Fatal("guest grant must carry an id")
		}
	}

	srv, err := server.New(server.Config{TenantID: tenant.TenantID, SPI: adapter})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)

	return &fixture{
		adapter:  adapter,
		srv:      ts,
		tenant:   tenant,
		room:     room,
		doc:      doc,
		granted:  granted,
		stranger: stranger,
		guest:    guest,
		operator: tenant.Actor,
	}
}

// get performs a request with the given actor headers and returns the raw
// response.
func (f *fixture) get(t *testing.T, path, actor string, extraHeaders map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if actor != "" {
		req.Header.Set("X-EVDR-Principal", actor)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := f.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (f *fixture) pageURL(page int) string {
	return "/v1/viewer/rooms/" + string(f.room.ID) + "/documents/" + string(f.doc.ID) + "/pages/" + strconv.Itoa(page)
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHealthz(t *testing.T) {
	f := newFixture(t)
	resp := f.get(t, "/v1/healthz", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if !strings.Contains(body(t, resp), `"status":"ok"`) {
		t.Errorf("body = %s", body(t, resp))
	}
}

func TestPageServedSinglePageOnly(t *testing.T) {
	f := newFixture(t)
	resp := f.get(t, f.pageURL(2), f.granted.ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body(t, resp))
	}
	got := body(t, resp)
	if got != "page two body" {
		t.Errorf("page 2 body = %q", got)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("Cache-Control = %q, want no-store (sensitive content)", cc)
	}
	if xcto := resp.Header.Get("X-Content-Type-Options"); xcto != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", xcto)
	}
}

func TestPageOutOfRangeIs404(t *testing.T) {
	f := newFixture(t)
	resp := f.get(t, f.pageURL(99), f.granted.ID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body(t, resp))
	}
	if !strings.Contains(body(t, resp), "page_not_found") {
		t.Errorf("body = %s", body(t, resp))
	}
}

func TestPageCountIsMetadataOnly(t *testing.T) {
	f := newFixture(t)
	resp := f.get(t, "/v1/viewer/rooms/"+string(f.room.ID)+"/documents/"+string(f.doc.ID)+"/pages",
		f.granted.ID, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body(t, resp))
	}
	var out struct {
		DocumentID string `json:"document_id"`
		PageCount  int    `json:"pageCount"`
	}
	if err := json.Unmarshal([]byte(body(t, resp)), &out); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, body(t, resp))
	}
	if out.PageCount != 3 {
		t.Errorf("pageCount = %d, want 3", out.PageCount)
	}
}

func TestNoWholeDocumentEndpointExists(t *testing.T) {
	f := newFixture(t)
	// Deliberately no route serves the document itself or its versions.
	paths := []string{
		"/v1/viewer/rooms/" + string(f.room.ID) + "/documents/" + string(f.doc.ID),
		"/v1/viewer/rooms/" + string(f.room.ID) + "/documents/" + string(f.doc.ID) + "/versions",
		"/v1/viewer/rooms/" + string(f.room.ID) + "/documents/" + string(f.doc.ID) + "/content",
		"/v1/viewer/documents/" + string(f.doc.ID) + "/pages/1",
	}
	for _, p := range paths {
		resp := f.get(t, p, f.granted.ID, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404 (no whole-document endpoint by design)", p, resp.StatusCode)
		}
	}
}

func TestMethodNotAllowed(t *testing.T) {
	f := newFixture(t)
	req, err := http.NewRequest(http.MethodPost, f.srv.URL+f.pageURL(1), strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-EVDR-Principal", f.granted.ID)
	resp, err := f.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST page = %d, want 405", resp.StatusCode)
	}
}

func TestAccessControl(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name  string
		actor string
		want  int
	}{
		{"no-principal-header", "", http.StatusUnauthorized},
		{"stranger-no-grant", f.stranger.ID, http.StatusForbidden},
		{"granted-viewer", f.granted.ID, http.StatusOK},
		{"guest-with-expiry", f.guest.ID, http.StatusOK},
		{"operator-creator", f.operator.ID, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := f.get(t, f.pageURL(1), tc.actor, nil)
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, body(t, resp))
			}
		})
	}
}

func TestRevokedGrantIsForbiddenImmediately(t *testing.T) {
	f := newFixture(t)
	grants, err := f.adapter.ListVersions(context.Background(), f.tenant, f.doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	_ = grants
	// Revoke the viewer's grant (find it via a fresh grant for a fresh user).
	revoked := spi.Actor{Kind: spi.ActorInternalUser, ID: "revoked-1"}
	grant, err := f.adapter.GrantAccess(context.Background(), f.tenant, f.room.ID, spi.AccessGrant{
		Subject: revoked.ID, ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.adapter.RevokeAccess(context.Background(), f.tenant, f.room.ID, grant.ID); err != nil {
		t.Fatal(err)
	}
	resp := f.get(t, f.pageURL(1), revoked.ID, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("revoked actor status = %d, want 403", resp.StatusCode)
	}
}

func TestUnknownDocumentAndRoom404(t *testing.T) {
	f := newFixture(t)
	resp := f.get(t, "/v1/viewer/rooms/"+string(f.room.ID)+"/documents/nope/pages/1", f.granted.ID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown document = %d, want 404", resp.StatusCode)
	}
	resp = f.get(t, "/v1/viewer/rooms/nope/documents/nope/pages/1", f.granted.ID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown room = %d, want 404", resp.StatusCode)
	}
}

func TestCrossTenantProbeIsIndistinguishableFromNotFound(t *testing.T) {
	// A document that exists in tenant-b must look like a plain 404 through a
	// tenant-a-bound viewer — the adapter scopes doc resolution to its own
	// tenant (no existence oracle), and the roomID path segment carries no
	// tenant meaning.
	f := newFixture(t)
	other := spi.TenantContext{TenantID: "tenant-b", Actor: f.operator}
	otherAdapter := memadapter.New("tenant-b")
	roomB, err := otherAdapter.CreateRoom(context.Background(), other, spi.CreateRoomRequest{
		Name: "B", Slug: "b",
		Retention: spi.RetentionPolicy{Anchor: spi.AnchorUpload, MinRetentionDays: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	docB, err := otherAdapter.PutDocument(context.Background(), other, roomB.ID, spi.PutDocumentRequest{
		Name: "secret-b.txt", ContentType: "text/plain",
		Content: strings.NewReader("tenant b secret\n"), SizeBytes: 16,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp := f.get(t, "/v1/viewer/rooms/"+string(roomB.ID)+"/documents/"+string(docB.ID)+"/pages/1",
		f.operator.ID, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("cross-tenant probe = %d, want 404 (no existence oracle)", resp.StatusCode)
	}
}

func TestVersionSelection(t *testing.T) {
	f := newFixture(t)
	// Upload a second version of the same document.
	v2, err := f.adapter.PutDocument(context.Background(), f.tenant, f.room.ID, spi.PutDocumentRequest{
		Name:        f.doc.Name,
		FolderPath:  "",
		ContentType: "text/plain",
		Content:     strings.NewReader("version two line\n"),
		SizeBytes:   17,
	})
	if err != nil {
		t.Fatal(err)
	}
	versions, err := f.adapter.ListVersions(context.Background(), f.tenant, f.doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("versions = %d, want 2", len(versions))
	}
	// Without ?version= the current (v2) content is served…
	resp := f.get(t, f.pageURL(1), f.granted.ID, nil)
	if got := body(t, resp); got != "version two line" {
		t.Errorf("current version page = %q", got)
	}
	// …and the immutable v1 is addressable explicitly.
	url := f.pageURL(1) + "?version=" + string(versions[0].ID)
	resp = f.get(t, url, f.granted.ID, nil)
	if got := body(t, resp); got != "page one body" {
		t.Errorf("v1 page = %q, want %q", got, "page one body")
	}
	_ = v2
}

func TestInvalidPageParameterIs400(t *testing.T) {
	f := newFixture(t)
	for _, p := range []string{"0", "-3", "abc"} {
		resp := f.get(t, "/v1/viewer/rooms/"+string(f.room.ID)+"/documents/"+string(f.doc.ID)+"/pages/"+p,
			f.granted.ID, nil)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("page %q = %d, want 400", p, resp.StatusCode)
		}
	}
}

func TestInvalidActorKindIs400(t *testing.T) {
	f := newFixture(t)
	resp := f.get(t, f.pageURL(1), f.granted.ID, map[string]string{"X-EVDR-Actor": "admin"})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestErrorEnvelopeIsGeneric(t *testing.T) {
	f := newFixture(t)
	resp := f.get(t, f.pageURL(1), f.stranger.ID, nil)
	got := body(t, resp)
	if strings.Contains(got, "memadapter") || strings.Contains(got, "grant") {
		t.Errorf("error envelope leaks internals: %s", got)
	}
	if !strings.Contains(got, "access_denied") {
		t.Errorf("error envelope lacks code: %s", got)
	}
}

func TestConfigValidation(t *testing.T) {
	if _, err := server.New(server.Config{}); err == nil {
		t.Error("empty config must be rejected")
	}
	if _, err := server.New(server.Config{TenantID: "t", SPI: nil}); err == nil {
		t.Error("nil SPI must be rejected")
	}
}
