package transcript

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Foundation's JSONSerialization `.sortedKeys` on macOS orders keys with
// `localizedStandardCompare` (ICU root collation, numeric): punctuation before digits before
// letters, digit runs by value, letters case-insensitively with lowercase first on a tie
// (`a` < `A` < `b`), accents after the plain letter. Tool inputs are sorted this way in
// `toolCall.input`, so the Go bridge must too (`-A`/`-n` Grep flags, camelCase MCP args).
//
// This covers ASCII exactly (checked against Swift) plus Latin-1 accented letters; other
// characters fall back to code point order within their class.

// ICU root order of ASCII space and punctuation.
const punctOrder = " _-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~"

type collElem struct {
	class   int    // 0 punct, 1 other symbol, 2 `$`, 3 digits, 4 letter, 5 other letter
	primary string // punct/letter weight, or the digit run without leading zeros
	second  int    // accent
	third   int    // case: 0 lower, 1 upper
}

// Latin-1 letters as base letter + accent rank.
var latinFold = map[rune]struct {
	base   string
	accent int
}{}

func init() {
	add := func(chars, base string) {
		for i, r := range []rune(chars) {
			latinFold[r] = struct {
				base   string
				accent int
			}{base, i + 1}
		}
	}
	add("áàâäãåā", "a")
	add("ÁÀÂÄÃÅĀ", "a")
	add("çć", "c")
	add("ÇĆ", "c")
	add("éèêëē", "e")
	add("ÉÈÊËĒ", "e")
	add("íìîïī", "i")
	add("ÍÌÎÏĪ", "i")
	add("ñń", "n")
	add("ÑŃ", "n")
	add("óòôöõøō", "o")
	add("ÓÒÔÖÕØŌ", "o")
	add("úùûüū", "u")
	add("ÚÙÛÜŪ", "u")
	add("ýÿ", "y")
	add("ÝŸ", "y")
	latinFold['ß'] = struct {
		base   string
		accent int
	}{"ss", 1}
	latinFold['ﬁ'] = struct {
		base   string
		accent int
	}{"fi", 1}
}

func collElems(s string) []collElem {
	var out []collElem
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r >= '0' && r <= '9':
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			digits := strings.TrimLeft(s[i:j], "0")
			if digits == "" {
				digits = "0"
			}
			out = append(out, collElem{class: 3, primary: digits})
			i = j
			continue
		case r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			upper := 0
			if r <= 'Z' {
				upper = 1
			}
			out = append(out, collElem{class: 4, primary: string(unicode.ToLower(r)), third: upper})
		case r == '$':
			out = append(out, collElem{class: 2})
		case r < utf8.RuneSelf:
			k := strings.IndexRune(punctOrder, r)
			if k < 0 {
				k = int(r) - 0x80 // controls: before everything
			}
			out = append(out, collElem{class: 0, primary: string(rune(0x100 + k))})
		default:
			if f, ok := latinFold[r]; ok {
				upper := 0
				if unicode.IsUpper(r) {
					upper = 1
				}
				for k, b := range f.base {
					e := collElem{class: 4, primary: string(b), third: upper}
					if k == 0 {
						e.second = f.accent
					}
					out = append(out, e)
				}
			} else if unicode.IsLetter(r) {
				out = append(out, collElem{class: 5, primary: string(unicode.ToLower(r)), third: boolInt(unicode.IsUpper(r))})
			} else {
				out = append(out, collElem{class: 1, primary: string(r)})
			}
		}
		i += n
	}
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func comparePrimary(a, b collElem) int {
	if a.class != b.class {
		return a.class - b.class
	}
	if a.class == 3 && len(a.primary) != len(b.primary) {
		return len(a.primary) - len(b.primary)
	}
	return strings.Compare(a.primary, b.primary)
}

// collateLess is `a.localizedStandardCompare(b) == .orderedAscending`.
func collateLess(a, b string) bool {
	ea, eb := collElems(a), collElems(b)
	levels := []func(x, y collElem) int{
		comparePrimary,
		func(x, y collElem) int { return x.second - y.second },
		func(x, y collElem) int { return x.third - y.third },
	}
	for _, cmp := range levels {
		for i := 0; i < len(ea) && i < len(eb); i++ {
			if c := cmp(ea[i], eb[i]); c != 0 {
				return c < 0
			}
		}
		if len(ea) != len(eb) {
			return len(ea) < len(eb)
		}
	}
	return a < b
}
