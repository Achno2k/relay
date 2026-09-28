package controls

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"relay/internal/api"
	"relay/internal/approval"
	"relay/internal/herdr"
	"relay/internal/transcript"
)

// codexDriver: no direct commands, so the bridge drives /model (model, then reasoning level,
// applied with `s` = "for this session only") and /permissions. The footer reads
// `<Model> <effort> · <cwd>`.
type codexDriver struct{ c *Controls }

var codexModes = []api.ControlChoice{
	{ID: "ask", Label: "Ask for approval"},
	{ID: "approveForMe", Label: "Approve for me"},
	{ID: "fullAccess", Label: "Full Access"},
}

var codexFooterRe = regexp.MustCompile(`^\s*(\S+)\s+(none|minimal|low|medium|high|xhigh|max|ultra)\s+·`)

// CodexFooter reads `GPT-5.6-Terra high · <cwd>` as (token, effort). The token is a display name
// or a slug.
func CodexFooter(screen string) (model, effort string, ok bool) {
	lines := lastLines(screen, 6)
	for i := len(lines) - 1; i >= 0; i-- {
		if m := codexFooterRe.FindStringSubmatch(lines[i]); m != nil {
			return m[1], m[2], true
		}
	}
	return "", "", false
}

// CodexSlug maps a footer token to a catalogue slug.
func CodexSlug(token string, catalog []CodexModel) string {
	lower := strings.ToLower(token)
	for _, m := range catalog {
		if strings.ToLower(m.DisplayName) == lower || m.Slug == lower {
			return m.Slug
		}
	}
	return lower
}

type TurnContext struct {
	Model  *string
	Effort *string
	Mode   string
	At     *time.Time
}

// LastTurnContext is the last `turn_context` in a rollout.
func LastTurnContext(data []byte) *TurnContext {
	var out *TurnContext
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) <= 20 {
			continue
		}
		var o map[string]any
		if json.Unmarshal(line, &o) != nil || o["type"] != "turn_context" {
			continue
		}
		p, ok := o["payload"].(map[string]any)
		if !ok {
			continue
		}
		var sandbox string
		if sp, ok := p["sandbox_policy"].(map[string]any); ok {
			sandbox, _ = sp["type"].(string)
		}
		mode := "ask"
		if sandbox == "danger-full-access" {
			mode = "fullAccess"
		} else if r, _ := p["approvals_reviewer"].(string); r == "auto_review" {
			mode = "approveForMe"
		}
		ctx := &TurnContext{Model: strp(p["model"]), Effort: strp(p["effort"]), Mode: mode}
		if ts, ok := o["timestamp"].(string); ok {
			if t, ok := api.ParseTimestamp(ts); ok {
				ctx.At = &t
			}
		}
		out = ctx
	}
	return out
}

func codexEntry(catalog []CodexModel, slug *string) (CodexModel, bool) {
	if slug == nil {
		return CodexModel{}, false
	}
	for _, m := range catalog {
		if m.Slug == *slug && m.Visible {
			return m, true
		}
	}
	return CodexModel{}, false
}

func codexName(catalog []CodexModel, slug string) string {
	for _, m := range catalog {
		if m.Slug == slug {
			return m.DisplayName
		}
	}
	return slug
}

func codexVisible(catalog []CodexModel) []api.ControlChoice {
	out := []api.ControlChoice{}
	for _, m := range catalog {
		if m.Visible {
			out = append(out, api.ControlChoice{ID: m.Slug, Label: m.DisplayName})
		}
	}
	return out
}

func (d codexDriver) read(_ context.Context, a herdr.Agent, _ *transcript.Ref, screen *string) State {
	catalog := d.c.catalogs.Codex()
	var s State
	var ctx *TurnContext
	if a.AgentSession != nil {
		if path, ok := d.c.catalogs.CodexRollout(a.AgentSession.Value); ok {
			ctx = LastTurnContext(readTail(path))
		}
	}
	if screen != nil {
		if model, effort, ok := CodexFooter(*screen); ok {
			s.Model = sp(CodexSlug(model, catalog))
			s.Effort = sp(effort)
		} else if ctx != nil {
			s.Model, s.Effort = ctx.Model, ctx.Effort
		}
	} else if ctx != nil {
		s.Model, s.Effort = ctx.Model, ctx.Effort
	}
	// turn_context is only written per turn; a mode the bridge just set wins until a newer turn.
	var newer *time.Time
	if ctx != nil {
		newer = ctx.At
	}
	s.PermissionMode = d.c.stickyModeFor(a.PaneID, sessionValue(a), newer)
	if s.PermissionMode == nil && ctx != nil {
		s.PermissionMode = sp(ctx.Mode)
	}
	return s
}

