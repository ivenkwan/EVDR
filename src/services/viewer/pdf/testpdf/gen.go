// Package testpdf builds deterministic, minimal, valid multi-page PDF
// fixtures for the viewer test suites.
//
// It exists so page slicing and watermark tests never depend on an external
// PDF library or on generated test assets checked into the tree: a fixture is
// produced in-memory as literal PDF syntax with a correct classic xref table.
// The generator is test support — it is NOT part of the production PDF
// pipeline and must not be imported by non-test code.
//
// Layout of a generated document (n pages):
//
//	1 0 obj        Catalog (/Pages 2 0 R)
//	2 0 obj        Pages root (/Kids [3 0 R 5 0 R ...], /Count n, optional
//	               /Resources so pages inherit the font from the tree root)
//	3,5,7,...      page objects (each /Contents <k> 0 R)
//	4,6,8,...      page content streams (one per page)
//	fontNum        Type1 /Helvetica, shared by every page
//	infoNum        /Info trailer entry with a /Title — used to assert that
//	               page extraction drops document-level metadata
//
// Content streams are written uncompressed by default; with Options.Flate they
// are compressed through /FlateDecode (compress/zlib) — the same filter the
// production slicer supports. Every page body embeds Marker(n), a unique
// greppable ASCII token (e.g. "MOCKPAGE02-SECRET-CONTENT"), so tests can prove
// an extracted page carries only its own body and that watermark operators
// really reached the page's content stream.
package testpdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
)

// Marker returns the unique body token stamped on page n (1-indexed).
func Marker(n int) string { return fmt.Sprintf("MOCKPAGE%02d-SECRET-CONTENT", n) }

// PageText returns the visible text drawn on page n of a generated fixture.
func PageText(n int) string { return fmt.Sprintf("Mock report page %d: %s", n, Marker(n)) }

// Options tunes fixture layout for the slice/stamp cases under test.
type Options struct {
	// Pages is the number of pages. Default 1.
	Pages int
	// Flate compresses each page's content stream with /FlateDecode.
	Flate bool
	// PageResources gives each page its own /Resources dict (with /F1 →
	// Helvetica); when false the font is inherited from the Pages root node,
	// which exercises PDF resource inheritance in the slicer.
	PageResources bool
	// VariedBoxes writes a distinct MediaBox per page; when false every page
	// is A4 [0 0 595 842].
	VariedBoxes bool
}

// Doc renders a fixture document. Output is deterministic for identical
// options and parses with the viewer's pdf engine.
func Doc(opts Options) []byte {
	if opts.Pages <= 0 {
		opts.Pages = 1
	}
	fontNum := 3 + 2*opts.Pages
	infoNum := fontNum + 1

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")

	// offsets[num] = byte offset of object num. Index 0 is unused.
	offsets := make([]int, infoNum+1)
	write := func(num int, body string) {
		offsets[num] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n", num)
		b.WriteString(body)
		b.WriteString("\nendobj\n")
	}

	write(1, "<< /Type /Catalog /Pages 2 0 R >>")

	kids := make([]string, 0, opts.Pages)
	for i := 1; i <= opts.Pages; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+2*(i-1)))
	}
	var rootRes string
	if !opts.PageResources {
		rootRes = fmt.Sprintf(" /Resources << /Font << /F1 %d 0 R >> >>", fontNum)
	}
	write(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d%s >>",
		strings.Join(kids, " "), opts.Pages, rootRes))

	boxFor := func(n int) string {
		if opts.VariedBoxes {
			if n%2 == 0 {
				return "[0 0 612 792]" // US Letter on even pages
			}
			return "[0 0 595 842]" // A4 on odd pages
		}
		return "[0 0 595 842]"
	}

	for i := 1; i <= opts.Pages; i++ {
		pageNum := 3 + 2*(i-1)
		contentNum := pageNum + 1
		var res string
		if opts.PageResources {
			res = fmt.Sprintf(" /Resources << /Font << /F1 %d 0 R >> >>", fontNum)
		}
		write(pageNum, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox %s /Contents %d 0 R%s >>",
			boxFor(i), contentNum, res))

		content := []byte(fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", PageText(i)))
		var filter string
		if opts.Flate {
			var zb bytes.Buffer
			zw := zlib.NewWriter(&zb)
			_, _ = zw.Write(content)
			_ = zw.Close()
			content = zb.Bytes()
			filter = " /Filter /FlateDecode"
		}
		write(contentNum, fmt.Sprintf("<< /Length %d%s >>\nstream\n%s\nendstream",
			len(content), filter, content))
	}

	write(fontNum, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	write(infoNum, "<< /Title (Fixture Confidential Report) /Producer (EVDR testpdf) >>")

	// Xref table and trailer.
	xrefOff := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", infoNum+1)
	b.WriteString("0000000000 65535 f \n")
	for num := 1; num <= infoNum; num++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[num])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\n", infoNum+1, infoNum)
	fmt.Fprintf(&b, "startxref\n%d\n%%%%EOF\n", xrefOff)
	return b.Bytes()
}
