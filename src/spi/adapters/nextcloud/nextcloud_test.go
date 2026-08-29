package nextcloud

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ivenkwan/evdr/src/spi"
	"github.com/ivenkwan/evdr/src/spi/conformance"
)

const (
	testUser   = "evdr-service"
	testPass   = "app-password"
	testTenant = spi.TenantID("tenant-a")
)

var operator = spi.Actor{Kind: spi.ActorInternalUser, ID: "op-1", DisplayName: "Operator"}

// ---- fake renderer (viewer-pipeline stand-in) ----

type renderCall struct {
	contentType string
	req         spi.RenderRequest
	content     string
}

// fakeRenderer renders one page per line of content and records every call so
// tests can assert what the adapter passed through (content, watermark).
type fakeRenderer struct {
	mu    sync.Mutex
	calls []renderCall
}

func (f *fakeRenderer) Render(ctx context.Context, content io.Reader, contentType string, req spi.RenderRequest) (spi.RenderStream, error) {
	b, err := io.ReadAll(content)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, renderCall{contentType: contentType, req: req, content: string(b)})
	f.mu.Unlock()
	text := strings.TrimRight(string(b), "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	var pages []spi.Page
	for i, ln := range lines {
		n := i + 1
		if n < req.FirstPage {
			continue
		}
		if req.LastPage > 0 && n > req.LastPage {
			break
		}
		pages = append(pages, spi.Page{Number: n, ContentType: "text/plain", Content: strings.NewReader(ln)})
	}
	return &pageStream{ctx: ctx, pages: pages}, nil
}

func (f *fakeRenderer) last() renderCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return renderCall{}
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeRenderer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// pageStream is a forward-only page stream honouring context cancellation.
type pageStream struct {
	ctx   context.Context
	pages []spi.Page
	i     int
	err   error
}

func (s *pageStream) Next() bool {
	if err := s.ctx.Err(); err != nil {
		s.err = err
		return false
	}
	if s.i >= len(s.pages) {
		return false
	}
	s.i++
	return true
}

func (s *pageStream) Page() spi.Page { return s.pages[s.i-1] }

func (s *pageStream) Err() error { return s.err }

func (s *pageStream) Close() error { return nil }

// ---- test environment ----

