package controls

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
)

// view is what the service's Agent snapshot shows for controls (Swift: service.agent(id:)).
type view struct {
	model, label, effort, mode, session string
}

func agentView(t *testing.T, c *Controls, id string) view {
	t.Helper()
	ctx := context.Background()
	a, err := c.herdr.Agent(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var v view
	if s := c.State(ctx, a, c.d.Locate(a)); s != nil {
		v.model, v.effort, v.mode = str(s.Model), str(s.Effort), str(s.PermissionMode)
		if s.Model != nil {
			v.label = str(c.Label(a, *s.Model))
		}
	}
	if a.AgentSession != nil {
		v.session = a.AgentSession.Value
	}
	return v
}

func control(t *testing.T, c *Controls, id string, kind api.ControlRequestKind, value string) (view, error) {
	t.Helper()
	pane, err := c.Apply(context.Background(), id, api.ControlRequest{Kind: kind, Value: value})
	if err != nil {
		return view{}, err
	}
	return agentView(t, c, pane), nil
}

func mustControl(t *testing.T, c *Controls, id string, kind api.ControlRequestKind, value string) view {
	t.Helper()
	v, err := control(t, c, id, kind, value)
	if err != nil {
		t.Fatalf("%s %s: %v", kind, value, err)
	}
	return v
}

func expectCode(t *testing.T, err error, code string, status int) {
	t.Helper()
	var e *api.Error
	if !errors.As(err, &e) {
		t.Fatalf("expected %s, got %v", code, err)
	}
	if e.Code != code || (status != 0 && e.Status != status) {
		t.Errorf("got %d %s (%s), want %d %s", e.Status, e.Code, e.Message, status, code)
	}
}

func agentInfo(pane, status, cwd, kind, sessionKind, session string) map[string]any {
	a := herdrtest.AgentJSON(pane, sp("e2e"), status, cwd,
		map[string]any{"source": "herdr:" + kind, "agent": kind, "kind": sessionKind, "value": session}, "")
	a["agent"] = kind
	return map[string]any{"type": "agent_info", "agent": a}
}

func paneRead(pane, text string) map[string]any {
	return map[string]any{"type": "pane_read", "read": map[string]any{"pane_id": pane, "workspace_id": "w14", "tab_id": "w14:t1",
		"source": "detection", "format": "text", "text": text, "revision": 1, "truncated": false}}
}

// MARK: ControlsTests

// fakeClaude is a fake Claude Code behind a fake herdr: slash commands print `Set … to …`,
// Shift+Tab cycles the footer mode, /model and /effort rewrite a settings file like the real one.
type fakeClaude struct {
	mu       sync.Mutex
	kind     string
	status   string
	mode     string
	cycle    []string
	session  string
	history  []string
	shiftTab int
	// Ask "Switch model?" before switching, like Claude does on a long cached conversation.
	confirmSwitch bool
	dialog        []string
	pendingModel  string
	settings      string
}

func newFakeClaude(t *testing.T) *fakeClaude {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte("{\"model\":\"opus\"}\n"), 0o600)
	return &fakeClaude{kind: "claude", status: "done", mode: "auto", cycle: []string{"auto", "manual", "acceptEdits", "plan"},
		session: "s1", settings: path}
}

func suffix(l []string, n int) []string {
	if len(l) > n {
		return l[len(l)-n:]
	}
	return l
}

func (f *fakeClaude) screen() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var footer string
	switch f.mode {
	case "auto":
		footer = "⏵⏵ auto mode on (shift+tab to cycle)"
	case "manual":
		footer = "⏸ manual mode on"
	case "acceptEdits":
		footer = "⏵⏵ accept edits on (shift+tab to cycle)"
	default:
		footer = "⏸ plan mode on (shift+tab to cycle)"
	}
	rule := strings.Repeat("─", 40)
	lines := append([]string(nil), suffix(f.history, 10)...)
	if f.dialog != nil {
		return strings.Join(append(lines, f.dialog...), "\n")
	}
	return strings.Join(append(lines, rule, "❯", rule, "  0/1.0M", "  "+footer+" · ← 1 agent"), "\n")
}

func claudeModelLabel(alias string) string {
	m, _ := choice(claudeCatalog.Models, alias)
	return m.Label
}

