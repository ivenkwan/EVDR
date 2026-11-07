package nextcloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ivenkwan/evdr/src/spi"
)

// --- M-2 regression: GetRenderStream must close the WebDAV transport body --
//
// The renderer consumes the version-content response body but has no handle
// to close it, so the adapter's ctxRenderStream owns the close. One leaked
// body per served page would otherwise exhaust connections/file descriptors
// under load. Both paths are pinned: the happy path (stream Close) and the
// renderer-error path (immediate close in GetRenderStream).

// trackedBody records Close() calls on the version-content response body.
type trackedBody struct {
	io.ReadCloser
	onClose func()
}

func (b *trackedBody) Close() error {
	err := b.ReadCloser.Close()
	if b.onClose != nil {
		b.onClose()
	}
	return err
}

// trackingTransport wraps the base transport and tracks the response body for
// every WebDAV version-content GET.
type trackingTransport struct {
	base    http.RoundTripper
	onTrack func()
}

func (t *trackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	// Only the version-content GET carries the render body; MKCOL/COPY hit
	// the /_evdr/versions/ path prefix too but are drained by the adapter.
	if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "/_evdr/versions/") {
		resp.Body = &trackedBody{ReadCloser: resp.Body, onClose: t.onTrack}
	}
	return resp, nil
}

// errRenderer fails immediately after reading the content, exercising the
// GetRenderStream error path (body must be closed before returning).
type errRenderer struct{}

func (errRenderer) Render(ctx context.Context, content io.Reader, _ string, _ spi.RenderRequest) (spi.RenderStream, error) {
	_, _ = io.ReadAll(content)
	return nil, errors.New("renderer: synthetic failure")
}

func TestGetRenderStreamClosesTransportBodyOnClose(t *testing.T) {
	var mu sync.Mutex
	closes := 0
	tr := &trackingTransport{base: http.DefaultTransport, onTrack: func() {
		mu.Lock()
		closes++
		mu.Unlock()
	}}
	e := newTestEnv(t)
	a, err := New(Config{
		BaseURL:     e.srv.URL,
		Username:    testUser,
		AppPassword: testPass,
		TenantID:    testTenant,
		Renderer:    e.renderer,
		HTTPClient:  &http.Client{Transport: tr},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	op := operator
	room, err := a.CreateRoom(ctx, e.tenant(op), spi.CreateRoomRequest{Name: "Leak Test", Slug: "leak-test"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	doc, err := a.PutDocument(ctx, e.tenant(op), room.ID, spi.PutDocumentRequest{
		Name:        "notes.txt",
		ContentType: "text/plain",
		Content:     strings.NewReader("page one\npage two"),
	})
	if err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	stream, err := a.GetRenderStream(ctx, e.tenant(op), doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if err != nil {
		t.Fatalf("GetRenderStream: %v", err)
	}
	if !stream.Next() {
		t.Fatalf("stream yielded no page (err=%v)", stream.Err())
	}
	page := stream.Page()
	b, err := io.ReadAll(page.Content)
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	if string(b) != "page one" {
		t.Fatalf("page content = %q, want %q", string(b), "page one")
	}
	mu.Lock()
	if closes != 0 {
		mu.Unlock()
		t.Fatalf("transport body closed before stream Close (closes=%d)", closes)
	}
	mu.Unlock()
	if err := stream.Close(); err != nil {
		t.Fatalf("stream.Close: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if closes != 1 {
		t.Fatalf("transport body closes = %d, want exactly 1 after Close", closes)
	}
}

func TestGetRenderStreamClosesTransportBodyOnRendererError(t *testing.T) {
	var mu sync.Mutex
	closes := 0
	tr := &trackingTransport{base: http.DefaultTransport, onTrack: func() {
		mu.Lock()
		closes++
		mu.Unlock()
	}}
	e := newTestEnv(t)
	a, err := New(Config{
		BaseURL:     e.srv.URL,
		Username:    testUser,
		AppPassword: testPass,
		TenantID:    testTenant,
		Renderer:    errRenderer{},
		HTTPClient:  &http.Client{Transport: tr},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	op := operator
	room, err := a.CreateRoom(ctx, e.tenant(op), spi.CreateRoomRequest{Name: "Leak Test", Slug: "leak-test-err"})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	doc, err := a.PutDocument(ctx, e.tenant(op), room.ID, spi.PutDocumentRequest{
		Name:        "notes.txt",
		ContentType: "text/plain",
		Content:     strings.NewReader("page one\npage two"),
	})
	if err != nil {
		t.Fatalf("PutDocument: %v", err)
	}

	if _, err := a.GetRenderStream(ctx, e.tenant(op), doc.ID, spi.RenderRequest{FirstPage: 1, LastPage: 1}); err == nil {
		t.Fatal("GetRenderStream succeeded, want renderer error")
	}
	mu.Lock()
	defer mu.Unlock()
	if closes != 1 {
		t.Fatalf("transport body closes = %d, want exactly 1 on the error path", closes)
	}
}
