package viewer_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ivenkwan/evdr/src/services/viewer"
	"github.com/ivenkwan/evdr/src/services/viewer/converter"
	"github.com/ivenkwan/evdr/src/services/viewer/pdf"
	"github.com/ivenkwan/evdr/src/services/viewer/pdf/testpdf"
	"github.com/ivenkwan/evdr/src/spi"
)

var testSpec = spi.WatermarkSpec{
	ViewerIdentity: "renderer-test@corp.example",
	Timestamp:      time.Date(2026, 9, 5, 8, 0, 0, 0, time.UTC),
	ClientIP:       "10.0.0.7",
	SessionID:      "sess-abc",
	Density:        "normal",
	Opacity:        0.4,
	Rotation:       30,
}

// collect drains a stream and returns per-page payloads.
func collect(t *testing.T, s spi.RenderStream) []struct {
	n   int
	ct  string
	raw []byte
} {
	t.Helper()
	defer s.Close()
	var out []struct {
		n   int
		ct  string
		raw []byte
	}
	for s.Next() {
		p := s.Page()
		raw, err := io.ReadAll(p.Content)
		if err != nil {
			t.Fatalf("read page %d: %v", p.Number, err)
		}
		out = append(out, struct {
			n   int
			ct  string
			raw []byte
		}{p.Number, p.ContentType, raw})
	}
	if err := s.Err(); err != nil {
		t.Fatalf("stream error: %v", err)
	}
	return out
}

func newTestRenderer() *viewer.Renderer {
	return viewer.NewRenderer(viewer.RendererConfig{Converter: converter.NewLibreOffice("/usr/bin/soffice")})
}

func TestRendererPDFDeliversOnlyRequestedPageWatermarked(t *testing.T) {
	src := testpdf.Doc(testpdf.Options{Pages: 3})
	stream, err := newTestRenderer().Render(context.Background(), bytes.NewReader(src), "application/pdf",
		spi.RenderRequest{FirstPage: 2, LastPage: 2, Watermark: testSpec})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	pages := collect(t, stream)
	if len(pages) != 1 {
		t.Fatalf("delivered %d pages, want exactly 1", len(pages))
	}
	p := pages[0]
	if p.n != 2 {
		t.Errorf("page number = %d, want 2", p.n)
	}
	if p.ct != "application/pdf" {
		t.Errorf("content type = %q", p.ct)
	}
	// The delivered payload is a single-page PDF with the watermark baked in
	// and no sibling content.
	d, err := pdf.Parse(p.raw)
	if err != nil {
		t.Fatalf("delivered page does not parse: %v", err)
	}
	if d.PageCount() != 1 {
		t.Errorf("delivered payload has %d pages — a full document crossed the boundary", d.PageCount())
	}
	if !bytes.Contains(p.raw, []byte("renderer-test@corp.example")) {
		t.Error("delivered page lacks the watermark identity")
	}
	if !bytes.Contains(p.raw, []byte(testpdf.Marker(2))) {
		t.Error("delivered page lost its own body")
	}
	for _, other := range []string{testpdf.Marker(1), testpdf.Marker(3)} {
		if bytes.Contains(p.raw, []byte(other)) {
			t.Errorf("delivered page leaks sibling content %q", other)
		}
	}
}

func TestRendererPDFFullRangeYieldsAllPagesSingletons(t *testing.T) {
	src := testpdf.Doc(testpdf.Options{Pages: 4})
	stream, err := newTestRenderer().Render(context.Background(), bytes.NewReader(src), "application/pdf",
		spi.RenderRequest{FirstPage: 1, LastPage: 0, Watermark: testSpec}) // 0 = unbounded
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	pages := collect(t, stream)
	if len(pages) != 4 {
		t.Fatalf("delivered %d pages, want 4", len(pages))
	}
	for i, p := range pages {
		if p.n != i+1 {
			t.Errorf("page %d has number %d", i, p.n)
		}
		d, err := pdf.Parse(p.raw)
		if err != nil {
			t.Fatalf("page %d does not parse: %v", p.n, err)
		}
		if d.PageCount() != 1 {
			t.Errorf("page %d payload has %d pages", p.n, d.PageCount())
		}
		if !bytes.Contains(p.raw, []byte("renderer-test@corp.example")) {
			t.Errorf("page %d lacks the watermark", p.n)
		}
	}
}

func TestRendererPDFClampsOutOfRangeToEmpty(t *testing.T) {
	src := testpdf.Doc(testpdf.Options{Pages: 2})
	r := newTestRenderer()
	// Entirely past the end → no pages, no error (mirrors adapter semantics).
	stream, err := r.Render(context.Background(), bytes.NewReader(src), "application/pdf",
		spi.RenderRequest{FirstPage: 99, LastPage: 100, Watermark: testSpec})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if pages := collect(t, stream); len(pages) != 0 {
		t.Errorf("out-of-range render delivered %d pages", len(pages))
	}
	// Requesting a prefix beyond the last page clamps to the real count.
	stream, err = r.Render(context.Background(), bytes.NewReader(src), "application/pdf",
		spi.RenderRequest{FirstPage: 1, LastPage: 500, Watermark: testSpec})
	if err != nil {
		t.Fatal(err)
	}
	if pages := collect(t, stream); len(pages) != 2 {
		t.Errorf("clamped render delivered %d pages, want 2", len(pages))
	}
}