func (f *fakeClaude) run(command string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.history = append(f.history, "❯ "+command)
	parts := strings.Fields(command)
	switch {
	case parts[0] == "/model" && f.confirmSwitch:
		label := claudeModelLabel(parts[1])
		f.pendingModel = label
		f.dialog = []string{strings.Repeat("▔", 40), "   Switch model?", "   Your next response will be slower",
			"   ❯ 1. Yes, switch to " + label, "     2. No, go back"}
	case parts[0] == "/model":
		f.history = append(f.history, "  ⎿  Set model to "+claudeModelLabel(parts[1])+" and saved as your default for new sessions")
		os.WriteFile(f.settings, []byte(`{"model":"`+parts[1]+"\"}\n"), 0o600)
	case parts[0] == "/effort":
		f.history = append(f.history, "  ⎿  Set effort level to "+parts[1]+" (saved as your default for new sessions): …")
		os.WriteFile(f.settings, []byte(`{"model":"opus","effortLevel":"`+parts[1]+"\"}\n"), 0o600)
	case parts[0] == "/compact":
		f.history = append(f.history, "  ⎿  Compacted (ctrl+o to see full summary)")
	case parts[0] == "/clear":
		n, _ := strconv.Atoi(f.session[1:])
		f.session = "s" + strconv.Itoa(n+1)
		f.history = []string{"▝▜██████▀  Sonnet 5 with medium effort · Claude Max"}
	}
}

func (f *fakeClaude) key(keys []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch strings.Join(keys, ",") {
	case "1":
		if f.dialog != nil && f.pendingModel != "" {
			f.dialog = nil
			f.history = append(f.history, "  ⎿  Set model to "+f.pendingModel+" and saved as your default for new sessions")
		}
	case "2":
		if f.dialog != nil {
			f.dialog = nil
			f.history = append(f.history, "  ⎿  Kept model as Opus 5.5")
		}
	case "esc":
		f.dialog = nil
	case "shift+tab":
		f.shiftTab++
		i := slices.Index(f.cycle, f.mode)
		f.mode = f.cycle[(i+1)%len(f.cycle)]
	}
}

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

func withClaude(t *testing.T, f *fakeClaude) (*Controls, *herdrtest.Server) {
	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		switch method {
		case "agent.get":
			f.mu.Lock()
			kind, status, session := f.kind, f.status, f.session
			f.mu.Unlock()
			return agentInfo("w14:p2", status, "/Users/dev/e2e", kind, "id", session)
		case "workspace.list":
			return map[string]any{"type": "workspace_list", "workspaces": herdrtest.Workspaces()}
		case "agent.read":
			return paneRead("w14:p2", f.screen())
		case "agent.prompt":
			f.run(params["text"].(string))
			return map[string]any{"type": "agent_prompted", "agent": map[string]any{}}
		case "agent.send_keys":
			f.key(strs(params["keys"]))
			return map[string]any{"type": "ok"}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})
	c := New(Deps{Herdr: herdr.NewClient(fake.SocketPath), Settings: &SettingsGuard{Path: f.settings},
		Catalogs: &Catalogs{Run: func([]string) []byte { return nil }, ClaudeSettings: f.settings, PiSettingsPath: "/nonexistent",
			PiModelsStorePath: "/nonexistent", CodexConfig: "/nonexistent", CodexSessions: "/nonexistent"}})
	return c, fake
}

func TestModelSwitchKeepsSavedDefault(t *testing.T) {
	f := newFakeClaude(t)
	c, fake := withClaude(t, f)
	before, _ := os.ReadFile(f.settings)
	v := mustControl(t, c, "w14:p2", api.ControlModel, "sonnet")
	if v.model != "claude-sonnet-5" || v.label != "Sonnet 5" {
		t.Errorf("view %+v", v)
	}
	if p := fake.Params("agent.prompt"); p != `{"target":"w14:p2","text":"/model sonnet"}` {
		t.Errorf("prompt %s", p)
	}
	if after, _ := os.ReadFile(f.settings); string(after) != string(before) {
		t.Errorf("settings %s", after)
	}
}

func TestAnswersSwitchModelConfirmation(t *testing.T) {
	f := newFakeClaude(t)
	f.confirmSwitch = true
	c, fake := withClaude(t, f)
	v := mustControl(t, c, "w14:p2", api.ControlModel, "sonnet")
	if v.model != "claude-sonnet-5" {
		t.Errorf("model %s", v.model)
	}
	if p := fake.Params("agent.send_keys"); p != `{"keys":["1"],"target":"w14:p2"}` {
		t.Errorf("keys %s", p)
	}
	if f.dialog != nil {
		t.Error("dialog left open")
	}
}

