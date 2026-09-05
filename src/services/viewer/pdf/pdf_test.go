package pdf

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ivenkwan/evdr/src/services/viewer/pdf/testpdf"
)

// parsePages parses data and returns the decoded content of page n's content
// stream (white-box: the test lives in package pdf).
func contentOf(t *testing.T, data []byte, pageNo int) []byte {
	t.Helper()
	d, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pageNo < 1 || pageNo > len(d.pages) {
		t.Fatalf("page %d out of range 1..%d", pageNo, len(d.pages))
	}
	dict, ok := d.pages[pageNo-1].value.(Dict)
	if !ok {
		t.Fatal("page is not a dict")
	}
	contentsRaw := dict[Name("Contents")]
	ref, ok := contentsRaw.(Ref)
	if !ok {
		t.Fatalf("page %d contents = %T, want Ref", pageNo, contentsRaw)
	}
	streamObj := d.objects[ref.Num]
	payload, err := streamData(d.data, streamObj)
	if err != nil {
		t.Fatalf("streamData: %v", err)
	}
	return payload
}

// pageCount parses data and returns its page count.
func pageCount(t *testing.T, data []byte) int {
	t.Helper()
	d, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d.PageCount()
}

func TestParsePageCount(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  testpdf.Options
		pages int
	}{
		{"three-pages", testpdf.Options{Pages: 3}, 3},
		{"single-page", testpdf.Options{Pages: 1}, 1},
		{"flate-three-pages", testpdf.Options{Pages: 3, Flate: true}, 3},
		{"per-page-resources", testpdf.Options{Pages: 2, PageResources: true}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := testpdf.Doc(tc.opts)
			d, err := Parse(data)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := d.PageCount(); got != tc.pages {
				t.Fatalf("PageCount = %d, want %d", got, tc.pages)
			}
			if !pdfHeader(data) {
				t.Error("pdfHeader must accept the fixture")
			}
		})
	}
}

func TestParseRejectsNonPDF(t *testing.T) {
	cases := map[string][]byte{
		"empty":    nil,
		"short":    []byte("hello"),
		"noheader": bytes.Repeat([]byte("not a pdf at all, sorry"), 100),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(data); err == nil {
				t.Fatal("Parse accepted garbage")
			} else if !errors.Is(err, ErrNotPDF) {
				t.Fatalf("error = %v, want ErrNotPDF", err)
			}
		})
	}
}

func TestExtractPageIsolation(t *testing.T) {
	data := testpdf.Doc(testpdf.Options{Pages: 3})
	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if d.PageCount() != 3 {
		t.Fatalf("fixture pages = %d", d.PageCount())
	}

	for _, n := range []int{1, 2, 3} {
		single, err := d.ExtractPage(n)
		if err != nil {
			t.Fatalf("ExtractPage(%d): %v", n, err)
		}
		// The extracted page is a standalone single-page document.
		if got := pageCount(t, single); got != 1 {
			t.Fatalf("extracted page %d has %d pages, want 1", n, got)
		}
		// Its own body is present, every other page's body is absent.
		if !bytes.Contains(single, []byte(testpdf.Marker(n))) {
			t.Errorf("extract(%d) lost its own marker", n)
		}
		for _, other := range []int{1, 2, 3} {
			if other == n {
				continue
			}
			if bytes.Contains(single, []byte(testpdf.Marker(other))) {
				t.Errorf("extract(%d) leaks page %d body", n, other)
			}
		}
		// Document-level metadata (/Info) is dropped on extraction.
		if bytes.Contains(single, []byte("Fixture Confidential Report")) {
			t.Errorf("extract(%d) retains trailer /Info metadata", n)
		}
	}
}

func TestExtractPageDeterministicAndIdempotent(t *testing.T) {
	data := testpdf.Doc(testpdf.Options{Pages: 3, Flate: true, PageResources: true})
	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	a, err := d.ExtractPage(2)
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.ExtractPage(2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("ExtractPage is not deterministic")
	}
	// Re-extracting the extracted page is byte-identical (stable subset).
	sub, err := Parse(a)
	if err != nil {
		t.Fatalf("extracted page does not re-parse: %v", err)
	}
	c, err := sub.ExtractPage(1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, c) {
		t.Error("re-extraction of a single page must be byte-identical")
	}
}

func TestExtractPageOutOfRange(t *testing.T) {
	data := testpdf.Doc(testpdf.Options{Pages: 2})
	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, 3, 99} {
		if _, err := d.ExtractPage(n); !errors.Is(err, ErrPageNotFound) {
			t.Errorf("ExtractPage(%d) error = %v, want ErrPageNotFound", n, err)
		}
	}
}

func TestExtractKeepsMediaBox(t *testing.T) {
	data := testpdf.Doc(testpdf.Options{Pages: 4, VariedBoxes: true})
	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]string{1: "595 842", 2: "612 792", 3: "595 842", 4: "612 792"}
	for n := 1; n <= 4; n++ {
		single, err := d.ExtractPage(n)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(single, []byte(want[n])) {
			t.Errorf("extract(%d) lost its media box %q", n, want[n])
		}
	}
}

