package controls

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"relay/internal/api"
	"relay/internal/approval"
	"relay/internal/transcript"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func st(model, effort, mode string) State {
	var s State
	if model != "" {
		s.Model = sp(model)
	}
	if effort != "" {
		s.Effort = sp(effort)
	}
	if mode != "" {
		s.PermissionMode = sp(mode)
	}
	return s
}

func (s State) String() string {
	return "{" + str(s.Model) + " " + str(s.Effort) + " " + str(s.PermissionMode) + "}"
}

func wantState(t *testing.T, what string, got, want State) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// MARK: ClaudeControlsTests

func TestLabels(t *testing.T) {
	for id, label := range map[string]string{
		"claude-opus-5-5":           "Opus 5.5",
		"claude-sonnet-5":           "Sonnet 5",
		"claude-haiku-4-5-20251001": "Haiku 4.5",
		"claude-fable-5-1":          "Fable 5.1",
		"claude-opus-5-5[1m]":       "Opus 5.5",
	} {
		if got := ClaudeLabel(id); str(got) != label {
			t.Errorf("ClaudeLabel(%q) = %s, want %s", id, str(got), label)
		}
	}
}

func TestLabelsAndIdsRoundTrip(t *testing.T) {
	for _, m := range Catalog().Models {
		id := ClaudeID(m.Label)
		if id == nil || !strings.Contains(*id, m.ID) || str(ClaudeLabel(*id)) != m.Label {
			t.Errorf("%s: id %s", m.Label, str(id))
		}
	}
	if str(ClaudeID("Opus 5.5 (1M context)")) != "claude-opus-5-5" {
		t.Error("1M context label")
	}
	if ClaudeLabel("<synthetic>") != nil || ClaudeID("Default (recommended)") != nil {
		t.Error("expected nil")
	}
}

func TestScanTakesTheLatestValues(t *testing.T) {
	wantState(t, "scan", ScanClaude(fixture(t, "controls-session.jsonl")), st("claude-sonnet-5", "high", "plan"))
}

func TestScanIgnoresSidechainsAndPartialLines(t *testing.T) {
	lines := `{"type":"assistant","isSidechain":true,"effort":"low","message":{"model":"claude-haiku-4-5"}}` + "\n" + `{"type":"assist`
	wantState(t, "sidechain", ScanClaude([]byte(lines)), State{})
	if str(ScanClaude([]byte(`{"type":"permission-mode","permissionMode":"manual"}`)).PermissionMode) != "default" {
		t.Error("manual → default")
	}
}

func TestFooter(t *testing.T) {
	for line, mode := range map[string]string{
		"⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent": "auto",
		"⏸ manual mode on · ← 1 agent":                     "default",
		"⏵⏵ accept edits on (shift+tab to cycle)":          "acceptEdits",
		"⏸ plan mode on (shift+tab to cycle)":              "plan",
		"⏵⏵ bypass permissions on (shift+tab to cycle)":    "bypassPermissions",
	} {
		if got, _ := FooterMode("────\n❯\n────\n  0/1.0M\n  " + line + "\n"); got != mode {
			t.Errorf("%q: %q, want %q", line, got, mode)
		}
	}
}

func TestFooterAndBanner(t *testing.T) {
	screen := string(fixture(t, "footer-plan.txt"))
	if m, _ := FooterMode(screen); m != "plan" {
		t.Errorf("mode %q", m)
	}
	b := Banner(screen)
	if b == nil {
		t.Fatal("no banner")
	}
	wantState(t, "banner", *b, st("claude-sonnet-5", "medium", ""))
	if _, ok := FooterMode("no footer here"); ok {
		t.Error("footer in plain text")
	}
	if e, _ := ScreenEffort("   ◐ medium · /effort\n───"); e != "medium" {
		t.Errorf("effort %q", e)
	}
}

func TestMergeFillsGaps(t *testing.T) {
	older := st("claude-opus-5-5", "low", "auto")
	wantState(t, "merged", st("claude-sonnet-5", "", "").Merged(&older), st("claude-sonnet-5", "low", "auto"))
}

