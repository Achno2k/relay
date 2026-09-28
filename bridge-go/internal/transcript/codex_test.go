package transcript

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
)

// Ported from CodexTranscriptTests.swift.

const codexCwd = "/Users/dev/project"

func codexMessages(t *testing.T) []api.Message {
	return Parse(fixture(t, "codex-session.jsonl"), FormatCodex, codexCwd, nil)
}

func TestCodexTranscript_userAndAssistantTurns(t *testing.T) {
	ms := codexMessages(t)
	expectEqual(t, roles(ms), []api.Role{api.RoleUser, api.RoleAssistant, api.RoleUser})
	expectEqual(t, ids(ms), []string{"u1", "rs1", "u2"})
	expectEqual(t, ms[0].Blocks, []api.Block{api.TextBlock("Fix the failing test in src/app.py")})
	expectEqual(t, ms[0].CreatedAt, "2026-09-24T14:00:02+00:00")
	expectEqual(t, ms[2].Blocks, []api.Block{api.TextBlock("Thanks")})
}

func TestCodexTranscript_assistantItemsMergeIntoOneMessage(t *testing.T) {
	blocks := codexMessages(t)[1].Blocks
	expectEqual(t, blocks[0], api.ThinkingBlock("**Finding the failing test**"))
	expectEqual(t, blocks[1], api.TextBlock("Running the tests first."))
	expectEqual(t, blocks[2], api.ToolCallBlock("exec-1", "Shell", "Ran pytest -q", `{"command":"pytest -q\necho done","cwd":"."}`))
	expectEqual(t, blocks[3], api.ToolResultBlock("exec-1", true, "1 failed in tests/test_app.py"))
	expectEqual(t, blocks[4], api.ToolCallBlock("exec-2", "Edit", "Edited src/app.py", `{"files":["src/app.py"]}`))
	expectEqual(t, blocks[5], api.ToolResultBlock("exec-2", false, "src/app.py\n@@ -1 +1 @@\n-return 1\n+return 2"))
	expectEqual(t, blocks[6], api.ToolCallBlock("exec-3", "docs.search", "Called docs.search", `{"q":"pytest"}`))
	expectEqual(t, blocks[7], api.ToolResultBlock("exec-3", false, "3 results"))
	expectEqual(t, blocks[8], api.ToolCallBlock("exec-4", "WebSearch", "Searched the web for pytest fixtures", `{"query":"pytest fixtures"}`))
	// A declined command is an error result.
	expectEqual(t, blocks[11], api.ToolResultBlock("exec-5", true, ""))
	expectEqual(t, blocks[len(blocks)-1], api.TextBlock("Fixed `src/app.py`."))
	expectEqual(t, len(blocks), 13) // the empty reasoning item adds nothing
}

func TestCodexTranscript_rawResponseItemsAreIgnored(t *testing.T) {
	data, err := api.Marshal(codexMessages(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"environment_context", "tools.exec_command", "/Users/dev"} {
		if strings.Contains(string(data), s) {
			t.Errorf("output contains %q", s)
		}
	}
}

func TestCodexTranscript_incrementalGrowth(t *testing.T) {
	p := NewParser(FormatCodex, codexCwd, nil)
	var gotIDs []string
	var counts []int
	for _, l := range bytes.Split(fixture(t, "codex-session.jsonl"), []byte("\n")) {
		if len(l) == 0 {
			continue
		}
		if m, ok := p.ConsumeLine(l); ok {
			gotIDs = append(gotIDs, m.ID)
			counts = append(counts, len(m.Blocks))
		}
	}
	expectEqual(t, gotIDs, []string{"u1", "rs1", "rs1", "rs1", "rs1", "rs1", "rs1", "rs1", "rs1", "u2"})
	expectEqual(t, counts, []int{1, 1, 2, 4, 6, 8, 10, 12, 13, 1})
}

func TestCodexTranscript_locatorFindsBySessionIdOrNewestInCwd(t *testing.T) {
	root := t.TempDir()
	day := filepath.Join(root, "2026", "09", "24")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(day, "rollout-2026-09-24T14-00-00-01a0d390-0000-7a41-979a-000000000002.jsonl")
	if err := os.WriteFile(a, fixture(t, "codex-session.jsonl"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(day, "rollout-2026-09-24T15-00-00-01a0d390-0000-7a41-979a-000000000009.jsonl")
	if err := os.WriteFile(b, []byte(`{"timestamp":"2026-09-24T15:00:00Z","type":"session_meta","payload":{"id":"x","cwd":"/Users/dev/other"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	rollouts := NewCodexRollouts(root)
	expectEqual(t, rollouts.Find("01a0d390-0000-7a41-979a-000000000002"), a)
	expectEqual(t, rollouts.Find("nope"), "")
	expectEqual(t, rollouts.Newest("/Users/dev/project", time.Time{}), a)
	expectEqual(t, rollouts.Newest("/Users/dev/other", time.Time{}), b)
	expectEqual(t, rollouts.Newest("/Users/dev/none", time.Time{}), "")

	locator := NewLocator(root, rollouts)
	agent := func(session string) herdr.Agent {
		a := herdr.Agent{PaneID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Agent: "codex",
			AgentStatus: api.StatusIdle, Cwd: "/Users/dev/project"}
		if session != "" {
			a.AgentSession = &herdr.AgentSession{Source: "herdr:codex", Agent: "codex", Kind: "id", Value: session}
		}
		return a
	}
	expectEqual(t, locator.Locate(agent("01a0d390-0000-7a41-979a-000000000002"), time.Time{}),
		&Ref{Path: a, Format: FormatCodex, Cwd: "/Users/dev/project"})
	// No session id from herdr yet: newest rollout started in the agent's folder.
	if ref := locator.Locate(agent(""), time.Time{}); ref == nil || ref.Path != a {
		t.Errorf("got %+v", ref)
	}
}
