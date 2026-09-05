package pdf

import (
	"bytes"
	"fmt"
	"reflect"
)

// StampFunc receives a per-page handle and returns the content-stream
// operators to append to that page. The handle exposes the page number, its
// media box (inherited up the tree when the page lacks one) and methods to
// declare the resources the operators reference — fonts and graphics-state
// dictionaries are inserted into the page's effective /Resources before the
// operators are appended, so the stamped bytes are self-contained.
type StampFunc func(p *Page) ([]byte, error)

// Page is the per-page handle passed to a StampFunc.
type Page struct {
	// Number is the 1-indexed page number within the document.
	Number int

	doc *Document
	obj *object
	run *stampRun
}

// stampRun carries state shared across the pages of one StampContent call so
// that identical resources are declared only once per owning dictionary.
type stampRun struct {
	fontByOwner map[int]Name
	gsByOwner   map[int]struct {
		name  Name
		state Dict
	}
}

func newStampRun() *stampRun {
	return &stampRun{
		fontByOwner: map[int]Name{},
		gsByOwner: map[int]struct {
			name  Name
			state Dict
		}{},
	}
}

// Box returns the page's media box, inheriting up the /Parent chain when the
// page dictionary lacks one (PDF 1.7 §7.7.3.3). Falls back to US Letter when
// the document defines none.
func (p *Page) Box() (Box, error) {
	for cur := p.obj; cur != nil; {
		dict, ok := cur.value.(Dict)
		if !ok {
			return Box{}, fmt.Errorf("%w: page %d is not a dictionary", ErrUnsupported, p.Number)
		}
		if mbRaw, ok := dict[Name("MediaBox")]; ok {
			mb, ok := boxValue(mbRaw)
			if !ok {
				return Box{}, fmt.Errorf("%w: page %d has a malformed /MediaBox", ErrUnsupported, p.Number)
			}
			return mb, nil
		}
		cur = p.resolveParent(cur, dict)
	}
	return Box{X1: 612, Y1: 792}, nil // US Letter default
}

func (p *Page) resolveParent(cur *object, dict Dict) *object {
	parentRaw, ok := dict[Name("Parent")]
	if !ok {
		return nil
	}
	ref, ok := parentRaw.(Ref)
	if !ok {
		return nil
	}
	return p.doc.objects[ref.Num]
}

// AddFont declares a standard Type1 Helvetica font on the page's effective
// /Resources and returns the resource name (e.g. "F1") operators should use.
// When several pages share one resources dictionary, the font is declared
// once and the same name is returned for every page of the same
// StampContent run.
func (p *Page) AddFont() (Name, error) {
	res, owner, err := p.resources()
	if err != nil {
		return "", err
	}
	ownerNum := -1
	if owner != nil {
		ownerNum = owner.num
	}
	if p.run != nil {
		if n, ok := p.run.fontByOwner[ownerNum]; ok {
			return n, nil
		}
	}
	fontsRaw, has := res[Name("Font")]
	var fonts Dict
	if has {
		f, ok := fontsRaw.(Dict)
		if !ok {
			return "", fmt.Errorf("%w: page %d: /Font is not a dictionary", ErrUnsupported, p.Number)
		}
		fonts = f
	} else {
		fonts = Dict{}
		res[Name("Font")] = fonts
	}
	name, err := freeName(fonts, "F")
	if err != nil {
		return "", fmt.Errorf("%w: page %d: %v", ErrUnsupported, p.Number, err)
	}
	fonts[name] = Dict{
		Name("Type"):     Name("Font"),
		Name("Subtype"):  Name("Type1"),
		Name("BaseFont"): Name("Helvetica"),
		Name("Encoding"): Name("WinAnsiEncoding"),
	}
	if p.run != nil {
		p.run.fontByOwner[ownerNum] = name
	}
	return name, nil
}

// AddExtGState declares a graphics-state dictionary (e.g. an alpha/opacity
// setting) on the page's effective /Resources and returns its resource name
// (e.g. "GS1"). Operators should reference it with "name gs". Identical
// states on a shared dictionary are declared once per StampContent run.
func (p *Page) AddExtGState(state Dict) (Name, error) {
	res, owner, err := p.resources()
	if err != nil {
		return "", err
	}
	ownerNum := -1
	if owner != nil {
		ownerNum = owner.num
	}
	if p.run != nil {
		if rec, ok := p.run.gsByOwner[ownerNum]; ok && reflect.DeepEqual(rec.state, state) {
			return rec.name, nil
		}
	}
	gsRaw, has := res[Name("ExtGState")]
	var gs Dict
	if has {
		g, ok := gsRaw.(Dict)
		if !ok {
			return "", fmt.Errorf("%w: page %d: /ExtGState is not a dictionary", ErrUnsupported, p.Number)
		}
		gs = g
	} else {
		gs = Dict{}
		res[Name("ExtGState")] = gs
	}
	name, err := freeName(gs, "GS")
	if err != nil {
		return "", fmt.Errorf("%w: page %d: %v", ErrUnsupported, p.Number, err)
	}
	gs[name] = state
	if p.run != nil {
		p.run.gsByOwner[ownerNum] = struct {
			name  Name
			state Dict
		}{name, state}
	}
	return name, nil
}

// freeName picks the first unused name of the form prefix+"<n>" (n ≥ 1) on
// dict.
func freeName(dict Dict, prefix string) (Name, error) {
	for i := 1; i <= 64; i++ {
		n := Name(fmt.Sprintf("%s%d", prefix, i))
		if _, taken := dict[n]; !taken {
			return n, nil
		}
	}
	return "", fmt.Errorf("no free resource name for prefix %s", prefix)
}