type testEnv struct {
	mock     *mockNextcloud
	srv      *httptest.Server
	a        *Adapter
	renderer *fakeRenderer
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	m, srv := newMockNextcloud(t, testUser)
	renderer := &fakeRenderer{}
	a, err := New(Config{
		BaseURL:     srv.URL,
		Username:    testUser,
		AppPassword: testPass,
		TenantID:    testTenant,
		Renderer:    renderer,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(srv.Close)
	return &testEnv{mock: m, srv: srv, a: a, renderer: renderer}
}

func (e *testEnv) tenant(actor spi.Actor) spi.TenantContext {
	return spi.TenantContext{TenantID: testTenant, Actor: actor}
}

func (e *testEnv) createRoom(t *testing.T, slug string) spi.Room {
	t.Helper()
	room, err := e.a.CreateRoom(context.Background(), e.tenant(operator),
		spi.CreateRoomRequest{Name: "Room " + slug, Slug: slug})
	if err != nil {
		t.Fatalf("CreateRoom(%q): %v", slug, err)
	}
	return room
}

func (e *testEnv) putDoc(t *testing.T, roomID spi.RoomID, name, folder, content string) spi.Document {
	t.Helper()
	doc, err := e.a.PutDocument(context.Background(), e.tenant(operator), roomID, spi.PutDocumentRequest{
		Name:        name,
		FolderPath:  folder,
		ContentType: "text/plain",
		Content:     strings.NewReader(content),
		SizeBytes:   int64(len(content)),
	})
	if err != nil {
		t.Fatalf("PutDocument(%q): %v", name, err)
	}
	return doc
}

func (e *testEnv) grantViewer(t *testing.T, roomID spi.RoomID, subject string, kind spi.ActorKind, c spi.GrantConstraints) spi.AccessGrant {
	t.Helper()
	g, err := e.a.GrantAccess(context.Background(), e.tenant(operator), roomID, spi.AccessGrant{
		Subject: subject, ActorKind: kind, Tier: spi.TierViewOnly, Constraints: c,
	})
	if err != nil {
		t.Fatalf("GrantAccess(%s): %v", subject, err)
	}
	return g
}

func wantErr(t *testing.T, err error, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

// ---- CreateRoom ----

func TestCreateRoom(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme-ipo")

		if room.ID == "" {
			t.Error("room.ID must be set")
		}
		if room.Slug != "acme-ipo" || room.State != spi.RoomActive {
			t.Errorf("room = %+v", room)
		}
		if room.Classification != spi.DefaultClassification {
			t.Errorf("classification = %q", room.Classification)
		}
		if room.CreatedBy.ID != operator.ID || room.CreatedAt.IsZero() {
			t.Errorf("CreatedBy/CreatedAt = %+v", room.CreatedBy)
		}

		// WebDAV folder + metadata writes happened against the right paths.
		if !e.mock.hasRequest(func(r mockRequest) bool {
			return r.Method == "MKCOL" && r.URL == "/remote.php/dav/files/"+testUser+"/evdr/acme-ipo"
		}) {
			t.Error("MKCOL for room folder not observed")
		}
		for _, p := range []string{
			"/remote.php/dav/files/" + testUser + "/evdr/acme-ipo/_evdr/room.json",
			"/remote.php/dav/files/" + testUser + "/evdr/acme-ipo/_evdr/grants.json",
		} {
			if !e.mock.hasRequest(func(r mockRequest) bool { return r.Method == http.MethodPut && r.URL == p }) {
				t.Errorf("PUT %s not observed", p)
			}
		}
		// The RoomID→slug index is maintained at the adapter root.
		if !e.mock.hasRequest(func(r mockRequest) bool {
			return r.Method == http.MethodPut && r.URL == "/remote.php/dav/files/"+testUser+"/evdr/_evdr/rooms.json"
		}) {
			t.Error("room index PUT not observed")
		}
		// Auth header present on every request.
		if !e.mock.hasRequest(func(r mockRequest) bool { return r.Header.Get("Authorization") == e.mock.basicHeader(testPass) }) {
			t.Error("Authorization header missing")
		}
	})

	t.Run("slug collision", func(t *testing.T) {
		e := newTestEnv(t)
		e.createRoom(t, "dup")
		_, err := e.a.CreateRoom(context.Background(), e.tenant(operator),
			spi.CreateRoomRequest{Name: "Dup", Slug: "dup"})
		wantErr(t, err, spi.ErrRoomExists)
	})

	t.Run("invalid slug", func(t *testing.T) {
		e := newTestEnv(t)
		for _, slug := range []string{"", "a/b", "..", "a\\b"} {
			if _, err := e.a.CreateRoom(context.Background(), e.tenant(operator),
				spi.CreateRoomRequest{Name: "X", Slug: slug}); err == nil {
				t.Errorf("slug %q: expected error", slug)
			}
		}
	})

	t.Run("cross-tenant rejected", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.a.CreateRoom(context.Background(), spi.TenantContext{TenantID: "other", Actor: operator},
			spi.CreateRoomRequest{Name: "X", Slug: "x"})
		wantErr(t, err, spi.ErrAccessDenied)
	})
}

