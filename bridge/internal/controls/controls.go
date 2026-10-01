// Package controls reads and changes an agent's model, effort and permission mode by driving the
// agent's own UI (slash commands, Shift+Tab, pickers) and waiting until the change shows up in the
// transcript or on screen. One driver per kind: claude, pi, codex.
package controls

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/transcript"
)

var (
	controlTimeout = 10 * time.Second
	compactTimeout = 120 * time.Second
	// herdr can take 10+ s to report the new session id after /clear.
	clearTimeout = 30 * time.Second
	// poll is waitFor's interval.
	poll = 250 * time.Millisecond
)

const controlTailBytes = 1 << 20

// maxControlCache bounds the per-transcript control cache (R8-19).
const maxControlCache = 256

// Deps is what the service passes in, so controls never imports it.
type Deps struct {
	Herdr    herdr.Client
	Catalogs *Catalogs      // nil = NewCatalogs()
	Settings *SettingsGuard // nil = NewSettingsGuard()
	// Kind is herdr's kind, else the kind POST /agents just started; "" = unknown.
	Kind   func(herdr.Agent) string
	Locate func(herdr.Agent) *transcript.Ref
	// ClearInput empties the agent's input box (service.clearInput) before every slash command, so
	// a command never glues onto leftover text.
	ClearInput func(ctx context.Context, id string, wait time.Duration) error
	// OnChange runs after a control applied, so the monitor can push agent.updated at once.
	OnChange func()
}

// Controls keeps the per-agent control state the Swift bridge kept on AgentService.
type Controls struct {
	d            Deps
	herdr        herdr.Client
	catalogs     *Catalogs
	settings     *SettingsGuard
	settingsLock sync.Mutex // one /model or /effort at a time: overlapping calls would restore each other's changes

	mu sync.Mutex
	// Values a control just confirmed on screen, held until the transcript catches up.
	confirmed map[string]held
	last      map[string]State
	// A mode the bridge set on an agent whose files only catch up on the next turn (codex).
	sticky       map[string]stickyMode
	controlCache map[string]cachedControls
}

type held struct {
	state State
	until time.Time
}

type stickyMode struct {
	mode    string
	at      time.Time
	session *string
}

type cachedControls struct {
	size  int64
	mtime time.Time
	state State
}

func New(d Deps) *Controls {
	if d.Catalogs == nil {
		d.Catalogs = NewCatalogs()
	}
	if d.Settings == nil {
		d.Settings = NewSettingsGuard()
	}
	if d.Kind == nil {
		d.Kind = func(a herdr.Agent) string {
			if a.Agent != "" {
				return a.Agent
			}
			if a.AgentSession != nil {
				return a.AgentSession.Agent
			}
			return ""
		}
	}
	if d.Locate == nil {
		d.Locate = func(herdr.Agent) *transcript.Ref { return nil }
	}
	if d.ClearInput == nil {
		d.ClearInput = func(context.Context, string, time.Duration) error { return nil }
	}
	if d.OnChange == nil {
		d.OnChange = func() {}
	}
	return &Controls{d: d, herdr: d.Herdr, catalogs: d.Catalogs, settings: d.Settings,
		confirmed: map[string]held{}, last: map[string]State{}, sticky: map[string]stickyMode{},
		controlCache: map[string]cachedControls{}}
}

func (c *Controls) Catalogs() *Catalogs { return c.catalogs }

// MARK: Drivers

type driver interface {
	// read is the current values from the screen (footer) and session file. Gaps are filled by the caller.
	read(ctx context.Context, a herdr.Agent, ref *transcript.Ref, screen *string) State
	label(model string) *string
	capabilities(ctx context.Context, a herdr.Agent, current State) api.AgentControls
	// apply applies an already-validated request and returns once it's confirmed.
	apply(ctx context.Context, r api.ControlRequest, a herdr.Agent, current State) error
	// kindControls is the choices for a kind with no agent yet (the New chat sheet), with saved defaults.
	kindControls() api.AgentControls
	// launchArgs is the agent's own launch flags for a model/effort ("" = not given).
	launchArgs(model, effort string) []string
	// agentModel is what Agent.model will read for a chosen model id (claude: alias → full id).
	agentModel(id string) string
}

func (c *Controls) driverForKind(kind string) driver {
	switch kind {
	case "claude":
		return claudeDriver{c}
	case "pi":
		return piDriver{c}
	case "codex":
		return codexDriver{c}
	}
	return nil
}

func (c *Controls) driverFor(a herdr.Agent) driver { return c.driverForKind(c.d.Kind(a)) }

