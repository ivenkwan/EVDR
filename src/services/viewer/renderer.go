package viewer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/ivenkwan/evdr/src/services/viewer/converter"
	"github.com/ivenkwan/evdr/src/services/viewer/pdf"
	"github.com/ivenkwan/evdr/src/services/viewer/watermark"
	"github.com/ivenkwan/evdr/src/spi"
)

// Renderer turns one document version's content into a forward-only
// spi.RenderStream of individually sliced, server-side-watermarked pages
// (FR-3.1, FR-3.2). It is the viewer pipeline that storage adapters wire in
// as their renderer (see src/spi README "Rendering (GetRenderStream)"; it is
// structurally identical to the Renderer interface the Nextcloud adapter
// consumes). A Renderer is stateless after construction and safe for
// concurrent use.
type Renderer struct {
	converter      converter.Converter
	pdfCompositor  watermark.Compositor
	textCompositor watermark.Compositor
}

// RendererConfig configures a Renderer. Zero values select defaults.
type RendererConfig struct {
	// Converter normalises office documents to PDF (FR-3.4). When nil,
	// office documents return spi.ErrUnsupported.
	Converter converter.Converter
	// PDFCompositor watermarks application/pdf pages; defaults to
	// watermark.NewPDF().
	PDFCompositor watermark.Compositor
	// TextCompositor watermarks text/plain pages; defaults to
	// watermark.NewText().
	TextCompositor watermark.Compositor
}

// NewRenderer returns a configured Renderer.
func NewRenderer(cfg RendererConfig) *Renderer {
	pdfC := cfg.PDFCompositor
	if pdfC == nil {
		pdfC = watermark.NewPDF()
	}
	txtC := cfg.TextCompositor
	if txtC == nil {
		txtC = watermark.NewText()
	}
	return &Renderer{converter: cfg.Converter, pdfCompositor: pdfC, textCompositor: txtC}
}

// Render implements the pipeline: it reads content, parses it once, and
// returns a stream that yields only the requested page range, one page at a
// time, each page watermarked before it is handed out. The full source never
// appears in the stream. Next() honours ctx cancellation and surfaces it via
// Err(). Callers must Close() the stream.
func (r *Renderer) Render(ctx context.Context, content io.Reader, contentType string, req spi.RenderRequest) (spi.RenderStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	src, err := io.ReadAll(content)
	if err != nil {
		return nil, fmt.Errorf("viewer: read document content: %w", err)
	}
	ct := strings.ToLower(contentType)

	if isPDF(src, ct) {
		return r.renderPDF(ctx, src, req)
	}
	if strings.HasPrefix(ct, "text/") {
		return r.renderText(ctx, src, req)
	}
	// Office formats need conversion before they can be sliced (FR-3.4).
	if r.converter != nil && r.converter.Supports(ct) {
		converted, err := r.converter.ConvertToPDF(ctx, src, "document")
		if err != nil {
			return nil, fmt.Errorf("viewer: %w", err)
		}
		return r.renderPDF(ctx, converted, req)
	}
	return nil, fmt.Errorf("%w: viewer: cannot render content type %q (office conversion worker is lab-deferred, FR-3.4)", spi.ErrUnsupported, contentType)
}

// isPDF reports whether the payload is a PDF, by declared content type or by
// magic bytes (the declared type is authoritative for text/plain renders of
// PDF bytes only when the header is absent — the header check runs second).
func isPDF(src []byte, ct string) bool {
	if strings.HasPrefix(ct, "application/pdf") {
		return true
	}
	return bytes.HasPrefix(bytes.TrimSpace(src), []byte("%PDF-"))
}