func TestCreateRoomRollback(t *testing.T) {
	e := newTestEnv(t)
	// Simulate a backend failure while writing the room metadata: the room
	// folder must be rolled back (best effort).
	e.mock.failStatus = map[string]int{"/remote.php/dav/files/" + testUser + "/evdr/boom/_evdr/room.json": http.StatusInternalServerError}
	_, err := e.a.CreateRoom(context.Background(), e.tenant(operator), spi.CreateRoomRequest{Name: "Boom", Slug: "boom"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !e.mock.hasRequest(func(r mockRequest) bool {
		return r.Method == "DELETE" && r.URL == "/remote.php/dav/files/"+testUser+"/evdr/boom"
	}) {
		t.Error("room folder not rolled back after failed CreateRoom")
	}
}

// ---- GrantAccess ----

func TestGrantAccess(t *testing.T) {
	cases := []struct {
		name    string
		kind    spi.ActorKind
		tier    spi.PermissionTier
		subject string
		notAfter time.Time
		wantShareType, wantPermissions, wantShareWith, wantExpire string
		wantErr error
	}{
		{
			name: "internal view-only", kind: spi.ActorInternalUser, tier: spi.TierViewOnly, subject: "alice",
			wantShareType: "0", wantPermissions: "1", wantShareWith: "alice",
		},
		{
			name: "internal download", kind: spi.ActorInternalUser, tier: spi.TierDownloadAllowed, subject: "bob",
			wantShareType: "0", wantPermissions: "1", wantShareWith: "bob",
		},
		{
			name: "internal edit", kind: spi.ActorInternalUser, tier: spi.TierEditAllowed, subject: "carol",
			wantShareType: "0", wantPermissions: "15", wantShareWith: "carol",
		},
		{
			name: "service account", kind: spi.ActorService, tier: spi.TierViewOnly, subject: "svc-audit",
			wantShareType: "0", wantPermissions: "1", wantShareWith: "svc-audit",
		},
		{
			name: "guest with expiry", kind: spi.ActorGuest, tier: spi.TierViewOnly, subject: "guest@example.com",
			notAfter: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			wantShareType: "3", wantPermissions: "1", wantExpire: "2026-09-01",
		},
		{
			name: "guest without expiry", kind: spi.ActorGuest, tier: spi.TierViewOnly, subject: "g@example.com",
			wantErr: spi.ErrInvalidGrant,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			room := e.createRoom(t, "acme")

			grant, err := e.a.GrantAccess(context.Background(), e.tenant(operator), room.ID, spi.AccessGrant{
				Subject: tc.subject, ActorKind: tc.kind, Tier: tc.tier,
				Constraints: spi.GrantConstraints{NotAfter: tc.notAfter},
			})
			if tc.wantErr != nil {
				wantErr(t, err, tc.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("GrantAccess: %v", err)
			}
			if grant.ID == "" || grant.Subject != tc.subject || grant.Tier != tc.tier {
				t.Errorf("grant = %+v", grant)
			}
			// The OCS share create carries the expected mapping.
			var form url.Values
			_ = e.mock.hasRequest(func(r mockRequest) bool {
				if r.Method != http.MethodPost || !strings.Contains(r.URL, "/ocs/v2.php/apps/files_sharing/api/v1/shares") {
					return false
				}
				if r.Header.Get("OCS-APIRequest") != "true" {
					t.Error("OCS-APIRequest header missing")
				}
				form = r.Form
				return true
			})
			if form == nil {
				t.Fatal("no OCS share create observed")
			}
			if got := form.Get("path"); got != "/evdr/acme" {
				t.Errorf("OCS path = %q", got)
			}
			if got := form.Get("shareType"); got != tc.wantShareType {
				t.Errorf("shareType = %q, want %q", got, tc.wantShareType)
			}
			if got := form.Get("permissions"); got != tc.wantPermissions {
				t.Errorf("permissions = %q, want %q", got, tc.wantPermissions)
			}
			if got := form.Get("shareWith"); got != tc.wantShareWith {
				t.Errorf("shareWith = %q, want %q", got, tc.wantShareWith)
			}
			if got := form.Get("expireDate"); got != tc.wantExpire {
				t.Errorf("expireDate = %q, want %q", got, tc.wantExpire)
			}
		})
	}
}

func TestGrantAccessErrors(t *testing.T) {
	t.Run("unknown room", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.a.GrantAccess(context.Background(), e.tenant(operator), spi.RoomID("nope"),
			spi.AccessGrant{Subject: "u", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly})
		wantErr(t, err, spi.ErrRoomNotFound)
	})
	t.Run("cross-tenant", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.a.GrantAccess(context.Background(), spi.TenantContext{TenantID: "other", Actor: operator},
			spi.RoomID("nope"), spi.AccessGrant{Subject: "u", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly})
		wantErr(t, err, spi.ErrRoomNotFound)
	})
	t.Run("sealed room", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "sealed")
		if _, err := e.a.SealRoom(context.Background(), e.tenant(operator), room.ID, spi.SealRequest{Reason: "hold"}); err != nil {
			t.Fatal(err)
		}
		_, err := e.a.GrantAccess(context.Background(), e.tenant(operator), room.ID,
			spi.AccessGrant{Subject: "u", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly})
		wantErr(t, err, spi.ErrRoomSealed)
	})
	t.Run("inverted window", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "window")
		_, err := e.a.GrantAccess(context.Background(), e.tenant(operator), room.ID, spi.AccessGrant{
			Subject: "u", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly,
			Constraints: spi.GrantConstraints{
				NotBefore: time.Now().Add(2 * time.Hour),
				NotAfter:  time.Now().Add(time.Hour),
			},
		})
		wantErr(t, err, spi.ErrInvalidGrant)
	})
}

// ---- RevokeAccess ----