func TestRealSwitchDialogIsDetected(t *testing.T) {
	d := dialog(string(fixture(t, "switch-model-dialog.txt")))
	if d == nil {
		t.Fatal("no dialog")
	}
	if d.Question != "Switch model?" || d.Options[0].Label != "Yes, switch to Sonnet 5" || !slices.Equal(d.Options[0].Keys, []string{"1"}) {
		t.Errorf("dialog %+v", d)
	}
	// A numbered list in the output during a redraw (no input box) is not a dialog.
	if dialog(string(fixture(t, "redraw-with-list.txt"))) != nil {
		t.Error("redraw is a dialog")
	}
	// The normal input box is not a dialog.
	if dialog(string(fixture(t, "input-empty.txt"))) != nil {
		t.Error("input box is a dialog")
	}
}

func TestKeptModelIsARefusal(t *testing.T) {
	if k, _ := kept("❯ /model sonnet\n  ⎿  Kept model as Opus 5.5\n───\n❯\n───", "/model sonnet"); k != "Opus 5.5" {
		t.Errorf("kept %q", k)
	}
	if k, _ := kept("<local-command-stdout>Kept model as `Opus 5.5`</local-command-stdout>", "/model sonnet"); k != "Opus 5.5" {
		t.Errorf("kept %q", k)
	}
	// An old "Kept" line before this command doesn't count.
	if _, ok := kept("  ⎿  Kept model as Opus 5.5\n❯ /model sonnet\n  ⎿  Set model to Sonnet 5", "/model sonnet"); ok {
		t.Error("old kept line counted")
	}
}

func TestEffort(t *testing.T) {
	f := newFakeClaude(t)
	c, _ := withClaude(t, f)
	if v := mustControl(t, c, "w14:p2", api.ControlEffort, "high"); v.effort != "high" {
		t.Errorf("effort %s", v.effort)
	}
	if s, _ := os.ReadFile(f.settings); string(s) != "{\"model\":\"opus\"}\n" {
		t.Errorf("settings %s", s)
	}
}

func TestModeCyclesWithShiftTab(t *testing.T) {
	f := newFakeClaude(t)
	c, _ := withClaude(t, f)
	if v := mustControl(t, c, "w14:p2", api.ControlPermissionMode, "plan"); v.mode != "plan" {
		t.Errorf("mode %s", v.mode)
	}
	if f.shiftTab != 3 {
		t.Errorf("presses %d", f.shiftTab)
	}
	if v := mustControl(t, c, "w14:p2", api.ControlPermissionMode, "default"); v.mode != "default" {
		t.Errorf("mode %s", v.mode)
	}
	// Already there: no key presses.
	presses := f.shiftTab
	mustControl(t, c, "w14:p2", api.ControlPermissionMode, "default")
	if f.shiftTab != presses {
		t.Errorf("pressed %d more", f.shiftTab-presses)
	}
}

func TestModeOutsideTheCycleIsUnsupported(t *testing.T) {
	f := newFakeClaude(t)
	c, _ := withClaude(t, f)
	_, err := control(t, c, "w14:p2", api.ControlPermissionMode, "bypassPermissions")
	expectCode(t, err, "unsupported", 400)
	_, err = control(t, c, "w14:p2", api.ControlPermissionMode, "bypassPermissions")
	expectCode(t, err, "unsupported", 400)
	// One full cycle each time, ending where it started.
	if f.mode != "auto" || f.shiftTab != 8 {
		t.Errorf("mode %s presses %d", f.mode, f.shiftTab)
	}
}

func TestClearFollowsTheNewSession(t *testing.T) {
	f := newFakeClaude(t)
	c, _ := withClaude(t, f)
	v := mustControl(t, c, "w14:p2", api.ControlClear, "")
	// The fresh session's banner fills model and effort before any reply.
	if v.session != "s2" || v.model != "claude-sonnet-5" || v.effort != "medium" {
		t.Errorf("view %+v", v)
	}
}

func TestCompact(t *testing.T) {
	f := newFakeClaude(t)
	c, fake := withClaude(t, f)
	mustControl(t, c, "w14:p2", api.ControlCompact, "")
	if p := fake.Params("agent.prompt"); p != `{"target":"w14:p2","text":"/compact"}` {
		t.Errorf("prompt %s", p)
	}
}

