package room_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ivenkwan/evdr/src/services/room"
	"github.com/ivenkwan/evdr/src/spi"
	"github.com/ivenkwan/evdr/src/spi/conformance/memadapter"
)

// testClient is a tiny JSON client for the room service under test.
type testClient struct {
	t      *testing.T
	ts     *httptest.Server
	secret string
}

func (c *testClient) do(method, path string, body any, header map[string]string) *http.Response {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.ts.URL+path, rdr)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.ts.Client().Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

func (c *testClient) raw(method, path string) *http.Response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.ts.URL+path, nil)
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	resp, err := c.ts.Client().Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// jsonBody decodes a response body as a generic map (t.Fatal on error).
func jsonBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body (status %d): %v", resp.StatusCode, err)
	}
	return out
}

// expectStatus asserts resp.StatusCode == want and drains the body.
func expectStatus(t *testing.T, resp *http.Response, want int) map[string]any {
	t.Helper()
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d; body: %s", resp.StatusCode, want, string(b))
	}
	var out map[string]any
	if len(b) > 0 {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

// newServer builds a room service over a memadapter bound to adapterTenant,
// authenticating requests as authTenant (both default "tenant-a").
func newServer(t *testing.T, adapterTenant, authTenant spi.TenantID, maxUpload int64) (*httptest.Server, *testClient) {
	t.Helper()
	if adapterTenant == "" {
		adapterTenant = "tenant-a"
	}
	if authTenant == "" {
		authTenant = adapterTenant
	}
	secret := "dev-secret-for-tests"
	auth, err := room.NewDevAuthenticator(room.DevPrincipalConfig{
		Secret:   secret,
		TenantID: authTenant,
		UserID:   "operator-1@evdr.local",
	})
	if err != nil {
		t.Fatalf("dev authenticator: %v", err)
	}
	backend := memadapter.New(adapterTenant)
	sink := &recordingSink{events: make(chan room.Event, 64)}
	srv, err := room.New(room.Config{
		SPI:            backend,
		Auth:           auth,
		Events:         sink,
		MaxUploadBytes: maxUpload,
	})
	if err != nil {
		t.Fatalf("room service: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, &testClient{t: t, ts: ts, secret: secret}
}

// recordingSink captures emitted events for assertions.
type recordingSink struct{ events chan room.Event }

func (r *recordingSink) Emit(_ context.Context, ev room.Event) { r.events <- ev }

func (r *recordingSink) drain() []room.Event {
	out := make([]room.Event, 0, len(r.events))
	for {
		select {
		case ev := <-r.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

const testSecret = "dev-secret-for-tests"

func createRoom(t *testing.T, c *testClient, slug string, body map[string]any) map[string]any {
	t.Helper()
	if body == nil {
		body = map[string]any{"name": "Acme M&A Room", "slug": slug}
	}
	resp := c.do("POST", "/api/v1/rooms", body, nil)
	out := expectStatus(t, resp, http.StatusCreated)
	t.Logf("created room slug=%s id=%v", slug, out["id"])
	return out
}

func uploadFile(t *testing.T, c *testClient, roomID string, fileName string, content []byte, fields map[string]string) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("write field %s: %v", k, err)
		}
	}
	part, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write content: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	req, err := http.NewRequest("POST", c.ts.URL+"/api/v1/rooms/"+roomID+"/documents", &buf)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.secret)
	resp, err := c.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

func TestAuthRequired(t *testing.T) {
	ts, c := newServer(t, "", "", 0)
	defer ts.Close()

	// No credential → 401 unauthenticated.
	resp := c.raw("GET", "/api/v1/rooms")
	out := expectStatus(t, resp, http.StatusUnauthorized)
	if out["code"] != "unauthenticated" {
		t.Fatalf("code = %v, want unauthenticated", out["code"])
	}
	// Wrong secret → 401.
	resp = c.do("GET", "/api/v1/rooms", nil, map[string]string{"Authorization": "Bearer wrong"})
	expectStatus(t, resp, http.StatusUnauthorized)
	// Malformed scheme → 401.
	resp = c.do("GET", "/api/v1/rooms", nil, map[string]string{"Authorization": "Basic dXNlcjpwYXNz"})
	expectStatus(t, resp, http.StatusUnauthorized)
	// Healthz stays open.
	resp = c.raw("GET", "/healthz")
	out = expectStatus(t, resp, http.StatusOK)
	if out["status"] != "ok" {
		t.Fatalf("healthz status = %v", out["status"])
	}
}

// ---------------------------------------------------------------------------
// Room lifecycle
// ---------------------------------------------------------------------------

func TestRoomLifecycle(t *testing.T) {
	_, c := newServer(t, "", "", 0)

	createRoom(t, c, "acme-ma", nil)

	// Duplicate slug → 409 room_exists (SPI sentinel mapped).
	resp := c.do("POST", "/api/v1/rooms", map[string]any{"name": "Acme Again", "slug": "acme-ma"}, nil)
	out := expectStatus(t, resp, http.StatusConflict)
	if out["code"] != "room_exists" {
		t.Fatalf("code = %v, want room_exists", out["code"])
	}
	// Invalid body → 400.
	resp = c.do("POST", "/api/v1/rooms", map[string]any{"name": "", "slug": "x"}, nil)
	expectStatus(t, resp, http.StatusBadRequest)
	resp = c.do("POST", "/api/v1/rooms", map[string]any{"name": "X", "slug": "UPPER_case!"}, nil)
	expectStatus(t, resp, http.StatusBadRequest)
	resp = c.do("POST", "/api/v1/rooms", map[string]any{"name": "X", "slug": "ok", "classification": "SECRET"}, nil)
	expectStatus(t, resp, http.StatusBadRequest)

	// List shows exactly the one created room.
	resp = c.do("GET", "/api/v1/rooms", nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	rooms := out["rooms"].([]any)
	if len(rooms) != 1 {
		t.Fatalf("rooms len = %d, want 1", len(rooms))
	}
	room := rooms[0].(map[string]any)
	if room["name"] != "Acme M&A Room" || room["state"] != "active" {
		t.Fatalf("unexpected room: %v", room)
	}
	if room["classification"] != "CONFIDENTIAL" {
		t.Fatalf("classification = %v, want default CONFIDENTIAL", room["classification"])
	}
	createdBy := room["createdBy"].(map[string]any)
	if createdBy["id"] != "operator-1@evdr.local" {
		t.Fatalf("createdBy.id = %v", createdBy["id"])
	}
	roomID := room["id"].(string)

	// Get by id.
	resp = c.do("GET", "/api/v1/rooms/"+roomID, nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	if out["slug"] != "acme-ma" {
		t.Fatalf("slug = %v", out["slug"])
	}
	// Unknown room → 404 room_not_found.
	resp = c.do("GET", "/api/v1/rooms/does-not-exist", nil, nil)
	out = expectStatus(t, resp, http.StatusNotFound)
	if out["code"] != "room_not_found" {
		t.Fatalf("code = %v, want room_not_found", out["code"])
	}
}

// ---------------------------------------------------------------------------
// Wrong-tenant probes surface the SPI's own errors
// ---------------------------------------------------------------------------

func TestWrongTenantProbesSurfaceSPIErrors(t *testing.T) {
	// Service authenticates principals in tenant-a but the adapter is bound
	// to tenant-b (mis-wired dev principal). All SPI calls must fail with the
	// SPI's own sentinels and never leak tenant-b's existence.
	_, c := newServer(t, "tenant-b", "tenant-a", 0)

	// Seed a room under tenant-b directly through the SPI.
	backend := memadapter.New("tenant-b")
	tc := spi.TenantContext{TenantID: "tenant-b", Actor: spi.Actor{Kind: spi.ActorInternalUser, ID: "other-operator"}}
	seeded, err := backend.CreateRoom(context.Background(), tc, spi.CreateRoomRequest{
		Name: "Secret room of tenant B", Slug: "tenant-b-only",
	})
	if err != nil {
		t.Fatalf("seed tenant-b room: %v", err)
	}

	// CreateRoom with the wrong tenant → spi.ErrAccessDenied → 403.
	resp := c.do("POST", "/api/v1/rooms", map[string]any{"name": "Probe", "slug": "probe"}, nil)
	out := expectStatus(t, resp, http.StatusForbidden)
	if out["code"] != "access_denied" {
		t.Fatalf("create code = %v, want access_denied", out["code"])
	}
	// Room-scoped probe on tenant-b's real room → room_not_found (no
	// existence oracle).
	resp = c.do("GET", "/api/v1/rooms/"+string(seeded.ID), nil, nil)
	out = expectStatus(t, resp, http.StatusNotFound)
	if out["code"] != "room_not_found" {
		t.Fatalf("get code = %v, want room_not_found", out["code"])
	}
	resp = c.do("GET", "/api/v1/rooms/"+string(seeded.ID)+"/documents", nil, nil)
	out = expectStatus(t, resp, http.StatusNotFound)
	if out["code"] != "room_not_found" {
		t.Fatalf("documents code = %v, want room_not_found", out["code"])
	}
	resp = c.do("GET", "/api/v1/rooms/"+string(seeded.ID)+"/grants", nil, nil)
	expectStatus(t, resp, http.StatusNotFound)
	// Own tenant list is empty and harmless.
	resp = c.do("GET", "/api/v1/rooms", nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	if len(out["rooms"].([]any)) != 0 {
		t.Fatalf("rooms visible across tenants: %v", out["rooms"])
	}
}

// ---------------------------------------------------------------------------
// Upload, versions, size limits (FR-2.1/FR-2.2)
// ---------------------------------------------------------------------------

func TestUploadMultipartAndVersioning(t *testing.T) {
	_, c := newServer(t, "", "", 0)
	room := createRoom(t, c, "dealdoc", nil)
	roomID := room["id"].(string)

	v1 := []byte("annual report 2025 — confidential draft")
	resp := uploadFile(t, c, roomID, "annual-report.pdf", v1, map[string]string{"folderPath": "finance"})
	out := expectStatus(t, resp, http.StatusCreated)
	docID := out["id"].(string)
	if out["name"] != "annual-report.pdf" || out["folderPath"] != "finance" {
		t.Fatalf("doc meta: %v", out)
	}
	if out["contentType"] == "" || out["currentVersion"] == "" {
		t.Fatalf("doc missing contentType/currentVersion: %v", out)
	}
	v1Version := out["currentVersion"].(string)

	// Second upload of the same name+folder → new immutable version, not an
	// overwrite (FR-2.2).
	v2 := []byte("annual report 2025 — second revision")
	resp = uploadFile(t, c, roomID, "annual-report.pdf", v2, map[string]string{"folderPath": "finance"})
	out = expectStatus(t, resp, http.StatusCreated)
	if out["id"] != docID {
		t.Fatalf("doc id changed on versioned upload: %v != %v", out["id"], docID)
	}
	v2Version := out["currentVersion"].(string)
	if v2Version == v1Version {
		t.Fatalf("currentVersion unchanged after versioned upload")
	}

	// Documents list shows one document with the latest current version.
	resp = c.do("GET", "/api/v1/rooms/"+roomID+"/documents", nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	docs := out["documents"].([]any)
	if len(docs) != 1 {
		t.Fatalf("documents len = %d, want 1", len(docs))
	}
	doc := docs[0].(map[string]any)
	if doc["currentVersion"] != v2Version || doc["sizeBytes"].(float64) != float64(len(v2)) {
		t.Fatalf("document projection stale: %v", doc)
	}

	// Versions list is a live SPI read: two versions, ascending.
	resp = c.do("GET", "/api/v1/rooms/"+roomID+"/documents/"+docID+"/versions", nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	versions := out["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("versions len = %d, want 2", len(versions))
	}
	first := versions[0].(map[string]any)
	second := versions[1].(map[string]any)
	if first["number"].(float64) != 1 || second["number"].(float64) != 2 {
		t.Fatalf("version numbers: %v, %v", first["number"], second["number"])
	}
	if first["sizeBytes"].(float64) != float64(len(v1)) || second["sizeBytes"].(float64) != float64(len(v2)) {
		t.Fatalf("version sizes: %v / %v", first["sizeBytes"], second["sizeBytes"])
	}
	if first["sha256"] == "" || first["sha256"] == second["sha256"] {
		t.Fatalf("version digests missing or equal")
	}

	// Unknown document in a known room → 404 document_not_found.
	resp = c.do("GET", "/api/v1/rooms/"+roomID+"/documents/nope/versions", nil, nil)
	out = expectStatus(t, resp, http.StatusNotFound)
	if out["code"] != "document_not_found" {
		t.Fatalf("code = %v, want document_not_found", out["code"])
	}
	// Upload into an unknown room → 404 room_not_found.
	resp = uploadFile(t, c, "ghost-room", "x.txt", []byte("x"), nil)
	out = expectStatus(t, resp, http.StatusNotFound)
	if out["code"] != "room_not_found" {
		t.Fatalf("code = %v, want room_not_found", out["code"])
	}
}

func TestUploadValidation(t *testing.T) {
	_, c := newServer(t, "", "", 0)
	room := createRoom(t, c, "val", nil)
	roomID := room["id"].(string)

	// Non-multipart body → 400.
	resp := c.do("POST", "/api/v1/rooms/"+roomID+"/documents", map[string]any{"nope": true}, nil)
	expectStatus(t, resp, http.StatusBadRequest)

	// Missing file part → 400.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("name", "lonely.txt")
	_ = mw.Close()
	req, _ := http.NewRequest("POST", c.ts.URL+"/api/v1/rooms/"+roomID+"/documents", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+testSecret)
	resp2, err := c.ts.Client().Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	expectStatus(t, resp2, http.StatusBadRequest)

	// Filename escaping the folder (via explicit name) → 400.
	resp = uploadFile(t, c, roomID, "evil.pdf", []byte("x"), map[string]string{"name": "../evil.pdf"})
	expectStatus(t, resp, http.StatusBadRequest)

	// Bad classification → 400.
	resp = uploadFile(t, c, roomID, "ok.txt", []byte("x"), map[string]string{"classification": "HIDDEN"})
	expectStatus(t, resp, http.StatusBadRequest)

	// Bad folder path → 400.
	resp = uploadFile(t, c, roomID, "ok.txt", []byte("x"), map[string]string{"folderPath": "../up"})
	expectStatus(t, resp, http.StatusBadRequest)
}

func TestUploadSizeLimit(t *testing.T) {
	// 1 KiB ceiling.
	_, c := newServer(t, "", "", 1024)
	room := createRoom(t, c, "big", nil)
	roomID := room["id"].(string)

	resp := uploadFile(t, c, roomID, "big.bin", bytes.Repeat([]byte("a"), 4096), nil)
	out := expectStatus(t, resp, http.StatusRequestEntityTooLarge)
	if out["code"] != "payload_too_large" {
		t.Fatalf("code = %v, want payload_too_large", out["code"])
	}
}

// ---------------------------------------------------------------------------
// Grants (FR-4.4 lifecycle via the SPI)
// ---------------------------------------------------------------------------

func TestGrantLifecycle(t *testing.T) {
	_, c := newServer(t, "", "", 0)
	room := createRoom(t, c, "grants", nil)
	roomID := room["id"].(string)

	// Internal user grant, view-only.
	resp := c.do("POST", "/api/v1/rooms/"+roomID+"/grants", map[string]any{
		"subject": "analyst@evdr.local", "actorKind": "internal_user", "tier": "view_only",
	}, nil)
	out := expectStatus(t, resp, http.StatusCreated)
	grantID := out["id"].(string)
	if out["tier"] != "view_only" || out["subject"] != "analyst@evdr.local" {
		t.Fatalf("grant: %v", out)
	}
	// Guest without expiry → SPI ErrInvalidGrant → 400 invalid_grant.
	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/grants", map[string]any{
		"subject": "guest@acme.com", "actorKind": "guest", "tier": "view_only",
	}, nil)
	out = expectStatus(t, resp, http.StatusBadRequest)
	if out["code"] != "invalid_grant" {
		t.Fatalf("code = %v, want invalid_grant", out["code"])
	}
	// Guest with expiry → created.
	notAfter := time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339)
	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/grants", map[string]any{
		"subject": "guest@acme.com", "actorKind": "guest", "tier": "view_only",
		"constraints": map[string]any{"notAfter": notAfter},
	}, nil)
	out = expectStatus(t, resp, http.StatusCreated)
	guestGrantID := out["id"].(string)
	// Unparseable actorKind → 400 bad_request (service-level enum).
	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/grants", map[string]any{
		"subject": "x@y.z", "actorKind": "superuser",
	}, nil)
	expectStatus(t, resp, http.StatusBadRequest)

	// List: two active grants.
	resp = c.do("GET", "/api/v1/rooms/"+roomID+"/grants", nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	if len(out["grants"].([]any)) != 2 {
		t.Fatalf("grants len = %d, want 2: %v", len(out["grants"].([]any)), out["grants"])
	}

	// Revoke the internal user grant → 204; re-revoke is idempotent-safe.
	resp = c.do("DELETE", "/api/v1/rooms/"+roomID+"/grants/"+grantID, nil, nil)
	expectStatus(t, resp, http.StatusNoContent)
	resp = c.do("DELETE", "/api/v1/rooms/"+roomID+"/grants/"+grantID, nil, nil)
	expectStatus(t, resp, http.StatusNoContent)
	// Unknown grant → SPI ErrGrantNotFound → 404.
	resp = c.do("DELETE", "/api/v1/rooms/"+roomID+"/grants/unknown-grant", nil, nil)
	out = expectStatus(t, resp, http.StatusNotFound)
	if out["code"] != "grant_not_found" {
		t.Fatalf("code = %v, want grant_not_found", out["code"])
	}
	// Guest grant still active.
	resp = c.do("GET", "/api/v1/rooms/"+roomID+"/grants", nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	grants := out["grants"].([]any)
	if len(grants) != 1 || grants[0].(map[string]any)["id"] != guestGrantID {
		t.Fatalf("remaining grants: %v", out["grants"])
	}
}

// ---------------------------------------------------------------------------
// Seal + retention semantics (FR-1.6 / FR-5.5 hooks)
// ---------------------------------------------------------------------------

func TestSealRoomFreezesMutations(t *testing.T) {
	_, c := newServer(t, "", "", 0)
	room := createRoom(t, c, "sealme", nil)
	roomID := room["id"].(string)
	resp := uploadFile(t, c, roomID, "frozen.txt", []byte("content"), nil)
	expectStatus(t, resp, http.StatusCreated)

	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/seal", map[string]any{"reason": "regulatory hold"}, nil)
	out := expectStatus(t, resp, http.StatusOK)
	receiptID := out["id"].(string)
	if out["roomId"] != roomID || out["sealedBy"].(map[string]any)["id"] != "operator-1@evdr.local" {
		t.Fatalf("seal receipt: %v", out)
	}
	// Room state reflects the seal.
	resp = c.do("GET", "/api/v1/rooms/"+roomID, nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	if out["state"] != "sealed" {
		t.Fatalf("room state = %v, want sealed", out["state"])
	}
	// Re-seal returns the existing receipt (idempotent).
	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/seal", map[string]any{"reason": "again"}, nil)
	out = expectStatus(t, resp, http.StatusOK)
	if out["id"] != receiptID {
		t.Fatalf("re-seal returned a new receipt: %v", out["id"])
	}
	// Mutations are rejected with the SPI sentinel → 409 room_sealed.
	resp = uploadFile(t, c, roomID, "late.txt", []byte("too late"), nil)
	out = expectStatus(t, resp, http.StatusConflict)
	if out["code"] != "room_sealed" {
		t.Fatalf("upload code = %v, want room_sealed", out["code"])
	}
	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/retention", map[string]any{"anchor": "upload", "minRetentionDays": 365}, nil)
	out = expectStatus(t, resp, http.StatusConflict)
	if out["code"] != "room_sealed" {
		t.Fatalf("retention code = %v, want room_sealed", out["code"])
	}
	// Reads still work on a sealed room.
	resp = c.do("GET", "/api/v1/rooms/"+roomID+"/documents", nil, nil)
	expectStatus(t, resp, http.StatusOK)
}

func TestRetentionFloorEnforcedBySPI(t *testing.T) {
	_, c := newServer(t, "", "", 0)
	body := map[string]any{
		"name": "Retained", "slug": "retained",
		"retention": map[string]any{"anchor": "upload", "minRetentionDays": 90, "maxRetentionDays": 0, "autoPurge": false},
	}
	room := createRoom(t, c, "retained", body)
	roomID := room["id"].(string)

	// Shortening below the stored floor → SPI ErrRetentionViolation → 409.
	resp := c.do("POST", "/api/v1/rooms/"+roomID+"/retention", map[string]any{"anchor": "upload", "minRetentionDays": 30}, nil)
	out := expectStatus(t, resp, http.StatusConflict)
	if out["code"] != "retention_violation" {
		t.Fatalf("code = %v, want retention_violation", out["code"])
	}
	// Raising the floor applies.
	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/retention", map[string]any{"anchor": "upload", "minRetentionDays": 120, "maxRetentionDays": 730}, nil)
	out = expectStatus(t, resp, http.StatusOK)
	if out["retention"].(map[string]any)["minRetentionDays"].(float64) != 120 {
		t.Fatalf("retention not applied: %v", out["retention"])
	}
	// Invalid retention anchor → 400 at the API boundary.
	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/retention", map[string]any{"anchor": "event", "minRetentionDays": 1}, nil)
	expectStatus(t, resp, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// Export streaming (FR-1.7 / SR-5.2)
// ---------------------------------------------------------------------------

func TestExportStreamsTarArchive(t *testing.T) {
	_, c := newServer(t, "", "", 0)
	room := createRoom(t, c, "exportme", nil)
	roomID := room["id"].(string)
	resp := uploadFile(t, c, roomID, "doc.txt", []byte("export me"), nil)
	expectStatus(t, resp, http.StatusCreated)

	resp = c.do("POST", "/api/v1/rooms/"+roomID+"/export", map[string]any{
		"includeAuditTrail": true, "includeVersions": true,
	}, nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("export status = %d: %s", resp.StatusCode, b)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/x-tar" {
		t.Fatalf("content type = %q, want application/x-tar", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	tr := tar.NewReader(bytes.NewReader(body))
	found := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		found[hdr.Name] = true
	}
	for _, want := range []string{"room.json", "manifest.json", "INTEGRITY_LETTER.txt", "audit/grants.json"} {
		if !found[want] {
			t.Fatalf("export missing %s (entries: %v)", want, found)
		}
	}
	hasDoc := false
	for name := range found {
		if strings.HasPrefix(name, "docs/") && strings.HasSuffix(name, "/doc.txt") {
			hasDoc = true
		}
	}
	if !hasDoc {
		t.Fatalf("export missing document content (entries: %v)", found)
	}
	// Exporting an unknown room → 404.
	resp = c.do("POST", "/api/v1/rooms/nope/export", map[string]any{}, nil)
	expectStatus(t, resp, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// Public portal branding (FR-1.1) — no management surface leaks
// ---------------------------------------------------------------------------

func TestPublicBrandingExposesOnlyPublicSurface(t *testing.T) {
	_, c := newServer(t, "", "", 0)
	body := map[string]any{
		"name": "Acme Data Room", "slug": "acme-public",
		"branding": map[string]any{
			"logoObjectKey": "acme/logo.png",
			"theme":         map[string]string{"--brand": "#123456"},
			"aboutPage":     "Room for the Acme diligence.",
		},
	}
	room := createRoom(t, c, "acme-public", body)
	roomID := room["id"].(string)

	resp := c.do("GET", "/api/v1/public/rooms/"+roomID+"/branding", nil, nil)
	out := expectStatus(t, resp, http.StatusOK)
	if out["name"] != "Acme Data Room" || out["slug"] != "acme-public" || out["state"] != "active" {
		t.Fatalf("public branding: %v", out)
	}
	if _, ok := out["documents"]; ok {
		t.Fatalf("public branding leaks documents")
	}
	if _, ok := out["grants"]; ok {
		t.Fatalf("public branding leaks grants")
	}
	if _, ok := out["retention"]; ok {
		t.Fatalf("public branding leaks retention")
	}
	branding := out["branding"].(map[string]any)
	if branding["aboutPage"] != "Room for the Acme diligence." || branding["logoObjectKey"] != "acme/logo.png" {
		t.Fatalf("branding: %v", branding)
	}
	theme := branding["theme"].(map[string]any)
	if theme["--brand"] != "#123456" {
		t.Fatalf("theme: %v", theme)
	}
	// Unknown room → 404.
	resp = c.do("GET", "/api/v1/public/rooms/ghost/branding", nil, nil)
	expectStatus(t, resp, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// Event sink seam
// ---------------------------------------------------------------------------

func TestEventsEmittedForDecisions(t *testing.T) {
	// Rebuild with a sink we can inspect: newServer wires its own sink, so
	// this test re-wires with a shared one.
	secret := testSecret
	auth, _ := room.NewDevAuthenticator(room.DevPrincipalConfig{Secret: secret, TenantID: "tenant-a"})
	sink := &recordingSink{events: make(chan room.Event, 64)}
	srv, err := room.New(room.Config{SPI: memadapter.New("tenant-a"), Auth: auth, Events: sink})
	if err != nil {
		t.Fatalf("service: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := &testClient{t: t, ts: ts, secret: secret}

	resp := c.do("POST", "/api/v1/rooms", map[string]any{"name": "Audited", "slug": "audited"}, nil)
	room := expectStatus(t, resp, http.StatusCreated)
	resp = c.do("GET", "/api/v1/public/rooms/"+room["id"].(string)+"/branding", nil, nil)
	expectStatus(t, resp, http.StatusOK)
	resp = c.do("GET", "/api/v1/rooms/does-not-exist", nil, nil)
	expectStatus(t, resp, http.StatusNotFound)

	actions := map[string]string{}
	for _, ev := range sink.drain() {
		actions[ev.Action] = ev.Result
	}
	if actions["room.create"] != "ok" {
		t.Fatalf("room.create event: %v", actions)
	}
	if actions["branding.public"] != "ok" {
		t.Fatalf("branding.public event: %v", actions)
	}
	if actions["room.get"] != "room_not_found" {
		t.Fatalf("failed decision must be emitted with its result code: %v", actions)
	}
	for _, ev := range sink.drain() {
		if ev.TenantID != "tenant-a" {
			t.Fatalf("event without tenant scope: %+v", ev)
		}
		if ev.Actor.ID == "" || ev.Actor.Kind == "" {
			t.Fatalf("event without actor: %+v", ev)
		}
	}
}

// ---------------------------------------------------------------------------
// Misc: catalog ordering + empty states
// ---------------------------------------------------------------------------

func TestEmptyAndOrderedLists(t *testing.T) {
	_, c := newServer(t, "", "", 0)
	resp := c.do("GET", "/api/v1/rooms", nil, nil)
	out := expectStatus(t, resp, http.StatusOK)
	if len(out["rooms"].([]any)) != 0 {
		t.Fatalf("expected empty rooms, got %v", out["rooms"])
	}
	// Ordering: second room must come after the first in the list.
	createRoom(t, c, "first-room", nil)
	time.Sleep(5 * time.Millisecond)
	createRoom(t, c, "second-room", nil)
	resp = c.do("GET", "/api/v1/rooms", nil, nil)
	out = expectStatus(t, resp, http.StatusOK)
	rooms := out["rooms"].([]any)
	if len(rooms) != 2 || rooms[0].(map[string]any)["slug"] != "first-room" {
		t.Fatalf("rooms out of order: %v", rooms)
	}
	// Unknown room documents/grants/versions endpoints all 404, not 500.
	for _, path := range []string{
		"/api/v1/rooms/missing/documents",
		"/api/v1/rooms/missing/grants",
		"/api/v1/rooms/missing/documents/d/versions",
	} {
		resp = c.do("GET", path, nil, nil)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, resp.StatusCode)
		}
		resp.Body.Close()
	}
}