func TestRevokeAccess(t *testing.T) {
	t.Run("revoke deletes share and is idempotent", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		grant := e.grantViewer(t, room.ID, "alice", spi.ActorInternalUser, spi.GrantConstraints{})

		if err := e.a.RevokeAccess(context.Background(), e.tenant(operator), room.ID, grant.ID); err != nil {
			t.Fatalf("RevokeAccess: %v", err)
		}
		if !e.mock.hasRequest(func(r mockRequest) bool {
			return r.Method == http.MethodDelete && r.URL == "/ocs/v2.php/apps/files_sharing/api/v1/shares/1"
		}) {
			t.Error("OCS share DELETE not observed")
		}
		// Re-revoking is idempotent-safe: it re-reads the ledger but must not
		// issue another OCS share DELETE.
		deletesBefore := 0
		for _, r := range e.mock.requests {
			if r.Method == http.MethodDelete {
				deletesBefore++
			}
		}
		if err := e.a.RevokeAccess(context.Background(), e.tenant(operator), room.ID, grant.ID); err != nil {
			t.Fatalf("re-revoke: %v", err)
		}
		deletesAfter := 0
		for _, r := range e.mock.requests {
			if r.Method == http.MethodDelete {
				deletesAfter++
			}
		}
		if deletesAfter != deletesBefore {
			t.Error("re-revoke issued an OCS share DELETE")
		}
	})
	t.Run("unknown grant", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		err := e.a.RevokeAccess(context.Background(), e.tenant(operator), room.ID, spi.GrantID("nope"))
		wantErr(t, err, spi.ErrGrantNotFound)
	})
	t.Run("unknown room", func(t *testing.T) {
		e := newTestEnv(t)
		err := e.a.RevokeAccess(context.Background(), e.tenant(operator), spi.RoomID("nope"), spi.GrantID("g"))
		wantErr(t, err, spi.ErrRoomNotFound)
	})
	t.Run("cross-tenant", func(t *testing.T) {
		e := newTestEnv(t)
		err := e.a.RevokeAccess(context.Background(), spi.TenantContext{TenantID: "other", Actor: operator}, spi.RoomID("nope"), spi.GrantID("g"))
		wantErr(t, err, spi.ErrRoomNotFound)
	})
}

// ---- PutDocument ----

func TestPutDocument(t *testing.T) {
	t.Run("new document version 1", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "report.pdf", "", "hello world")

		if doc.ID == "" || doc.RoomID != room.ID || doc.Name != "report.pdf" {
			t.Errorf("doc = %+v", doc)
		}
		if doc.SizeBytes != int64(len("hello world")) {
			t.Errorf("size = %d", doc.SizeBytes)
		}
		// PUT to the live path and COPY to the immutable archive v1.
		if !e.mock.hasRequest(func(r mockRequest) bool {
			return r.Method == http.MethodPut && r.URL == "/remote.php/dav/files/"+testUser+"/evdr/acme/docs/report.pdf"
		}) {
			t.Error("live PUT not observed")
		}
		if !e.mock.hasRequest(func(r mockRequest) bool {
			return r.Method == "COPY" && strings.Contains(r.Header.Get("Destination"), "/evdr/acme/_evdr/versions/"+string(doc.ID)+"/v1")
		}) {
			t.Error("archive COPY v1 not observed")
		}
		// The recorded digest matches the streamed content.
		sum := sha256.Sum256([]byte("hello world"))
		if got := hex.EncodeToString(sum[:]); got != "" {
			versions, err := e.a.ListVersions(context.Background(), e.tenant(operator), doc.ID)
			if err != nil {
				t.Fatal(err)
			}
			if versions[0].SHA256 != got {
				t.Errorf("version sha256 = %s, want %s", versions[0].SHA256, got)
			}
		}
	})

	t.Run("re-upload creates version N+1", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		e.putDoc(t, room.ID, "report.pdf", "", "v1 content")
		doc2 := e.putDoc(t, room.ID, "report.pdf", "", "v2 content")

		if !e.mock.hasRequest(func(r mockRequest) bool {
			return r.Method == "COPY" && strings.Contains(r.Header.Get("Destination"), "/evdr/acme/_evdr/versions/"+string(doc2.ID)+"/v2")
		}) {
			t.Error("archive COPY v2 not observed")
		}
		versions, err := e.a.ListVersions(context.Background(), e.tenant(operator), doc2.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(versions) != 2 || versions[0].Number != 1 || versions[1].Number != 2 {
			t.Fatalf("versions = %+v", versions)
		}
	})

	t.Run("folder hierarchy creates collections", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "deal.pdf", "legal/nda", "x")

		if doc.FolderPath != "legal/nda" {
			t.Errorf("FolderPath = %q", doc.FolderPath)
		}
		for _, dir := range []string{"docs", "docs/legal", "docs/legal/nda"} {
			if !e.mock.hasRequest(func(r mockRequest) bool {
				return r.Method == "MKCOL" && r.URL == "/remote.php/dav/files/"+testUser+"/evdr/acme/"+dir
			}) {
				t.Errorf("MKCOL %s not observed", dir)
			}
		}
		if !e.mock.hasRequest(func(r mockRequest) bool {
			return r.Method == http.MethodPut && r.URL == "/remote.php/dav/files/"+testUser+"/evdr/acme/docs/legal/nda/deal.pdf"
		}) {
			t.Error("nested PUT not observed")
		}
	})

	t.Run("unknown room", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.a.PutDocument(context.Background(), e.tenant(operator), spi.RoomID("nope"),
			spi.PutDocumentRequest{Name: "a.txt", Content: strings.NewReader("x")})
		wantErr(t, err, spi.ErrRoomNotFound)
	})

	t.Run("cross-tenant", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.a.PutDocument(context.Background(), spi.TenantContext{TenantID: "other", Actor: operator},
			spi.RoomID("nope"), spi.PutDocumentRequest{Name: "a.txt", Content: strings.NewReader("x")})
		wantErr(t, err, spi.ErrRoomNotFound)
	})

	t.Run("sealed room", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "sealed")
		if _, err := e.a.SealRoom(context.Background(), e.tenant(operator), room.ID, spi.SealRequest{Reason: "hold"}); err != nil {
			t.Fatal(err)
		}
		_, err := e.a.PutDocument(context.Background(), e.tenant(operator), room.ID,
			spi.PutDocumentRequest{Name: "a.txt", Content: strings.NewReader("x")})
		wantErr(t, err, spi.ErrRoomSealed)
	})

	t.Run("quota exceeded", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "quota")
		e.mock.failStatus = map[string]int{"/remote.php/dav/files/" + testUser + "/evdr/quota/docs": http.StatusInsufficientStorage}
		_, err := e.a.PutDocument(context.Background(), e.tenant(operator), room.ID,
			spi.PutDocumentRequest{Name: "a.txt", Content: strings.NewReader("x")})
		wantErr(t, err, spi.ErrQuotaExceeded)
	})
}