func TestCompactSummaryIsNotAUserBubble(t *testing.T) {
	ms := transcript.Parse(fixture(t, "controls-session.jsonl"), transcript.FormatClaude, "", nil)
	var ids []string
	for _, m := range ms {
		ids = append(ids, m.ID)
	}
	if !reflect.DeepEqual(ids, []string{"u1", "a1"}) {
		t.Errorf("ids %v", ids)
	}
}

func TestLastPromptLine(t *testing.T) {
	if l, ok := approval.LastPromptLine("❯ /model sonnet\n  ⎿  Set model\n───\n❯\n───"); !ok || l != "" {
		t.Errorf("%q %v", l, ok)
	}
	if l, _ := approval.LastPromptLine("───\n❯ /effort high\n───\n  /effort  Set effort level"); l != "/effort high" {
		t.Errorf("%q", l)
	}
}

func TestCatalogMatchesContractFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "fixtures", "controls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c api.Controls
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c, Catalog()) {
		t.Errorf("fixture %+v", c)
	}
}

func TestSettingsGuardRestoresExactBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte("{\n  \"model\": \"opus\"\n}\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	g := &SettingsGuard{Path: path}
	snap := g.Snapshot()
	if err := os.WriteFile(path, []byte(`{"model":"sonnet"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if !g.Restore(snap) {
		t.Error("nothing restored")
	}
	if got, _ := os.ReadFile(path); string(got) != string(original) {
		t.Errorf("bytes %q", got)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Errorf("perms %v", info.Mode().Perm())
	}
	if g.Restore(snap) {
		t.Error("restored an unchanged file")
	}
}

// MARK: AgentDriversTests

func TestPiFooter(t *testing.T) {
	check := func(screen string, want *State) {
		t.Helper()
		got := PiFooter(screen)
		if (got == nil) != (want == nil) || (got != nil && !got.Equal(*want)) {
			t.Errorf("PiFooter(%q) = %v, want %v", screen, got, want)
		}
	}
	s := st("openai-codex/gpt-5.6-terra", "low", "")
	check(string(fixture(t, "pi-footer.txt")), &s)
	s2 := st("anthropic/claude-sonnet-5", "", "")
	check("   (anthropic) claude-sonnet-5", &s2)
	s3 := st("openai-codex/gpt-5.6-sol", "off", "")
	check("$0.000 (sub) 0.0%/272k (auto)     (openai-codex) gpt-5.6-sol • thinking off", &s3)
	check("nothing here", nil)
}

func TestPiSessionFile(t *testing.T) {
	wantState(t, "scan", ScanPi(fixture(t, "pi-controls-session.jsonl")), st("anthropic/claude-sonnet-5", "low", ""))
}

func TestPiListAndOrdering(t *testing.T) {
	models := ParsePiList(string(fixture(t, "pi-list-models.txt")))
	if len(models) != 6 || models[5] != (PiModel{Provider: "opencode-go", ID: "plain-model", Thinking: false}) {
		t.Fatalf("models %+v", models)
	}
	settings := PiSettings{DefaultModel: sp("openai-codex/gpt-5.6-sol"), EnabledModels: []string{"claude-sonnet-5", "opencode-go/*"}}
	ordered := piOrdered(models, settings, sp("openai-codex/gpt-5.6-sol"))
	var ids []string
	for _, c := range ordered {
		ids = append(ids, c.ID)
	}
	want := []string{"openai-codex/gpt-5.6-sol", "anthropic/claude-sonnet-5", "opencode-go/gpt-5.5", "opencode-go/plain-model",
		"anthropic/claude-haiku-4-5", "openai-codex/gpt-5.5"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("ordered %v", ids)
	}
	// Same model id under two providers gets the provider in its label.
	if c, _ := choice(ordered, "opencode-go/gpt-5.5"); c.Label != "gpt-5.5 (opencode-go)" {
		t.Errorf("label %q", c.Label)
	}
	if c, _ := choice(ordered, "anthropic/claude-sonnet-5"); c.Label != "claude-sonnet-5" {
		t.Errorf("label %q", c.Label)
	}
	// A current model pi doesn't list still shows up, first.
	if first := piOrdered(models, settings, sp("local/custom"))[0]; first != (api.ControlChoice{ID: "local/custom", Label: "custom"}) {
		t.Errorf("first %+v", first)
	}
}

func TestPiSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"defaultProvider":"openai-codex","defaultModel":"gpt-5.6-sol","defaultThinkingLevel":"high","enabledModels":["sonnet"]}`), 0o600)
	s := (&Catalogs{Run: func([]string) []byte { return nil }, PiSettingsPath: path}).PiSettings()
	if str(s.DefaultModel) != "openai-codex/gpt-5.6-sol" || str(s.DefaultThinking) != "high" || !reflect.DeepEqual(s.EnabledModels, []string{"sonnet"}) {
		t.Errorf("settings %+v", s)
	}
}

