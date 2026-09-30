package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/api"
)

const editsCwd = "/Users/dev/shop"

func claudeLine(t *testing.T, id string, blocks ...map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type": "assistant", "uuid": id, "isSidechain": false, "timestamp": "2026-09-30T10:00:00Z",
		"message": map[string]any{"role": "assistant", "content": blocks},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func toolUse(id, name string, input map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}
}

func toolCalls(ms []api.Message) map[string]api.Block {
	out := map[string]api.Block{}
	for _, m := range ms {
		for _, b := range m.Blocks {
			if b.Type == api.BlockToolCall {
				out[b.ID] = b
			}
		}
	}
	return out
}

func TestClaudeEdits(t *testing.T) {
	line := claudeLine(t, "a1",
		toolUse("r", "Read", map[string]any{"file_path": editsCwd + "/src/a.go"}),
		toolUse("w", "Write", map[string]any{"file_path": editsCwd + "/new.txt", "content": "see " + editsCwd + "/src/a.go\n"}),
		toolUse("e", "Edit", map[string]any{"file_path": "/etc/hosts", "old_string": "x", "new_string": "y", "replace_all": true}),
		toolUse("m", "MultiEdit", map[string]any{"file_path": editsCwd + "/b.go", "edits": []any{
			map[string]any{"old_string": "1", "new_string": "2"},
			map[string]any{"old_string": "3", "new_string": "4", "replace_all": true},
		}}),
		toolUse("g", "Grep", map[string]any{"pattern": "x", "path": editsCwd + "/src"}),
		toolUse("up", "Read", map[string]any{"file_path": "/Users/dev/other/x"}),
	)
	calls := toolCalls(Parse([]byte(line), FormatClaude, editsCwd, nil))
	expectEqual(t, calls["r"].Path, "src/a.go")
	expectEqual(t, calls["r"].Edit, (*api.ToolEdit)(nil))
	expectEqual(t, calls["w"].Path, "new.txt")
	expectEqual(t, *calls["w"].Edit, api.ToolEdit{Kind: api.ToolEditWrite, Content: "see src/a.go\n"})
	// Outside the cwd: no path, but the edit is still there.
	expectEqual(t, calls["e"].Path, "")
	expectEqual(t, *calls["e"].Edit, api.ToolEdit{Kind: api.ToolEditEdit, Changes: []api.ToolEditChange{{Old: "x", New: "y", ReplaceAll: true}}})
	expectEqual(t, calls["m"].Edit.Changes, []api.ToolEditChange{{Old: "1", New: "2"}, {Old: "3", New: "4", ReplaceAll: true}})
	expectEqual(t, calls["g"].Path, "")
	expectEqual(t, calls["up"].Path, "")
}

func TestPiEdits(t *testing.T) {
	line := func(id, name string, args map[string]any) string {
		b, _ := json.Marshal(map[string]any{"type": "message", "id": id, "timestamp": "2026-09-30T10:00:00Z",
			"message": map[string]any{"role": "assistant", "content": []any{
				map[string]any{"type": "toolCall", "id": id, "name": name, "arguments": args},
			}}})
		return string(b)
	}
	data := strings.Join([]string{
		line("p1", "edit", map[string]any{"path": "src/x.ts", "edits": []any{map[string]any{"oldText": "a", "newText": "b"}}}),
		line("p2", "edit", map[string]any{"path": editsCwd + "/y.ts", "oldText": "c", "newText": "d"}),
		line("p3", "write", map[string]any{"path": "../z.ts", "content": "z"}),
	}, "\n")
	calls := toolCalls(Parse([]byte(data), FormatPi, editsCwd, nil))
	expectEqual(t, calls["p1"].Path, "src/x.ts")
	expectEqual(t, calls["p1"].Edit.Changes, []api.ToolEditChange{{Old: "a", New: "b"}})
	expectEqual(t, calls["p2"].Path, "y.ts")
	expectEqual(t, calls["p2"].Edit.Changes, []api.ToolEditChange{{Old: "c", New: "d"}})
	expectEqual(t, calls["p3"].Path, "")
	expectEqual(t, calls["p3"].Edit.Kind, api.ToolEditWrite)
}

func TestCodexEdit(t *testing.T) {
	s := NewScrubber(editsCwd)
	e := codexEdit(map[string]any{editsCwd + "/n.txt": map[string]any{"type": "add", "content": "hi\n"}}, s)
	expectEqual(t, *e, api.ToolEdit{Kind: api.ToolEditWrite, Content: "hi\n"})
	e = codexEdit(map[string]any{
		editsCwd + "/n.txt": map[string]any{"type": "add", "content": "a\nb"},
		editsCwd + "/o.txt": map[string]any{"type": "update", "unified_diff": "@@ -1 +1 @@\n-x\n+y\n"},
	}, s)
	expectEqual(t, e.Diff, "--- /dev/null\n+++ b/n.txt\n@@ -0,0 +1,2 @@\n+a\n+b\n--- a/o.txt\n+++ b/o.txt\n@@ -1 +1 @@\n-x\n+y\n")
}

