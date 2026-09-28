package service

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
	"relay/internal/transcript"
	"relay/internal/uploads"
)

// fakeClaude's input box refills after Esc (like Claude Code) and empties on ctrl+u.
// Swift: InputClearingTests.Screen.
type fakeClaude struct {
	mu      sync.Mutex
	box     string
	status  string
	keys    [][]string
	texts   []string
	prompts []string
}

func (f *fakeClaude) snapshot() (keys [][]string, texts, prompts []string, box string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.keys...), append([]string(nil), f.texts...), append([]string(nil), f.prompts...), f.box
}

func withClearingService(t *testing.T, f *fakeClaude) *Service {
	t.Helper()
	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch method {
		case "agent.get":
			return map[string]any{"type": "agent_info", "agent": herdrtest.AgentJSON("w14:p2", str("e2e"), f.status, "/Users/dev/e2e", nil, "")}
		case "agent.read":
			rule := strings.Repeat("─", 40)
			text := "⎿  Interrupted\n" + rule + "\n❯ " + strings.ReplaceAll(f.box, "\n", "\n  ") + "\n" + rule + "\n  status"
			return map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": "w14:p2", "workspace_id": "w14", "tab_id": "w14:t1",
				"source": "detection", "format": "text", "text": text, "revision": 1, "truncated": false}}
		case "agent.send_keys":
			var keys []string
			for _, k := range params["keys"].([]any) {
				keys = append(keys, k.(string))
			}
			f.keys = append(f.keys, keys)
			if reflect.DeepEqual(keys, []string{"esc"}) {
				f.box = "first prompt\nsecond line"
			}
			if len(keys) > 0 && keys[0] == "ctrl+u" {
				f.box = ""
			}
			return map[string]any{"type": "ok"}
		case "agent.prompt":
			f.prompts = append(f.prompts, params["text"].(string))
			return map[string]any{"type": "agent_prompted", "agent": map[string]any{}}
		case "pane.send_text":
			f.texts = append(f.texts, params["text"].(string))
			return map[string]any{"type": "ok"}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})
	return New(Deps{
		Herdr:   herdr.NewClient(fake.SocketPath),
		Locator: transcript.NewLocator("/nonexistent", transcript.NewCodexRollouts("/nonexistent")),
		Uploads: uploads.NewStore(t.TempDir()),
	})
}

func ctrlU(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "ctrl+u"
	}
	return out
}

func TestInputClearing_StopClearsTheRestoredPrompt(t *testing.T) {
	f := &fakeClaude{status: "idle"}
	s := withClearingService(t, f)
	if err := s.SendKeys(context.Background(), "w14:p2", []string{"esc"}); err != nil {
		t.Fatal(err)
	}
	keys, _, _, box := f.snapshot()
	if !reflect.DeepEqual(keys, [][]string{{"esc"}, ctrlU(4)}) || box != "" {
		t.Errorf("keys %v box %q", keys, box)
	}
}

func TestInputClearing_PromptClearsLeftoverInputFirst(t *testing.T) {
	f := &fakeClaude{status: "idle", box: "leftover"}
	s := withClearingService(t, f)
	if err := s.Prompt(context.Background(), "w14:p2", "second", nil); err != nil {
		t.Fatal(err)
	}
	keys, _, prompts, _ := f.snapshot()
	if !reflect.DeepEqual(keys, [][]string{ctrlU(2)}) || !reflect.DeepEqual(prompts, []string{"second"}) {
		t.Errorf("keys %v prompts %v", keys, prompts)
	}
}

func TestInputClearing_EmptyInputPromptsWithoutKeys(t *testing.T) {
	f := &fakeClaude{status: "idle"}
	s := withClearingService(t, f)
	if err := s.Prompt(context.Background(), "w14:p2", "hi", nil); err != nil {
		t.Fatal(err)
	}
	if keys, _, _, _ := f.snapshot(); len(keys) != 0 {
		t.Errorf("keys %v", keys)
	}
}

func TestInputClearing_BlockedAgentIsNeverCleared(t *testing.T) {
	f := &fakeClaude{status: "blocked", box: "leftover"}
	s := withClearingService(t, f)
	if err := s.SendKeys(context.Background(), "w14:p2", []string{"down", "down"}); err != nil {
		t.Fatal(err)
	}
	if keys, _, _, _ := f.snapshot(); !reflect.DeepEqual(keys, [][]string{{"down", "down"}}) {
		t.Errorf("keys %v", keys)
	}
}

func TestInputClearing_TextTypesLiterallyThenEnter(t *testing.T) {
	f := &fakeClaude{status: "blocked"}
	s := withClearingService(t, f)
	ctx := context.Background()
	if err := s.Text(ctx, "w14:p2", "Green tea, please", true); err != nil {
		t.Fatal(err)
	}
	keys, texts, _, _ := f.snapshot()
	if !reflect.DeepEqual(texts, []string{"Green tea, please"}) || !reflect.DeepEqual(keys, [][]string{{"enter"}}) {
		t.Errorf("texts %v keys %v", texts, keys)
	}
	if err := s.Text(ctx, "w14:p2", "draft", false); err != nil {
		t.Fatal(err)
	}
	if keys, _, _, _ := f.snapshot(); !reflect.DeepEqual(keys, [][]string{{"enter"}}) {
		t.Errorf("keys %v", keys)
	}
}