// ---- GetRenderStream ----

func TestGetRenderStream(t *testing.T) {
	t.Run("granted viewer renders pages with watermark", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "page one\npage two\npage three")
		e.grantViewer(t, room.ID, "alice", spi.ActorInternalUser, spi.GrantConstraints{})

		watermark := spi.WatermarkSpec{ViewerIdentity: "alice", Timestamp: time.Now(), ClientIP: "10.0.0.1", SessionID: "s-1", Density: "dense", Opacity: 0.75, Rotation: 15}
		stream, err := e.a.GetRenderStream(context.Background(), e.tenant(spi.Actor{Kind: spi.ActorInternalUser, ID: "alice"}),
			doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 2, Watermark: watermark})
		if err != nil {
			t.Fatalf("GetRenderStream: %v", err)
		}
		var nums []int
		for stream.Next() {
			nums = append(nums, stream.Page().Number)
		}
		stream.Close()
		if stream.Err() != nil {
			t.Fatalf("stream err: %v", stream.Err())
		}
		if len(nums) != 2 || nums[0] != 1 || nums[1] != 2 {
			t.Errorf("pages = %v, want [1 2]", nums)
		}
		// The renderer received the immutable archive content and the
		// watermark spec unchanged.
		call := e.renderer.last()
		if call.content != "page one\npage two\npage three" {
			t.Errorf("renderer content = %q", call.content)
		}
		if call.req.Watermark != watermark {
			t.Errorf("watermark = %+v, want %+v", call.req.Watermark, watermark)
		}
		// The version archive was fetched over WebDAV.
		if !e.mock.hasRequest(func(r mockRequest) bool {
			return r.Method == http.MethodGet && strings.Contains(r.URL, "/_evdr/versions/"+string(doc.ID)+"/v1")
		}) {
			t.Error("version archive GET not observed")
		}
	})

	t.Run("stranger denied", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "x")
		_, err := e.a.GetRenderStream(context.Background(), e.tenant(spi.Actor{Kind: spi.ActorInternalUser, ID: "stranger"}),
			doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
		wantErr(t, err, spi.ErrAccessDenied)
		if e.renderer.callCount() != 0 {
			t.Error("renderer must not be invoked for denied access")
		}
	})

	t.Run("expired grant denied", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "x")
		e.grantViewer(t, room.ID, "alice", spi.ActorInternalUser,
			spi.GrantConstraints{NotAfter: time.Now().Add(-time.Hour).UTC()})
		_, err := e.a.GetRenderStream(context.Background(), e.tenant(spi.Actor{Kind: spi.ActorInternalUser, ID: "alice"}),
			doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
		wantErr(t, err, spi.ErrAccessDenied)
	})

	t.Run("revoked grant denied", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "x")
		grant := e.grantViewer(t, room.ID, "alice", spi.ActorInternalUser, spi.GrantConstraints{})
		if err := e.a.RevokeAccess(context.Background(), e.tenant(operator), room.ID, grant.ID); err != nil {
			t.Fatal(err)
		}
		_, err := e.a.GetRenderStream(context.Background(), e.tenant(spi.Actor{Kind: spi.ActorInternalUser, ID: "alice"}),
			doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
		wantErr(t, err, spi.ErrAccessDenied)
	})

	t.Run("unknown document", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.a.GetRenderStream(context.Background(), e.tenant(operator), spi.DocumentID("nope"),
			spi.RenderRequest{FirstPage: 1, LastPage: 1})
		wantErr(t, err, spi.ErrDocumentNotFound)
	})

	t.Run("cross-tenant", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "x")
		_, err := e.a.GetRenderStream(context.Background(), spi.TenantContext{TenantID: "other", Actor: operator},
			doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
		wantErr(t, err, spi.ErrDocumentNotFound)
	})

	t.Run("context cancellation unblocks stream", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "one\ntwo")
		e.grantViewer(t, room.ID, "alice", spi.ActorInternalUser, spi.GrantConstraints{})

		ctx, cancel := context.WithCancel(context.Background())
		stream, err := e.a.GetRenderStream(ctx, e.tenant(spi.Actor{Kind: spi.ActorInternalUser, ID: "alice"}),
			doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 10})
		if err != nil {
			t.Fatalf("GetRenderStream: %v", err)
		}
		defer stream.Close()
		cancel()
		if stream.Next() {
			t.Error("Next returned true after cancellation")
		}
		if !errors.Is(stream.Err(), context.Canceled) {
			t.Errorf("Err = %v, want context.Canceled", stream.Err())
		}
	})

	t.Run("no renderer configured", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "x")
		noRenderer, err := New(Config{BaseURL: e.srv.URL, Username: testUser, AppPassword: testPass, TenantID: testTenant})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := noRenderer.GetRenderStream(context.Background(), e.tenant(operator), doc.ID,
			spi.RenderRequest{FirstPage: 1, LastPage: 1}); !errors.Is(err, spi.ErrUnsupported) {
			t.Fatalf("error = %v, want ErrUnsupported", err)
		}
	})
}