func TestRefusesWhileBusy(t *testing.T) {
	for status, code := range map[string]string{"working": "agent_busy", "blocked": "agent_blocked"} {
		f := newFakeClaude(t)
		f.status = status
		c, fake := withClaude(t, f)
		_, err := control(t, c, "w14:p2", api.ControlModel, "opus")
		expectCode(t, err, code, 409)
		if slices.Contains(fake.Methods(), "agent.prompt") {
			t.Errorf("%s: prompted", status)
		}
	}
}

func TestOnlyClaude(t *testing.T) {
	f := newFakeClaude(t)
	f.kind = "gemini"
	c, _ := withClaude(t, f)
	_, err := control(t, c, "w14:p2", api.ControlCompact, "")
	expectCode(t, err, "unsupported", 400)
	if v := agentView(t, c, "w14:p2"); v.model != "<nil>" && v.model != "" {
		t.Errorf("model %s", v.model)
	}
}

// ForKind (GET /controls?kind=) for claude, and the unknown kind.
func TestForKind(t *testing.T) {
	f := newFakeClaude(t)
	c, _ := withClaude(t, f)
	k, err := c.ForKind("claude")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range k.Models {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{"opus", "sonnet", "haiku", "fable"}) || str(k.DefaultModel) != "opus" {
		t.Errorf("claude %+v", k)
	}
	_, err = c.ForKind("gemini")
	expectCode(t, err, "unsupported", 400)
}

// MARK: AgentDriversFlowTests

type codexEntryT struct {
	slug, name string
	efforts    []string
}

var tuiCodexModels = []codexEntryT{
	{"gpt-6-luna", "GPT-6-Luna", []string{"low", "medium", "high"}},
	{"gpt-5.6-terra", "GPT-5.6-Terra", []string{"low", "medium", "high", "xhigh", "max", "ultra"}},
	{"gpt-5.5", "GPT-5.5", []string{"low", "medium", "high", "xhigh"}},
}

type tuiPicker struct {
	title  string
	labels []string
	cursor int
}

// tui is a fake pi or codex TUI behind a fake herdr, to drive the real drivers end to end.
type tui struct {
	mu      sync.Mutex
	kind    string
	session string
	status  string
	// pi
	piModel    string
	piThinking string
	piRejects  []string
	// codex
	model        string
	effort       string
	picker       *tuiPicker
	pickedModel  string
	savedDefault bool
	lines        []string
	mode         string
}

func newTUI(kind string) *tui {
	return &tui{kind: kind, session: "s1", status: "idle", piModel: "openai-codex/gpt-5.6-sol", piThinking: "high",
		model: "gpt-5.6-terra", effort: "high", mode: "ask"}
}

func (u *tui) screen() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.kind == "pi" {
		i := strings.Index(u.piModel, "/")
		p, m := u.piModel[:i], u.piModel[strings.LastIndex(u.piModel, "/")+1:]
		return strings.Join(append(append([]string(nil), suffix(u.lines, 6)...), "────", "────", "/Users/dev/p (main)",
			"$0.000 (sub) 0.0%/272k (auto)     ("+p+") "+m+" • "+u.piThinking), "\n")
	}
	out := append([]string(nil), suffix(u.lines, 6)...)
	if p := u.picker; p != nil {
		out = append(out, "  "+p.title)
		for i, l := range p.labels {
			mark := " "
			if i == p.cursor {
				mark = "›"
			}
			out = append(out, fmt.Sprintf("%s %d. %s  description", mark, i+1, l))
		}
		out = append(out, "  enter default · s session · esc back")
	} else {
		name := u.model
		for _, m := range tuiCodexModels {
			if m.slug == u.model {
				name = m.name
			}
		}
		out = append(out, "› Ask Codex to do anything", "  "+name+" "+u.effort+" · /Users/dev/p  ⚠ 3 warnings")
	}
	return strings.Join(out, "\n")
}

