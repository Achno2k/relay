package controls

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"relay/internal/api"
	"relay/internal/approval"
	"relay/internal/herdr"
	"relay/internal/transcript"
)

// claudeDriver: Claude Code's slash commands and Shift+Tab.
type claudeDriver struct{ c *Controls }

var claudeSupports = api.ControlSupport{Model: true, Effort: true, Mode: true, Compact: true, Clear: true}

func (d claudeDriver) read(_ context.Context, _ herdr.Agent, ref *transcript.Ref, screen *string) State {
	var s State
	if ref != nil {
		s = d.c.transcriptControls(ref)
	}
	if screen != nil {
		if mode, ok := FooterMode(*screen); ok {
			s.PermissionMode = sp(mode)
		}
		s = s.Merged(Banner(*screen))
	}
	return s
}

func (d claudeDriver) label(model string) *string { return ClaudeLabel(model) }

func (d claudeDriver) capabilities(context.Context, herdr.Agent, State) api.AgentControls {
	c := Catalog()
	return api.AgentControls{Models: c.Models, Efforts: c.Efforts, Modes: c.Modes, Supports: claudeSupports}
}

func (d claudeDriver) kindControls() api.AgentControls {
	c := Catalog()
	savedModel, savedEffort := d.c.catalogs.ClaudeDefaults()
	var model *string
	if savedModel != nil {
		for _, m := range c.Models {
			if strings.Contains(strings.ToLower(*savedModel), m.ID) {
				model = sp(m.ID)
				break
			}
		}
	}
	return api.AgentControls{Models: c.Models, Efforts: c.Efforts, Modes: c.Modes, Supports: claudeSupports,
		DefaultModel: model, DefaultEffort: savedEffort}
}

func (d claudeDriver) launchArgs(model, effort string) []string {
	var out []string
	if model != "" {
		out = append(out, "--model", model)
	}
	if effort != "" {
		out = append(out, "--effort", effort)
	}
	return out
}

func (d claudeDriver) agentModel(id string) string {
	if m, ok := choice(claudeCatalog.Models, id); ok {
		if full := ClaudeID(m.Label); full != nil {
			return *full
		}
	}
	return id
}

// apply: values are already validated.
func (d claudeDriver) apply(ctx context.Context, r api.ControlRequest, a herdr.Agent, _ State) error {
	c := d.c
	switch r.Kind {
	case api.ControlModel:
		m, ok := choice(claudeCatalog.Models, r.Value)
		if !ok {
			return api.BadRequest("unknown model " + r.Value)
		}
		if err := c.slashSetting(ctx, a, "/model "+r.Value, "Set model to "+m.Label); err != nil {
			return err
		}
		c.Hold(a.PaneID, State{Model: ClaudeID(m.Label)})
	case api.ControlEffort:
		if !claudeEffort(r.Value) {
			return api.BadRequest("unknown effort " + r.Value)
		}
		if err := c.slashSetting(ctx, a, "/effort "+r.Value, "Set effort level to "+r.Value); err != nil {
			return err
		}
		c.Hold(a.PaneID, State{Effort: sp(r.Value)})
	case api.ControlPermissionMode:
		if _, ok := choice(claudeCatalog.Modes, r.Value); !ok && r.Value != "dontAsk" {
			return api.BadRequest("unknown mode " + r.Value)
		}
		return c.cycleMode(ctx, a, r.Value)
	case api.ControlCompact:
		ref := c.d.Locate(a)
		var start int64
		if ref != nil {
			start = fileSize(ref.Path)
		}
		s, err := c.screen(ctx, a)
		if err != nil {
			return err
		}
		compactedBefore := count("Compacted", s)
		if err := c.slash(ctx, a, "/compact"); err != nil {
			return err
		}
		return waitFor(ctx, compactTimeout, "/compact", func() (bool, error) {
			if ref != nil {
				tail := readFrom(ref.Path, start)
				return strings.Contains(tail, `"compact_boundary"`) || strings.Contains(tail, "Compacted"), nil
			}
			s, err := c.screen(ctx, a)
			if err != nil {
				return false, err
			}
			return count("Compacted", s) > compactedBefore, nil
		})
	case api.ControlClear:
		before := sessionValue(a)
		if err := c.slash(ctx, a, "/clear"); err != nil {
			return err
		}
		c.dropConfirmed(a.PaneID)
		return waitFor(ctx, clearTimeout, "/clear", func() (bool, error) {
			now, err := c.herdr.Agent(ctx, a.PaneID)
			if err != nil {
				return false, err
			}
			v := sessionValue(now)
			return v != nil && !sameP(v, before), nil
		})
	}
	return nil
}

