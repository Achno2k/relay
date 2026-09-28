package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"relay/internal/api"
	"relay/internal/controls"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
	"relay/internal/server"
	"relay/internal/transcript"
	"relay/internal/uploads"
)

// testCatalogs: saved defaults in dir, model lists from the fixtures. Swift: NewAgentTests.catalogs.
func testCatalogs(t *testing.T, dir string) *controls.Catalogs {
	t.Helper()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("claude.json", `{"model":"sonnet","effortLevel":"high"}`)
	write("config.toml", "model = \"gpt-5.6-terra\"\nmodel_reasoning_effort = \"xhigh\"\n\n[projects.\"/x\"]\nmodel = \"ignored\"\n")
	write("pi.json", `{"defaultProvider":"openai-codex","defaultModel":"gpt-5.6-sol","defaultThinkingLevel":"high"}`)
	piList, err := os.ReadFile("testdata/pi-list-models.txt")
	if err != nil {
		t.Fatal(err)
	}
	codexJSON, err := os.ReadFile("testdata/codex-models.json")
	if err != nil {
		t.Fatal(err)
	}
	store, _ := filepath.Abs("testdata/pi-models-store.json")
	return &controls.Catalogs{
		Run: func(args []string) []byte {
			if args[0] == "pi" {
				return piList
			}
			return codexJSON
		},
		PiSettingsPath:    filepath.Join(dir, "pi.json"),
		PiModelsStorePath: store,
		CodexSessions:     filepath.Join(dir, "sessions"),
		ClaudeSettings:    filepath.Join(dir, "claude.json"),
		CodexConfig:       filepath.Join(dir, "config.toml"),
	}
}

func withNewAgentService(t *testing.T) (*Service, *herdrtest.Server) {
	t.Helper()
	dir := t.TempDir()
	pane := func(id, cwd string) map[string]any {
		return map[string]any{"pane_id": id, "workspace_id": "w14", "tab_id": "w14:t1", "terminal_id": "t", "focused": false,
			"agent_status": "unknown", "revision": 1, "cwd": cwd}
	}
	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		switch method {
		case "pane.list":
			return map[string]any{"type": "pane_list", "panes": []any{pane("w14:p1", "/Users/dev"), pane("w14:p2", "/Users/dev/e2e")}}
		case "tab.create":
			cwd, _ := params["cwd"].(string)
			return map[string]any{"type": "tab_created", "tab": map[string]any{"tab_id": "w14:t9", "workspace_id": "w14"}, "root_pane": pane("w14:p9", cwd)}
		case "agent.start":
			name, _ := params["name"].(string)
			return map[string]any{"type": "agent_started", "argv": []any{}, "agent": herdrtest.AgentJSON("w14:p9", &name, "idle", "/Users/dev/e2e", nil, "")}
		case "agent.get":
			a := herdrtest.AgentJSON("w14:p9", str("n"), "idle", "/Users/dev/e2e", nil, "")
			delete(a, "agent") // not classified yet
			return map[string]any{"type": "agent_info", "agent": a}
		case "workspace.list":
			return map[string]any{"type": "workspace_list", "workspaces": herdrtest.Workspaces()}
		case "agent.read":
			return map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": "w14:p9", "workspace_id": "w14", "tab_id": "w14:t9",
				"source": "detection", "format": "text", "text": "", "revision": 1, "truncated": false}}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})
	svc := New(Deps{
		Herdr:    herdr.NewClient(fake.SocketPath),
		Locator:  transcript.NewLocator(dir, transcript.NewCodexRollouts(filepath.Join(dir, "sessions"))),
		Uploads:  uploads.NewStore(filepath.Join(dir, "uploads")),
		Catalogs: testCatalogs(t, dir),
		Settings: &controls.SettingsGuard{},
	})
	return svc, fake
}

func choiceIDs(cs []api.ControlChoice) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.ID
	}
	return out
}

