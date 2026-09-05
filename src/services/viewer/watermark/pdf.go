package watermark

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/ivenkwan/evdr/src/services/viewer/pdf"
	"github.com/ivenkwan/evdr/src/spi"
)

// PDF composites a watermark into single-page (or multi-page) PDF payloads by
// appending a rotated identity band to each page's content stream. The stamp
// is part of the page bytes — no overlay, no client-side layer (FR-3.2,
// TR-4.2).
type PDF struct{}

// NewPDF returns a PDF compositor.
func NewPDF() *PDF { return &PDF{} }

// Apply implements Compositor for "application/pdf" payloads.
func (c *PDF) Apply(ctx context.Context, spec spi.WatermarkSpec, contentType string, payload []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "application/pdf") {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, contentType)
	}
	line := Line(spec)
	if line == "" {
		return payload, nil // no tokens: nothing identifiable to stamp
	}
	out, err := pdf.StampContent(payload, func(p *pdf.Page) ([]byte, error) {
		return stampOps(p, line, spec)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// stampOps builds the content-stream operator block for one page.
func stampOps(p *pdf.Page, line string, spec spi.WatermarkSpec) ([]byte, error) {
	font, err := p.AddFont()
	if err != nil {
		return nil, err
	}
	box, err := p.Box()
	if err != nil {
		return nil, err
	}
	size := math.Min(box.Width(), box.Height()) / 45
	size = clamp(size, 6, 18)

	var b strings.Builder
	b.WriteString("q\n")

	alpha := spec.Opacity
	if alpha > 0 && alpha < 1 {
		gs, err := p.AddExtGState(pdf.Dict{
			pdf.Name("Type"): pdf.Name("ExtGState"),
			pdf.Name("ca"):   alpha,
			pdf.Name("CA"):   alpha,
		})
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "/%s gs\n", gs)
	}

	// Dark slate fill; actual transparency is handled by the gs state above.
	b.WriteString("0.18 0.20 0.24 rg\n")

	// Text metrics: a rough Helvetica advance (0.5 em per character) is close
	// enough to centre the band; the stamp is for deterrence/attribution, not
	// typography.
	textW := 0.5 * size * float64(len(line))
	textH := 0.7 * size
	cx := box.X0 + box.Width()/2
	cy := box.Y0 + box.Height()/2

	// A single diagonal band through the middle of the page.
	if spec.Rotation == 0 {
		spec.Rotation = 45
	}
	theta := spec.Rotation * math.Pi / 180
	a, bm := math.Cos(theta), math.Sin(theta)
	// Compose T(cx,cy) * R(theta) * T(-textW/2, -textH/2) into one Tm.
	halfW, halfH := textW/2, textH/2
	e := cx - (halfW*a - halfH*bm)
	f := cy - (halfW*bm + halfH*a)

	fmt.Fprintf(&b, "BT\n/%s %s Tf\n", font, num(size))
	fmt.Fprintf(&b, "%s %s %s %s %s %s Tm\n",
		num(a), num(bm), num(-bm), num(a), num(e), num(f))
	fmt.Fprintf(&b, "(%s) Tj\nET\n", escapePDFString(sanitizeASCII(line)))
	b.WriteString("Q\n")
	return []byte(b.String()), nil
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', 4, 64) }

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
