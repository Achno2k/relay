package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"relay/internal/api"
)

// Ported from TranscriptParserTests.swift.

const claudeCwd = "/Users/dev/shop-api"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func claudeMessages(t *testing.T) []api.Message {
	return Parse(fixture(t, "claude-session.jsonl"), FormatClaude, claudeCwd, nil)
}

func roles(ms []api.Message) []api.Role {
	out := make([]api.Role, len(ms))
	for i, m := range ms {
		out[i] = m.Role
	}
	return out
}

func ids(ms []api.Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

func expectEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("\n got: %#v\nwant: %#v", got, want)
	}
}

func TestTranscriptParser_keepsOnlyRealUserAndAssistantMessages(t *testing.T) {
	ms := claudeMessages(t)
	expectEqual(t, roles(ms), []api.Role{api.RoleUser, api.RoleAssistant, api.RoleUser, api.RoleAssistant})
	expectEqual(t, ids(ms), []string{"u1", "a1", "u2", "a6"})
}

func TestTranscriptParser_dropsMetaCommandsRemindersAndSidechains(t *testing.T) {
	var texts []string
	for _, m := range claudeMessages(t) {
		for _, b := range m.Blocks {
			if b.Type == api.BlockText {
				texts = append(texts, b.Text)
			}
		}
	}
	text := strings.Join(texts, "\n")
	for _, s := range []string{"meta line", "/model", "Set model", "ignore", "sidechain"} {
		if strings.Contains(text, s) {
			t.Errorf("text contains %q", s)
		}
	}
}

func TestTranscriptParser_mergesAssistantLinesAndAttachesToolResults(t *testing.T) {
	a := claudeMessages(t)[1]
	expectEqual(t, a.CreatedAt, "2026-09-23T13:00:04+00:00")
	expectEqual(t, a.Blocks, []api.Block{
		api.ThinkingBlock("Need to find where the token is read first."),
		api.ToolCallBlock("t1", "Grep", "Searched for read_token", `{"path":"src","pattern":"read_token"}`),
		api.ToolResultBlock("t1", false, "src/auth.py:14: def read_token():\nsrc/auth.py:31:     tok = read_token()"),
		api.ToolCallBlock("t2", "Edit", "Edited src/auth.py", `{"file_path":"src/auth.py","new_string":"b","old_string":"a"}`),
		api.ToolResultBlock("t2", false, "The file src/auth.py has been updated."),
		api.ToolCallBlock("t3", "Bash", "Ran uv run pytest -q", `{"command":"uv run pytest -q\necho done","description":"Run tests"}`),
		api.ToolResultBlock("t3", true, "1 failed, 41 passed in tmp"),
		api.TextBlock("Cached the token in `src/auth.py`. One test failed. See https://docs.python.org/3/library/functools.html for details."),
	})
}

func TestTranscriptParser_scrubsUserTextPaths(t *testing.T) {
	expectEqual(t, claudeMessages(t)[0].Blocks, []api.Block{
		api.TextBlock("The auth middleware in src/auth.py re-reads the token file on every request. Cache it."),
	})
}

func TestTranscriptParser_incrementalConsumeReportsGrowingMessage(t *testing.T) {
	p := NewParser(FormatClaude, claudeCwd, nil)
	var gotIDs []string
	var counts []int
	for _, l := range bytes.Split(fixture(t, "claude-session.jsonl"), []byte("\n")) {
		if len(l) == 0 {
			continue
		}
		if m, ok := p.ConsumeLine(l); ok {
			gotIDs = append(gotIDs, m.ID)
			counts = append(counts, len(m.Blocks))
		}
	}
	expectEqual(t, gotIDs, []string{"u1", "a1", "a1", "a1", "a1", "a1", "a1", "a1", "a1", "u2", "a6"})
	expectEqual(t, counts, []int{1, 1, 2, 3, 4, 5, 6, 7, 8, 1, 1})
	expectEqual(t, p.Messages(), claudeMessages(t))
}

func TestTranscriptParser_orphanToolResultIsDropped(t *testing.T) {
	line := `{"type":"user","isSidechain":false,"uuid":"x","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t","content":"hi"}]}}`
	p := NewParser(FormatClaude, "", nil)
	if _, ok := p.ConsumeLine([]byte(line)); ok {
		t.Error("expected no message")
	}
	if len(p.Messages()) != 0 {
		t.Error("expected no messages")
	}
}

func TestTranscriptParser_ignoresGarbageAndPartialLines(t *testing.T) {
	p := NewParser(FormatClaude, "", nil)
	for _, l := range []string{`{"type":"user","mess`, "not json"} {
		if _, ok := p.ConsumeLine([]byte(l)); ok {
			t.Errorf("parsed %q", l)
		}
	}
}

func TestTranscriptParser_truncatesLongPreviews(t *testing.T) {
	long := strings.Repeat("x", 1000)
	lines := strings.Join([]string{
		`{"type":"assistant","uuid":"a","message":{"content":[{"type":"tool_use","id":"t","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","uuid":"r","message":{"content":[{"type":"tool_result","tool_use_id":"t","content":"` + long + `"}]}}`,
	}, "\n")
	ms := Parse([]byte(lines), FormatClaude, "", nil)
	b := ms[0].Blocks[1]
	if b.Type != api.BlockToolResult {
		t.Fatalf("expected toolResult, got %v", b.Type)
	}
	if n := utf8.RuneCountInString(b.Preview); n != PreviewLimit+1 {
		t.Errorf("preview length %d", n)
	}
	if !strings.HasSuffix(b.Preview, "…") {
		t.Error("no ellipsis")
	}
}