// ---- ListVersions ----

func TestListVersions(t *testing.T) {
	t.Run("ascending", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "one")
		e.putDoc(t, room.ID, "a.txt", "", "two")

		versions, err := e.a.ListVersions(context.Background(), e.tenant(operator), doc.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(versions) != 2 || versions[0].Number != 1 || versions[1].Number != 2 {
			t.Fatalf("versions = %+v", versions)
		}
		if versions[0].SHA256 == "" || versions[1].SHA256 == "" {
			t.Error("digests missing")
		}
	})
	t.Run("unknown document", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.a.ListVersions(context.Background(), e.tenant(operator), spi.DocumentID("nope"))
		wantErr(t, err, spi.ErrDocumentNotFound)
	})
	t.Run("cross-tenant", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		doc := e.putDoc(t, room.ID, "a.txt", "", "x")
		_, err := e.a.ListVersions(context.Background(), spi.TenantContext{TenantID: "other", Actor: operator}, doc.ID)
		wantErr(t, err, spi.ErrDocumentNotFound)
	})
}

// ---- ApplyRetention ----

func TestApplyRetention(t *testing.T) {
	t.Run("extend ok, shorten violates", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme") // floor 0 (no retention set)
		if err := e.a.ApplyRetention(context.Background(), e.tenant(operator), room.ID,
			spi.RetentionPolicy{Anchor: spi.AnchorUpload, MinRetentionDays: 90}); err != nil {
			t.Fatalf("extend: %v", err)
		}
		err := e.a.ApplyRetention(context.Background(), e.tenant(operator), room.ID,
			spi.RetentionPolicy{Anchor: spi.AnchorUpload, MinRetentionDays: 30})
		wantErr(t, err, spi.ErrRetentionViolation)
	})
	t.Run("unknown room", func(t *testing.T) {
		e := newTestEnv(t)
		err := e.a.ApplyRetention(context.Background(), e.tenant(operator), spi.RoomID("nope"),
			spi.RetentionPolicy{MinRetentionDays: 90})
		wantErr(t, err, spi.ErrRoomNotFound)
	})
	t.Run("sealed room", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		if _, err := e.a.SealRoom(context.Background(), e.tenant(operator), room.ID, spi.SealRequest{Reason: "hold"}); err != nil {
			t.Fatal(err)
		}
		err := e.a.ApplyRetention(context.Background(), e.tenant(operator), room.ID,
			spi.RetentionPolicy{MinRetentionDays: 90})
		wantErr(t, err, spi.ErrRoomSealed)
	})
}

