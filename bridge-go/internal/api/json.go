// Package api holds the wire types from docs/api.md and their exact JSON shapes.
//
// Shape rules copied from the Swift bridge's Codable output:
//   - optional fields that Swift synthesized are omitted when nil (`omitzero`);
//   - fields Swift encodes explicitly (Agent's optionals, reply.live's text/tool) are `null`;
//   - non-optional arrays are `[]`, never `null`;
//   - `<`, `>` and `&` are not HTML-escaped and `/` is not escaped.
package api

import (
	"bytes"
	"encoding/json"
)

// Version is what /health reports.
const Version = "0.1.0"

// Marshal encodes v like the Swift bridge's JSONEncoder: no HTML escaping, no trailing newline.
// Use it (not json.Marshal) for every response body and WS frame.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// nonNil turns a nil slice into an empty one so it encodes as `[]`.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
