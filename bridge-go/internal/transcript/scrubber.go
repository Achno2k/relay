package transcript

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Scrubber keeps full filesystem paths off the wire.
// Paths under the cwd become cwd-relative; any other absolute path keeps only its last component.
// The zero value has no cwd.
type Scrubber struct {
	cwd string
}

func NewScrubber(cwd string) Scrubber {
	if utf8.RuneCountInString(cwd) <= 1 {
		return Scrubber{}
	}
	return Scrubber{cwd: strings.TrimSuffix(cwd, "/")}
}

// Cwd is the cwd paths are made relative to, "" for none.
func (s Scrubber) Cwd() string { return s.cwd }

// Characters that end a path component. ICU's `\s` is [\t\n\f\r\p{Z}].
const stopClass = "\\t\\n\\f\\r\\p{Z}/\"'`<>|;,()\\[\\]{}\\\\"

// An absolute (or ~-relative) path with at least two components, anchored. RE2 has no
// lookbehind, so `(?<![A-Za-z0-9_.\-~/:@])` is checked by hand in Scrub.
var absolutePath = regexp.MustCompile(`^~?/(?:[^` + stopClass + `]+/)+[^` + stopClass + `]*`)

func isStop(r rune) bool {
	switch r {
	case '\t', '\n', '\f', '\r', '/', '"', '\'', '`', '<', '>', '|', ';', ',', '(', ')', '[', ']', '{', '}', '\\':
		return true
	}
	return unicode.In(r, unicode.Z)
}

// A character that makes a leading `/` part of a URL, a relative path or a word.
func blocksPath(r rune) bool {
	return isWordChar(r) || r == '~' || r == '/' || r == ':' || r == '@'
}

// [A-Za-z0-9_.\-]
func isWordChar(r rune) bool {
	return r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-')
}

func (s Scrubber) Scrub(text string) string {
	if s.cwd != "" && strings.Contains(text, s.cwd) {
		// `<cwd>/x` -> `x`, then bare `<cwd>` -> `.`
		text = replaceCwd(text, s.cwd+"/", "", func(next rune, ok bool) bool { return ok && !isStop(next) })
		text = replaceCwd(text, s.cwd, ".", func(next rune, ok bool) bool { return !ok || !isWordChar(next) })
	}
	return scrubAbsolute(text)
}

// replaceCwd replaces each occurrence of lit whose following character passes keep,
// scanning left to right like a regex with a lookahead.
func replaceCwd(s, lit, with string, keep func(next rune, ok bool) bool) string {
	var b strings.Builder
	i, last := 0, 0
	for {
		j := strings.Index(s[i:], lit)
		if j < 0 {
			break
		}
		start := i + j
		end := start + len(lit)
		next, size := utf8.DecodeRuneInString(s[end:])
		if keep(next, size > 0) {
			b.WriteString(s[last:start])
			b.WriteString(with)
			last, i = end, end
			continue
		}
		_, size = utf8.DecodeRuneInString(s[start:])
		i = start + size
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

func scrubAbsolute(s string) string {
	if !strings.ContainsRune(s, '/') {
		return s
	}
	var b strings.Builder
	last := 0
	prev := rune(-1)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == '/' || r == '~') && (prev < 0 || !blocksPath(prev)) {
			if m := absolutePath.FindString(s[i:]); m != "" {
				b.WriteString(s[last:i])
				b.WriteString(LastComponent(m))
				i += len(m)
				last = i
				prev, _ = utf8.DecodeLastRuneInString(m)
				continue
			}
		}
		prev = r
		i += size
	}
	if last == 0 {
		return s
	}
	b.WriteString(s[last:])
	return b.String()
}

// LastComponent is the last non-empty `/` component of path, or path itself when it has none.
func LastComponent(path string) string {
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			return parts[i]
		}
	}
	return path
}

// CwdName is the last component of a cwd; never the full path.
func CwdName(cwd string) string {
	if cwd == "" {
		return ""
	}
	return LastComponent(cwd)
}