// ---- SealRoom ----

func TestSealRoom(t *testing.T) {
	e := newTestEnv(t)
	room := e.createRoom(t, "acme")
	e.putDoc(t, room.ID, "a.txt", "", "one")
	e.putDoc(t, room.ID, "a.txt", "", "two")
	grant := e.grantViewer(t, room.ID, "alice", spi.ActorInternalUser, spi.GrantConstraints{})

	receipt, err := e.a.SealRoom(context.Background(), e.tenant(operator), room.ID,
		spi.SealRequest{Reason: "litigation", LegalHoldRef: "case-42"})
	if err != nil {
		t.Fatalf("SealRoom: %v", err)
	}
	if receipt.ID == "" || receipt.RoomID != room.ID || receipt.SealedAt.IsZero() {
		t.Errorf("receipt = %+v", receipt)
	}
	if receipt.FrozenObjects != 3 {
		t.Errorf("FrozenObjects = %d, want 3", receipt.FrozenObjects)
	}
	// Seal receipt persisted: re-seal returns the stored one.
	again, err := e.a.SealRoom(context.Background(), e.tenant(operator), room.ID, spi.SealRequest{Reason: "again"})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != receipt.ID {
		t.Errorf("re-seal receipt %s != %s", again.ID, receipt.ID)
	}

	// Mutations blocked; reads fine; revocation still permitted.
	if _, err := e.a.GrantAccess(context.Background(), e.tenant(operator), room.ID,
		spi.AccessGrant{Subject: "x", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly}); !errors.Is(err, spi.ErrRoomSealed) {
		t.Errorf("grant on sealed = %v", err)
	}
	if err := e.a.RevokeAccess(context.Background(), e.tenant(operator), room.ID, grant.ID); err != nil {
		t.Errorf("revoke on sealed must be allowed: %v", err)
	}
	if _, err := e.a.ListVersions(context.Background(), e.tenant(operator), spi.DocumentID("x")); !errors.Is(err, spi.ErrDocumentNotFound) {
		t.Errorf("read on sealed = %v", err)
	}
}

// ---- ExportRoom ----

