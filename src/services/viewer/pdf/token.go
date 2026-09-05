package pdf

import (
	"fmt"
	"strings"
)

// ---- low-level scanner helpers ----

func isWS(c byte) bool {
	return c == 0 || c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f'
}

func isDelim(c byte) bool {
	return c == '(' || c == ')' || c == '<' || c == '>' || c == '[' || c == ']' ||
		c == '{' || c == '}' || c == '/' || c == '%'
}

// skipWS advances over whitespace and %-comments.
func skipWS(data []byte, p int) int {
	for p < len(data) {
		if isWS(data[p]) {
			p++
			continue
		}
		if data[p] == '%' {
			for p < len(data) && data[p] != '\n' && data[p] != '\r' {
				p++
			}
			continue
		}
		break
	}
	return p
}

// skipOneEOL consumes at most one end-of-line marker (CRLF or LF or CR).
func skipOneEOL(data []byte, p int) int {
	if p < len(data) && data[p] == '\r' {
		if p+1 < len(data) && data[p+1] == '\n' {
			return p + 2
		}
		return p + 1
	}
	if p < len(data) && data[p] == '\n' {
		return p + 1
	}
	return p
}

// skipToEOL advances to just after the next EOL.
func skipToEOL(data []byte, p int) int {
	for p < len(data) && data[p] != '\n' && data[p] != '\r' {
		p++
	}
	return skipOneEOL(data, p)
}

// readKeyword reads a bare keyword token ([A-Za-z0-9]+).
func readKeyword(data []byte, p int) (string, int) {
	start := p
	for p < len(data) && !isWS(data[p]) && !isDelim(data[p]) {
		p++
	}
	return string(data[start:p]), p
}

// keywordAt reports whether the keyword kw starts exactly at p.
func keywordAt(data []byte, p int, kw string) bool {
	if p+len(kw) > len(data) {
		return false
	}
	if string(data[p:p+len(kw)]) != kw {
		return false
	}
	// Keyword must end at a delimiter or whitespace.
	if p+len(kw) < len(data) {
		c := data[p+len(kw)]
		if !isWS(c) && !isDelim(c) && !(c == 'R') {
			return false
		}
	}
	return true
}

