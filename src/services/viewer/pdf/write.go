package pdf

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
)

// outObj is one object scheduled for writing.
type outObj struct {
	num       int
	value     any
	stream    []byte
	hasStream bool
}

// writeSubset serialises exactly the objects in included (renumbered
// sequentially in ascending original order), applying page-tree /Kids trims
// from kidsOverride, and returns a standalone deterministic PDF.
func (d *Document) writeSubset(included map[int]bool, kidsOverride map[int][]Ref) ([]byte, error) {
	nums := make([]int, 0, len(included))
	for n := range included {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	renum := map[int]int{}
	plan := make([]outObj, 0, len(nums))
	for i, n := range nums {
		renum[n] = i + 1
	}
	for _, n := range nums {
		obj := d.objects[n]
		value := obj.value
		if kids, ok := kidsOverride[n]; ok {
			clone := cloneDict(value)
			clone[Name("Kids")] = toArray(kids)
			clone[Name("Count")] = int64(len(kids))
			value = clone
		}
		plan = append(plan, outObj{num: n, value: value, stream: obj.stream, hasStream: obj.hasStream})
	}
	return writePlan(plan, d.rootRef, renum)
}

// writeAll serialises the full document in place (identity numbering) after
// stamping has mutated object values.
func (d *Document) writeAll() ([]byte, error) {
	nums := make([]int, 0, len(d.objects))
	for n := range d.objects {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	plan := make([]outObj, 0, len(nums))
	for _, n := range nums {
		obj := d.objects[n]
		plan = append(plan, outObj{num: n, value: obj.value, stream: obj.stream, hasStream: obj.hasStream})
	}
	return writePlan(plan, d.rootRef, nil)
}

// writePlan emits header, objects, xref table and trailer for a plan. renum
// maps original object numbers to output numbers (nil = identity). Object
// values are serialised deterministically (sorted dictionary keys).
func writePlan(plan []outObj, root Ref, renum map[int]int) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := map[int]int{}
	bodyOffsets := make([]int, 0, len(plan))
	for _, o := range plan {
		target := o.num
		if renum != nil {
			target = renum[o.num]
		}
		offsets[target] = b.Len()
		bodyOffsets = append(bodyOffsets, target)
		fmt.Fprintf(&b, "%d 0 obj\n", target)
		value := o.value
		if o.hasStream {
			value = cloneDictWithLength(value, len(o.stream))
		}
		if err := serializeValue(&b, value, renum); err != nil {
			return nil, err
		}
		if o.hasStream {
			b.WriteString("\nstream\n")
			b.Write(o.stream)
			b.WriteString("\nendstream")
		}
		b.WriteString("\nendobj\n")
	}
	sort.Ints(bodyOffsets)

	size := 1
	if len(bodyOffsets) > 0 {
		size = bodyOffsets[len(bodyOffsets)-1] + 1
	}
	xrefOff := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n", size)
	b.WriteString("0000000000 65535 f \n")
	for n := 1; n < size; n++ {
		if off, ok := offsets[n]; ok {
			fmt.Fprintf(&b, "%010d 00000 n \n", off)
		} else {
			b.WriteString("0000000000 65535 f \n")
		}
	}
	rootNum := root.Num
	if renum != nil {
		ren, ok := renum[root.Num]
		if !ok {
			return nil, fmt.Errorf("pdf: catalog object %d missing from plan", root.Num)
		}
		rootNum = ren
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root %d 0 R >>\n", size, rootNum)
	fmt.Fprintf(&b, "startxref\n%d\n%%%%EOF\n", xrefOff)
	return b.Bytes(), nil
}

func toArray(refs []Ref) Array {
	arr := make(Array, 0, len(refs))
	for _, r := range refs {
		arr = append(arr, r)
	}
	return arr
}

func cloneDict(v any) Dict {
	src, ok := v.(Dict)
	if !ok {
		return nil
	}
	dst := Dict{}
	for k, e := range src {
		dst[k] = e
	}
	return dst
}

func cloneDictWithLength(v any, length int) any {
	dst := cloneDict(v)
	if dst == nil {
		return v
	}
	dst[Name("Length")] = int64(length)
	return dst
}

// serializeValue writes one PDF object value. Indirect references are
// renumbered through renum when it is non-nil.
func serializeValue(b *bytes.Buffer, v any, renum map[int]int) error {
	switch tv := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if tv {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int64:
		b.WriteString(strconv.FormatInt(tv, 10))
	case float64:
		b.WriteString(floatFmt(tv))
	case string:
		writeLiteralString(b, tv)
	case Name:
		writeName(b, tv)
	case Ref:
		num := tv.Num
		if renum != nil {
			ren, ok := renum[tv.Num]
			if !ok {
				return fmt.Errorf("pdf: dangling reference %d %d R in subset", tv.Num, tv.Gen)
			}
			num = ren
		}
		fmt.Fprintf(b, "%d %d R", num, tv.Gen)
	case Dict:
		b.WriteString("<<")
		keys := make([]string, 0, len(tv))
		for k := range tv {
			keys = append(keys, string(k))
		}
		sort.Strings(keys)
		for _, k := range keys {
			writeName(b, Name(k))
			b.WriteByte(' ')
			if err := serializeValue(b, tv[Name(k)], renum); err != nil {
				return err
			}
			b.WriteByte(' ')
		}
		b.WriteString(">>")
	case Array:
		b.WriteByte('[')
		for _, e := range tv {
			b.WriteByte(' ')
			if err := serializeValue(b, e, renum); err != nil {
				return err
			}
		}
		if len(tv) > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte(']')
	default:
		return fmt.Errorf("pdf: cannot serialize value of type %T", v)
	}
	return nil
}

// writeLiteralString writes a literal string with PDF escapes.
func writeLiteralString(b *bytes.Buffer, s string) {
	b.WriteByte('(')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '(', ')':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 || c > 0x7e {
				fmt.Fprintf(b, `\%03o`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte(')')
}

// writeName writes a name object with # escapes.
func writeName(b *bytes.Buffer, n Name) {
	b.WriteByte('/')
	for i := 0; i < len(n); i++ {
		c := n[i]
		switch {
		case c == '#' || c == '/' || c < 0x21 || c > 0x7e:
			fmt.Fprintf(b, "#%02X", c)
		default:
			b.WriteByte(c)
		}
	}
}
