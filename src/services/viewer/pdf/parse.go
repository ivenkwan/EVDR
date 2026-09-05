package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// loadXref locates the startxref pointer in the file tail, follows the chain
// of classic xref tables (/Prev), and parses every object into the returned
// map. Stream payloads are captured as views into data (not decoded), except
// DecodeStream callers do that on demand.
func loadXref(data []byte) (map[int]*object, Dict, error) {
	start := findStartXref(data)
	if start < 0 {
		return nil, nil, fmt.Errorf("%w: no startxref found", ErrNotPDF)
	}
	pos := start
	objects := map[int]*object{}
	var trailer Dict
	seen := map[int]bool{} // guard against cyclic /Prev chains

	for {
		if seen[pos] {
			return nil, nil, fmt.Errorf("%w: cyclic xref /Prev chain", ErrUnsupported)
		}
		seen[pos] = true
		var err error
		trailer, pos, err = parseXrefTable(data, pos, objects)
		if err != nil {
			return nil, nil, err
		}
		prevRaw, hasPrev := trailer[Name("Prev")]
		if !hasPrev {
			break
		}
		prev, ok := prevRaw.(int64)
		if !ok {
			return nil, nil, fmt.Errorf("%w: /Prev is not an integer", ErrNotPDF)
		}
		if prev <= 0 {
			break
		}
		pos = int(prev)
	}
	return objects, trailer, nil
}

// findStartXref scans the tail of the file for the startxref keyword at a
// line boundary and returns the numeric offset that follows it.
func findStartXref(data []byte) int {
	tail := data
	if len(tail) > 4096 {
		tail = data[len(data)-4096:]
	}
	kw := []byte("startxref")
	base := len(data) - len(tail)
	for i := len(tail) - len(kw); i >= 0; i-- {
		if !bytes.Equal(tail[i:i+len(kw)], kw) {
			continue
		}
		if i > 0 {
			c := tail[i-1]
			if !(c == '\n' || c == '\r' || c == ' ' || c == '\t' || c == '\f') {
				continue
			}
		}
		j := i + len(kw)
		for j < len(tail) && (tail[j] == ' ' || tail[j] == '\t' || tail[j] == '\r' || tail[j] == '\n') {
			j++
		}
		end := j
		for end < len(tail) && tail[end] >= '0' && tail[end] <= '9' {
			end++
		}
		if end == j {
			continue
		}
		off := 0
		for _, c := range tail[j:end] {
			off = off*10 + int(c-'0')
		}
		return base + off
	}
	return -1
}

// parseXrefTable parses one classic xref table at pos: the "xref" keyword,
// subsections of "start count" plus entry rows, then the trailer dict. Every
// live (type n) entry is parsed into objects. Returns the trailer dict and
// the previous-table offset position to continue from (trailer /Prev is
// consumed by the caller). The returned pos is the trailer's /Prev value.
func parseXrefTable(data []byte, pos int, objects map[int]*object) (Dict, int, error) {
	var err error
	p := pos
	p = skipWS(data, p)
	var kw string
	if kw, p = readKeyword(data, p); kw != "xref" {
		// xref-stream based documents (PDF 1.5+) are outside the v0 subset.
		return nil, 0, fmt.Errorf("%w: xref stream at %d (only classic xref tables are supported)", ErrUnsupported, pos)
	}
	for {
		p = skipWS(data, p)
		var np int
		if kw, np = readKeyword(data, p); kw == "trailer" {
			p = np
			var trailerVal any
			if trailerVal, _, err = parseValue(data, p); err != nil {
				return nil, 0, err
			}
			trailer, ok := trailerVal.(Dict)
			if !ok {
				return nil, 0, fmt.Errorf("%w: trailer is not a dictionary", ErrNotPDF)
			}
			var prev int64
			if prevRaw, ok := trailer[Name("Prev")].(int64); ok {
				prev = prevRaw
			}
			return trailer, int(prev), nil
		}
		// Subsection header: "start count".
		var start, count int64
		if start, p, err = readInt(data, p); err != nil {
			return nil, 0, fmt.Errorf("%w: xref subsection header at %d: %v", ErrUnsupported, p, err)
		}
		if count, p, err = readInt(data, p); err != nil {
			return nil, 0, fmt.Errorf("%w: xref subsection count at %d: %v", ErrUnsupported, p, err)
		}
		for i := int64(0); i < count; i++ {
			p = skipWS(data, p)
			var off, gen int64
			var np int
			if off, np, err = readInt(data, p); err != nil {
				return nil, 0, fmt.Errorf("%w: xref row %d at %d", ErrUnsupported, start+i, p)
			}
			p = np
			if gen, np, err = readInt(data, p); err != nil {
				return nil, 0, err
			}
			p = np
			p = skipWS(data, p)
			if p >= len(data) {
				return nil, 0, fmt.Errorf("%w: truncated xref row %d", ErrUnsupported, start+i)
			}
			typ := data[p]
			p++
			// Skip the rest of the row (to end of line).
			p = skipToEOL(data, p)
			if typ != 'n' {
				continue // free (f) or compressed (2) entries: v0 ignores both
			}
			num := int(start + i)
			if _, exists := objects[num]; exists {
				continue // newer table wins
			}
			var obj *object
			if obj, err = parseObjectAt(data, int(off)); err != nil {
				return nil, 0, err
			}
			if obj.num != num {
				return nil, 0, fmt.Errorf("%w: xref entry %d points at object %d", ErrUnsupported, num, obj.num)
			}
			obj.gen = int(gen)
			objects[num] = obj
		}
	}
}

