package herdr

import (
	"strconv"
	"strings"
)

// Key-name validation for POST /agents/:id/keys (api.md: "Key names are herdr's (`esc`,
// `enter`, `up`, `down`, `ctrl+u`, digits, …)"), so a malformed client can't type garbage
// into a live pane.

var namedKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range []string{
		"esc", "escape", "enter", "return", "tab", "backtab", "space",
		"up", "down", "left", "right",
		"backspace", "delete", "insert", "home", "end", "pageup", "pagedown",
	} {
		m[k] = true
	}
	for i := 1; i <= 24; i++ {
		m["f"+strconv.Itoa(i)] = true
	}
	return m
}()

var modifiers = map[string]bool{"ctrl": true, "shift": true, "alt": true, "cmd": true, "option": true, "meta": true}

func printableASCII(s string) bool {
	if len(s) != 1 {
		return false
	}
	return s[0] >= 0x20 && s[0] < 0x7f
}

// IsValidKey: a named key, a single printable ASCII character, a 1–2 digit menu row, or
// `modifier+key` (`ctrl+u`, `shift+tab`).
func IsValidKey(key string) bool {
	if key == "" || len(key) > 32 {
		return false
	}
	lower := strings.ToLower(key)
	if namedKeys[lower] || printableASCII(key) {
		return true
	}
	// A numbered menu row (the approval parser's `\d{1,2}` options).
	if len(key) <= 2 && allDigits(key) {
		return true
	}
	parts := strings.Split(lower, "+")
	if len(parts) != 2 || !modifiers[parts[0]] || parts[1] == "" {
		return false
	}
	return namedKeys[parts[1]] || printableASCII(parts[1])
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// FirstInvalidKey returns the first key that isn't valid.
func FirstInvalidKey(keys []string) (string, bool) {
	for _, k := range keys {
		if !IsValidKey(k) {
			return k, true
		}
	}
	return "", false
}