// MARK: Slash commands

// slash submits a slash command into an empty input box with herdr's agent.prompt (text and Enter
// as one ordered write). If the command is still sitting on the last `❯` line afterwards (Claude
// can drop an Enter while re-rendering, seen live after /model), it presses Enter again.
func (c *Controls) slash(ctx context.Context, a herdr.Agent, command string) error {
	if err := c.d.ClearInput(ctx, a.PaneID, 0); err != nil {
		return err
	}
	if err := c.herdr.Prompt(ctx, a.PaneID, command); err != nil {
		return err
	}
	for attempt := range 4 {
		for range 6 {
			if err := sleep(ctx, 150*time.Millisecond); err != nil {
				return err
			}
			screen, err := c.screen(ctx, a)
			if err != nil {
				return err
			}
			// A dialog (e.g. "Switch model?") means the command was taken.
			if dialog(screen) != nil {
				return nil
			}
			line, ok := approval.LastPromptLine(screen)
			if !ok || line != command {
				return nil
			}
		}
		if attempt < 3 {
			if err := c.keys(ctx, a, "enter"); err != nil {
				return err
			}
		}
	}
	return timeoutError(command + " stayed in the input box")
}

// slashSetting runs /model x or /effort x, confirmed by Claude's `Set … to <value>` output, then
// puts the saved default back.
func (c *Controls) slashSetting(ctx context.Context, a herdr.Agent, command, confirm string) error {
	c.settingsLock.Lock()
	defer c.settingsLock.Unlock()
	return c.slashSettingLocked(ctx, a, command, confirm)
}

func (c *Controls) slashSettingLocked(ctx context.Context, a herdr.Agent, command, confirm string) error {
	ref := c.d.Locate(a)
	var start int64
	if ref != nil {
		start = fileSize(ref.Path)
	}
	saved := c.settings.Snapshot()
	before, _ := c.screen(ctx, a)
	keptBefore := keptCount(before)
	defer func() {
		c.settings.Restore(saved)
		// Claude may write the file again a moment later.
		go func() {
			time.Sleep(time.Second)
			c.settings.Restore(saved)
		}()
	}()
	if err := c.slash(ctx, a, command); err != nil {
		return err
	}
	// The transcript quotes the value in backticks (Set model to `Sonnet 5`); the screen doesn't.
	mentions := func(text string) bool { return strings.Contains(strings.ReplaceAll(text, "`", ""), confirm) }
	answered := false
	err := waitFor(ctx, controlTimeout, command, func() (bool, error) {
		tail := ""
		if ref != nil {
			tail = readFrom(ref.Path, start)
		}
		if mentions(tail) {
			return true, nil
		}
		screen, err := c.screen(ctx, a)
		if err != nil {
			return false, err
		}
		// Claude kept the old value (its confirmation was answered No): a refusal, not a timeout.
		// Only new output counts: the transcript bytes written since the command, or on screen
		// (no transcript) more "Kept" lines than before.
		if strings.HasPrefix(command, "/model") {
			if ref != nil {
				if k, ok := kept(tail, command); ok {
					return false, api.NewError(http.StatusConflict, "control_refused", "Claude kept "+k)
				}
			}
			if ref == nil && keptCount(screen) > keptBefore {
				if k, ok := kept(screen, command); ok {
					return false, api.NewError(http.StatusConflict, "control_refused", "Claude kept "+k)
				}
			}
		}
		if d := dialog(screen); d != nil {
			// e.g. "Switch model? … ❯ 1. Yes, switch to Sonnet 5 / 2. No, go back" on a long, cached
			// conversation. herdr doesn't always report it as blocked.
			var yes *api.ApprovalOption
			for i := range d.Options {
				if strings.HasPrefix(strings.ToLower(d.Options[i].Label), "yes") {
					yes = &d.Options[i]
					break
				}
			}
			if yes == nil {
				if err := c.keys(ctx, a, "esc"); err != nil {
					return false, err
				}
				return false, api.NewError(http.StatusBadRequest, "unsupported", `Claude asked "`+d.Question+`" for `+command)
			}
			if !answered {
				answered = true
				if err := c.keys(ctx, a, yes.Keys...); err != nil {
					return false, err
				}
			}
			return false, nil
		}
		// Stale output of the same value would only confirm what's already true.
		return mentions(screen), nil
	})
	var e *api.Error
	if errors.As(err, &e) && e.Code == "control_timeout" {
		// Never leave a dialog of ours open.
		if screen, serr := c.screen(ctx, a); serr == nil && dialog(screen) != nil {
			_ = c.keys(ctx, a, "esc")
		}
	}
	return err
}