func (u *tui) prompt(text string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	parts := strings.Fields(text)
	switch {
	case u.kind == "pi" && parts[0] == "/model":
		u.piModel = parts[1]
		u.lines = append(u.lines, " Model: "+parts[1])
	case u.kind == "pi" && parts[0] == "/thinking" && slices.Contains(u.piRejects, parts[1]):
		u.lines = append(u.lines, ` Error: Unknown thinking level "`+parts[1]+`". Available levels: minimal, low, medium, high, xhigh, max.`)
	case u.kind == "pi" && parts[0] == "/thinking":
		u.piThinking = parts[1]
		u.lines = append(u.lines, " Thinking level: "+parts[1])
	case u.kind == "pi" && parts[0] == "/new":
		n, _ := strconv.Atoi(u.session[1:])
		u.session = "s" + strconv.Itoa(n+1)
	case u.kind == "codex" && parts[0] == "/model":
		cur := 0
		var names []string
		for i, m := range tuiCodexModels {
			names = append(names, m.name)
			if m.slug == u.model {
				cur = i
			}
		}
		u.picker = &tuiPicker{"Select Model and Effort", names, cur}
	case u.kind == "codex" && parts[0] == "/permissions":
		var labels []string
		for _, m := range codexModes {
			l := m.Label
			if m.ID == u.mode {
				l += " (current)"
			}
			labels = append(labels, l)
		}
		u.picker = &tuiPicker{"Update Model Permissions", labels, 0}
	}
}

func cleanPickerLabel(raw string) string {
	s := strings.Split(raw, "  ")[0]
	s = strings.ReplaceAll(strings.ReplaceAll(s, "(default)", ""), "(current)", "")
	return strings.TrimSpace(s)
}

func (u *tui) key(k string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	p := u.picker
	if p == nil {
		return
	}
	switch k {
	case "down":
		p.cursor = (p.cursor + 1) % len(p.labels)
	case "up":
		p.cursor = (p.cursor + len(p.labels) - 1) % len(p.labels)
	case "esc":
		u.picker = nil
	case "enter", "s":
		label := cleanPickerLabel(p.labels[p.cursor])
		switch {
		case p.title == "Select Model and Effort":
			for _, m := range tuiCodexModels {
				if m.name != label {
					continue
				}
				u.pickedModel = m.slug
				var direct []string
				hasMax := false
				for _, e := range m.efforts {
					if e == "max" || e == "ultra" {
						hasMax = hasMax || e == "max"
						continue
					}
					direct = append(direct, effortLabel(e))
				}
				if hasMax {
					direct = append(direct, "More reasoning…")
				}
				u.picker = &tuiPicker{"Select Reasoning Level for " + m.name, direct, 1}
			}
		case strings.HasPrefix(p.title, "Select Reasoning Level") || p.title == "Advanced Reasoning":
			if strings.HasPrefix(label, "More reasoning") {
				for _, m := range tuiCodexModels {
					if m.slug == u.pickedModel {
						var adv []string
						for _, e := range m.efforts {
							if e == "max" || e == "ultra" {
								adv = append(adv, effortLabel(e))
							}
						}
						u.picker = &tuiPicker{"Advanced Reasoning", adv, 0}
					}
				}
				return
			}
			effort := ""
			for id, l := range effortLabels {
				if l == label {
					effort = id
				}
			}
			u.model, u.effort, u.picker = u.pickedModel, effort, nil
			if k == "enter" {
				u.savedDefault = true
				u.lines = append(u.lines, "• Model changed to "+u.model+" "+effort)
			} else {
				u.lines = append(u.lines, "• Model changed to "+u.model+" "+effort+" for this session only")
			}
		case p.title == "Update Model Permissions" && k == "enter":
			u.picker = nil
			// Like codex: picking the current row prints nothing.
			if !strings.Contains(p.labels[p.cursor], "(current)") {
				u.lines = append(u.lines, "• Permission selection requested: "+label)
				for _, m := range codexModes {
					if m.Label == label {
						u.mode = m.ID
					}
				}
			}
		}
	}
}

func withTUI(t *testing.T, u *tui) *Controls {
	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		switch method {
		case "agent.get":
			u.mu.Lock()
			kind, status, session := u.kind, u.status, u.session
			u.mu.Unlock()
			sk := "id"
			if kind == "pi" {
				sk = "path"
			}
			return agentInfo("w14:p9", status, "/Users/dev/p", kind, sk, session)
		case "workspace.list":
			return map[string]any{"type": "workspace_list", "workspaces": herdrtest.Workspaces()}
		case "agent.read":
			return paneRead("w14:p9", u.screen())
		case "agent.prompt":
			u.prompt(params["text"].(string))
			return map[string]any{"type": "agent_prompted", "agent": map[string]any{}}
		case "agent.send_keys":
			for _, k := range strs(params["keys"]) {
				u.key(k)
			}
			return map[string]any{"type": "ok"}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})
	piList := fixture(t, "pi-list-models.txt")
	codexJSON := fixture(t, "codex-models.json")
	cat := &Catalogs{
		Run: func(args []string) []byte {
			if args[0] == "pi" {
				return piList
			}
			return codexJSON
		},
		PiSettingsPath: "/nonexistent", PiModelsStorePath: filepath.Join("testdata", "pi-models-store.json"),
		ClaudeSettings: "/nonexistent", CodexConfig: "/nonexistent", CodexSessions: "/nonexistent",
	}
	return New(Deps{Herdr: herdr.NewClient(fake.SocketPath), Catalogs: cat, Settings: &SettingsGuard{}})
}