func (c *Controls) HasDriver(kind string) bool { return c.driverForKind(kind) != nil }

// LaunchArgs is the kind's launch flags for a validated model/effort ("" = not given).
func (c *Controls) LaunchArgs(kind, model, effort string) []string {
	if d := c.driverForKind(kind); d != nil {
		return d.launchArgs(model, effort)
	}
	return nil
}

// AgentModel is what Agent.model will read for a chosen model id (claude: alias → full id).
func (c *Controls) AgentModel(kind, id string) string {
	if d := c.driverForKind(kind); d != nil {
		return d.agentModel(id)
	}
	return id
}

// LabelForKind is the kind's display name for a model id.
func (c *Controls) LabelForKind(kind, model string) *string {
	if d := c.driverForKind(kind); d != nil {
		return d.label(model)
	}
	return nil
}

// Label is modelLabel for an agent's model.
func (c *Controls) Label(a herdr.Agent, model string) *string {
	if d := c.driverFor(a); d != nil {
		return d.label(model)
	}
	return nil
}

// ForKind is GET /controls?kind=: choices and saved defaults for a kind with no agent yet.
func (c *Controls) ForKind(kind string) (api.AgentControls, error) {
	d := c.driverForKind(kind)
	if d == nil {
		return api.AgentControls{}, api.NewError(http.StatusBadRequest, "unsupported", "no controls for "+kind+" agents")
	}
	return d.kindControls(), nil
}

// MARK: State

// State is the agent's current controls: the transcript tail for model/effort, the footer for the
// permission mode (the transcript's permission-mode lines lag), a value a control just confirmed,
// and gaps filled from the last known values (e.g. right after /clear). nil when the kind has no
// driver.
func (c *Controls) State(ctx context.Context, a herdr.Agent, ref *transcript.Ref) *State {
	d := c.driverFor(a)
	if d == nil {
		return nil
	}
	var screen *string
	if r, err := c.herdr.Read(ctx, a.PaneID, herdr.SourceDetection, 0, false); err == nil {
		screen = &r.Text
	}
	s := d.read(ctx, a, ref, screen)
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.confirmed[a.PaneID]; ok {
		if e.until.Before(time.Now()) {
			delete(c.confirmed, a.PaneID)
		} else {
			s.Model = or(e.state.Model, s.Model)
			s.Effort = or(e.state.Effort, s.Effort)
			s.PermissionMode = or(e.state.PermissionMode, s.PermissionMode)
		}
	}
	var last *State
	if l, ok := c.last[a.PaneID]; ok {
		last = &l
	}
	merged := s.Merged(last)
	c.last[a.PaneID] = merged
	return &merged
}

// Hold keeps values a control (or a launch flag) just set for 15 s, until the transcript or footer
// catches up. Claude sometimes writes /model and /effort output to the transcript seconds late.
func (c *Controls) Hold(paneID string, s State) {
	c.mu.Lock()
	defer c.mu.Unlock()
	merged := s
	if e, ok := c.confirmed[paneID]; ok && e.until.After(time.Now()) {
		merged = s.Merged(&e.state)
	}
	c.confirmed[paneID] = held{merged, time.Now().Add(15 * time.Second)}
}

// Forget drops everything kept for a closed pane (pane ids are never reused).
func (c *Controls) Forget(paneID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.confirmed, paneID)
	delete(c.last, paneID)
	delete(c.sticky, paneID)
}

func (c *Controls) dropConfirmed(paneID string) {
	c.mu.Lock()
	delete(c.confirmed, paneID)
	c.mu.Unlock()
}

func (c *Controls) setStickyMode(paneID string, mode *string, session *string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if mode == nil {
		delete(c.sticky, paneID)
		return
	}
	c.sticky[paneID] = stickyMode{*mode, time.Now(), session}
}

func (c *Controls) stickyModeFor(paneID string, session *string, newerThan *time.Time) *string {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.sticky[paneID]
	if !ok || !sameP(e.session, session) {
		return nil
	}
	if newerThan != nil && newerThan.After(e.at) {
		delete(c.sticky, paneID)
		return nil
	}
	return sp(e.mode)
}

func sessionValue(a herdr.Agent) *string {
	if a.AgentSession == nil {
		return nil
	}
	return sp(a.AgentSession.Value)
}