func (d codexDriver) label(model string) *string {
	return sp(codexName(d.c.catalogs.Codex(), model))
}

func (d codexDriver) capabilities(_ context.Context, _ herdr.Agent, current State) api.AgentControls {
	catalog := d.c.catalogs.Codex()
	models := codexVisible(catalog)
	if current.Model != nil {
		if _, ok := choice(models, *current.Model); !ok {
			models = append([]api.ControlChoice{{ID: *current.Model, Label: codexName(catalog, *current.Model)}}, models...)
		}
	}
	// The effort step needs the current model's row in the picker.
	entry, _ := codexEntry(catalog, current.Model)
	efforts := effortChoices(entry.Efforts)
	anyVisible := slices.ContainsFunc(catalog, func(m CodexModel) bool { return m.Visible })
	return api.AgentControls{Models: models, Efforts: efforts, Modes: codexModes,
		Supports: api.ControlSupport{Model: anyVisible, Effort: len(efforts) > 0, Mode: true, Compact: true, Clear: true}}
}

func (d codexDriver) kindControls() api.AgentControls {
	catalog := d.c.catalogs.Codex()
	savedModel, savedEffort := d.c.catalogs.CodexDefaults()
	models := codexVisible(catalog)
	if savedModel != nil {
		if _, ok := choice(models, *savedModel); !ok {
			models = append([]api.ControlChoice{{ID: *savedModel, Label: codexName(catalog, *savedModel)}}, models...)
		}
	}
	byModel := map[string][]api.ControlChoice{}
	for _, m := range catalog {
		if m.Visible {
			byModel[m.Slug] = effortChoices(m.Efforts)
		}
	}
	efforts := []api.ControlChoice{}
	if savedModel != nil {
		if e, ok := byModel[*savedModel]; ok {
			efforts = e
		}
	}
	return api.AgentControls{Models: models, Efforts: efforts, Modes: codexModes,
		Supports:     api.ControlSupport{Model: len(models) > 0, Effort: true, Mode: true, Compact: true, Clear: true},
		DefaultModel: savedModel, DefaultEffort: savedEffort, EffortsByModel: byModel}
}

func (d codexDriver) launchArgs(model, effort string) []string {
	var out []string
	if model != "" {
		out = append(out, "-m", model)
	}
	if effort != "" {
		out = append(out, "-c", `model_reasoning_effort="`+effort+`"`)
	}
	return out
}

func (d codexDriver) agentModel(id string) string { return id }

func (d codexDriver) apply(ctx context.Context, r api.ControlRequest, a herdr.Agent, current State) error {
	c := d.c
	catalog := c.catalogs.Codex()
	switch r.Kind {
	case api.ControlModel:
		entry, ok := codexEntry(catalog, &r.Value)
		if !ok {
			return api.NewError(http.StatusBadRequest, "unsupported", r.Value+" isn't in codex's /model picker, so it can't be selected")
		}
		effort := "medium"
		switch {
		case current.Effort != nil && slices.Contains(entry.Efforts, *current.Effort):
			effort = *current.Effort
		case entry.DefaultEffort != nil:
			effort = *entry.DefaultEffort
		case len(entry.Efforts) > 0:
			effort = entry.Efforts[0]
		}
		return d.pickModel(ctx, a, entry, effort)
	case api.ControlEffort:
		entry, ok := codexEntry(catalog, current.Model)
		if !ok {
			return api.NewError(http.StatusBadRequest, "unsupported", "the current model isn't in codex's /model picker, so its effort can't be changed")
		}
		return d.pickModel(ctx, a, entry, r.Value)
	case api.ControlPermissionMode:
		m, _ := choice(codexModes, r.Value)
		label := m.Label
		before, err := c.screen(ctx, a)
		if err != nil {
			return err
		}
		requested := "Permission selection requested: " + label
		if err := c.submit(ctx, a, "/permissions"); err != nil {
			return err
		}
		if _, err := c.waitForPicker(ctx, a, "Permissions"); err != nil {
			return err
		}
		// Already in that mode: codex prints nothing when you pick the current row.
		s, err := c.screen(ctx, a)
		if err != nil {
			return err
		}
		if strings.Contains(s, label+" (current)") {
			if err := c.keys(ctx, a, "esc"); err != nil {
				return err
			}
			c.setStickyMode(a.PaneID, sp(r.Value), sessionValue(a))
			return nil
		}
		if err := c.drivePicker(ctx, a, "Permissions", func(l string) bool { return l == label }, "enter", label); err != nil {
			return err
		}
		if err := waitFor(ctx, controlTimeout, "/permissions "+label, func() (bool, error) {
			s, err := c.screen(ctx, a)
			if err != nil {
				return false, err
			}
			// Full Access can ask to confirm first.
			if p, ok := approval.ParsePicker(s); ok && !(p.Title != nil && strings.Contains(*p.Title, "Permissions")) {
				for _, l := range p.Labels {
					if strings.HasPrefix(l, "Yes") || strings.HasPrefix(l, "Continue") || strings.HasPrefix(l, "Allow") {
						yes := l
						return false, c.drivePicker(ctx, a, pickerTitle(p), func(x string) bool { return x == yes }, "enter", yes)
					}
				}
			}
			return count(requested, s) > count(requested, before), nil
		}); err != nil {
			return err
		}
		c.setStickyMode(a.PaneID, sp(r.Value), sessionValue(a))
	case api.ControlCompact:
		before, err := c.screen(ctx, a)
		if err != nil {
			return err
		}
		const done = "Context compacted"
		if err := c.submit(ctx, a, "/compact"); err != nil {
			return err
		}
		return c.waitForScreen(ctx, a, before, compactTimeout, "/compact", func(s string) bool {
			return count(done, s) > count(done, before)
		})
	case api.ControlClear:
		before, err := c.screen(ctx, a)
		if err != nil {
			return err
		}
		const resume = "To continue this session, run codex resume"
		if err := c.submit(ctx, a, "/new"); err != nil {
			return err
		}
		// "Where should the new conversation run?" appears in git checkouts; without it, /new went
		// straight through.
		_ = c.drivePicker(ctx, a, "Where should the new conversation run",
			func(l string) bool { return strings.HasPrefix(l, "Current checkout") }, "enter", "Current checkout")
		c.dropConfirmed(a.PaneID)
		c.setStickyMode(a.PaneID, nil, nil)
		return c.waitForScreen(ctx, a, before, controlTimeout, "/new", func(s string) bool {
			return count(resume, s) > count(resume, before)
		})
	}
	return nil
}