func TestPiThinkingLevelsPerModel(t *testing.T) {
	c := &Catalogs{Run: func([]string) []byte { return nil }, PiSettingsPath: "/nonexistent", PiModelsStorePath: filepath.Join("testdata", "pi-models-store.json")}
	for model, want := range map[string][]string{
		"anthropic/claude-fable-5":   {"minimal", "low", "medium", "high", "xhigh", "max"},
		"anthropic/claude-sonnet-5":  {"off", "minimal", "low", "medium", "high", "xhigh", "max"},
		"anthropic/claude-haiku-4-5": {"off", "minimal", "low", "medium", "high"},
		"openai-codex/gpt-5.5":       {"off", "minimal", "low", "medium", "high"},
		"opencode-go/plain-model":    {"off"},
		"unknown/model":              nil,
	} {
		if got := c.PiLevels(model); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %v, want %v", model, got, want)
		}
	}
	// What pi says at runtime wins.
	c.LearnPiLevels("unknown/model", []string{"low", "high"})
	if got := c.PiLevels("unknown/model"); !reflect.DeepEqual(got, []string{"low", "high"}) {
		t.Errorf("learned %v", got)
	}
	got := PiAvailableLevels(` Error: Unknown thinking level "off". Available levels: minimal, low, medium, high, xhigh, max.`)
	if !reflect.DeepEqual(got, []string{"minimal", "low", "medium", "high", "xhigh", "max"}) {
		t.Errorf("available %v", got)
	}
}

func TestCodexFooter(t *testing.T) {
	model, effort, ok := CodexFooter(string(fixture(t, "codex-footer.txt")))
	if !ok || model != "GPT-5.6-Terra" || effort != "high" {
		t.Errorf("footer %q %q %v", model, effort, ok)
	}
	if m, _, _ := CodexFooter("  gpt-5.6-sol high · /x  ⚠ 1 warning"); m != "gpt-5.6-sol" {
		t.Errorf("model %q", m)
	}
}

func TestCodexCatalogAndSlugs(t *testing.T) {
	cat := ParseCodexCatalog(fixture(t, "codex-models.json"))
	var slugs []string
	for _, m := range cat {
		slugs = append(slugs, m.Slug)
	}
	if !reflect.DeepEqual(slugs, []string{"gpt-hidden", "gpt-6-luna", "gpt-5.6-terra", "gpt-5.5"}) { // by priority
		t.Errorf("slugs %v", slugs)
	}
	if cat[0].Visible {
		t.Error("gpt-hidden visible")
	}
	if CodexSlug("GPT-5.6-Terra", cat) != "gpt-5.6-terra" || CodexSlug("gpt-5.6-sol", cat) != "gpt-5.6-sol" {
		t.Error("slugs")
	}
}

