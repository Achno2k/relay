package harness

import (
	"strings"
	"testing"
)

func TestStripFences(t *testing.T) {
	cases := []struct{ in, want string }{
		{"{\"a\":1}", `{"a":1}`},
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```\n{\"a\":1}\n```", `{"a":1}`},
		{"Here you go:\n```json\n{\"a\":1}\n```\nhope that helps", `{"a":1}`},
		{"  \n```JSON\n[1,2]\n```\n", "[1,2]"},
		{"```{\"a\":1}```", `{"a":1}`},
		{"plain text answer", "plain text answer"},
	}
	for _, c := range cases {
		if got := stripFences(c.in); got != c.want {
			t.Errorf("stripFences(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAnswerValidatesJSONOnlyWithSchema(t *testing.T) {
	if _, err := answer("not json", ""); err != nil {
		t.Fatalf("no schema should not validate: %v", err)
	}
	got, err := answer("```json\n{\"ok\":true}\n```", `{"type":"object"}`)
	if err != nil {
		t.Fatalf("valid JSON rejected: %v", err)
	}
	if got != `{"ok":true}` {
		t.Fatalf("got %q", got)
	}
	if _, err := answer("sorry, I cannot", `{"type":"object"}`); err == nil {
		t.Fatal("expected error for non-JSON answer with schema")
	}
}

func TestWithSchema(t *testing.T) {
	if got := withSchema("do it", ""); got != "do it" {
		t.Fatalf("empty schema changed prompt: %q", got)
	}
	got := withSchema("do it", `{"type":"object"}`)
	if !strings.Contains(got, "do it") || !strings.Contains(got, `{"type":"object"}`) || !strings.Contains(got, "ONLY") {
		t.Fatalf("schema instruction missing: %q", got)
	}
}

func TestParseClaudeJSON(t *testing.T) {
	out := `{"type":"result","subtype":"success","is_error":false,"result":"` + "```json\\n{\\\"steps\\\":[]}\\n```" + `"}`
	got, err := parseClaudeJSON(out, `{"type":"object"}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != `{"steps":[]}` {
		t.Fatalf("got %q", got)
	}
	if _, err := parseClaudeJSON(`{"type":"result","is_error":true,"result":"boom"}`, ""); err == nil {
		t.Fatal("expected error for is_error result")
	}
	if _, err := parseClaudeJSON("not json at all", ""); err == nil {
		t.Fatal("expected error for unparseable envelope")
	}
}

func TestLastAgentMessage(t *testing.T) {
	out := `{"type":"thread.started","thread_id":"x"}
2026-01-01T00:00:00Z ERROR some log line that is not json
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"thinking"}}
{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"first"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"last"}}
{"type":"turn.completed"}`
	got, err := lastAgentMessage(out)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got != "last" {
		t.Fatalf("got %q, want last", got)
	}

	legacy := `{"msg":{"type":"agent_message","message":"legacy answer"}}`
	if got, err := lastAgentMessage(legacy); err != nil || got != "legacy answer" {
		t.Fatalf("legacy shape: got %q err %v", got, err)
	}

	failed := `{"type":"turn.started"}
{"type":"turn.failed","error":{"message":"model not supported"}}`
	_, err = lastAgentMessage(failed)
	if err == nil || !strings.Contains(err.Error(), "model not supported") {
		t.Fatalf("expected turn.failed error, got %v", err)
	}

	if _, err := lastAgentMessage(""); err == nil {
		t.Fatal("expected error for empty output")
	}
}
