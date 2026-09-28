package live

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

// firstChar is the first Character (grapheme cluster) of s, "" when s is empty.
func firstChar(s string) string { return s[:transcript.NextGrapheme(s)] }

// dropChars drops the first n Characters of s.
func dropChars(s string, n int) string {
	for ; n > 0 && s != ""; n-- {
		s = s[transcript.NextGrapheme(s):]
	}
	return s
}

// dropLastChar drops the last Character of s.
func dropLastChar(s string) string {
	i := 0
	for i < len(s) {
		n := transcript.NextGrapheme(s[i:])
		if i+n >= len(s) {
			return s[:i]
		}
		i += n
	}
	return s
}

func charCount(s string) int { return transcript.GraphemeCount(s) }

func charPrefix(s string, n int) string {
	p, _ := transcript.GraphemePrefix(s, n)
	return p
}

// icu rewrites the ICU classes the Swift patterns use (`\s`, `\S`, `\d`) into their Unicode
// RE2 equivalents: RE2's own are ASCII only. None of the patterns use them inside brackets.
var icuClasses = strings.NewReplacer(
	`\s`, `[\t\n\f\r\p{Z}]`,
	`\S`, `[^\t\n\f\r\p{Z}]`,
	`\d`, `\p{Nd}`,
)

func icu(pattern string) *regexp.Regexp { return regexp.MustCompile(icuClasses.Replace(pattern)) }

// groups is the capture groups of the first match, or nil. Unmatched groups are "".
func groups(re *regexp.Regexp, text string) []string {
	m := re.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	return m[1:]
}

func strPtr(s string) *string { return &s }

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
