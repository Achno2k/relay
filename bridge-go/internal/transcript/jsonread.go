package transcript

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// readJSONObject parses one JSONL line the way Foundation's JSONSerialization does, so the Go
// bridge keeps and drops the same lines as the Swift one:
//   - the first of duplicate keys wins;
//   - trailing commas in objects and arrays are fine, and so is a leading UTF-8 BOM;
//   - invalid UTF-8, lone surrogate escapes, raw control characters in strings and numbers out
//     of Double's range reject the line.
//
// Values are map[string]any, []any, string, json.Number, bool and nil. Only an object is
// accepted at the top level.
func readJSONObject(data []byte) (map[string]any, bool) {
	if !utf8.Valid(data) {
		return nil, false
	}
	r := jsonReader{s: strings.TrimPrefix(string(data), "\uFEFF")}
	r.space()
	if r.i >= len(r.s) || r.s[r.i] != '{' {
		return nil, false
	}
	v, ok := r.value(0)
	if !ok {
		return nil, false
	}
	r.space()
	if r.i != len(r.s) {
		return nil, false
	}
	return v.(map[string]any), true
}

type jsonReader struct {
	s string
	i int
}

const maxJSONDepth = 512

func (r *jsonReader) space() {
	for r.i < len(r.s) {
		switch r.s[r.i] {
		case ' ', '\t', '\n', '\r':
			r.i++
		default:
			return
		}
	}
}

func (r *jsonReader) value(depth int) (any, bool) {
	if depth > maxJSONDepth {
		return nil, false
	}
	r.space()
	if r.i >= len(r.s) {
		return nil, false
	}
	switch c := r.s[r.i]; {
	case c == '{':
		r.i++
		m := map[string]any{}
		for {
			r.space()
			if r.i < len(r.s) && r.s[r.i] == '}' {
				r.i++
				return m, true
			}
			if r.i >= len(r.s) || r.s[r.i] != '"' {
				return nil, false
			}
			k, ok := r.str()
			if !ok {
				return nil, false
			}
			r.space()
			if r.i >= len(r.s) || r.s[r.i] != ':' {
				return nil, false
			}
			r.i++
			v, ok := r.value(depth + 1)
			if !ok {
				return nil, false
			}
			if _, dup := m[k]; !dup {
				m[k] = v
			}
			r.space()
			if r.i >= len(r.s) {
				return nil, false
			}
			switch r.s[r.i] {
			case ',':
				r.i++
			case '}':
				r.i++
				return m, true
			default:
				return nil, false
			}
		}
	case c == '[':
		r.i++
		a := []any{}
		for {
			r.space()
			if r.i < len(r.s) && r.s[r.i] == ']' {
				r.i++
				return a, true
			}
			v, ok := r.value(depth + 1)
			if !ok {
				return nil, false
			}
			a = append(a, v)
			r.space()
			if r.i >= len(r.s) {
				return nil, false
			}
			switch r.s[r.i] {
			case ',':
				r.i++
			case ']':
				r.i++
				return a, true
			default:
				return nil, false
			}
		}
	case c == '"':
		return r.str()
	case c == 't':
		return true, r.literal("true")
	case c == 'f':
		return false, r.literal("false")
	case c == 'n':
		return nil, r.literal("null")
	case c == '-' || c >= '0' && c <= '9':
		return r.number()
	}
	return nil, false
}

func (r *jsonReader) literal(lit string) bool {
	if !strings.HasPrefix(r.s[r.i:], lit) {
		return false
	}
	r.i += len(lit)
	return true
}

func (r *jsonReader) number() (any, bool) {
	start := r.i
	digits := func() int {
		n := 0
		for r.i < len(r.s) && r.s[r.i] >= '0' && r.s[r.i] <= '9' {
			r.i++
			n++
		}
		return n
	}
	if r.s[r.i] == '-' {
		r.i++
	}
	intStart := r.i
	if n := digits(); n == 0 || n > 1 && r.s[intStart] == '0' {
		return nil, false
	}
	if r.i < len(r.s) && r.s[r.i] == '.' {
		r.i++
		if digits() == 0 {
			return nil, false
		}
	}
	if r.i < len(r.s) && (r.s[r.i] == 'e' || r.s[r.i] == 'E') {
		r.i++
		if r.i < len(r.s) && (r.s[r.i] == '+' || r.s[r.i] == '-') {
			r.i++
		}
		if digits() == 0 {
			return nil, false
		}
	}
	lit := r.s[start:r.i]
	if f, err := strconv.ParseFloat(lit, 64); err != nil || math.IsInf(f, 0) {
		// Integers too big for a Double's exponent don't exist; anything else out of range is NaN
		// to Foundation, which rejects the line.
		if _, ierr := strconv.ParseInt(lit, 10, 64); ierr != nil {
			return nil, false
		}
	}
	return json.Number(lit), true
}

func (r *jsonReader) str() (string, bool) {
	r.i++ // opening quote
	var b strings.Builder
	start := r.i
	for r.i < len(r.s) {
		c := r.s[r.i]
		switch {
		case c == '"':
			b.WriteString(r.s[start:r.i])
			r.i++
			return b.String(), true
		case c < 0x20:
			return "", false
		case c == '\\':
			b.WriteString(r.s[start:r.i])
			r.i++
			if r.i >= len(r.s) {
				return "", false
			}
			e := r.s[r.i]
			r.i++
			switch e {
			case '"', '\\', '/':
				b.WriteByte(e)
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case 'u':
				u, ok := r.hex4()
				if !ok {
					return "", false
				}
				switch {
				case utf16.IsSurrogate(rune(u)):
					if u >= 0xDC00 || !strings.HasPrefix(r.s[r.i:], `\u`) {
						return "", false
					}
					r.i += 2
					lo, ok := r.hex4()
					if !ok || lo < 0xDC00 || lo > 0xDFFF {
						return "", false
					}
					b.WriteRune(utf16.DecodeRune(rune(u), rune(lo)))
				default:
					b.WriteRune(rune(u))
				}
			default:
				return "", false
			}
			start = r.i
		default:
			r.i++
		}
	}
	return "", false
}

func (r *jsonReader) hex4() (uint16, bool) {
	if r.i+4 > len(r.s) {
		return 0, false
	}
	v, err := strconv.ParseUint(r.s[r.i:r.i+4], 16, 16)
	if err != nil {
		return 0, false
	}
	r.i += 4
	return uint16(v), true
}