// parseObjectAt parses one indirect object "N G obj ... endobj" at off.
func parseObjectAt(data []byte, off int) (*object, error) {
	p := skipWS(data, off)
	num, np, err := readInt(data, p)
	if err != nil {
		return nil, fmt.Errorf("%w: object header at %d: %v", ErrUnsupported, off, err)
	}
	p = np
	gen, np, err := readInt(data, p)
	if err != nil {
		return nil, fmt.Errorf("%w: object header at %d: %v", ErrUnsupported, off, err)
	}
	p = skipWS(data, np)
	if kw, np2 := readKeyword(data, p); kw != "obj" {
		return nil, fmt.Errorf("%w: expected obj keyword at %d, got %q", ErrUnsupported, p, kw)
	} else {
		p = np2
	}

	obj := &object{num: int(num), gen: int(gen)}
	val, np, err := parseValue(data, p)
	if err != nil {
		return nil, fmt.Errorf("%w: object %d: %v", ErrUnsupported, num, err)
	}
	p = np
	obj.value = val

	// Optional stream keyword after a dictionary value.
	p = skipWS(data, p)
	if dict, isDict := val.(Dict); isDict && keywordAt(data, p, "stream") {
		p += len("stream")
		p = skipOneEOL(data, p)
		length, known := resolveStreamLength(data, dict)
		if !known {
			// Fall back to scanning for endstream.
			end := bytesIndexAt(data, p, []byte("endstream"))
			if end < 0 {
				return nil, fmt.Errorf("%w: object %d: unterminated stream", ErrUnsupported, num)
			}
			obj.stream = data[p:end]
			obj.hasStream = true
			p = end
		} else if length < 0 || int64(p)+length > int64(len(data)) {
			return nil, fmt.Errorf("%w: object %d: stream length %d out of bounds", ErrUnsupported, num, length)
		} else {
			obj.stream = data[p : p+int(length)]
			obj.hasStream = true
			p += int(length)
		}
		// Consume EOL + endstream + any trailing object junk up to endobj.
		end := bytesIndexAt(data, p, []byte("endstream"))
		if end < 0 {
			return nil, fmt.Errorf("%w: object %d: missing endstream", ErrUnsupported, num)
		}
		p = end + len("endstream")
	}

	// Consume up to and including the endobj keyword (tolerate junk).
	if idx := bytesIndexAt(data, p, []byte("endobj")); idx >= 0 {
		p = idx + len("endobj")
	}
	_ = p
	return obj, nil
}

// resolveStreamLength resolves the stream /Length from its dict: direct
// integer, or an indirect reference to an integer object. The referenced
// object may live anywhere in the file, so we scan forward (bounded) and
// finally over the whole file for its header.
func resolveStreamLength(data []byte, dict Dict) (int64, bool) {
	switch lv := dict[Name("Length")].(type) {
	case int64:
		return lv, true
	case float64:
		return int64(lv), true
	case Ref:
		return findIntObject(data, lv.Num)
	default:
		return 0, false
	}
}

// findIntObject locates "N 0 obj" in the file and parses the single integer
// that follows. Returns ok=false when not found.
func findIntObject(data []byte, num int) (int64, bool) {
	hdr := []byte(fmt.Sprintf("%d 0 obj", num))
	from := 0
	for {
		idx := bytesIndexAt(data, from, hdr)
		if idx < 0 {
			return 0, false
		}
		p := skipWS(data, idx+len(hdr))
		v, _, err := readInt(data, p)
		if err == nil {
			return v, true
		}
		from = idx + 1
		if from >= len(data) {
			return 0, false
		}
	}
}

// streamReader decodes an object's stream payload through its filter chain.
// The v0 engine supports no filter and /FlateDecode only.
func streamData(data []byte, obj *object) ([]byte, error) {
	if !obj.hasStream {
		return nil, fmt.Errorf("%w: object %d is not a stream", ErrUnsupported, obj.num)
	}
	dict, _ := obj.value.(Dict)
	filterRaw, has := dict[Name("Filter")]
	if !has {
		return obj.stream, nil
	}
	var filters []Name
	switch f := filterRaw.(type) {
	case Name:
		filters = []Name{f}
	case Array:
		for _, e := range f {
			n, ok := e.(Name)
			if !ok {
				return nil, fmt.Errorf("%w: object %d: /Filter entry not a name", ErrUnsupported, obj.num)
			}
			filters = append(filters, n)
		}
	}
	if _, hasParms := dict[Name("DecodeParms")]; hasParms {
		// Predictor parameters would change the decoded byte stream.
		return nil, fmt.Errorf("%w: object %d: /DecodeParms not supported", ErrUnsupported, obj.num)
	}
	out := obj.stream
	for _, f := range filters {
		switch f {
		case Name("FlateDecode"):
			zr, err := zlib.NewReader(bytes.NewReader(out))
			if err != nil {
				return nil, fmt.Errorf("%w: object %d: flate decode: %v", ErrUnsupported, obj.num, err)
			}
			decoded, err := io.ReadAll(zr)
			zr.Close()
			if err != nil {
				return nil, fmt.Errorf("%w: object %d: flate decode: %v", ErrUnsupported, obj.num, err)
			}
			out = decoded
		default:
			return nil, fmt.Errorf("%w: object %d: filter /%s not supported", ErrUnsupported, obj.num, f)
		}
	}
	return out, nil
}

// reflate re-encodes stream payload with /FlateDecode, replacing the object's
// stream bytes and filter entry. Returns the new payload.
func reflate(dict Dict, payload []byte) []byte {
	var zb bytes.Buffer
	zw := zlib.NewWriter(&zb)
	_, _ = zw.Write(payload)
	_ = zw.Close()
	dict[Name("Length")] = int64(zb.Len())
	dict[Name("Filter")] = Name("FlateDecode")
	return zb.Bytes()
}