// transcriptControls scans a Claude transcript's last 1 MB, cached by size and mtime.
func (c *Controls) transcriptControls(ref *transcript.Ref) State {
	info, err := os.Stat(ref.Path)
	if err != nil {
		return State{}
	}
	c.mu.Lock()
	hit, ok := c.controlCache[ref.Path]
	c.mu.Unlock()
	if ok && hit.size == info.Size() && hit.mtime.Equal(info.ModTime()) {
		return hit.state
	}
	f, err := os.Open(ref.Path)
	if err != nil {
		return State{}
	}
	defer f.Close()
	start := max(0, info.Size()-controlTailBytes)
	data, _ := io.ReadAll(io.NewSectionReader(f, start, info.Size()-start))
	state := ScanClaude(data)
	c.mu.Lock()
	if _, ok := c.controlCache[ref.Path]; !ok && len(c.controlCache) >= maxControlCache {
		// Transcripts of closed agents and old sessions: start over rather than grow forever.
		clear(c.controlCache)
	}
	c.controlCache[ref.Path] = cachedControls{info.Size(), info.ModTime(), state}
	c.mu.Unlock()
	return state
}

// readTail is the last 1 MB of a file (session logs can be large).
func readTail(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	start := max(0, info.Size()-controlTailBytes)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}
	data, _ := io.ReadAll(f)
	return data
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// readFrom is a file's bytes from offset on.
func readFrom(path string, offset int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, _ := io.ReadAll(f)
	return string(data)
}

// MARK: Routes

// ForAgent is GET /agents/:id/controls: what this agent's own UI can change, with its own lists.
func (c *Controls) ForAgent(ctx context.Context, id string) (api.AgentControls, error) {
	a, err := c.herdr.Agent(ctx, id)
	if err != nil {
		return api.AgentControls{}, err
	}
	d := c.driverFor(a)
	if d == nil {
		return api.AgentControls{Models: []api.ControlChoice{}, Efforts: []api.ControlChoice{}, Modes: []api.ControlChoice{}}, nil
	}
	current := State{}
	if s := c.State(ctx, a, c.d.Locate(a)); s != nil {
		current = *s
	}
	return d.capabilities(ctx, a, current), nil
}

// Apply is POST /agents/:id/control once the body is valid: it waits out a brief "working",
// refuses busy or blocked agents, validates the value against the agent's own lists, applies it
// and waits for confirmation. Returns the agent's pane id, for re-reading the fresh Agent.
func (c *Controls) Apply(ctx context.Context, id string, r api.ControlRequest) (string, error) {
	a, err := c.herdr.Agent(ctx, id)
	if err != nil {
		return "", err
	}
	kind := c.d.Kind(a)
	if kind == "" {
		kind = "unknown"
	}
	d := c.driverFor(a)
	if d == nil {
		return "", api.NewError(http.StatusBadRequest, "unsupported", "controls aren't available for "+kind+" agents")
	}
	// codex flips to "working" for a moment after some UI actions; give it a beat to settle.
	for i := 0; i < 12 && a.AgentStatus == api.StatusWorking; i++ {
		if err := sleep(ctx, 250*time.Millisecond); err != nil {
			return "", err
		}
		if a, err = c.herdr.Agent(ctx, id); err != nil {
			return "", err
		}
	}
	switch a.AgentStatus {
	case api.StatusWorking:
		return "", api.NewError(http.StatusConflict, "agent_busy", "agent is working")
	case api.StatusBlocked:
		return "", api.NewError(http.StatusConflict, "agent_blocked", "agent is waiting at a dialog")
	}
	current := State{}
	if s := c.State(ctx, a, c.d.Locate(a)); s != nil {
		current = *s
	}
	caps := d.capabilities(ctx, a, current)
	if err := validate(r, caps, kind); err != nil {
		return "", err
	}
	if err := d.apply(ctx, r, a, current); err != nil {
		return "", err
	}
	c.d.OnChange()
	return a.PaneID, nil
}

func validate(r api.ControlRequest, caps api.AgentControls, kind string) error {
	check := func(supported bool, name string, value *string, list []api.ControlChoice) error {
		if !supported {
			return api.NewError(http.StatusBadRequest, "unsupported", kind+" agents don't support changing "+name)
		}
		if value == nil {
			return nil
		}
		if _, ok := choice(list, *value); ok {
			return nil
		}
		ids := make([]string, len(list))
		for i, c := range list {
			ids[i] = c.ID
		}
		shown := strings.Join(ids[:min(8, len(ids))], ", ")
		if len(ids) > 8 {
			shown += fmt.Sprintf(", … (%d in GET /agents/:id/controls)", len(ids))
		}
		return api.BadRequest(*value + " isn't one of this agent's " + name + " options (" + shown + ")")
	}
	switch r.Kind {
	case api.ControlModel:
		return check(caps.Supports.Model, "model", &r.Value, caps.Models)
	case api.ControlEffort:
		return check(caps.Supports.Effort, "effort", &r.Value, caps.Efforts)
	case api.ControlPermissionMode:
		v := &r.Value
		if kind == "claude" && r.Value == "dontAsk" {
			v = nil
		}
		return check(caps.Supports.Mode, "permission mode", v, caps.Modes)
	case api.ControlCompact:
		return check(caps.Supports.Compact, "compact", nil, nil)
	case api.ControlClear:
		return check(caps.Supports.Clear, "clear", nil, nil)
	}
	return api.BadRequest("unknown control " + string(r.Kind))
}

