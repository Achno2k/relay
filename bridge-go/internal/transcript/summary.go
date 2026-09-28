package transcript

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// One-line human summaries for tool calls ("Edited api.md", "Ran swift test").

const (
	InputLimit   = 1000
	PreviewLimit = 400
)

func Summary(name string, input map[string]any, s Scrubber) string {
	str := func(keys ...string) (string, bool) {
		for _, k := range keys {
			if v, ok := input[k].(string); ok && v != "" {
				return v, true
			}
		}
		return "", false
	}
	path := func(keys ...string) (string, bool) {
		v, ok := str(keys...)
		if !ok {
			return "", false
		}
		return s.Scrub(v), true
	}
	or := func(prefix string, v string, ok bool, fallback string) string {
		if ok {
			return prefix + v
		}
		return fallback
	}
	var raw string
	switch strings.ToLower(name) {
	case "edit", "multiedit":
		v, ok := path("file_path", "path")
		raw = or("Edited ", v, ok, "Edited a file")
	case "write":
		v, ok := path("file_path", "path")
		raw = or("Wrote ", v, ok, "Wrote a file")
	case "read":
		v, ok := path("file_path", "path")
		raw = or("Read ", v, ok, "Read a file")
	case "notebookedit":
		v, ok := path("notebook_path")
		raw = or("Edited ", v, ok, "Edited a notebook")
	case "bash":
		v, ok := str("command")
		raw = or("Ran ", FirstLine(v), ok, "Ran a command")
	case "grep":
		v, ok := str("pattern")
		raw = or("Searched for ", v, ok, "Searched")
	case "glob", "find", "ls":
		v, ok := str("pattern")
		if !ok {
			v, ok = path("path")
		}
		raw = or("Listed ", v, ok, "Listed files")
	case "webfetch":
		v, ok := str("url")
		raw = or("Fetched ", v, ok, "Fetched a page")
	case "websearch":
		v, ok := str("query")
		raw = or("Searched the web for ", v, ok, "Searched the web")
	case "task", "agent":
		v, ok := str("description")
		raw = or("Ran agent: ", v, ok, "Ran an agent")
	case "todowrite":
		raw = "Updated todos"
	case "skill":
		v, ok := str("skill", "command")
		raw = or("Used skill ", v, ok, "Used a skill")
	case "toolsearch":
		raw = "Loaded tools"
	default:
		raw = name
	}
	return Truncate(s.Scrub(raw), 120)
}

// InputString is the tool input as sorted-key JSON (like Foundation's JSONSerialization with
// .sortedKeys and .withoutEscapingSlashes), scrubbed and truncated. Anything but an object or
// array is "{}".
func InputString(input any, s Scrubber) string {
	switch input.(type) {
	case map[string]any, []any, []string:
	default:
		return "{}"
	}
	var b strings.Builder
	if !writeJSON(&b, input) {
		return "{}"
	}
	return Truncate(s.Scrub(b.String()), InputLimit)
}

func Preview(text string, s Scrubber) string {
	return Truncate(s.Scrub(text), PreviewLimit)
}

// FirstLine is the first non-empty line, trimmed of spaces and tabs. Like Swift, a CR LF pair
// is one character and doesn't end a line.
func FirstLine(s string) string {
	line := s
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			if i > start {
				line = s[start:i]
				break
			}
			start = i + 1
		}
	}
	return strings.TrimFunc(line, func(r rune) bool { return r == '\t' || unicode.Is(unicode.Zs, r) })
}

// Truncate cuts s to n Characters plus "…".
func Truncate(s string, n int) string {
	p, cut := GraphemePrefix(s, n)
	if !cut {
		return s
	}
	return p + "…"
}

func writeJSON(b *strings.Builder, v any) bool {
	switch v := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(v))
	case string:
		writeJSONString(b, v)
	case json.Number:
		writeJSONNumber(b, v)
	case float64:
		writeJSONNumber(b, json.Number(strconv.FormatFloat(v, 'g', -1, 64)))
	case int:
		b.WriteString(strconv.Itoa(v))
	case []string:
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, e)
		}
		b.WriteByte(']')
	case []any:
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			if !writeJSON(b, e) {
				return false
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return collateLess(keys[i], keys[j]) })
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSONString(b, k)
			b.WriteByte(':')
			if !writeJSON(b, v[k]) {
				return false
			}
		}
		b.WriteByte('}')
	default:
		return false
	}
	return true
}

// Integers print as is; other numbers as C's %.17g, which is what Foundation writes.
func writeJSONNumber(b *strings.Builder, n json.Number) {
	if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		b.WriteString(strconv.FormatInt(i, 10))
		return
	}
	if !strings.ContainsAny(string(n), ".eE") {
		// An integer past Int64 is an NSDecimalNumber, written digit for digit.
		b.WriteString(string(n))
		return
	}
	f, err := n.Float64()
	if err != nil {
		b.WriteString("0")
		return
	}
	if f == 0 && math.Signbit(f) {
		b.WriteString("-0")
		return
	}
	if f == float64(int64(f)) && f > -1e18 && f < 1e18 {
		b.WriteString(strconv.FormatInt(int64(f), 10))
		return
	}
	b.WriteString(formatG17(f))
}

// formatG17 matches C's printf("%.17g").
func formatG17(f float64) string {
	s := strconv.FormatFloat(f, 'e', 16, 64) // d.dddddddddddddddde±XX
	mant, exp, _ := strings.Cut(s, "e")
	x, _ := strconv.Atoi(exp)
	if x < -4 || x >= 17 {
		mant = trimZeros(mant)
		sign := "+"
		if x < 0 {
			sign, x = "-", -x
		}
		e := strconv.Itoa(x)
		if len(e) < 2 {
			e = "0" + e
		}
		return mant + "e" + sign + e
	}
	return trimZeros(strconv.FormatFloat(f, 'f', 16-x, 64))
}

func trimZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

func writeJSONString(b *strings.Builder, s string) {
	const hex = "0123456789abcdef"
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
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
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hex[r>>4])
				b.WriteByte(hex[r&0xF])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
