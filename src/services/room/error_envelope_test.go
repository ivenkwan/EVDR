package room_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivenkwan/evdr/src/services/room"
	"github.com/ivenkwan/evdr/src/spi"
	"github.com/ivenkwan/evdr/src/spi/conformance/memadapter"
)

// --- M-1 regression: the error envelope must not leak internal detail ------
//
// 5xx envelopes carry a fixed client-facing message; the underlying error
// (adapter URLs, upstream statuses, topology) stays in the server log only.
// 4xx messages are service-generated (validation text, SPI sentinels) and
// remain verbatim.

// leakyAuth is an Authenticator whose malfunction carries realistic internal
// detail (upstream URL) that must never reach the client.
type leakyAuth struct{ err error }

func (a leakyAuth) Authenticate(*http.Request) (room.Principal, error) {
	return room.Principal{}, a.err
}

func TestWriteErrorInternalIsGeneric(t *testing.T) {
	// A non-ErrUnauthenticated authenticator failure maps to internalErr (500)
	// — the leak path fixed by M-1.
	auth := leakyAuth{err: errors.New("keycloak JWKS fetch failed: https://keycloak.evdr.internal/realms/evdr/protocol/openid-connect/certs: 502")}
	sink := &recordingSink{events: make(chan room.Event, 8)}
	srv, err := room.New(room.Config{
		SPI:            memadapter.New("tenant-a"),
		Auth:           auth,
		Events:         sink,
		MaxUploadBytes: 100 << 20,
	})
	if err != nil {
		t.Fatalf("room.New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/rooms", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/rooms: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	body := jsonBody(t, resp)
	if body["code"] != "internal" {
		t.Fatalf("code = %v, want internal", body["code"])
	}
	msg, _ := body["message"].(string)
	if msg != "internal server error" {
		t.Fatalf("message = %q, want the fixed generic message", msg)
	}
	if strings.Contains(msg, "JWKS") || strings.Contains(msg, "keycloak") {
		t.Fatalf("message %q leaks internal detail", msg)
	}
}

func TestWriteErrorClientErrorKeepsDetail(t *testing.T) {
	// 4xx messages stay verbatim: the SPI's not-found sentinel is the
	// documented no-existence-oracle surface, and validation text is
	// service-generated.
	_, c := newServer(t, "", "", 100<<20)
	resp := c.do(http.MethodGet, "/api/v1/rooms/no-such-room", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	body := jsonBody(t, resp)
	if body["code"] != "room_not_found" {
		t.Fatalf("code = %v, want room_not_found", body["code"])
	}
	if body["message"] != spi.ErrRoomNotFound.Error() {
		t.Fatalf("message = %v, want the SPI sentinel text %q", body["message"], spi.ErrRoomNotFound.Error())
	}
}