// MARK: Helpers

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// swiftDuration is how Swift prints a Duration of whole seconds ("10.0 seconds"), kept for the
// error messages.
func swiftDuration(d time.Duration) string {
	return fmt.Sprintf("%.1f seconds", d.Seconds())
}

func timeoutError(msg string) error {
	return api.NewError(http.StatusGatewayTimeout, "control_timeout", msg)
}

// waitFor polls done every 250 ms until it holds, it fails, or timeout passes (504).
func waitFor(ctx context.Context, timeout time.Duration, what string, done func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := sleep(ctx, poll); err != nil {
			return err
		}
		ok, err := done()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return timeoutError(what + " sent, but no confirmation within " + swiftDuration(timeout))
}

func (c *Controls) screen(ctx context.Context, a herdr.Agent) (string, error) {
	r, err := c.herdr.Read(ctx, a.PaneID, herdr.SourceDetection, 0, false)
	return r.Text, err
}

// submit sends a slash command to a pi or codex agent. The input box is emptied first, so the
// command never glues onto text left there (R8-11).
func (c *Controls) submit(ctx context.Context, a herdr.Agent, command string) error {
	if err := c.d.ClearInput(ctx, a.PaneID, 0); err != nil {
		return err
	}
	return c.herdr.Prompt(ctx, a.PaneID, command)
}

// controlFailed is 502 control_failed quoting the agent's own error line, with paths made
// cwd-relative like everywhere else (R8-3).
func controlFailed(a herdr.Agent, line string) error {
	return api.NewError(http.StatusBadGateway, "control_failed", transcript.NewScrubber(a.CwdOrForeground()).Scrub(line))
}

func (c *Controls) keys(ctx context.Context, a herdr.Agent, keys ...string) error {
	return c.herdr.SendKeys(ctx, a.PaneID, keys)
}

func count(text, screen string) int { return strings.Count(screen, text) }

func lastLineContaining(screen, needle string) (string, bool) {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], needle) {
			return lines[i], true
		}
	}
	return "", false
}

// waitForScreen waits until done holds on the screen. Only new output counts: an error line
// (`■ …Error…`) fails the control only if there are more of them than in before.
func (c *Controls) waitForScreen(ctx context.Context, a herdr.Agent, before string, timeout time.Duration, what string, done func(string) bool) error {
	errorsBefore := count("■ Error", before)
	return waitFor(ctx, timeout, what, func() (bool, error) {
		s, err := c.screen(ctx, a)
		if err != nil {
			return false, err
		}
		if count("■ Error", s) > errorsBefore {
			if line, ok := lastLineContaining(s, "■ Error"); ok {
				return false, controlFailed(a, strings.Trim(line, "■ "))
			}
		}
		return done(s), nil
	})
}

// Effort labels, shared by pi and codex.
var effortLabels = map[string]string{
	"none": "None", "off": "Off", "minimal": "Minimal", "low": "Low", "medium": "Medium",
	"high": "High", "xhigh": "Extra high", "max": "Max", "ultra": "Ultra",
}

func effortLabel(id string) string {
	if l, ok := effortLabels[id]; ok {
		return l
	}
	return capitalized(id)
}

func effortChoices(ids []string) []api.ControlChoice {
	out := make([]api.ControlChoice, len(ids))
	for i, id := range ids {
		out[i] = api.ControlChoice{ID: id, Label: effortLabel(id)}
	}
	return out
}

// capitalized is Foundation's String.capitalized.
func capitalized(s string) string {
	var b strings.Builder
	start := true
	for _, r := range s {
		if strings.ContainsRune(" \t\n\r", r) {
			start = true
			b.WriteRune(r)
			continue
		}
		if start {
			b.WriteString(strings.ToUpper(string(r)))
			start = false
		} else {
			b.WriteString(strings.ToLower(string(r)))
		}
	}
	return b.String()
}
