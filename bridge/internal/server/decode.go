package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"relay/internal/api"
)

// fields maps exact JSON keys to pointers to decode them into (each a pointer to a pointer, so
// absent and null stay nil).
type fields map[string]any

var errInvalidJSON = api.BadRequest("invalid JSON body")

// decodeBody reads a JSON object body the way the Swift bridge's Decodable did: at most
// MaxBodyBytes (413 too_large), case-sensitive keys, unknown keys ignored, and every `required`
// key present and not null. Anything else is 400 "invalid JSON body".
func decodeBody(w http.ResponseWriter, r *http.Request, into fields, required ...string) error {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return api.NewError(http.StatusRequestEntityTooLarge, "too_large",
				"request body is limited to "+strconv.Itoa(MaxBodyBytes)+" bytes")
		}
		return errInvalidJSON
	}
	return decodeObject(data, into, required...)
}

func decodeObject(data []byte, into fields, required ...string) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil || obj == nil {
		return errInvalidJSON
	}
	for key, dst := range into {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return errInvalidJSON
		}
	}
	for _, key := range required {
		raw, ok := obj[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errInvalidJSON
		}
	}
	return nil
}
