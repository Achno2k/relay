package transcript

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"relay/internal/api"
)

// Go-only: synthetic edge-case transcripts (testdata/parity/*.jsonl) parsed by the Swift
// bridge's TranscriptParser (captured in *.swift.json, with the steps `consume(line:)` reported)
// must parse the same here, message for message and byte for byte in every block.
func TestSwiftParserParity(t *testing.T) {
	for _, c := range []struct {
		name   string
		format Format
		cwd    string
	}{
		{"claude", FormatClaude, "/Users/dev/shop-api"},
		{"pi", FormatPi, "/Users/dev/website"},
		{"codex", FormatCodex, "/Users/dev/project"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var want struct {
				Messages []api.Message `json:"messages"`
				Steps    []string      `json:"steps"`
			}
			if err := json.Unmarshal(fixture(t, "parity/"+c.name+".swift.json"), &want); err != nil {
				t.Fatal(err)
			}
			p := NewParser(c.format, c.cwd, nil)
			var steps []string
			for _, l := range bytes.Split(fixture(t, "parity/"+c.name+".jsonl"), []byte("\n")) {
				if len(l) == 0 {
					continue
				}
				if m, ok := p.ConsumeLine(l); ok {
					steps = append(steps, m.ID+":"+itoa(len(m.Blocks)))
				}
			}
			expectEqual(t, steps, want.Steps)
			got := p.Messages()
			if len(got) != len(want.Messages) {
				t.Fatalf("got %d messages, want %d", len(got), len(want.Messages))
			}
			for i := range got {
				g, w := got[i], want.Messages[i]
				// A line without a usable timestamp gets "now" on both sides; the Swift side's "now"
				// was when the fixture was captured.
				if isNow(g.CreatedAt) {
					g.CreatedAt = w.CreatedAt
				}
				// Round 10 and 12 fields are Go-only (the Swift bridge is frozen).
				g.Blocks = append([]api.Block(nil), g.Blocks...)
				for j := range g.Blocks {
					g.Blocks[j].Path, g.Blocks[j].Edit, g.Blocks[j].Plan = "", nil, ""
					g.Blocks[j].Images = nil
				}
				expectEqual(t, g, w)
			}
		})
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func isNow(s string) bool {
	tm, ok := api.ParseTimestamp(s)
	return ok && time.Since(tm).Abs() < 24*time.Hour
}
