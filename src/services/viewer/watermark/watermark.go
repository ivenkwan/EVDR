// Package watermark composites server-side watermarks into single-page
// payloads before the viewer delivers them (FR-3.2, TR-4.2, ADR-0001).
//
// # Compliance property
//
// The watermark MUST be part of the bytes served to the browser: this package
// never returns an overlay or a side-channel instruction — Apply rewrites the
// payload itself. Client-side overlays are strippable and therefore
// unacceptable (TR-4.2). Content engineered here is deterministic for a given
// (spec, payload) pair so identical requests produce identical bytes.
//
// # Token line
//
// Token selection follows FR-1.3/FR-3.2: viewer identity, timestamp, client
// IP/domain and session ID. The compositor renders exactly what it is given
// in spi.WatermarkSpec — the caller (the viewer HTTP layer or a policy
// resolver) is responsible for resolving tokens from the authenticated
// session, never from client input. An empty spec (no tokens) is treated as a
// no-op and returns the payload unchanged; the policy engine upstream is what
// makes watermarking mandatory.
//
// Compositors
//
//   - PDF: appends a rotated text band (identity line) to the page content
//     stream via the pdf engine; alpha/opacity is applied through a graphics
//     state resource, the font is a standard base-14 Helvetica, so no font
//     file is embedded.
//   - Text: prefixes the token line to text/plain pages (the reference
//     adapters' rendered pages).
//
// Unsupported content types return ErrUnsupportedFormat.
package watermark

import (
	"context"
	"errors"

	"github.com/ivenkwan/evdr/src/spi"
)

// ErrUnsupportedFormat is returned by a compositor when it cannot watermark
// the given content type.
var ErrUnsupportedFormat = errors.New("watermark: cannot watermark this page content type")

// Compositor bakes a watermark into one page's bytes. Implementations must be
// safe for concurrent use and deterministic for identical inputs.
type Compositor interface {
	// Apply returns payload with the watermark baked in server-side. When
	// spec carries no tokens the payload is returned unchanged. contentType
	// is authoritative (e.g. "application/pdf", "text/plain").
	Apply(ctx context.Context, spec spi.WatermarkSpec, contentType string, payload []byte) ([]byte, error)
}

// CompositorFunc adapts a function to the Compositor interface.
type CompositorFunc func(ctx context.Context, spec spi.WatermarkSpec, contentType string, payload []byte) ([]byte, error)

// Apply implements Compositor.
func (f CompositorFunc) Apply(ctx context.Context, spec spi.WatermarkSpec, contentType string, payload []byte) ([]byte, error) {
	return f(ctx, spec, contentType, payload)
}

// tokenTimeFormat renders the timestamp token in UTC RFC 3339 (second
// precision). It is a constant so watermark bytes are reproducible from a
// fixed spec.
const tokenTimeFormat = "2006-01-02T15:04:05Z"

// Line renders the identity token line for spec: the tokens of FR-1.3 joined
// by " | ", skipping empty tokens. The timestamp is normalised to UTC.
func Line(spec spi.WatermarkSpec) string {
	ts := ""
	if !spec.Timestamp.IsZero() {
		ts = spec.Timestamp.UTC().Format(tokenTimeFormat)
	}
	parts := make([]string, 0, 4)
	for _, p := range []string{spec.ViewerIdentity, ts, spec.ClientIP, spec.SessionID} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		out := parts[0]
		for _, p := range parts[1:] {
			out += " | " + p
		}
		return out
	}
}

// sanitizeASCII replaces every byte above 0x7e with '?' so the token line
// stays encodable in the base-14 WinAnsi fonts used by the PDF compositor.
func sanitizeASCII(s string) string {
	needs := false
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7e {
			needs = true
			break
		}
	}
	if !needs {
		return s
	}
	b := []byte(s)
	for i, c := range b {
		if c > 0x7e {
			b[i] = '?'
		}
	}
	return string(b)
}

// escapePDFString escapes a string for embedding in a PDF literal string
// (backslash, parens, control bytes).
func escapePDFString(s string) string {
	out := make([]byte, 0, len(s)+8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '(', ')':
			out = append(out, '\\', c)
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		default:
			if c < 0x20 || c > 0x7e {
				out = append(out, '\\', '0'+c/64%8, '0'+c/8%8, '0'+c%8)
			} else {
				out = append(out, c)
			}
		}
	}
	return string(out)
}