func deref2(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// Swift: NewAgentTests (launchFlags, codexConfigTopLevelOnly and settingsLock live in controls;
// codexFallbackIgnoresOlderRollouts in transcript).
func TestNewAgent_KindControlsWithDefaults(t *testing.T) {
	s, _ := withNewAgentService(t)
	claude, err := s.KindControls("claude")
	if err != nil {
		t.Fatal(err)
	}
	if deref2(claude.DefaultModel) != "sonnet" || deref2(claude.DefaultEffort) != "high" || claude.EffortsByModel != nil {
		t.Errorf("claude %+v", claude)
	}
	codex, err := s.KindControls("codex")
	if err != nil {
		t.Fatal(err)
	}
	if deref2(codex.DefaultModel) != "gpt-5.6-terra" || deref2(codex.DefaultEffort) != "xhigh" {
		t.Errorf("codex defaults %v %v", deref2(codex.DefaultModel), deref2(codex.DefaultEffort))
	}
	if got := strings.Join(choiceIDs(codex.EffortsByModel["gpt-5.5"]), ","); got != "low,medium,high,xhigh" {
		t.Errorf("codex gpt-5.5 efforts %s", got)
	}
	if got := strings.Join(choiceIDs(codex.Efforts), ","); got != "low,medium,high,xhigh,max,ultra" {
		t.Errorf("codex efforts %s", got)
	}
	pi, err := s.KindControls("pi")
	if err != nil {
		t.Fatal(err)
	}
	if deref2(pi.DefaultModel) != "openai-codex/gpt-5.6-sol" || len(pi.Models) == 0 || pi.Models[0].ID != "openai-codex/gpt-5.6-sol" {
		t.Errorf("pi %v %v", deref2(pi.DefaultModel), pi.Models)
	}
	if got := strings.Join(choiceIDs(pi.EffortsByModel["opencode-go/plain-model"]), ","); got != "off" {
		t.Errorf("pi plain-model efforts %s", got)
	}
	var e *api.Error
	if _, err := s.KindControls("gemini"); !errors.As(err, &e) {
		t.Errorf("gemini: %v", err)
	}
}

func TestNewAgent_CreatePassesFlagsAndReflectsChoice(t *testing.T) {
	s, fake := withNewAgentService(t)
	a, err := s.Create(context.Background(), server.CreateRequest{WorkspaceID: "w14", Kind: "claude", Name: str("c1"),
		Model: str("opus"), Effort: str("low"), CwdFromPane: str("w14:p2")})
	if err != nil {
		t.Fatal(err)
	}
	if deref2(a.Model) != "claude-opus-5-5" || deref2(a.ModelLabel) != "Opus 5.5" || deref2(a.Effort) != "low" || a.Kind != "claude" {
		t.Errorf("%+v", a)
	}
	if p := fake.Params("agent.start"); !strings.Contains(p, `"args":["--model","opus","--effort","low"]`) {
		t.Errorf("start %s", p)
	}
	if p := fake.Params("tab.create"); !strings.Contains(p, `"cwd":"/Users/dev/e2e"`) {
		t.Errorf("tab %s", p)
	}
}

func TestNewAgent_JustCreatedAgentStaysPendingUntilHerdrDetectsIt(t *testing.T) {
	s, fake := withNewAgentService(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, server.CreateRequest{WorkspaceID: "w14", Kind: "claude", Name: str("c3")}); err != nil {
		t.Fatal(err)
	}
	// herdr still reports no kind a moment later (the fake never classifies it).
	a, err := s.Agent(ctx, "w14:p9")
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != "claude" || a.TranscriptState != api.TranscriptPending {
		t.Errorf("%+v", a)
	}
	p, err := s.Messages(ctx, "w14:p9", nil, 50)
	if err != nil || len(p.Messages) != 0 {
		t.Errorf("%+v %v", p, err)
	}
	if strings.Contains(fake.Params("agent.read"), `"source":"recent"`) {
		t.Errorf("screen read %s", fake.Params("agent.read"))
	}
}

func TestNewAgent_CreateWithoutChoicesPassesNoArgs(t *testing.T) {
	s, fake := withNewAgentService(t)
	if _, err := s.Create(context.Background(), server.CreateRequest{WorkspaceID: "w14", Kind: "codex", Name: str("c2")}); err != nil {
		t.Fatal(err)
	}
	if p := fake.Params("agent.start"); strings.Contains(p, "args") {
		t.Errorf("start %s", p)
	}
	if p := fake.Params("tab.create"); !strings.Contains(p, `"cwd":"/Users/dev"`) { // first pane
		t.Errorf("tab %s", p)
	}
}

func TestNewAgent_CreateValidatesBeforeOpeningATab(t *testing.T) {
	s, fake := withNewAgentService(t)
	ctx := context.Background()
	for _, c := range []struct {
		kind   string
		model  *string
		effort *string
	}{
		{"pi", str("anthropic/claude-fable-5"), str("off")},
		{"claude", str("gpt-9"), nil},
		{"codex", str("gpt-5.5"), str("ultra")},
		{"gemini", str("x"), nil},
	} {
		_, err := s.Create(ctx, server.CreateRequest{WorkspaceID: "w14", Kind: c.kind, Model: c.model, Effort: c.effort})
		var e *api.Error
		if !errors.As(err, &e) {
			t.Errorf("%s %s: %v", c.kind, deref2(c.model), err)
		}
	}
	if slices.Contains(fake.Methods(), "tab.create") {
		t.Error("opened a tab")
	}
	_, err := s.Create(ctx, server.CreateRequest{WorkspaceID: "w14", Kind: "pi", CwdFromPane: str("w9:p9")})
	var e *api.Error
	if !errors.As(err, &e) {
		t.Errorf("cwdFromPane: %v", err)
	}
}
