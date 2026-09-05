// Package pdf is a dependency-free PDF slicing engine for the EVDR secure
// viewer (FR-3.1, TR-4.1).
//
// # Scope and guarantees
//
// The viewer must never serve a full document: only individually extracted
// pages cross the HTTP boundary, and the watermark is baked into the page
// bytes server-side before delivery. This package implements the two
// primitives the rest of the pipeline needs:
//
//   - PageCount / ExtractPage — parse a source PDF once, then pull out any
//     single page as a standalone, valid single-page PDF carrying only the
//     objects that page (transitively) references. Document-level metadata
//     (trailer /Info) is deliberately dropped, and sibling pages are pruned
//     from the page tree.
//   - StampContent — append content-stream operators to every page (font and
//     graphics-state resources are declared automatically), which is how the
//     server-side watermark compositor bakes identity tokens into the bytes.
//
// Supported subset (v0, documented limitation)
//
// This is a deliberately small engine, not a general PDF library: classic
// (non-stream) xref tables; objects reached by xref offset; unencrypted
// documents; stream filters none or /FlateDecode (compress/zlib); page
// content that is a single stream object (or a /Contents array whose last
// element is one). Documents outside this subset return ErrUnsupported. The
// engine is dependency-free so the repo root go.mod stays untouched; a fuller
// PDF library can replace this package behind the same surface later without
// touching the renderer or the HTTP layer.
//
// All byte output is deterministic: same input produces identical output.
package pdf

import (
	"errors"
	"fmt"
	"math"
	"strconv"
)

// Errors returned by the engine. Callers match with errors.Is.
var (
	// ErrNotPDF — the bytes do not look like a PDF document.
	ErrNotPDF = errors.New("pdf: not a PDF document")
	// ErrUnsupported — the document uses a PDF feature outside the v0
	// subset (encryption, xref streams, object streams, exotic filters…).
	ErrUnsupported = errors.New("pdf: document structure outside the v0 engine subset")
	// ErrPageNotFound — the requested 1-indexed page is out of range.
	ErrPageNotFound = errors.New("pdf: page out of range")
	// ErrLayoutUnsupported — the page's content layout (contents array of
	// multiple streams or an inline stream) cannot be stamped by v0.
	ErrLayoutUnsupported = errors.New("pdf: page layout unsupported by the v0 engine")
)

// Name is a PDF name object (without the leading '/').
type Name string

// Ref is an indirect object reference.
type Ref struct {
	Num int
	Gen int
}

// Dict is a PDF dictionary. Keys are Name; values are the object model below.
type Dict map[Name]any

// Array is a PDF array.
type Array []any

// Object model values are: nil (null), bool, int64, float64, string,
// Name, Ref, Array, Dict.

// Box is a page media box in PDF user-space points (y-up).
type Box struct {
	X0, Y0, X1, Y1 float64
}

// Width returns the box width in points.
func (b Box) Width() float64 { return b.X1 - b.X0 }

// Height returns the box height in points.
func (b Box) Height() float64 { return b.Y1 - b.Y0 }

// Document is a parsed PDF. It is safe for concurrent reads (PageCount,
// ExtractPage) once Parse returns.
type Document struct {
	data    []byte
	objects map[int]*object
	rootRef Ref
	pages   []*object // leaf /Type /Page objects in document order
}

type object struct {
	num       int
	gen       int
	value     any    // nil, bool, int64, float64, string, Name, Ref, Array, Dict
	stream    []byte // stream payload view into Document.data; nil when not a stream object
	hasStream bool
}