// renderPDF parses src as a PDF and builds the page-range stream.
func (r *Renderer) renderPDF(ctx context.Context, src []byte, req spi.RenderRequest) (spi.RenderStream, error) {
	doc, err := pdf.Parse(src)
	if err != nil {
		if errors.Is(err, pdf.ErrUnsupported) || errors.Is(err, pdf.ErrNotPDF) || errors.Is(err, pdf.ErrLayoutUnsupported) {
			return nil, fmt.Errorf("%w: viewer: %v", spi.ErrUnsupported, err)
		}
		return nil, fmt.Errorf("viewer: render PDF: %w", err)
	}
	count := doc.PageCount()
	if count == 0 {
		return nil, fmt.Errorf("%w: viewer: document has no pages", spi.ErrUnsupported)
	}
	first := req.FirstPage
	if first < 1 {
		first = 1
	}
	last := req.LastPage
	if last <= 0 || last > count {
		last = count
	}
	if first > last {
		// Empty range (e.g. FirstPage past the end): the stream yields no
		// pages, mirroring the reference adapters' clamping semantics.
		first, last = 1, 0
	}
	seq := make([]int, 0, last-first+1)
	for n := first; n <= last; n++ {
		seq = append(seq, n)
	}
	return &pageStream{
		ctx:         ctx,
		doc:         doc,
		pages:       seq,
		compositor:  r.pdfCompositor,
		spec:        req.Watermark,
		contentType: "application/pdf",
	}, nil
}

// renderText builds one page per non-empty line of the payload. The
// reference adapters render text documents the same way (one page per line),
// which keeps fixture and integration behaviour uniform.
func (r *Renderer) renderText(ctx context.Context, src []byte, req spi.RenderRequest) (spi.RenderStream, error) {
	text := strings.TrimRight(string(src), "\r\n")
	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" {
			continue // blank lines carry no page content
		}
		lines = append(lines, ln)
	}
	count := len(lines)
	first := req.FirstPage
	if first < 1 {
		first = 1
	}
	last := req.LastPage
	if last <= 0 || last > count {
		last = count
	}
	if first > count {
		first, last = 1, 0 // empty range
	}
	seq := make([]int, 0, last-first+1)
	for n := first; n <= last; n++ {
		seq = append(seq, n)
	}
	return &pageStream{
		ctx:         ctx,
		textLines:   lines,
		pages:       seq,
		compositor:  r.textCompositor,
		spec:        req.Watermark,
		contentType: "text/plain",
	}, nil
}

// pageStream is the forward-only page stream returned by Render.
type pageStream struct {
	ctx         context.Context
	doc         *pdf.Document // PDF mode
	textLines   []string      // text mode
	pages       []int         // 1-indexed page numbers to deliver, in order
	pos         int
	compositor  watermark.Compositor
	spec        spi.WatermarkSpec
	contentType string

	current spi.Page
	err     error
	closed  bool
}

// Next implements spi.RenderStream.
func (s *pageStream) Next() bool {
	if s.err != nil || s.closed || s.pos >= len(s.pages) {
		return false
	}
	if err := s.ctx.Err(); err != nil {
		s.err = err
		return false
	}
	n := s.pages[s.pos]
	var payload []byte
	var err error
	if s.doc != nil {
		var single []byte
		single, err = s.doc.ExtractPage(n)
		if err == nil {
			payload, err = s.compositor.Apply(s.ctx, s.spec, s.contentType, single)
		}
	} else {
		payload, err = s.compositor.Apply(s.ctx, s.spec, s.contentType, []byte(s.textLines[n-1]))
	}
	if err != nil {
		s.err = err
		return false
	}
	s.pos++
	s.current = spi.Page{
		Number:      n,
		ContentType: s.contentType,
		Content:     bytes.NewReader(payload),
	}
	return true
}

// Page implements spi.RenderStream.
func (s *pageStream) Page() spi.Page { return s.current }

// Err implements spi.RenderStream.
func (s *pageStream) Err() error { return s.err }

// Close implements spi.RenderStream. Idempotent.
func (s *pageStream) Close() error { s.closed = true; return nil }

// Compile-time assertions.
var _ spi.RenderStream = (*pageStream)(nil)
