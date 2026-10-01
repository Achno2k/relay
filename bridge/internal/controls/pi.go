package controls

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/transcript"
)

// piDriver: `/model <provider>/<id>` and `/thinking <level>` switch directly (session only; pi
// saves a default only on Ctrl+S). The footer's right side is `(<provider>) <id> • <thinking>`.
type piDriver struct{ c *Controls }

var piFooterRe = regexp.MustCompile(`\(([\w.\-]+)\)\s+([\w.:\-]+)(?:\s+•\s+(?:thinking\s+)?([a-z]+))?\s*$`)

func piEffort(e string) bool {
	for _, l := range piAllLevels {
		if l == e {
			return true
		}
	}
	return false
}

// PiFooter reads model and thinking from pi's footer (`• high`, or `• thinking off`).
func PiFooter(screen string) *State {
	lines := lastLines(screen, 8)
	for i := len(lines) - 1; i >= 0; i-- {
		m := piFooterRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		s := State{Model: sp(m[1] + "/" + m[2])}
		if m[3] != "" && piEffort(m[3]) {
			s.Effort = sp(m[3])
		}
		return &s
	}
	return nil
}

// PiAvailableLevels reads `Error: Unknown thinking level "off". Available levels: minimal, low, …`.
func PiAvailableLevels(line string) []string {
	i := strings.Index(line, "Available levels:")
	if i < 0 {
		return nil
	}
	var out []string
	for _, p := range strings.Split(strings.Trim(line[i+len("Available levels:"):], " ."), ",") {
		if p = trimBlank(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ScanPi is the last model_change / thinking_level_change in a pi session file.
func ScanPi(data []byte) State {
	var s State
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var o map[string]any
		if json.Unmarshal(line, &o) != nil {
			continue
		}
		switch o["type"] {
		case "model_change":
			p, ok1 := o["provider"].(string)
			m, ok2 := o["modelId"].(string)
			if ok1 && ok2 {
				s.Model = sp(p + "/" + m)
			}
		case "thinking_level_change":
			if t, ok := o["thinkingLevel"].(string); ok {
				s.Effort = sp(t)
			}
		}
	}
	return s
}

func (d piDriver) read(_ context.Context, _ herdr.Agent, ref *transcript.Ref, screen *string) State {
	var file State
	if ref != nil {
		file = ScanPi(readTail(ref.Path))
	}
	var s State
	if screen != nil {
		if f := PiFooter(*screen); f != nil {
			s = *f
		}
	}
	return s.Merged(&file)
}

func (d piDriver) label(model string) *string {
	parts := strings.SplitN(model, "/", 2)
	return sp(parts[len(parts)-1])
}

// piOrdered is the saved default and scoped models first, then everything else in --list-models
// order.
func piOrdered(models []PiModel, settings PiSettings, current *string) []api.ControlChoice {
	matches := func(m PiModel, pattern string) bool {
		p := pattern
		if parts := split(pattern, ":"); len(parts) > 0 {
			p = parts[0]
		}
		if strings.HasSuffix(p, "*") {
			pre := strings.TrimSuffix(p, "*")
			return strings.HasPrefix(m.Full(), pre) || strings.HasPrefix(m.ID, pre)
		}
		return m.Full() == p || m.ID == p
	}
	contains := func(list []PiModel, m PiModel) bool {
		for _, x := range list {
			if x == m {
				return true
			}
		}
		return false
	}
	var first []PiModel
	if settings.DefaultModel != nil {
		for _, m := range models {
			if m.Full() == *settings.DefaultModel {
				first = append(first, m)
				break
			}
		}
	}
	for _, p := range settings.EnabledModels {
		for _, m := range models {
			if matches(m, p) && !contains(first, m) {
				first = append(first, m)
			}
		}
	}
	ordered := append([]PiModel(nil), first...)
	for _, m := range models {
		if !contains(first, m) {
			ordered = append(ordered, m)
		}
	}
	ids := map[string]int{}
	for _, m := range models {
		ids[m.ID]++
	}
	out := make([]api.ControlChoice, 0, len(ordered)+1)
	for _, m := range ordered {
		label := m.ID
		if ids[m.ID] > 1 {
			label = m.ID + " (" + m.Provider + ")"
		}
		out = append(out, api.ControlChoice{ID: m.Full(), Label: label})
	}
	if current != nil {
		if _, ok := choice(out, *current); !ok {
			label := *current
			if p := split(*current, "/"); len(p) > 0 {
				label = p[len(p)-1]
			}
			out = append([]api.ControlChoice{{ID: *current, Label: label}}, out...)
		}
	}
	return out
}

func piThinking(models []PiModel, id string) bool {
	for _, m := range models {
		if m.Full() == id {
			return m.Thinking
		}
	}
	return true
}

func (d piDriver) capabilities(_ context.Context, _ herdr.Agent, current State) api.AgentControls {
	cat := d.c.catalogs
	models := cat.Pi()
	var levels []string
	if current.Model != nil {
		levels = cat.PiLevels(*current.Model)
	}
	if levels == nil {
		// Per model: e.g. claude-fable-5 has no "off", gpt-5.6-sol has off…max.
		if current.Model == nil || piThinking(models, *current.Model) {
			levels = piAllLevels
		} else {
			levels = []string{"off"}
		}
	}
	return api.AgentControls{
		Models:   piOrdered(models, cat.PiSettings(), current.Model),
		Efforts:  effortChoices(levels),
		Modes:    []api.ControlChoice{},
		Supports: api.ControlSupport{Model: len(models) > 0 || current.Model != nil, Effort: len(levels) > 1, Mode: false, Compact: true, Clear: true},
	}
}

func (d piDriver) kindControls() api.AgentControls {
	cat := d.c.catalogs
	models := cat.Pi()
	settings := cat.PiSettings()
	levels := func(id string) []string {
		if l := cat.PiLevels(id); l != nil {
			return l
		}
		if piThinking(models, id) {
			return piAllLevels
		}
		return []string{"off"}
	}
	ordered := piOrdered(models, settings, nil)
	byModel := map[string][]api.ControlChoice{}
	for _, m := range ordered {
		byModel[m.ID] = effortChoices(levels(m.ID))
	}
	efforts := effortChoices(piAllLevels)
	if settings.DefaultModel != nil {
		efforts = effortChoices(levels(*settings.DefaultModel))
	}
	return api.AgentControls{
		Models: ordered, Efforts: efforts, Modes: []api.ControlChoice{},
		Supports:     api.ControlSupport{Model: len(models) > 0, Effort: true, Mode: false, Compact: true, Clear: true},
		DefaultModel: settings.DefaultModel, DefaultEffort: settings.DefaultThinking, EffortsByModel: byModel,
	}
}

func (d piDriver) launchArgs(model, effort string) []string {
	if model != "" {
		if effort != "" {
			return []string{"--model", model + ":" + effort}
		}
		return []string{"--model", model}
	}
	if effort != "" {
		return []string{"--thinking", effort}
	}
	return nil
}

func (d piDriver) agentModel(id string) string { return id }

func (d piDriver) apply(ctx context.Context, r api.ControlRequest, a herdr.Agent, current State) error {
	c := d.c
	switch r.Kind {
	case api.ControlModel:
		id := r.Value
		if err := c.submit(ctx, a, "/model "+id); err != nil {
			return err
		}
		if err := waitFor(ctx, controlTimeout, "/model "+id, func() (bool, error) {
			s, err := c.screen(ctx, a)
			if err != nil {
				return false, err
			}
			f := PiFooter(s)
			return f != nil && eqp(f.Model, id), nil
		}); err != nil {
			return err
		}
		// Switching model resets thinking to that model's default; the footer shows it.
		c.Hold(a.PaneID, State{Model: sp(id)})
	case api.ControlEffort:
		level := r.Value
		before, err := c.screen(ctx, a)
		if err != nil {
			return err
		}
		const unknown = "Unknown thinking level"
		if err := c.submit(ctx, a, "/thinking "+level); err != nil {
			return err
		}
		if err := waitFor(ctx, controlTimeout, "/thinking "+level, func() (bool, error) {
			s, err := c.screen(ctx, a)
			if err != nil {
				return false, err
			}
			if count(unknown, s) > count(unknown, before) {
				if line, ok := lastLineContaining(s, unknown); ok {
					available := PiAvailableLevels(line)
					if current.Model != nil && len(available) > 0 {
						c.catalogs.LearnPiLevels(*current.Model, available)
					}
					name := "this model"
					if current.Model != nil {
						name = *d.label(*current.Model)
					}
					return false, api.NewError(http.StatusBadRequest, "unsupported",
						name+" doesn't support thinking "+level+"; available: "+strings.Join(available, ", "))
				}
			}
			f := PiFooter(s)
			return f != nil && eqp(f.Effort, level), nil
		}); err != nil {
			return err
		}
		c.Hold(a.PaneID, State{Effort: sp(level)})
	case api.ControlCompact:
		ref := c.d.Locate(a)
		var start int64
		if ref != nil {
			start = fileSize(ref.Path)
		}
		before, err := c.screen(ctx, a)
		if err != nil {
			return err
		}
		const failed = "Compaction failed"
		if err := c.submit(ctx, a, "/compact"); err != nil {
			return err
		}
		return waitFor(ctx, compactTimeout, "/compact", func() (bool, error) {
			if ref != nil && strings.Contains(readFrom(ref.Path, start), `"compaction"`) {
				return true, nil
			}
			// Only a new failure line counts (e.g. "Nothing to compact (session too small)").
			s, err := c.screen(ctx, a)
			if err != nil {
				return false, err
			}
			if count(failed, s) > count(failed, before) {
				if line, ok := lastLineContaining(s, failed); ok {
					return false, controlFailed(a, trimBlank(line))
				}
			}
			return false, nil
		})
	case api.ControlClear:
		before := sessionValue(a)
		if err := c.submit(ctx, a, "/new"); err != nil {
			return err
		}
		c.dropConfirmed(a.PaneID)
		return waitFor(ctx, controlTimeout, "/new", func() (bool, error) {
			now, err := c.herdr.Agent(ctx, a.PaneID)
			if err != nil {
				return false, err
			}
			v := sessionValue(now)
			return v != nil && !sameP(v, before), nil
		})
	case api.ControlPermissionMode:
		return api.NewError(http.StatusBadRequest, "unsupported", "pi has no permission modes")
	}
	return nil
}
