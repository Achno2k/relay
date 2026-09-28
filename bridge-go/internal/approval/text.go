package approval

import (
	"regexp"
	"strings"
	"unicode"

	"relay/internal/transcript"
)

// Swift's screen parsers work on Characters and ICU regexes. These helpers keep the Go port
// cutting and matching in the same places.

// isWS is Foundation's CharacterSet.whitespaces: Unicode Zs plus tab.
func isWS(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) }

// trimWS is `trimmingCharacters(in: .whitespaces)`.
func trimWS(s string) string { return strings.TrimFunc(s, isWS) }

// isWSOrNewline is CharacterSet.whitespacesAndNewlines.
func isWSOrNewline(r rune) bool {
	return unicode.In(r, unicode.Z) || r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r' || r == 0x85
}

// chars splits s into Characters (grapheme clusters).
func chars(s string) []string {
	var out []string
	for s != "" {
		n := transcript.NextGrapheme(s)
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}

// dropFirstChar drops the first Character of s.
func dropFirstChar(s string) string { return s[transcript.NextGrapheme(s):] }

// icu rewrites the ICU classes the Swift patterns use (`\s`, `\S`, `\d`) into their Unicode
// RE2 equivalents: RE2's own are ASCII only. None of the patterns use `\s` inside brackets.
var icuClasses = strings.NewReplacer(
	`\s`, `[\t\n\f\r\p{Z}]`,
	`\S`, `[^\t\n\f\r\p{Z}]`,
	`\d`, `\p{Nd}`,
)

func icu(pattern string) *regexp.Regexp { return regexp.MustCompile(icuClasses.Replace(pattern)) }

// allIn: every rune of s is in set (true for "", like Swift's allSatisfy).
func allIn(s, set string) bool {
	for _, r := range s {
		if !strings.ContainsRune(set, r) {
			return false
		}
	}
	return true
}