func TestTranscriptParser_parsesPi(t *testing.T) {
	ms := Parse(fixture(t, "pi-session.jsonl"), FormatPi, "/Users/dev/website", nil)
	expectEqual(t, roles(ms), []api.Role{api.RoleUser, api.RoleAssistant})
	expectEqual(t, ms[0].Blocks, []api.Block{api.TextBlock("List the pages")})
	expectEqual(t, ms[1].ID, "p2")
	expectEqual(t, ms[1].Blocks, []api.Block{
		api.ThinkingBlock("Look at the pages dir."),
		api.ToolCallBlock("c1", "bash", "Ran ls pages", `{"command":"ls pages"}`),
		api.ToolResultBlock("c1", false, "index.tsx\nabout.tsx"),
		api.TextBlock("Two pages: index and about."),
	})
}

func TestTranscriptParser_encodesLikeTheContract(t *testing.T) {
	m := api.Message{ID: "m", Role: api.RoleAssistant, CreatedAt: "2026-09-23T13:00:04+00:00", Blocks: []api.Block{
		api.ToolResultBlock("t1", false, "ok"),
	}}
	data, err := api.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	// Swift sorts keys here; compare as sorted-key JSON.
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	sorted, _ := json.Marshal(v)
	expectEqual(t, string(sorted), `{"blocks":[{"isError":false,"preview":"ok","toolCallId":"t1","type":"toolResult"}],"createdAt":"2026-09-23T13:00:04+00:00","id":"m","role":"assistant"}`)
}

func TestTranscriptParser_decodesContractFixture(t *testing.T) {
	data, err := os.ReadFile("../../../docs/fixtures/messages.json")
	if err != nil {
		t.Fatal(err)
	}
	var page api.MessagePage
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 4 {
		t.Errorf("got %d messages", len(page.Messages))
	}
}

// Go-only: InputString matches Foundation's JSONSerialization output (numbers, escapes, sorting).
func TestInputStringMatchesFoundation(t *testing.T) {
	line := `{"a":"x\u0001\b\f\t\n\r/<>& é😀\"\\","n":[1,1.0,0.1,1e21,1e2,-0,123456789012345678,1.5e-7,true,false,null],"Z":1,"b":{"z":1,"a":2}}`
	d := json.NewDecoder(strings.NewReader(line))
	d.UseNumber()
	var v map[string]any
	if err := d.Decode(&v); err != nil {
		t.Fatal(err)
	}
	// Captured from Swift: JSONSerialization.data(withJSONObject:options:[.sortedKeys,.withoutEscapingSlashes]).
	want := "{\"a\":\"x\\u0001\\b\\f\\t\\n\\r/<>&\u2028é😀\\\"\\\\\",\"b\":{\"a\":2,\"z\":1},\"n\":[1,1,0.10000000000000001,1e+21,100,0,123456789012345678,1.4999999999999999e-07,true,false,null],\"Z\":1}"
	expectEqual(t, InputString(v, Scrubber{}), want)
	expectEqual(t, InputString("s", Scrubber{}), "{}")
	expectEqual(t, InputString(nil, Scrubber{}), "{}")
}

// Go-only: key order of InputString matches Foundation's `.sortedKeys` on macOS
// (testdata/swift-keyorder.json: random key sets in the order Swift wrote them).
func TestInputStringKeyOrderMatchesFoundation(t *testing.T) {
	var sets [][]string
	if err := json.Unmarshal(fixture(t, "swift-keyorder.json"), &sets); err != nil {
		t.Fatal(err)
	}
	for _, want := range sets {
		m := map[string]any{}
		for _, k := range want {
			m[k] = json.Number("0")
		}
		var parts []string
		for _, k := range want {
			var b strings.Builder
			writeJSONString(&b, k)
			parts = append(parts, b.String()+":0")
		}
		var got strings.Builder
		writeJSON(&got, m)
		expectEqual(t, got.String(), "{"+strings.Join(parts, ",")+"}")
	}
}

// Go-only (R8-9): lines Claude Code injects are not user bubbles; shell mode shows as typed.
func TestTranscriptParser_injectedLines(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"user","uuid":"n1","message":{"content":"<task-notification>\n<task-id>b1</task-id>\n<summary>done</summary>\n</task-notification>"}}`,
		`{"type":"user","uuid":"s1","message":{"content":"<bash-input>ls /Users/dev/shop-api/src</bash-input>"}}`,
		`{"type":"user","uuid":"o1","message":{"content":"<bash-stdout>a.py</bash-stdout><bash-stderr></bash-stderr>"}}`,
		`{"type":"user","uuid":"o2","message":{"content":"<bash-stderr>boom</bash-stderr>"}}`,
		`{"type":"user","uuid":"n2","message":{"content":[{"type":"text","text":"  <task-notification>x</task-notification>"}]}}`,
		`{"type":"user","uuid":"u1","message":{"content":"a real prompt mentioning <task-notification>"}}`,
	}, "\n")
	ms := Parse([]byte(lines), FormatClaude, claudeCwd, nil)
	expectEqual(t, ids(ms), []string{"s1", "u1"})
	expectEqual(t, ms[0].Blocks, []api.Block{api.TextBlock("! ls src")})
	expectEqual(t, ms[1].Blocks, []api.Block{api.TextBlock("a real prompt mentioning <task-notification>")})
}