// Parse validates and indexes a PDF document. The returned Document keeps a
// view of data; callers must not mutate data afterwards.
func Parse(data []byte) (*Document, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("%w: %d bytes is too short", ErrNotPDF, len(data))
	}
	if !pdfHeader(data) {
		return nil, fmt.Errorf("%w: missing %%PDF header", ErrNotPDF)
	}

	objects, trailer, err := loadXref(data)
	if err != nil {
		return nil, err
	}
	rootRef, ok := trailer[Name("Root")].(Ref)
	if !ok {
		return nil, fmt.Errorf("%w: trailer has no /Root", ErrNotPDF)
	}
	root := objects[rootRef.Num]
	if root == nil {
		return nil, fmt.Errorf("%w: /Root object %d missing", ErrUnsupported, rootRef.Num)
	}
	cat, ok := root.value.(Dict)
	if !ok {
		return nil, fmt.Errorf("%w: /Root is not a dictionary", ErrNotPDF)
	}
	if _, enc := cat[Name("Encrypt")]; enc {
		return nil, fmt.Errorf("%w: encrypted documents are not supported", ErrUnsupported)
	}
	pagesRef, ok := cat[Name("Pages")].(Ref)
	if !ok {
		return nil, fmt.Errorf("%w: catalog has no /Pages tree", ErrNotPDF)
	}
	pages, err := collectPages(objects, pagesRef, nil)
	if err != nil {
		return nil, err
	}
	return &Document{data: data, objects: objects, rootRef: rootRef, pages: pages}, nil
}

func pdfHeader(data []byte) bool {
	// The header may be preceded by up to 1024 bytes of junk (spec 7.5.1).
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	idx := bytesIndex(head, []byte("%PDF-"))
	return idx >= 0
}

func bytesIndex(haystack, needle []byte) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// PageCount returns the number of pages in the document (1-indexed).
func (d *Document) PageCount() int { return len(d.pages) }

// ExtractPage returns page n (1-indexed) as a standalone single-page PDF
// containing only the objects reachable from that page and the document
// catalog. Trailer /Info and all sibling pages are dropped, so an extracted
// page never carries another page's content. The result is deterministic.
func (d *Document) ExtractPage(n int) ([]byte, error) {
	if n < 1 || n > len(d.pages) {
		return nil, fmt.Errorf("%w: page %d of %d", ErrPageNotFound, n, len(d.pages))
	}
	leaf := d.pages[n-1]

	included, err := d.pageClosure(leaf)
	if err != nil {
		return nil, err
	}
	kidsOverride := d.trimKids(included)
	return d.writeSubset(included, kidsOverride)
}

// trimKids computes, per included Pages-tree node, the filtered /Kids array
// (only kids that are themselves included) so single-page extraction prunes
// sibling pages while preserving /Parent chains and inherited resources.
func (d *Document) trimKids(included map[int]bool) map[int][]Ref {
	out := map[int][]Ref{}
	for num := range included {
		obj := d.objects[num]
		if obj == nil {
			continue
		}
		dict, ok := obj.value.(Dict)
		if !ok {
			continue
		}
		if _, hasCount := dict[Name("Count")]; !hasCount {
			continue // leaf page or non-tree node
		}
		kids, ok := dict[Name("Kids")].(Array)
		if !ok {
			continue
		}
		var kept []Ref
		for _, kid := range kids {
			if r, ok := kid.(Ref); ok && included[r.Num] {
				kept = append(kept, r)
			}
		}
		if len(kept) > 0 {
			out[num] = kept
		}
	}
	return out
}