func (d codexDriver) pickModel(ctx context.Context, a herdr.Agent, entry CodexModel, effort string) error {
	c := d.c
	before, err := c.screen(ctx, a)
	if err != nil {
		return err
	}
	if err := d.choose(ctx, a, entry, effort); err != nil {
		// Never leave a picker open: a later Enter would commit it as the saved default.
		if _, open, serr := c.picker(ctx, a); serr != nil {
			return serr
		} else if open {
			_ = c.keys(ctx, a, "esc")
			_ = sleep(ctx, 200*time.Millisecond)
			if _, open, serr := c.picker(ctx, a); serr != nil {
				return serr
			} else if open {
				_ = c.keys(ctx, a, "esc")
			}
		}
		return err
	}
	// The footer shows the display name (or slug) and effort once applied.
	if err := c.waitForScreen(ctx, a, before, controlTimeout, "/model "+entry.Slug+" "+effort, func(s string) bool {
		model, e, ok := CodexFooter(s)
		if !ok {
			return false
		}
		lower := strings.ToLower(model)
		return (lower == strings.ToLower(entry.DisplayName) || lower == entry.Slug) && e == effort
	}); err != nil {
		return err
	}
	c.Hold(a.PaneID, State{Model: sp(entry.Slug), Effort: sp(effort)})
	return nil
}

func (d codexDriver) choose(ctx context.Context, a herdr.Agent, entry CodexModel, effort string) error {
	c := d.c
	if err := c.submit(ctx, a, "/model"); err != nil {
		return err
	}
	if err := c.drivePicker(ctx, a, "Select Model", func(l string) bool { return l == entry.DisplayName }, "enter", entry.DisplayName); err != nil {
		return err
	}
	label := effortLabel(effort)
	p, err := c.waitForPicker(ctx, a, "Reasoning Level")
	if err != nil {
		return err
	}
	if slices.Contains(p.Labels, label) {
		return c.drivePicker(ctx, a, "Reasoning Level", func(l string) bool { return l == label }, "s", label)
	}
	// Max and Ultra sit behind "More reasoning…".
	if err := c.drivePicker(ctx, a, "Reasoning Level", func(l string) bool { return strings.HasPrefix(l, "More reasoning") }, "enter", "More reasoning"); err != nil {
		return err
	}
	// Wait for the submenu itself (the old picker can linger for a frame).
	for range 30 {
		if err := sleep(ctx, 100*time.Millisecond); err != nil {
			return err
		}
		sub, ok, err := c.picker(ctx, a)
		if err != nil {
			return err
		}
		if ok && slices.Contains(sub.Labels, label) {
			break
		}
	}
	return c.drivePicker(ctx, a, "", func(l string) bool { return l == label }, "s", label)
}
