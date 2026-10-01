package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The contract guard: every docs/fixtures file decodes into the api types and re-encodes to the
// same structure. Fixtures are read from docs/ directly (not copied) so they can't drift.
const fixtures = "../../../docs/fixtures"

// derived keys the bridge always sends even when an older fixture leaves them out.
var derived = map[string]bool{"transcriptState": true}

func fixtureTypes() map[string]func() any {
	return map[string]func() any{
		"agent-controls-claude.json": func() any { return new(AgentControls) },
		"agent-controls-codex.json":  func() any { return new(AgentControls) },
		"agent-controls-pi.json":     func() any { return new(AgentControls) },
		"agents-multi.json":          func() any { return new([]Agent) },
		"agents.json":                func() any { return new([]Agent) },
		"approval-plan.json":         func() any { return new(Approval) },
		"approval.json":              func() any { return new(Approval) },
		"attachment.json":            func() any { return new(Attachment) },
		"controls.json":              func() any { return new(Controls) },
		"file.json":                  func() any { return new(FileContent) },
		"machine.json":               func() any { return new(Machine) },
		"messages-attachments.json":  func() any { return new(MessagePage) },
		"messages-edits.json":        func() any { return new(MessagePage) },
		"messages.json":              func() any { return new(MessagePage) },
		"usage.json":                 func() any { return new(UsageSnapshot) },
		"workspaces.json":            func() any { return new([]Workspace) },
		"ws-events.jsonl":            nil, // line by line below
	}
}

func TestEveryFixtureIsCovered(t *testing.T) {
	entries, err := os.ReadDir(fixtures)
	if err != nil {
		t.Fatal(err)
	}
	types := fixtureTypes()
	for _, e := range entries {
		if _, ok := types[e.Name()]; !ok {
			t.Errorf("fixture %s has no api type in fixtures_test.go", e.Name())
		}
	}
}

func TestFixturesRoundTrip(t *testing.T) {
	for name, mk := range fixtureTypes() {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(fixtures, name))
			if err != nil {
				t.Fatal(err)
			}
			if mk == nil {
				sc := bufio.NewScanner(bytes.NewReader(data))
				sc.Buffer(nil, 1<<20)
				for sc.Scan() {
					if strings.TrimSpace(sc.Text()) != "" {
						roundTrip(t, sc.Bytes(), new(ServerEvent))
					}
				}
				return
			}
			roundTrip(t, data, mk())
		})
	}
}

func roundTrip(t *testing.T, data []byte, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("decode: %v\n%s", err, data)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	json.Unmarshal(data, &want)
	json.Unmarshal(out, &got)
	for _, d := range diff("$", want, got) {
		t.Error(d)
	}
}

// diff compares fixture (want) and our output (got). A key the fixture has as null may be null
// or missing in got (Swift omits nil optionals it synthesized, and writes null where it encodes
// them explicitly); got may add null keys and the derived keys. Numbers compare as numbers.
func diff(path string, want, got any) []string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return []string{path + ": want object, got " + typeName(got)}
		}
		var out []string
		for k, wv := range w {
			gv, has := g[k]
			if wv == nil {
				if has && gv != nil {
					out = append(out, path+"."+k+": want null/missing, got "+typeName(gv))
				}
				continue
			}
			if !has {
				out = append(out, path+"."+k+": missing")
				continue
			}
			out = append(out, diff(path+"."+k, wv, gv)...)
		}
		for k, gv := range g {
			if _, has := w[k]; !has && gv != nil && !derived[k] {
				out = append(out, path+"."+k+": unexpected key")
			}
		}
		return out
	case []any:
		g, ok := got.([]any)
		if !ok {
			return []string{path + ": want array, got " + typeName(got)}
		}
		if len(g) != len(w) {
			return []string{path + ": length differs"}
		}
		var out []string
		for i := range w {
			out = append(out, diff(path+"["+itoa(i)+"]", w[i], g[i])...)
		}
		return out
	}
	if !reflect.DeepEqual(want, got) {
		return []string{path + ": value differs"}
	}
	return nil
}

func typeName(v any) string {
	if v == nil {
		return "null"
	}
	return reflect.TypeOf(v).String()
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
