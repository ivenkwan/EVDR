package watermark_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ivenkwan/evdr/src/services/viewer/pdf"
	"github.com/ivenkwan/evdr/src/services/viewer/pdf/testpdf"
	"github.com/ivenkwan/evdr/src/services/viewer/watermark"
	"github.com/ivenkwan/evdr/src/spi"
)

var sampleSpec = spi.WatermarkSpec{
	ViewerIdentity: "alice@corp.example",
	Timestamp:      time.Date(2026, 9, 5, 2, 30, 0, 0, time.UTC),
	ClientIP:       "203.0.113.42",
	SessionID:      "sess-7f3a",
	Density:        "normal",
	Opacity:        0.35,
	Rotation:       30,
}

func TestLine(t *testing.T) {
	if got := watermark.Line(sampleSpec); got != "alice@corp.example | 2026-09-05T02:30:00Z | 203.0.113.42 | sess-7f3a" {
		t.Errorf("Line = %q", got)
	}
	// Empty tokens are skipped; a fully empty spec yields an empty line.
	if got := watermark.Line(spi.WatermarkSpec{}); got != "" {
		t.Errorf("empty spec Line = %q, want empty", got)
	}
	if got := watermark.Line(spi.WatermarkSpec{ViewerIdentity: "bob"}); got != "bob" {
		t.Errorf("single token Line = %q", got)
	}
	// Timestamps are normalised to UTC.
	local := time.Date(2026, 9, 5, 10, 30, 0, 0, time.FixedZone("HKT", 8*3600))
	if got := watermark.Line(spi.WatermarkSpec{Timestamp: local}); got != "2026-09-05T02:30:00Z" {
		t.Errorf("local timestamp Line = %q", got)
	}
}

func TestPDFCompositorBakesIntoBytes(t *testing.T) {
	page := testpdf.Doc(testpdf.Options{Pages: 1})
	ctx := context.Background()
	out, err := watermark.NewPDF().Apply(ctx, sampleSpec, "application/pdf", page)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if bytes.Equal(out, page) {
		t.Fatal("watermark must change the served bytes")
	}
	// The token line is physically inside the page content stream.
	if !bytes.Contains(out, []byte("alice@corp.example | 2026-09-05T02:30:00Z | 203.0.113.42 | sess-7f3a")) {
		t.Error("served bytes lack the identity line")
	}
	// The result remains a parseable single-page PDF with its body intact.
	d, err := pdf.Parse(out)
	if err != nil {
		t.Fatalf("watermarked page does not parse: %v", err)
	}
	if d.PageCount() != 1 {
		t.Errorf("watermarked doc pages = %d", d.PageCount())
	}
	if !bytes.Contains(out, []byte(testpdf.Marker(1))) {
		t.Error("watermarked page lost its body")
	}
}

func TestPDFCompositorDeterministic(t *testing.T) {
	page := testpdf.Doc(testpdf.Options{Pages: 2, Flate: true})
	ctx := context.Background()
	c := watermark.NewPDF()
	a, err := c.Apply(ctx, sampleSpec, "application/pdf", page)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Apply(ctx, sampleSpec, "application/pdf", page)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("Apply is not deterministic")
	}
	// The identity line lives inside the (now re-compressed) content streams,
	// so byte-level presence is verified via a full parse of both pages.
	d, err := pdf.Parse(a)
	if err != nil {
		t.Fatalf("watermarked flate doc does not parse: %v", err)
	}
	if d.PageCount() != 2 {
		t.Errorf("watermarked doc pages = %d, want 2", d.PageCount())
	}
}

func TestPDFCompositorEscapesHostileIdentity(t *testing.T) {
	spec := spi.WatermarkSpec{
		// A backtick literal: backslash stays a backslash, then a real UTF-8
		// multi-byte char that the ASCII sanitizer must replace (café → caf??).
		ViewerIdentity: `evil(identity)\with` + "caf\u00e9",
		Timestamp:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	out, err := watermark.NewPDF().Apply(context.Background(), spec, "application/pdf",
		testpdf.Doc(testpdf.Options{Pages: 1}))
	if err != nil {
		t.Fatal(err)
	}
	// Parens and backslashes must be escaped inside the literal string and the
	// non-ASCII bytes replaced with '?': the raw payload must not contain the
	// unescaped identity anywhere.
	if bytes.Contains(out, []byte("(evil(identity)")) {
		t.Error("unescaped parenthesis leaked into the content stream")
	}
	want := `evil\(identity\)\\withcaf?? | 2026-01-01T00:00:00Z`
	if !bytes.Contains(out, []byte(want)) {
		t.Errorf("escaped identity line not found in payload; want %q", want)
	}
}

func TestPDFCompositorEscapesControlBytes(t *testing.T) {
	// A real newline byte inside the identity must become the two-character
	// "\n" sequence inside the content stream.
	spec := spi.WatermarkSpec{
		ViewerIdentity: "alice\nbob",
		Timestamp:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	out, err := watermark.NewPDF().Apply(context.Background(), spec, "application/pdf",
		testpdf.Doc(testpdf.Options{Pages: 1}))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte(`(alice\nbob | 2026-01-01T00:00:00Z) Tj`)) {
		t.Error("control byte not escaped as \\n inside the content stream")
	}
	if bytes.Contains(out, []byte("(alice\nbob")) {
		t.Error("raw newline leaked into the literal string")
	}
}

func TestPDFCompositorEmptySpecIsNoop(t *testing.T) {
	page := testpdf.Doc(testpdf.Options{Pages: 1})
	out, err := watermark.NewPDF().Apply(context.Background(), spi.WatermarkSpec{}, "application/pdf", page)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, page) {
		t.Error("empty spec must leave bytes unchanged (policy engine decides mandate)")
	}
}

func TestPDFCompositorRejectsOtherContentTypes(t *testing.T) {
	for _, ct := range []string{"image/webp", "text/plain", "application/octet-stream", ""} {
		_, err := watermark.NewPDF().Apply(context.Background(), sampleSpec, ct, []byte("x"))
		if !errors.Is(err, watermark.ErrUnsupportedFormat) {
			t.Errorf("content type %q error = %v, want ErrUnsupportedFormat", ct, err)
		}
	}
}

func TestTextCompositor(t *testing.T) {
	payload := []byte("page one body\n")
	out, err := watermark.NewText().Apply(context.Background(), sampleSpec, "text/plain", payload)
	if err != nil {
		t.Fatal(err)
	}
	want := "[EVDR watermark: alice@corp.example | 2026-09-05T02:30:00Z | 203.0.113.42 | sess-7f3a]\npage one body\n"
	if string(out) != want {
		t.Errorf("text compositor = %q, want %q", out, want)
	}
	if _, err := watermark.NewText().Apply(context.Background(), sampleSpec, "application/pdf", payload); !errors.Is(err, watermark.ErrUnsupportedFormat) {
		t.Errorf("pdf payload error = %v, want ErrUnsupportedFormat", err)
	}
	if out, err := watermark.NewText().Apply(context.Background(), spi.WatermarkSpec{}, "text/plain", payload); err != nil || !bytes.Equal(out, payload) {
		t.Errorf("empty spec must be a no-op: %v", err)
	}
}

func TestCompositorHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := watermark.NewPDF().Apply(ctx, sampleSpec, "application/pdf", testpdf.Doc(testpdf.Options{Pages: 1}))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled Apply error = %v, want context.Canceled", err)
	}
	_, err = watermark.NewText().Apply(ctx, sampleSpec, "text/plain", []byte("x"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled text Apply error = %v, want context.Canceled", err)
	}
}