func TestCodexTurnContext(t *testing.T) {
	ctx := LastTurnContext(fixture(t, "codex-rollout.jsonl"))
	if ctx == nil {
		t.Fatal("nil")
	}
	at, _ := api.ParseTimestamp("2026-09-24T13:20:05.000Z")
	if str(ctx.Model) != "gpt-5.6-terra" || str(ctx.Effort) != "high" || ctx.Mode != "approveForMe" || ctx.At == nil || !ctx.At.Equal(at) {
		t.Errorf("ctx %+v", ctx)
	}
	full := `{"timestamp":"2026-09-24T13:21:00Z","type":"turn_context","payload":{"model":"m","effort":"low","approvals_reviewer":"user","sandbox_policy":{"type":"danger-full-access"}}}`
	if c := LastTurnContext([]byte(full)); c == nil || c.Mode != "fullAccess" {
		t.Errorf("full access %+v", c)
	}
}

func TestPickerParsing(t *testing.T) {
	p, ok := approval.ParsePicker(string(fixture(t, "codex-model-picker.txt")))
	if !ok {
		t.Fatal("no picker")
	}
	if pickerTitle(p) != "Select Model and Effort" || !reflect.DeepEqual(p.Labels, []string{"GPT-6-Luna", "GPT-5.6-Terra", "GPT-5.6-Luna", "GPT-5.5"}) || p.Cursor != 1 {
		t.Errorf("picker %+v", p)
	}
	sub, ok := approval.ParsePicker("  Advanced Reasoning\n  ⚠ Consumes usage limits faster\n› 1. Max  For difficult problems · higher usage\n  enter default · s session")
	if !ok || !reflect.DeepEqual(sub.Labels, []string{"Max"}) {
		t.Errorf("sub %+v", sub)
	}
	if !titleMatches(sub, "") || titleMatches(sub, "Reasoning Level") {
		t.Error("titleMatches")
	}
	// The input line isn't a picker.
	if _, ok := approval.ParsePicker("› Ask Codex to do anything\n  gpt-5.5 high · /x"); ok {
		t.Error("input line parsed as a picker")
	}
}

func TestEffortLabels(t *testing.T) {
	var labels []string
	for _, c := range effortChoices([]string{"xhigh", "ultra", "off"}) {
		labels = append(labels, c.Label)
	}
	if !reflect.DeepEqual(labels, []string{"Extra high", "Ultra", "Off"}) {
		t.Errorf("labels %v", labels)
	}
}

func TestContractFixturesDecode(t *testing.T) {
	docs := filepath.Join("..", "..", "..", "docs", "fixtures")
	for _, kind := range []string{"claude", "pi", "codex"} {
		data, _ := os.ReadFile(filepath.Join(docs, "agent-controls-"+kind+".json"))
		var c api.AgentControls
		if err := json.Unmarshal(data, &c); err != nil || len(c.Models) == 0 {
			t.Errorf("%s: %v", kind, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(docs, "agents-multi.json"))
	var agents []api.Agent
	if err := json.Unmarshal(data, &agents); err != nil || len(agents) != 2 || agents[0].Kind != "pi" || agents[1].Kind != "codex" {
		t.Errorf("agents %+v %v", agents, err)
	}
}

// R8-17: with no settings file before the command, the one Claude creates for /model is removed.
func TestSettingsGuardRemovesAFileThatDidNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	g := &SettingsGuard{Path: path}
	snap := g.Snapshot()
	os.WriteFile(path, []byte(`{"model":"sonnet"}`), 0o600)
	if !g.Restore(snap) {
		t.Error("nothing restored")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file kept: %v", err)
	}
	if g.Restore(snap) {
		t.Error("restored twice")
	}
}

// R8-17 end to end: a /model switch on a machine without ~/.claude/settings.json leaves none behind.
func TestModelSwitchWithoutSettingsFile(t *testing.T) {
	f := newFakeClaude(t)
	os.Remove(f.settings)
	c, _ := withClaude(t, f)
	mustControl(t, c, "w14:p2", api.ControlModel, "sonnet")
	if _, err := os.Stat(f.settings); !os.IsNotExist(err) {
		t.Errorf("settings file left behind: %v", err)
	}
}
