package service

import (
	"encoding/json"
	"io"

	"relay/internal/api"
	"relay/internal/machine"
)

func testMachine() api.Machine { return machine.Current() }

func decodeJSON(r io.Reader, v any) error { return json.NewDecoder(r).Decode(v) }
