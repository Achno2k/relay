package api

import (
	"encoding/json"
	"time"
)

// FormatTime is ISO 8601 in UTC with an explicit offset, e.g. `2026-09-23T13:04:01+00:00`.
func FormatTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05") + "+00:00"
}

var timeLayouts = []string{
	time.RFC3339Nano,                     // Z or +hh:mm, optional fraction
	"2006-01-02T15:04:05.999999999Z0700", // +hhmm
}

// ParseTimestamp parses ISO 8601 with or without fractional seconds.
func ParseTimestamp(s string) (time.Time, bool) {
	for _, l := range timeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// NormalizeTimestamp turns transcript timestamps (`...Z`, fractional seconds, or epoch millis)
// into FormatTime. ok is false for anything else.
func NormalizeTimestamp(raw any) (string, bool) {
	var ms float64
	switch v := raw.(type) {
	case string:
		t, ok := ParseTimestamp(v)
		if !ok {
			return "", false
		}
		return FormatTime(t), true
	case float64:
		ms = v
	case int:
		ms = float64(v)
	case int64:
		ms = float64(v)
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return "", false
		}
		ms = f
	default:
		return "", false
	}
	sec := ms / 1000
	whole := int64(sec)
	return FormatTime(time.Unix(whole, int64((sec-float64(whole))*1e9))), true
}