func TestExportRoom(t *testing.T) {
	t.Run("archive matches manifest digests", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		e.putDoc(t, room.ID, "a.txt", "", "version one\npage two\npage three")
		e.putDoc(t, room.ID, "a.txt", "", "version two\npage two\npage three")
		e.putDoc(t, room.ID, "b.txt", "sub", "other doc")
		e.grantViewer(t, room.ID, "alice", spi.ActorInternalUser, spi.GrantConstraints{})

		pkg, err := e.a.ExportRoom(context.Background(), e.tenant(operator), room.ID, spi.ExportOptions{
			IncludeVersions: true, IncludeAuditTrail: true,
		})
		if err != nil {
			t.Fatalf("ExportRoom: %v", err)
		}
		defer pkg.Content.Close()

		if pkg.RoomID != room.ID || len(pkg.IntegrityLetter) == 0 {
			t.Errorf("package = %+v", pkg)
		}
		verifyTar(t, pkg)

		docEntries := 0
		for _, en := range pkg.Manifest.Entries {
			if strings.HasPrefix(en.Path, "docs/") {
				docEntries++
			}
		}
		if docEntries != 3 { // a v1, a v2, b v1
			t.Errorf("doc entries = %d, want 3", docEntries)
		}
	})

	t.Run("current version only by default", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		e.putDoc(t, room.ID, "a.txt", "", "one")
		e.putDoc(t, room.ID, "a.txt", "", "two")

		pkg, err := e.a.ExportRoom(context.Background(), e.tenant(operator), room.ID, spi.ExportOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer pkg.Content.Close()
		verifyTar(t, pkg)
		docEntries := 0
		for _, en := range pkg.Manifest.Entries {
			if strings.HasPrefix(en.Path, "docs/") {
				docEntries++
			}
		}
		if docEntries != 1 {
			t.Errorf("doc entries = %d, want 1 (current only)", docEntries)
		}
	})

	t.Run("sealed room export includes seal receipt", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		e.putDoc(t, room.ID, "a.txt", "", "one")
		if _, err := e.a.SealRoom(context.Background(), e.tenant(operator), room.ID, spi.SealRequest{Reason: "ediscovery"}); err != nil {
			t.Fatal(err)
		}
		pkg, err := e.a.ExportRoom(context.Background(), e.tenant(operator), room.ID, spi.ExportOptions{IncludeAuditTrail: true})
		if err != nil {
			t.Fatalf("sealed export: %v", err)
		}
		defer pkg.Content.Close()
		verifyTar(t, pkg)
		sealSeen := false
		for _, en := range pkg.Manifest.Entries {
			if en.Path == "audit/seal.json" {
				sealSeen = true
			}
		}
		if !sealSeen {
			t.Error("sealed export missing audit/seal.json")
		}
	})

	t.Run("stranger denied", func(t *testing.T) {
		e := newTestEnv(t)
		room := e.createRoom(t, "acme")
		e.putDoc(t, room.ID, "a.txt", "", "one")
		_, err := e.a.ExportRoom(context.Background(), e.tenant(spi.Actor{Kind: spi.ActorInternalUser, ID: "stranger"}),
			room.ID, spi.ExportOptions{})
		wantErr(t, err, spi.ErrAccessDenied)
	})

	t.Run("unknown room", func(t *testing.T) {
		e := newTestEnv(t)
		_, err := e.a.ExportRoom(context.Background(), e.tenant(operator), spi.RoomID("nope"), spi.ExportOptions{})
		wantErr(t, err, spi.ErrRoomNotFound)
	})
}

// verifyTar walks an export archive and checks every hashed entry against the
// manifest.
func verifyTar(t *testing.T, pkg spi.ExportPackage) {
	t.Helper()
	digests := map[string]spi.ManifestEntry{}
	for _, en := range pkg.Manifest.Entries {
		digests[en.Path] = en
	}
	tr := tar.NewReader(pkg.Content)
	checked := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if hdr.Name == "manifest.json" || hdr.Name == "INTEGRITY_LETTER.txt" {
			continue
		}
		en, ok := digests[hdr.Name]
		if !ok {
			t.Fatalf("entry %q missing from manifest", hdr.Name)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		if got := hex.EncodeToString(sum[:]); got != en.SHA256 {
			t.Errorf("entry %q digest mismatch", hdr.Name)
		}
		if int64(len(content)) != en.SizeBytes {
			t.Errorf("entry %q size mismatch", hdr.Name)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no entries verified")
	}
}

// ---- error translation ----

func TestOCSErrorTranslation(t *testing.T) {
	e := newTestEnv(t)
	room := e.createRoom(t, "acme")
	// Force an OCS failure on share create: the error must wrap the OCS
	// details, not be silently swallowed.
	e.mock.failStatus = map[string]int{"/ocs/v2.php/apps/files_sharing/api/v1/shares": http.StatusInternalServerError}
	_, err := e.a.GrantAccess(context.Background(), e.tenant(operator), room.ID,
		spi.AccessGrant{Subject: "u", ActorKind: spi.ActorInternalUser, Tier: spi.TierViewOnly})
	if err == nil {
		t.Fatal("expected OCS failure error")
	}
	if errors.Is(err, spi.ErrAccessDenied) {
		t.Errorf("transport failure must not map to ErrAccessDenied: %v", err)
	}
}

// ---- conformance suite wiring (TR-2.4) ----

// ncHarness wires the shared conformance suite to the Nextcloud adapter with
// the mock server as backend.
type ncHarness struct{}

func (ncHarness) New(t *testing.T) (spi.RoomSPI, spi.TenantContext, func()) {
	t.Helper()
	_, srv := newMockNextcloud(t, testUser)
	a, err := New(Config{
		BaseURL:     srv.URL,
		Username:    testUser,
		AppPassword: testPass,
		TenantID:    testTenant,
		Renderer:    &fakeRenderer{},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a, spi.TenantContext{TenantID: testTenant, Actor: operator}, srv.Close
}

// TestConformanceSuite runs the shared SPI conformance suite against the
// Nextcloud adapter (TR-2.4). This is the same suite the NativeAdapter must
// pass in Phase 2.5.
func TestConformanceSuite(t *testing.T) {
	conformance.RunSuite(t, ncHarness{})
}