func TestExtractReducesObjectCount(t *testing.T) {
	data := testpdf.Doc(testpdf.Options{Pages: 3})
	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	single, err := d.ExtractPage(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(single) > len(data)/2 {
		t.Errorf("extracted page (%d bytes) not smaller than the 3-page source (%d bytes)", len(single), len(data))
	}
}

func TestStampContentRaw(t *testing.T) {
	data := testpdf.Doc(testpdf.Options{Pages: 2})
	const marker = "STAMP-MARKER-9f31"
	stamped, err := StampContent(data, func(p *Page) ([]byte, error) {
		font, err := p.AddFont()
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("BT /%s 8 Tf 0 0 Td (%s) Tj ET", font, marker)), nil
	})
	if err != nil {
		t.Fatalf("StampContent: %v", err)
	}
	if bytes.Equal(stamped, data) {
		t.Fatal("stamping must change the document bytes")
	}
	// Every page carries the marker inside its own content stream.
	for n := 1; n <= 2; n++ {
		if !bytes.Contains(contentOf(t, stamped, n), []byte(marker)) {
			t.Errorf("page %d content stream lacks the stamp", n)
		}
		// The original body survives alongside the stamp.
		if !bytes.Contains(contentOf(t, stamped, n), []byte(testpdf.Marker(n))) {
			t.Errorf("page %d lost its body", n)
		}
	}
	if !bytes.Contains(contentOf(t, stamped, 1), []byte(testpdf.Marker(2))) {
		t.Log("ok: page 1 body does not leak page 2 body") // sanity of fixture
	}
	if bytes.Contains(contentOf(t, stamped, 1), []byte(testpdf.Marker(2))) {
		t.Error("page 1 content stream must not contain page 2's body")
	}
	// The stamp must not corrupt the document structure.
	if got := pageCount(t, stamped); got != 2 {
		t.Errorf("stamped document pages = %d", got)
	}
	// Determinism.
	again, err := StampContent(data, func(p *Page) ([]byte, error) {
		font, err := p.AddFont()
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("BT /%s 8 Tf 0 0 Td (%s) Tj ET", font, marker)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stamped, again) {
		t.Error("StampContent is not deterministic")
	}
}

func TestStampContentFlate(t *testing.T) {
	data := testpdf.Doc(testpdf.Options{Pages: 2, Flate: true})
	const marker = "FLATE-STAMP-77"
	stamped, err := StampContent(data, func(p *Page) ([]byte, error) {
		gs, err := p.AddExtGState(Dict{
			Name("Type"): Name("ExtGState"),
			Name("ca"):   0.4,
			Name("CA"):   0.4,
		})
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("q /%s gs Q (subtype %s) Tj", gs, marker)), nil
	})
	if err != nil {
		t.Fatalf("StampContent(flate): %v", err)
	}
	for n := 1; n <= 2; n++ {
		payload := contentOf(t, stamped, n)
		if !bytes.Contains(payload, []byte(marker)) {
			t.Errorf("page %d lacks the flate stamp: %q", n, payload)
		}
		if !bytes.Contains(payload, []byte(testpdf.Marker(n))) {
			t.Errorf("page %d lost its original body", n)
		}
	}
}

func TestStampExtractThenStampRoundTrip(t *testing.T) {
	// The pipeline order: slice a single page, then stamp it. The stamped
	// single page must remain a parseable single-page PDF.
	data := testpdf.Doc(testpdf.Options{Pages: 3})
	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	single, err := d.ExtractPage(3)
	if err != nil {
		t.Fatal(err)
	}
	stamped, err := StampContent(single, func(p *Page) ([]byte, error) {
		font, err := p.AddFont()
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("BT /%s 10 Tf 72 72 Td (WM-SLICE-ROUNDTRIP) Tj ET", font)), nil
	})
	if err != nil {
		t.Fatalf("stamp of extracted page: %v", err)
	}
	if got := pageCount(t, stamped); got != 1 {
		t.Fatalf("stamped single page has %d pages", got)
	}
	if !bytes.Contains(contentOf(t, stamped, 1), []byte("WM-SLICE-ROUNDTRIP")) {
		t.Error("round-tripped page lost its watermark")
	}
	if !bytes.Contains(stamped, []byte(testpdf.Marker(3))) {
		t.Error("round-tripped page lost its body marker")
	}
	if bytes.Contains(stamped, []byte(testpdf.Marker(1))) || bytes.Contains(stamped, []byte(testpdf.Marker(2))) {
		t.Error("round-tripped page leaks sibling content")
	}
}

func TestStampFontAddedOncePerSharedResources(t *testing.T) {
	// Default fixtures share the font through the Pages-root /Resources.
	// Both pages must end up with F1 declared exactly once.
	data := testpdf.Doc(testpdf.Options{Pages: 2})
	stamped, err := StampContent(data, func(p *Page) ([]byte, error) {
		font, err := p.AddFont()
		if err != nil {
			return nil, err
		}
		return []byte(fmt.Sprintf("BT /%s 9 Tf 10 10 Td (x) Tj ET", font)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The stamp-added font carries /Encoding /WinAnsiEncoding (the fixture
	// font does not), so one occurrence proves shared resources declared the
	// added font exactly once for both pages.
	if got := strings.Count(string(stamped), "WinAnsiEncoding"); got != 1 {
		t.Errorf("added font declared %d times on shared resources, want 1", got)
	}
	if got := strings.Count(string(stamped), "/BaseFont /Helvetica"); got != 2 {
		t.Errorf("Helvetica font dicts = %d, want 2 (fixture + one added)", got)
	}
}

func TestStampErrorsOnUnsupportedContents(t *testing.T) {
	data := testpdf.Doc(testpdf.Options{Pages: 1})
	// Build a broken layout: /Contents points at a plain (non-stream) object.
	d, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	pageDict := d.pages[0].value.(Dict)
	streamRef := pageDict[Name("Contents")].(Ref)
	plain := d.objects[streamRef.Num]
	_ = plain
	// Remove the content stream object's stream-ness by pointing Contents at
	// the catalog instead.
	pageDict[Name("Contents")] = d.rootRef
	out, err := d.writeAll()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := StampContent(out, func(p *Page) ([]byte, error) {
		return []byte("q Q"), nil
	}); err == nil {
		t.Fatal("stamping a non-stream /Contents must fail")
	} else if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v, want ErrUnsupported", err)
	}
}