// kept is "Kept model as Opus 5.5" printed after command (on screen or in the new transcript bytes).
func kept(text, command string) (string, bool) {
	clean := strings.ReplaceAll(text, "`", "")
	region := clean
	if i := strings.LastIndex(clean, command); i >= 0 {
		region = clean[i+len(command):]
	}
	i := strings.Index(region, "Kept model as ")
	if i < 0 {
		return "", false
	}
	rest := region[i+len("Kept model as "):]
	if j := strings.IndexAny(rest, "\n<"); j >= 0 {
		rest = rest[:j]
	}
	v := trimBlank(rest)
	return v, v != ""
}

func keptCount(screen string) int { return strings.Count(screen, "Kept model as ") }

var dialogRowRe = regexp.MustCompile(`^❯\s*\d{1,2}\.\s+.+$`)

// dialog is a live menu that has replaced the input box: its `❯` cursor sits on a numbered option
// among the last few lines. Numbered lists in the conversation (or an old "⎿ Interrupted" line)
// never have the cursor, and the screen can briefly lack an input box while it redraws.
func dialog(screen string) *api.Approval {
	if _, ok := approval.InputBoxContent(screen); ok {
		return nil
	}
	var tail []string
	for _, l := range strings.Split(screen, "\n") {
		if t := trimBlank(l); t != "" {
			tail = append(tail, t)
		}
	}
	if len(tail) > 8 {
		tail = tail[len(tail)-8:]
	}
	hasCursor := false
	for _, l := range tail {
		if dialogRowRe.MatchString(l) {
			hasCursor = true
			break
		}
	}
	if !hasCursor {
		return nil
	}
	return approval.Parse(strings.Join(tail, "\n"), "", transcript.NewScrubber(""), "", "")
}

// MARK: Permission mode

// cycleMode presses Shift+Tab until the footer shows target, at most one full cycle.
func (c *Controls) cycleMode(ctx context.Context, a herdr.Agent, target string) error {
	current := func() (string, error) {
		s, err := c.screen(ctx, a)
		if err != nil {
			return "", err
		}
		m, ok := FooterMode(s)
		if !ok {
			return "", timeoutError("can't see the permission mode in the footer")
		}
		return m, nil
	}
	start, err := current()
	if err != nil {
		return err
	}
	if start == target {
		return nil
	}
	seen := []string{start}
	for range 8 {
		// One press per call: two presses in one send_keys only move one step.
		if err := c.keys(ctx, a, "shift+tab"); err != nil {
			return err
		}
		mode := seen[len(seen)-1]
		for range 10 {
			if err := sleep(ctx, 150*time.Millisecond); err != nil {
				return err
			}
			if mode, err = current(); err != nil {
				return err
			}
			if mode != seen[len(seen)-1] {
				break
			}
		}
		if mode == target {
			return nil
		}
		for _, s := range seen {
			if s == mode {
				return api.NewError(http.StatusBadRequest, "unsupported",
					target+" isn't in this agent's Shift+Tab cycle ("+strings.Join(seen, " → ")+")")
			}
		}
		seen = append(seen, mode)
	}
	return timeoutError("mode never reached " + target)
}
