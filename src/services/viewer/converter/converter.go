// Package converter defines the office-to-PDF conversion seam of the secure
// viewer (FR-3.4, TR-4.3).
//
// The secure viewer normalises every office document (DOCX, XLSX, PPTX, ODT,
// …) to PDF so all content flows through the uniform page-slicing and
// watermarking pipeline. Conversion itself is a heavy, lab-deferred worker:
// LibreOffice headless is NOT installed on developer machines and no
// e2e test executes `soffice`. This package therefore defines the interface
// upstream code programs against and ships the LibreOffice stub that will be
// swapped for the real worker when the conversion service lands — see
// README.md ("Lab-deferred").
package converter

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ErrUnavailable is returned by a converter that is configured but cannot
// perform conversions (e.g. the LibreOffice binary is not installed).
var ErrUnavailable = errors.New("converter: office-to-PDF conversion unavailable")

// Converter normalises one office document payload to PDF bytes.
//
// Implementations must be safe for concurrent use. src is the raw document
// payload; name is the original file name (extension drives LibreOffice's
// input filter).
type Converter interface {
	// Supports reports whether contentType is a document this converter can
	// normalise.
	Supports(contentType string) bool
	// ConvertToPDF converts src (a document of a Supported content type,
	// named name) into PDF bytes.
	ConvertToPDF(ctx context.Context, src []byte, name string) ([]byte, error)
}

// LibreOffice is the FR-3.4/TR-4.3 worker stub. It is intentionally inert in
// this wave: the real implementation shells out to
// `soffice --headless --convert-to pdf` and ships with the conversion
// worker. The stub exists so service wiring compiles, fails loudly and
// honestly, and never fakes a conversion result.
type LibreOffice struct {
	// sofficePath is the absolute path to the soffice binary the real worker
	// will use; kept for configuration validation and error messages.
	sofficePath string
}

// NewLibreOffice configures the LibreOffice converter stub for the given
// soffice binary path.
func NewLibreOffice(sofficePath string) *LibreOffice {
	return &LibreOffice{sofficePath: sofficePath}
}

// officeContentTypes is the MIME set the LibreOffice worker will accept.
var officeContentTypes = []string{
	"application/msword", // .doc
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document", // .docx
	"application/vnd.ms-excel", // .xls
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",         // .xlsx
	"application/vnd.ms-powerpoint",                                             // .ppt
	"application/vnd.openxmlformats-officedocument.presentationml.presentation", // .pptx
	"application/vnd.oasis.opendocument.text",                                   // .odt
	"application/vnd.oasis.opendocument.spreadsheet",                            // .ods
	"application/vnd.oasis.opendocument.presentation",                           // .odp
	"application/rtf", // .rtf
}

// Supports reports whether contentType is an office document MIME type.
func (c *LibreOffice) Supports(contentType string) bool {
	ct := strings.ToLower(contentType)
	for _, known := range officeContentTypes {
		if ct == known {
			return true
		}
	}
	return false
}

// ConvertToPDF implements Converter. The real worker is lab-deferred
// (FR-3.4, TR-4.3); this stub always fails with ErrUnavailable and never
// executes soffice.
func (c *LibreOffice) ConvertToPDF(ctx context.Context, src []byte, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("%w: LibreOffice headless worker (%q) is not shipped in this wave; conversion runs in the lab worker (FR-3.4, TR-4.3)",
		ErrUnavailable, c.sofficePath)
}

// Compile-time assertion.
var _ Converter = (*LibreOffice)(nil)