// collectPages walks the /Pages tree from pagesRef and returns leaf page
// objects in document order. visited guards against cyclic trees.
func collectPages(objects map[int]*object, pagesRef Ref, visited map[int]bool) ([]*object, error) {
	if visited == nil {
		visited = map[int]bool{}
	}
	node := objects[pagesRef.Num]
	if node == nil {
		return nil, fmt.Errorf("%w: pages node %d missing", ErrUnsupported, pagesRef.Num)
	}
	if visited[pagesRef.Num] {
		return nil, fmt.Errorf("%w: cyclic /Pages tree at %d", ErrUnsupported, pagesRef.Num)
	}
	visited[pagesRef.Num] = true
	dict, ok := node.value.(Dict)
	if !ok {
		return nil, fmt.Errorf("%w: pages node %d is not a dictionary", ErrUnsupported, pagesRef.Num)
	}
	var leaves []*object
	if kidsRaw, ok := dict[Name("Kids")]; ok {
		kids, ok := kidsRaw.(Array)
		if !ok {
			return nil, fmt.Errorf("%w: /Kids of %d is not an array", ErrUnsupported, pagesRef.Num)
		}
		for _, kidRaw := range kids {
			kidRef, ok := kidRaw.(Ref)
			if !ok {
				return nil, fmt.Errorf("%w: /Kids of %d holds a non-reference", ErrUnsupported, pagesRef.Num)
			}
			kid := objects[kidRef.Num]
			if kid == nil {
				return nil, fmt.Errorf("%w: page object %d missing", ErrUnsupported, kidRef.Num)
			}
			kd, ok := kid.value.(Dict)
			if !ok {
				return nil, fmt.Errorf("%w: page %d is not a dictionary", ErrUnsupported, kidRef.Num)
			}
			if _, isTree := kd[Name("Kids")]; isTree {
				sub, err := collectPages(objects, kidRef, visited)
				if err != nil {
					return nil, err
				}
				leaves = append(leaves, sub...)
			} else {
				leaves = append(leaves, kid)
			}
		}
	}
	return leaves, nil
}

// pageClosure returns the object numbers a single page needs: everything
// reachable from the catalog and from the page, except that no sibling pages
// are pulled in through /Kids arrays of the page tree.
func (d *Document) pageClosure(leaf *object) (map[int]bool, error) {
	included := map[int]bool{}
	queue := []Ref{d.rootRef, {Num: leaf.num}}
	for len(queue) > 0 {
		r := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if included[r.Num] {
			continue
		}
		obj := d.objects[r.Num]
		if obj == nil {
			return nil, fmt.Errorf("%w: referenced object %d is missing", ErrUnsupported, r.Num)
		}
		included[r.Num] = true
		if err := d.enqueueValue(obj.value, true, &queue); err != nil {
			return nil, err
		}
	}
	return included, nil
}

// enqueueValue walks a value tree, queueing references. topLevel marks the
// value of the object itself: for a Pages-tree node only then are /Kids
// skipped (siblings are pruned); nested values always traverse fully because
// a page's own dict is not a Pages node and shared resource dicts never list
// pages.
func (d *Document) enqueueValue(v any, topLevel bool, queue *[]Ref) error {
	switch tv := v.(type) {
	case Ref:
		*queue = append(*queue, tv)
		return nil
	case Array:
		for _, e := range tv {
			if err := d.enqueueValue(e, false, queue); err != nil {
				return err
			}
		}
		return nil
	case Dict:
		_, isPagesNode := tv[Name("Count")]
		for k, e := range tv {
			if topLevel && isPagesNode && k == Name("Kids") {
				continue
			}
			if err := d.enqueueValue(e, false, queue); err != nil {
				return err
			}
		}
		return nil
	default:
		return nil
	}
}

// dictInt reads an integer from a dict value; ok=false when absent or wrong
// type.
func dictInt(d Dict, key Name) (int64, bool) {
	switch v := d[key].(type) {
	case int64:
		return v, true
	case float64:
		return int64(v), true
	default:
		return 0, false
	}
}

// dictName reads a name from a dict value.
func dictName(d Dict, key Name) (Name, bool) {
	n, ok := d[key].(Name)
	return n, ok
}

// numValue converts any numeric object value to float64.
func numValue(v any) (float64, bool) {
	switch tv := v.(type) {
	case int64:
		return float64(tv), true
	case float64:
		return tv, true
	default:
		return 0, false
	}
}

// boxValue reads a [4]number array as a Box.
func boxValue(v any) (Box, bool) {
	arr, ok := v.(Array)
	if !ok || len(arr) != 4 {
		return Box{}, false
	}
	var out [4]float64
	for i, e := range arr {
		f, ok := numValue(e)
		if !ok {
			return Box{}, false
		}
		out[i] = f
	}
	return Box{out[0], out[1], out[2], out[3]}, true
}

// floatFmt renders a float deterministically with 4 decimals (trailing zeroes
// retained) — output never depends on map iteration or locale.
func floatFmt(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "0"
	}
	return strconv.FormatFloat(f, 'f', 4, 64)
}
