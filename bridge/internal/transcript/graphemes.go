package transcript

import (
	"unicode"
	"unicode/utf8"
)

// Swift counts and cuts strings by Character (extended grapheme cluster), so the
// truncation limits must too, or a preview with emoji or accents would cut in a different
// place than the Swift bridge did. This covers the rules that occur in agent output:
// CR LF, combining marks and other extenders, ZWJ emoji sequences, emoji modifiers,
// variation selectors, tags and regional-indicator pairs.

func isExtend(r rune) bool {
	switch {
	case r == 0x200D: // ZWJ
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF: // variation selectors
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // emoji modifiers
		return true
	case r >= 0xE0020 && r <= 0xE007F: // tags
		return true
	case r == 0x200C:
		return true
	}
	return unicode.In(r, unicode.Mn, unicode.Me, unicode.Mc)
}

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

func isPictographic(r rune) bool {
	if isRegionalIndicator(r) || r >= 0x1F3FB && r <= 0x1F3FF {
		return false
	}
	return r >= 0x1F000 && r <= 0x1FAFF || r >= 0x2600 && r <= 0x27BF || r >= 0x2300 && r <= 0x23FF ||
		r == 0x00A9 || r == 0x00AE || r >= 0x2190 && r <= 0x21FF || r >= 0x2B00 && r <= 0x2BFF
}

// NextGrapheme returns the byte length of the first grapheme cluster in s.
func NextGrapheme(s string) int {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return 0
	}
	if r == '\r' {
		if len(s) > 1 && s[1] == '\n' {
			return 2
		}
		return 1
	}
	if r == '\n' || r < 0x20 || r == 0x7F {
		return n
	}
	i := n
	// GB11: ExtPict Extend* ZWJ × ExtPict. 1 = after ExtPict Extend*, 2 = then a ZWJ.
	state := 0
	if isPictographic(r) {
		state = 1
	}
	if isRegionalIndicator(r) {
		if r2, n2 := utf8.DecodeRuneInString(s[i:]); isRegionalIndicator(r2) {
			i += n2
		}
	}
	for i < len(s) {
		r2, n2 := utf8.DecodeRuneInString(s[i:])
		switch {
		case r2 == 0x200D:
			if state == 1 {
				state = 2
			} else {
				state = 0
			}
		case isExtend(r2):
			if state != 1 {
				state = 0
			}
		case state == 2 && isPictographic(r2):
			state = 1
		default:
			return i
		}
		i += n2
	}
	return i
}

// GraphemeCount counts Swift Characters.
func GraphemeCount(s string) int {
	n := 0
	for len(s) > 0 {
		s = s[NextGrapheme(s):]
		n++
	}
	return n
}

// GraphemePrefix is the first n Characters of s, and whether s was longer.
func GraphemePrefix(s string, n int) (string, bool) {
	i := 0
	for k := 0; k < n && i < len(s); k++ {
		i += NextGrapheme(s[i:])
	}
	return s[:i], i < len(s)
}