// resources returns the page's effective /Resources dictionary, resolving
// inheritance up the /Parent chain. It returns the dict and the object it
// lives on (nil when the dict is inline on the page itself); when no
// resources exist anywhere on the chain a fresh dict is attached to the page
// so stamping never mutates a sibling's state.
func (p *Page) resources() (Dict, *object, error) {
	for cur := p.obj; cur != nil; {
		dict, ok := cur.value.(Dict)
		if !ok {
			return nil, nil, fmt.Errorf("%w: page %d is not a dictionary", ErrUnsupported, p.Number)
		}
		if resRaw, ok := dict[Name("Resources")]; ok {
			switch res := resRaw.(type) {
			case Dict:
				return res, cur, nil
			case Ref:
				obj := p.doc.objects[res.Num]
				if obj == nil {
					return nil, nil, fmt.Errorf("%w: page %d: /Resources object %d missing", ErrUnsupported, p.Number, res.Num)
				}
				d, ok := obj.value.(Dict)
				if !ok {
					return nil, nil, fmt.Errorf("%w: page %d: /Resources object is not a dictionary", ErrUnsupported, p.Number)
				}
				return d, obj, nil
			default:
				return nil, nil, fmt.Errorf("%w: page %d: /Resources has an invalid type", ErrUnsupported, p.Number)
			}
		}
		cur = p.resolveParent(cur, dict)
	}
	// No resources on the chain: attach a fresh dict to the page itself.
	pageDict, ok := p.obj.value.(Dict)
	if !ok {
		return nil, nil, fmt.Errorf("%w: page %d is not a dictionary", ErrUnsupported, p.Number)
	}
	fresh := Dict{}
	pageDict[Name("Resources")] = fresh
	return fresh, p.obj, nil
}

// StampContent parses data, invokes stamp for every page, appends the
// returned operators to each page's content stream (decode → append →
// re-encode when the stream is /FlateDecode compressed), and returns the
// rewritten whole document. The output is deterministic.
func StampContent(data []byte, stamp StampFunc) ([]byte, error) {
	d, err := Parse(data)
	if err != nil {
		return nil, err
	}
	run := newStampRun()
	for i, leaf := range d.pages {
		page := &Page{Number: i + 1, doc: d, obj: leaf, run: run}
		ops, err := stamp(page)
		if err != nil {
			return nil, fmt.Errorf("pdf: stamp page %d: %w", page.Number, err)
		}
		if len(bytes.TrimSpace(ops)) == 0 {
			continue
		}
		if err := d.appendOps(leaf, ops); err != nil {
			return nil, fmt.Errorf("pdf: stamp page %d: %w", page.Number, err)
		}
	}
	return d.writeAll()
}

// appendOps appends ops to one page's content. The page must reference a
// single stream object (or a /Contents array whose last element is one);
// inline and multiple-stream layouts are outside the v0 subset.
func (d *Document) appendOps(page *object, ops []byte) error {
	dict, ok := page.value.(Dict)
	if !ok {
		return fmt.Errorf("%w: page object %d is not a dictionary", ErrUnsupported, page.num)
	}
	contentsRaw, ok := dict[Name("Contents")]
	if !ok {
		return fmt.Errorf("%w: page %d has no content stream", ErrLayoutUnsupported, page.num)
	}
	var streamRef Ref
	switch c := contentsRaw.(type) {
	case Ref:
		streamRef = c
	case Array:
		// Multiple streams draw in order; v0 appends to the last one, which
		// preserves the draw order for the appended operators.
		if len(c) == 0 {
			return fmt.Errorf("%w: page %d has an empty /Contents array", ErrLayoutUnsupported, page.num)
		}
		last, ok := c[len(c)-1].(Ref)
		if !ok {
			return fmt.Errorf("%w: page %d /Contents array tail is not a stream reference", ErrLayoutUnsupported, page.num)
		}
		streamRef = last
	default:
		return fmt.Errorf("%w: page %d has inline content (unsupported in v0)", ErrLayoutUnsupported, page.num)
	}
	streamObj := d.objects[streamRef.Num]
	if streamObj == nil || !streamObj.hasStream {
		return fmt.Errorf("%w: page %d content stream object %d missing", ErrUnsupported, page.num, streamRef.Num)
	}
	streamDict, ok := streamObj.value.(Dict)
	if !ok {
		return fmt.Errorf("%w: content stream %d is not a dictionary", ErrUnsupported, streamRef.Num)
	}
	wasFlate := false
	if fRaw, has := streamDict[Name("Filter")]; has {
		if f, ok := fRaw.(Name); ok && f == Name("FlateDecode") {
			wasFlate = true
		} else {
			return fmt.Errorf("%w: content stream %d filter cannot be stamped", ErrLayoutUnsupported, streamRef.Num)
		}
	}
	payload, err := streamData(d.data, streamObj)
	if err != nil {
		return err
	}
	combined := make([]byte, 0, len(payload)+len(ops))
	combined = append(combined, payload...)
	if len(combined) > 0 && combined[len(combined)-1] != '\n' {
		combined = append(combined, '\n')
	}
	combined = append(combined, ops...)
	if wasFlate {
		streamObj.stream = reflate(streamDict, combined)
	} else {
		streamDict[Name("Length")] = int64(len(combined))
		streamObj.stream = combined
	}
	return nil
}
