package watermark

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/ivenkwan/evdr/src/spi"
)

// Text composites a watermark into text/plain page payloads — the format the
// in-memory reference adapters render — by prepending the identity token line
// to the page body. The marker is part of the served bytes.
type Text struct{}

// NewText returns a text compositor.
func NewText() *Text { return &Text{} }

// watermarkPrefix marks the token line inside text pages so downstream
// tooling (and tests) can recognise compositor output unambiguously.
const watermarkPrefix = "[EVDR watermark: "

// Apply implements Compositor for "text/*" payloads.
func (c *Text) Apply(ctx context.Context, spec spi.WatermarkSpec, contentType string, payload []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "text/") {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedFormat, contentType)
	}
	line := Line(spec)
	if line == "" {
		return payload, nil // no tokens: nothing identifiable to stamp
	}
	var b bytes.Buffer
	b.WriteString(watermarkPrefix)
	b.WriteString(line)
	b.WriteString("]\n")
	b.Write(payload)
	return b.Bytes(), nil
}