func ids(list []api.ControlChoice) []string {
	var out []string
	for _, c := range list {
		out = append(out, c.ID)
	}
	return out
}

func TestPiModelAndThinking(t *testing.T) {
	u := newTUI("pi")
	c := withTUI(t, u)
	before := agentView(t, c, "w14:p9")
	if before.model != "openai-codex/gpt-5.6-sol" || before.label != "gpt-5.6-sol" || before.effort != "high" {
		t.Errorf("before %+v", before)
	}
	caps, err := c.ForAgent(context.Background(), "w14:p9")
	if err != nil {
		t.Fatal(err)
	}
	if caps.Supports != (api.ControlSupport{Model: true, Effort: true, Mode: false, Compact: true, Clear: true}) {
		t.Errorf("supports %+v", caps.Supports)
	}
	if !slices.Equal(ids(caps.Efforts), piAllLevels) {
		t.Errorf("efforts %v", ids(caps.Efforts))
	}
	if v := mustControl(t, c, "w14:p9", api.ControlModel, "anthropic/claude-sonnet-5"); v.model != "anthropic/claude-sonnet-5" {
		t.Errorf("model %s", v.model)
	}
	if v := mustControl(t, c, "w14:p9", api.ControlEffort, "minimal"); v.effort != "minimal" {
		t.Errorf("effort %s", v.effort)
	}
	if v := mustControl(t, c, "w14:p9", api.ControlClear, ""); v.session != "s2" {
		t.Errorf("session %s", v.session)
	}
	_, err = control(t, c, "w14:p9", api.ControlModel, "nope/x")
	expectCode(t, err, "bad_request", 400)
	_, err = control(t, c, "w14:p9", api.ControlPermissionMode, "plan")
	expectCode(t, err, "unsupported", 400)
}

func TestPiEffortsFollowTheModel(t *testing.T) {
	u := newTUI("pi")
	u.piModel = "anthropic/claude-fable-5"
	c := withTUI(t, u)
	caps, _ := c.ForAgent(context.Background(), "w14:p9")
	if !slices.Equal(ids(caps.Efforts), []string{"minimal", "low", "medium", "high", "xhigh", "max"}) {
		t.Errorf("efforts %v", ids(caps.Efforts))
	}
	// "off" isn't offered, and asking anyway is a clear 400 before anything is typed.
	_, err := control(t, c, "w14:p9", api.ControlEffort, "off")
	expectCode(t, err, "bad_request", 400)
}

func TestPiRejectionIsUnsupportedNotTimeout(t *testing.T) {
	u := newTUI("pi")
	u.piModel = "unlisted/model" // not in models-store: the bridge offers everything
	u.piRejects = []string{"off"}
	c := withTUI(t, u)
	start := time.Now()
	_, err := control(t, c, "w14:p9", api.ControlEffort, "off")
	expectCode(t, err, "unsupported", 400)
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %v", time.Since(start))
	}
	// Learned: now "off" isn't offered for that model.
	learned, _ := c.ForAgent(context.Background(), "w14:p9")
	if slices.Contains(ids(learned.Efforts), "off") {
		t.Errorf("efforts %v", ids(learned.Efforts))
	}
}

func TestPiModelWithoutThinkingOnlyOffersOff(t *testing.T) {
	u := newTUI("pi")
	u.piModel = "opencode-go/plain-model"
	c := withTUI(t, u)
	caps, _ := c.ForAgent(context.Background(), "w14:p9")
	if !slices.Equal(ids(caps.Efforts), []string{"off"}) || caps.Supports.Effort {
		t.Errorf("caps %+v", caps)
	}
}