func TestRendererTextPages(t *testing.T) {
	r := newTestRenderer()
	stream, err := r.Render(context.Background(), strings.NewReader("line one\nline two\nline three\n"),
		"text/plain", spi.RenderRequest{FirstPage: 2, LastPage: 3, Watermark: testSpec})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	pages := collect(t, stream)
	if len(pages) != 2 {
		t.Fatalf("delivered %d pages", len(pages))
	}
	if pages[0].n != 2 || pages[1].n != 3 {
		t.Errorf("page numbers = %d,%d", pages[0].n, pages[1].n)
	}
	if pages[0].ct != "text/plain" {
		t.Errorf("content type = %q", pages[0].ct)
	}
	want := "[EVDR watermark: renderer-test@corp.example | 2026-09-05T08:00:00Z | 10.0.0.7 | sess-abc]\nline two"
	if string(pages[0].raw) != want {
		t.Errorf("text page 2 = %q, want %q", pages[0].raw, want)
	}
}

func TestRendererOfficeRequiresConverter(t *testing.T) {
	docxCT := "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	// No converter configured → unsupported (FR-3.4 worker not deployed).
	noConv := viewer.NewRenderer(viewer.RendererConfig{})
	_, err := noConv.Render(context.Background(), bytes.NewReader([]byte("x")), docxCT,
		spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, spi.ErrUnsupported) {
		t.Fatalf("nil-converter error = %v, want spi.ErrUnsupported", err)
	}
	// Stub converter → honest ErrUnavailable, never a fake PDF.
	stub := viewer.NewRenderer(viewer.RendererConfig{Converter: converter.NewLibreOffice("/usr/bin/soffice")})
	_, err = stub.Render(context.Background(), bytes.NewReader([]byte("x")), docxCT,
		spi.RenderRequest{FirstPage: 1, LastPage: 1})
	if !errors.Is(err, converter.ErrUnavailable) {
		t.Fatalf("stub error = %v, want converter.ErrUnavailable", err)
	}
}

func TestRendererRejectsUnknownFormats(t *testing.T) {
	r := newTestRenderer()
	for _, ct := range []string{"image/png", "image/webp", "application/octet-stream", ""} {
		_, err := r.Render(context.Background(), strings.NewReader("x"), ct,
			spi.RenderRequest{FirstPage: 1, LastPage: 1})
		if !errors.Is(err, spi.ErrUnsupported) {
			t.Errorf("content type %q error = %v, want spi.ErrUnsupported", ct, err)
		}
	}
}

func TestRendererPDFBytesWithoutDeclaredType(t *testing.T) {
	src := testpdf.Doc(testpdf.Options{Pages: 2})
	// Adapters sometimes mislabel; magic bytes win for actual PDFs.
	stream, err := newTestRenderer().Render(context.Background(), bytes.NewReader(src), "application/octet-stream",
		spi.RenderRequest{FirstPage: 1, LastPage: 1, Watermark: testSpec})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if pages := collect(t, stream); len(pages) != 1 {
		t.Errorf("magic-byte PDF delivered %d pages", len(pages))
	}
}

func TestRendererCancellationSurfacesOnNext(t *testing.T) {
	src := testpdf.Doc(testpdf.Options{Pages: 3})
	ctx, cancel := context.WithCancel(context.Background())
	stream, err := newTestRenderer().Render(ctx, bytes.NewReader(src), "application/pdf",
		spi.RenderRequest{FirstPage: 1, LastPage: 3, Watermark: testSpec})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	cancel()
	if stream.Next() {
		t.Fatal("Next returned true after cancellation")
	}
	if !errors.Is(stream.Err(), context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", stream.Err())
	}
}

func TestRendererConcurrentUse(t *testing.T) {
	r := newTestRenderer()
	src := testpdf.Doc(testpdf.Options{Pages: 5})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			spec := testSpec
			spec.ViewerIdentity = fmt.Sprintf("user-%d@corp.example", seed)
			stream, err := r.Render(context.Background(), bytes.NewReader(src), "application/pdf",
				spi.RenderRequest{FirstPage: 1, LastPage: 5, Watermark: spec})
			if err != nil {
				t.Errorf("Render: %v", err)
				return
			}
			n := 0
			for stream.Next() {
				raw, _ := io.ReadAll(stream.Page().Content)
				if !bytes.Contains(raw, []byte(fmt.Sprintf("user-%d@corp.example", seed))) {
					t.Errorf("page %d missing own watermark", stream.Page().Number)
				}
				n++
			}
			_ = stream.Close()
			if n != 5 {
				t.Errorf("delivered %d pages, want 5", n)
			}
		}(i)
	}
	wg.Wait()
}

func TestRendererCloseIsIdempotent(t *testing.T) {
	stream, err := newTestRenderer().Render(context.Background(), bytes.NewReader(testpdf.Doc(testpdf.Options{Pages: 1})),
		"application/pdf", spi.RenderRequest{FirstPage: 1, LastPage: 1, Watermark: testSpec})
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Errorf("second Close must be nil: %v", err)
	}
	if stream.Next() {
		t.Error("Next after Close must be false")
	}
}