// bytesIndexAt is a simple substring search from offset from.
func bytesIndexAt(data []byte, from int, needle []byte) int {
	if from < 0 {
		from = 0
	}
	for i := from; i+len(needle) <= len(data); i++ {
		match := true
		for j := range needle {
			if data[i+j] != needle[j] {
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

// readInt parses a decimal integer (optionally signed).
func readInt(data []byte, p int) (int64, int, error) {
	p = skipWS(data, p)
	neg := false
	if p < len(data) && (data[p] == '+' || data[p] == '-') {
		neg = data[p] == '-'
		p++
	}
	start := p
	var v int64
	for p < len(data) && data[p] >= '0' && data[p] <= '9' {
		v = v*10 + int64(data[p]-'0')
		p++
	}
	if p == start {
		return 0, p, fmt.Errorf("integer expected at offset %d", p)
	}
	if neg {
		v = -v
	}
	return v, p, nil
}

// readNumber parses a PDF number: integer when integral, float otherwise.
func readNumber(data []byte, p int) (any, int, error) {
	p = skipWS(data, p)
	neg := false
	if p < len(data) && (data[p] == '+' || data[p] == '-') {
		neg = data[p] == '-'
		p++
	}
	start := p
	isReal := false
	for p < len(data) {
		c := data[p]
		if c >= '0' && c <= '9' {
			p++
			continue
		}
		if c == '.' || c == 'e' || c == 'E' {
			isReal = true
			p++
			continue
		}
		if (c == '+' || c == '-') && p > start && (data[p-1] == 'e' || data[p-1] == 'E') {
			p++
			continue
		}
		break
	}
	if p == start {
		return nil, p, fmt.Errorf("number expected at offset %d", p)
	}
	s := string(data[start:p])
	if neg {
		s = "-" + s
	}
	if isReal {
		f := 0.0
		if _, err := fmt.Sscanf(s, "%g", &f); err != nil {
			return nil, p, fmt.Errorf("bad real %q at %d: %v", s, start, err)
		}
		return f, p, nil
	}
	v := int64(0)
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			return nil, p, fmt.Errorf("bad integer %q", s)
		}
		v = v*10 + int64(c-'0')
	}
	return v, p, nil
}

// parseValue parses one PDF object value at p. Indirect reference triples
// ("n g R") are recognised and returned as Ref.
func parseValue(data []byte, p int) (any, int, error) {
	p = skipWS(data, p)
	if p >= len(data) {
		return nil, p, fmt.Errorf("value expected at EOF")
	}
	c := data[p]
	switch {
	case c == '/':
		name, np, err := parseName(data, p)
		return name, np, err
	case c == '(':
		return parseLiteralString(data, p)
	case c == '<':
		if p+1 < len(data) && data[p+1] == '<' {
			return parseDict(data, p)
		}
		return parseHexString(data, p)
	case c == '[':
		return parseArray(data, p)
	case c == ']' || c == '>':
		return nil, p, fmt.Errorf("unexpected %q at %d", string(c), p)
	case c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.':
		return parseNumberOrRef(data, p)
	}
	// Keyword-like atom: true / false / null / R / stream / endobj ...
	kw, np := readKeyword(data, p)
	switch kw {
	case "true":
		return true, np, nil
	case "false":
		return false, np, nil
	case "null":
		return nil, np, nil
	case "R":
		return nil, p, fmt.Errorf("stray R at %d", p)
	}
	return nil, p, fmt.Errorf("unexpected token %q at %d", kw, p)
}

// parseNumberOrRef parses a number, and when followed by "g R" turns it into
// a Ref.
func parseNumberOrRef(data []byte, p int) (any, int, error) {
	v, np, err := readNumber(data, p)
	if err != nil {
		return nil, p, err
	}
	iv, isInt := v.(int64)
	if !isInt {
		return v, np, nil
	}
	// Lookahead: another integer then R?
	save := np
	g, gp, err := readInt(data, save)
	if err != nil {
		return v, np, nil
	}
	kw, kp := readKeyword(data, skipWS(data, gp))
	if kw != "R" {
		return v, np, nil
	}
	if iv < 0 || g < 0 {
		return nil, p, fmt.Errorf("invalid reference %d %d R", iv, g)
	}
	return Ref{Num: int(iv), Gen: int(g)}, kp, nil
}

// parseName parses a name object starting at the '/'.
func parseName(data []byte, p int) (Name, int, error) {
	p++ // consume '/'
	var sb strings.Builder
	for p < len(data) {
		c := data[p]
		if isWS(c) || isDelim(c) {
			break
		}
		if c == '#' && p+2 < len(data) {
			hi := hexVal(data[p+1])
			lo := hexVal(data[p+2])
			if hi < 0 || lo < 0 {
				return "", p, fmt.Errorf("bad # escape in name at %d", p)
			}
			sb.WriteByte(byte(hi<<4 | lo))
			p += 3
			continue
		}
		sb.WriteByte(c)
		p++
	}
	return Name(sb.String()), p, nil
}

// parseDict parses "<< ... >>" into a Dict.
func parseDict(data []byte, p int) (Dict, int, error) {
	p += 2 // consume '<<'
	dict := Dict{}
	for {
		p = skipWS(data, p)
		if p+1 < len(data) && data[p] == '>' && data[p+1] == '>' {
			return dict, p + 2, nil
		}
		if p >= len(data) {
			return nil, p, fmt.Errorf("unterminated dictionary")
		}
		if data[p] != '/' {
			// Some writers omit keys after a value; bail out with context.
			return nil, p, fmt.Errorf("dictionary key expected at %d", p)
		}
		key, np, err := parseName(data, p)
		if err != nil {
			return nil, np, err
		}
		p = np
		val, np2, err := parseValue(data, p)
		if err != nil {
			return nil, np2, err
		}
		p = np2
		dict[key] = val
	}
}

// parseArray parses "[ ... ]" into an Array.
func parseArray(data []byte, p int) (Array, int, error) {
	p++ // consume '['
	var arr Array
	for {
		p = skipWS(data, p)
		if p >= len(data) {
			return nil, p, fmt.Errorf("unterminated array")
		}
		if data[p] == ']' {
			return arr, p + 1, nil
		}
		v, np, err := parseValue(data, p)
		if err != nil {
			return nil, np, err
		}
		arr = append(arr, v)
		p = np
	}
}

// parseLiteralString parses "(...)" handling escapes and balanced parens.
func parseLiteralString(data []byte, p int) (string, int, error) {
	p++ // consume '('
	depth := 1
	var sb strings.Builder
	for p < len(data) {
		c := data[p]
		switch {
		case c == '(':
			depth++
			sb.WriteByte(c)
			p++
		case c == ')':
			depth--
			if depth == 0 {
				return sb.String(), p + 1, nil
			}
			sb.WriteByte(c)
			p++
		case c == '\\':
			if p+1 >= len(data) {
				return "", p, fmt.Errorf("unterminated string escape")
			}
			e := data[p+1]
			switch e {
			case 'n':
				sb.WriteByte('\n')
				p += 2
			case 'r':
				sb.WriteByte('\r')
				p += 2
			case 't':
				sb.WriteByte('\t')
				p += 2
			case 'b':
				sb.WriteByte('\b')
				p += 2
			case 'f':
				sb.WriteByte('\f')
				p += 2
			case '(', ')', '\\':
				sb.WriteByte(e)
				p += 2
			case '\r':
				if p+2 < len(data) && data[p+2] == '\n' {
					p += 3 // line continuation CRLF
				} else {
					p += 2
				}
			case '\n':
				p += 2
			default:
				if e >= '0' && e <= '7' {
					v := 0
					n := 0
					for n < 3 && p+1+n < len(data) && data[p+1+n] >= '0' && data[p+1+n] <= '7' {
						v = v*8 + int(data[p+1+n]-'0')
						n++
					}
					sb.WriteByte(byte(v))
					p += 1 + n
				} else {
					sb.WriteByte(e) // unknown escape: pass through
					p += 2
				}
			}
		default:
			sb.WriteByte(c)
			p++
		}
	}
	return "", p, fmt.Errorf("unterminated literal string")
}

// parseHexString parses "<...>" hexadecimal string data.
func parseHexString(data []byte, p int) (string, int, error) {
	p++ // consume '<'
	var sb strings.Builder
	hi := -1
	for p < len(data) {
		c := data[p]
		if c == '>' {
			if hi >= 0 {
				sb.WriteByte(byte(hi << 4))
			}
			return sb.String(), p + 1, nil
		}
		if isWS(c) {
			p++
			continue
		}
		v := hexVal(c)
		if v < 0 {
			return "", p, fmt.Errorf("bad hex digit %q", string(c))
		}
		if hi < 0 {
			hi = v
		} else {
			sb.WriteByte(byte(hi<<4 | v))
			hi = -1
		}
		p++
	}
	return "", p, fmt.Errorf("unterminated hex string")
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}