func TestCodexModelPickerSessionOnly(t *testing.T) {
	u := newTUI("codex")
	c := withTUI(t, u)
	before := agentView(t, c, "w14:p9")
	if before.model != "gpt-5.6-terra" || before.label != "GPT-5.6-Terra" {
		t.Errorf("before %+v", before)
	}
	caps, _ := c.ForAgent(context.Background(), "w14:p9")
	if !slices.Equal(ids(caps.Models), []string{"gpt-6-luna", "gpt-5.6-terra", "gpt-5.5"}) { // hidden model left out
		t.Errorf("models %v", ids(caps.Models))
	}
	if !slices.Equal(ids(caps.Efforts), []string{"low", "medium", "high", "xhigh", "max", "ultra"}) {
		t.Errorf("efforts %v", ids(caps.Efforts))
	}
	if !slices.Equal(ids(caps.Modes), []string{"ask", "approveForMe", "fullAccess"}) {
		t.Errorf("modes %v", ids(caps.Modes))
	}
	a := mustControl(t, c, "w14:p9", api.ControlModel, "gpt-5.5")
	if a.model != "gpt-5.5" || a.effort != "high" { // kept: gpt-5.5 supports it
		t.Errorf("a %+v", a)
	}
	if b := mustControl(t, c, "w14:p9", api.ControlModel, "gpt-5.6-terra"); b.model != "gpt-5.6-terra" {
		t.Errorf("b %+v", b)
	}
	if v := mustControl(t, c, "w14:p9", api.ControlEffort, "ultra"); v.effort != "ultra" { // behind "More reasoning…"
		t.Errorf("c %+v", v)
	}
	if d := mustControl(t, c, "w14:p9", api.ControlModel, "gpt-6-luna"); d.effort != "medium" { // ultra unsupported there: its default
		t.Errorf("d %+v", d)
	}
	if u.savedDefault || u.picker != nil {
		t.Errorf("saved %v picker %+v", u.savedDefault, u.picker)
	}
}

func TestCodexModes(t *testing.T) {
	u := newTUI("codex")
	c := withTUI(t, u)
	if v := mustControl(t, c, "w14:p9", api.ControlPermissionMode, "fullAccess"); v.mode != "fullAccess" {
		t.Errorf("mode %s", v.mode)
	}
	if v := mustControl(t, c, "w14:p9", api.ControlPermissionMode, "ask"); v.mode != "ask" {
		t.Errorf("mode %s", v.mode)
	}
	// Already in "ask": no confirmation line from codex, still a 202, and the picker is closed.
	if v := mustControl(t, c, "w14:p9", api.ControlPermissionMode, "ask"); v.mode != "ask" {
		t.Errorf("mode %s", v.mode)
	}
	if u.picker != nil {
		t.Error("picker open")
	}
}

func TestCodexRefusals(t *testing.T) {
	u := newTUI("codex")
	u.model = "gpt-5.6-sol" // configured, but not in the catalogue
	c := withTUI(t, u)
	caps, _ := c.ForAgent(context.Background(), "w14:p9")
	if caps.Models[0] != (api.ControlChoice{ID: "gpt-5.6-sol", Label: "gpt-5.6-sol"}) || caps.Supports.Effort {
		t.Errorf("caps %+v", caps)
	}
	_, err := control(t, c, "w14:p9", api.ControlEffort, "low")
	expectCode(t, err, "unsupported", 400)
	_, err = control(t, c, "w14:p9", api.ControlModel, "gpt-5.6-sol")
	expectCode(t, err, "unsupported", 400)
	_, err = control(t, c, "w14:p9", api.ControlModel, "gpt-hidden")
	expectCode(t, err, "bad_request", 400)
	if u.picker != nil {
		t.Error("picker open")
	}
}

func TestCodexNoPickerLeftOpenOnFailure(t *testing.T) {
	u := newTUI("codex")
	c := withTUI(t, u)
	a, err := c.herdr.Agent(context.Background(), "w14:p9")
	if err != nil {
		t.Fatal(err)
	}
	// gpt-5.5 has no Max; asking for it after picking the model fails inside the picker.
	err = codexDriver{c}.pickModel(context.Background(), a,
		CodexModel{Slug: "gpt-5.5", DisplayName: "GPT-5.5", Visible: true, Efforts: []string{"low"}, DefaultEffort: sp("low")}, "max")
	expectCode(t, err, "unsupported", 400)
	if u.picker != nil || u.savedDefault {
		t.Errorf("picker %+v saved %v", u.picker, u.savedDefault)
	}
}
