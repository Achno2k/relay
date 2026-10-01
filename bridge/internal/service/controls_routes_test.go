package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"relay/internal/api"
	"relay/internal/controls"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
	"relay/internal/server"
	"relay/internal/transcript"
	"relay/internal/uploads"
)

// screenClaude is a Claude Code screen that answers slash commands and Shift+Tab.
// Swift: ControlsTests.Claude (the full control suite lives in internal/controls).
type screenClaude struct {
	mu      sync.Mutex
	status  string
	mode    string
	cycle   []string
	session string
	history []string
}

func (c *screenClaude) screen() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	footer := "⏸ plan mode on (shift+tab to cycle)"
	switch c.mode {
	case "auto":
		footer = "⏵⏵ auto mode on (shift+tab to cycle)"
	case "manual":
		footer = "⏸ manual mode on"
	case "acceptEdits":
		footer = "⏵⏵ accept edits on (shift+tab to cycle)"
	}
	rule := strings.Repeat("─", 40)
	h := c.history[max(0, len(c.history)-10):]
	return strings.Join(append(slices.Clone(h), rule, "❯", rule, "  0/1.0M", "  "+footer+" · ← 1 agent"), "\n")
}

func (c *screenClaude) run(command string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.history = append(c.history, "❯ "+command)
}

func (c *screenClaude) shiftTab() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = c.cycle[(slices.Index(c.cycle, c.mode)+1)%len(c.cycle)]
}

func TestControls_Routes(t *testing.T) {
	claude := &screenClaude{status: "done", mode: "auto", cycle: []string{"auto", "manual", "acceptEdits", "plan"}, session: "s1"}
	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		switch method {
		case "agent.get":
			claude.mu.Lock()
			a := herdrtest.AgentJSON("w14:p2", str("e2e"), claude.status, "/Users/dev/e2e",
				map[string]any{"source": "herdr:claude", "agent": "claude", "kind": "id", "value": claude.session}, "")
			claude.mu.Unlock()
			return map[string]any{"type": "agent_info", "agent": a}
		case "workspace.list":
			return map[string]any{"type": "workspace_list", "workspaces": herdrtest.Workspaces()}
		case "agent.read":
			return map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": "w14:p2", "workspace_id": "w14", "tab_id": "w14:t1",
				"source": "detection", "format": "text", "text": claude.screen(), "revision": 1, "truncated": false}}
		case "agent.prompt":
			text, _ := params["text"].(string)
			claude.run(text)
			return map[string]any{"type": "agent_prompted", "agent": map[string]any{}}
		case "agent.send_keys":
			if keys, _ := params["keys"].([]any); reflect.DeepEqual(keys, []any{"shift+tab"}) {
				claude.shiftTab()
			}
			return map[string]any{"type": "ok"}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})
	settings := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(settings, []byte("{\"model\":\"opus\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New(Deps{
		Herdr:    herdr.NewClient(fake.SocketPath),
		Locator:  transcript.NewLocator("/nonexistent", transcript.NewCodexRollouts("/nonexistent")),
		Uploads:  uploads.NewStore(t.TempDir()),
		Settings: &controls.SettingsGuard{Path: settings},
	})
	ts := httptest.NewServer(server.New(server.Options{Backend: svc, Hub: server.NewHub(), Token: "t"}))
	defer ts.Close()
	app := routesApp{ts: ts, fake: fake}
	h := map[string]string{"Authorization": "Bearer t"}

	r := app.do(t, "GET", "/controls", nil, h)
	want, _ := api.Marshal(controls.Catalog())
	if string(r.body) != string(want) {
		t.Errorf("catalog %s", r.body)
	}
	c := decodeInto[api.AgentControls](t, app.do(t, "GET", "/controls?kind=claude", nil, h))
	if got := strings.Join(choiceIDs(c.Models), ","); got != "opus,sonnet,haiku,fable" {
		t.Errorf("models %s", got)
	}
	if r := app.do(t, "GET", "/controls?kind=gemini", nil, h); r.status != http.StatusBadRequest {
		t.Errorf("gemini %d", r.status)
	}
	for _, bad := range []string{`{}`, `{"model":"opus","effort":"high"}`, `{"command":"nuke"}`, `{"model":"gpt"}`} {
		if r := app.do(t, "POST", "/agents/w14%3Ap2/control", []byte(bad), h); r.status != http.StatusBadRequest {
			t.Errorf("%s: %d %s", bad, r.status, r.body)
		}
	}
	r = app.do(t, "POST", "/agents/w14%3Ap2/control", []byte(`{"permissionMode":"plan"}`), h)
	if r.status != http.StatusAccepted {
		t.Fatalf("plan: %d %s", r.status, r.body)
	}
	if a := decodeInto[api.Agent](t, r); deref2(a.PermissionMode) != "plan" {
		t.Errorf("permissionMode %s", fmt.Sprint(deref2(a.PermissionMode)))
	}
}