func TestEditCaps(t *testing.T) {
	line := strings.Repeat("x", 99) + "\n"
	big := strings.Repeat(line, EditStringLimit/len(line)+5)
	e := capEdit(&api.ToolEdit{Kind: api.ToolEditEdit, Changes: []api.ToolEditChange{
		{Old: big, New: big}, {Old: big, New: big}, {Old: big, New: big}, {Old: "tail", New: "tail"},
	}})
	total := 0
	for _, c := range e.Changes {
		for _, s := range []string{c.Old, c.New} {
			if len(s) > EditStringLimit || (len(s) >= len(line) && !strings.HasSuffix(s, "\n")) {
				t.Errorf("bad cut: %d bytes", len(s))
			}
			total += len(s)
		}
	}
	if !e.Truncated || total > EditTotalLimit || e.Changes[3].Old != "" {
		t.Errorf("truncated=%v total=%d last=%q", e.Truncated, total, e.Changes[3].Old)
	}
}

func TestExitPlanModePlan(t *testing.T) {
	dir := t.TempDir()
	old := PlansDir
	PlansDir = dir
	t.Cleanup(func() { PlansDir = old })
	planPath := filepath.Join(dir, "quiet-orange-kettle.md")

	// input.plan wins.
	calls := toolCalls(Parse([]byte(claudeLine(t, "a1",
		toolUse("x", "ExitPlanMode", map[string]any{"plan": "# Inline\nedit " + editsCwd + "/a.go\n"}))), FormatClaude, editsCwd, nil))
	expectEqual(t, calls["x"].Plan, "# Inline\nedit a.go\n")

	// Otherwise the plan file on disk (it may have been edited after the Write).
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.WriteFile(planPath, []byte("# On disk\n"), 0o600))
	data := claudeLine(t, "a1", toolUse("w", "Write", map[string]any{"file_path": planPath, "content": "# Written\n"})) + "\n" +
		claudeLine(t, "a2", toolUse("x", "ExitPlanMode", map[string]any{}))
	calls = toolCalls(Parse([]byte(data), FormatClaude, editsCwd, nil))
	expectEqual(t, calls["x"].Plan, "# On disk\n")
	expectEqual(t, calls["w"].Path, "")

	// File gone: the Write's content.
	must(os.Remove(planPath))
	calls = toolCalls(Parse([]byte(data), FormatClaude, editsCwd, nil))
	expectEqual(t, calls["x"].Plan, "# Written\n")

	// No plan at all: left out.
	calls = toolCalls(Parse([]byte(claudeLine(t, "a1", toolUse("x", "ExitPlanMode", map[string]any{}))), FormatClaude, editsCwd, nil))
	expectEqual(t, calls["x"].Plan, "")
}

func TestPendingPlan(t *testing.T) {
	call := func(id, plan string) api.Block {
		b := api.ToolCallBlock(id, "ExitPlanMode", "ExitPlanMode", "{}")
		b.Plan = plan
		return b
	}
	msgs := []api.Message{{Blocks: []api.Block{call("x1", "# Old"), api.ToolResultBlock("x1", false, "ok")}}}
	if _, ok := PendingPlan(msgs); ok {
		t.Error("an answered ExitPlanMode is not pending")
	}
	msgs = append(msgs, api.Message{Blocks: []api.Block{api.TextBlock("hi"), call("x2", "# New")}})
	if plan, ok := PendingPlan(msgs); !ok || plan != "# New" {
		t.Errorf("got %q %v", plan, ok)
	}
}

func TestLatestPlan(t *testing.T) {
	dir := t.TempDir()
	old := PlansDir
	PlansDir = dir
	t.Cleanup(func() { PlansDir = old })
	a := filepath.Join(dir, "a.md")
	data := claudeLine(t, "a1", toolUse("w1", "Write", map[string]any{"file_path": filepath.Join(dir, "old.md"), "content": "# Old\n"})) + "\n" +
		claudeLine(t, "a2", toolUse("w2", "Write", map[string]any{"file_path": a, "content": "# A\n"}))
	expectEqual(t, LatestPlan([]byte(data), FormatClaude, editsCwd), "# A\n")
	if err := os.WriteFile(a, []byte("# A, edited\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expectEqual(t, LatestPlan([]byte(data), FormatClaude, editsCwd), "# A, edited\n")
	expectEqual(t, LatestPlan([]byte(claudeLine(t, "a1", toolUse("r", "Read", map[string]any{"file_path": a}))), FormatClaude, editsCwd), "")
}

func TestIsPlanQuestion(t *testing.T) {
	expectEqual(t, IsPlanQuestion("Claude has written up a plan and is ready to execute. Would you like to proceed?"), true)
	expectEqual(t, IsPlanQuestion("Do you want to proceed?"), false)
	expectEqual(t, IsPlanQuestion("Do you want to make this edit to plan.md?"), false)
}
