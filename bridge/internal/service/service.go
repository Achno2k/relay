// Package service is what the routes do, re-derived from herdr on every call (AgentService in
// Swift), the monitor that turns herdr events into WS deltas (AgentMonitor), and the app wiring
// (Run).
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"relay/internal/api"
	"relay/internal/approval"
	"relay/internal/controls"
	"relay/internal/files"
	"relay/internal/herdr"
	"relay/internal/server"
	"relay/internal/transcript"
	"relay/internal/uploads"
)

// Service implements server.Backend.
type Service struct {
	herdr    herdr.Client
	locator  *transcript.Locator
	uploads  *uploads.Store
	controls *controls.Controls
	onChange func()

	mu sync.Mutex
	// When the bridge first saw each pane's agent, and when its herdr state_change_seq last changed.
	seen map[string]seenState
	// Parsed transcripts by path, for /messages (at most maxCachedTranscripts).
	cache map[string]*cachedTranscript
	// Kinds from POST /agents, for the moment before herdr has classified the new agent.
	createdKinds map[string]createdKind
	// Recently served tool images (GET …/tool-images).
	images imageCache
}

var _ server.Backend = (*Service)(nil)

type seenState struct {
	seq     uint64
	changed time.Time // zero: not seen changing
	first   time.Time
}

type cachedTranscript struct {
	size     int64
	mtime    time.Time
	messages []api.Message
	used     time.Time
}

type createdKind struct {
	kind  string
	until time.Time
}

const maxCachedTranscripts = 8

type Deps struct {
	Herdr   herdr.Client
	Locator *transcript.Locator // nil: default paths
	Uploads *uploads.Store      // nil: <relay home>/uploads
	// Catalogs and Settings go to the controls drivers; nil means the real files and CLIs.
	Catalogs *controls.Catalogs
	Settings *controls.SettingsGuard
	// OnChange runs after a control changed something, so the monitor can push agent.updated.
	OnChange func()
}

func New(opt Deps) *Service {
	s := &Service{
		herdr:        opt.Herdr,
		locator:      opt.Locator,
		uploads:      opt.Uploads,
		onChange:     opt.OnChange,
		seen:         map[string]seenState{},
		cache:        map[string]*cachedTranscript{},
		createdKinds: map[string]createdKind{},
	}
	if s.locator == nil {
		s.locator = transcript.NewLocator("", nil)
	}
	if s.uploads == nil {
		s.uploads = uploads.NewStore("")
	}
	if s.onChange == nil {
		s.onChange = func() {}
	}
	s.controls = controls.New(controls.Deps{
		Herdr:      opt.Herdr,
		Catalogs:   opt.Catalogs,
		Settings:   opt.Settings,
		Kind:       s.kindOf,
		Locate:     func(a herdr.Agent) *transcript.Ref { return s.locator.Locate(a, time.Time{}) },
		ClearInput: s.clearInput,
		OnChange:   func() { s.onChange() },
	})
	return s
}

// SetOnChange replaces the control-change hook (the monitor is built after the service).
func (s *Service) SetOnChange(f func()) { s.onChange = f }

func (s *Service) Uploads() *uploads.Store { return s.uploads }

// MARK: Agents

// Snapshot is one agent as the API shows it, with the herdr record and transcript behind it.
type Snapshot struct {
	Agent      api.Agent
	Raw        herdr.Agent
	Transcript *transcript.Ref
}

func (s *Service) Snapshots(ctx context.Context) ([]Snapshot, error) {
	var (
		raw        []herdr.Agent
		ws         []herdr.Workspace
		errA, errW error
		wg         sync.WaitGroup
	)
	wg.Add(1)
	go func() { defer wg.Done(); ws, errW = s.herdr.Workspaces(ctx) }()
	raw, errA = s.herdr.Agents(ctx)
	wg.Wait()
	if errW != nil {
		return nil, errW
	}
	if errA != nil {
		return nil, errA
	}
	names := map[string]string{}
	for _, w := range ws {
		if _, dup := names[w.WorkspaceID]; !dup {
			names[w.WorkspaceID] = w.Label
		}
	}
	out := make([]Snapshot, len(raw))
	for i, a := range raw {
		wg.Add(1)
		go func() {
			defer wg.Done()
			name, ok := names[a.WorkspaceID]
			out[i] = s.snapshot(ctx, a, name, ok)
		}()
	}
	wg.Wait()
	return out, nil
}

func (s *Service) Agents(ctx context.Context) ([]api.Agent, error) {
	snaps, err := s.Snapshots(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.Agent, len(snaps))
	for i, sn := range snaps {
		out[i] = sn.Agent
	}
	return out, nil
}

func (s *Service) SnapshotOf(ctx context.Context, id string) (Snapshot, error) {
	var (
		ws   []herdr.Workspace
		errW error
		wg   sync.WaitGroup
	)
	wg.Add(1)
	go func() { defer wg.Done(); ws, errW = s.herdr.Workspaces(ctx) }()
	a, err := s.herdr.Agent(ctx, id)
	wg.Wait()
	if err != nil {
		return Snapshot{}, err
	}
	if errW != nil {
		return Snapshot{}, errW
	}
	for _, w := range ws {
		if w.WorkspaceID == a.WorkspaceID {
			return s.snapshot(ctx, a, w.Label, true), nil
		}
	}
	return s.snapshot(ctx, a, "", false), nil
}

func (s *Service) Agent(ctx context.Context, id string) (api.Agent, error) {
	sn, err := s.SnapshotOf(ctx, id)
	return sn.Agent, err
}

func (s *Service) snapshot(ctx context.Context, a herdr.Agent, workspaceName string, hasName bool) Snapshot {
	ref := s.locator.Locate(a, s.firstSeen(a).Add(-5*time.Second))
	var mtime time.Time
	if ref != nil {
		if fi, err := os.Stat(ref.Path); err == nil {
			mtime = fi.ModTime()
		}
	}
	state := s.controls.State(ctx, a, ref)
	kind := s.kindOf(a)
	if kind == "" {
		kind = "unknown"
	}
	if !hasName {
		workspaceName = a.WorkspaceID
	}
	agent := api.Agent{
		ID:            a.PaneID,
		Name:          a.Name,
		Kind:          kind,
		Title:         title(a),
		WorkspaceID:   a.WorkspaceID,
		WorkspaceName: workspaceName,
		CwdName:       transcript.CwdName(a.CwdOrForeground()),
		Status:        a.AgentStatus,
		HasTranscript: ref != nil,
		UpdatedAt:     api.FormatTime(s.updatedAt(a, mtime)),
	}
	if state != nil {
		agent.Model = state.Model
		if state.Model != nil {
			agent.ModelLabel = s.controls.Label(a, *state.Model)
		}
		agent.PermissionMode = state.PermissionMode
		agent.Effort = state.Effort
	}
	agent.TranscriptState = api.TranscriptStateOf(agent.Kind, ref != nil)
	if sess := a.AgentSession; sess != nil {
		id := sess.Value
		if sess.Kind == "path" {
			base := filepath.Base(sess.Value)
			id = strings.TrimSuffix(base, filepath.Ext(base))
		}
		agent.SessionID = &id
	}
	return Snapshot{Agent: agent, Raw: a, Transcript: ref}
}

// kindOf is herdr's kind, else the kind this bridge just started in that pane (so a new agent
// reads `pending`, never `unsupported` with a screen dump, while herdr is still detecting it).
// "" means unknown.
func (s *Service) kindOf(a herdr.Agent) string {
	k := a.Agent
	if k == "" && a.AgentSession != nil {
		k = a.AgentSession.Agent
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if k != "" {
		delete(s.createdKinds, a.PaneID)
		return k
	}
	e, ok := s.createdKinds[a.PaneID]
	if !ok {
		return ""
	}
	if e.until.Before(time.Now()) {
		delete(s.createdKinds, a.PaneID)
		return ""
	}
	return e.kind
}

// forget drops everything kept per pane once its agent is gone (herdr never reuses pane ids).
func (s *Service) forget(paneID string) {
	s.mu.Lock()
	delete(s.seen, paneID)
	delete(s.createdKinds, paneID)
	s.mu.Unlock()
	if f, ok := any(s.controls).(interface{ Forget(string) }); ok {
		f.Forget(paneID)
	}
}

func (s *Service) rememberCreated(paneID, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createdKinds[paneID] = createdKind{kind: kind, until: time.Now().Add(120 * time.Second)}
}

func title(a herdr.Agent) string {
	scrub := transcript.NewScrubber(a.Cwd)
	for _, t := range []string{a.TerminalTitleStripped, a.Title} {
		if t = stableTitle(strings.Trim(t, " \t")); t != "" {
			return scrub.Scrub(t)
		}
	}
	switch {
	case a.Name != nil:
		return *a.Name
	case a.Agent != "":
		return a.Agent
	}
	return a.PaneID
}

// codexTitleChrome is what codex animates in its terminal title: a braille spinner while working,
// and a blinking `[ ! ]` / `[ . ] Action Required | ` while blocked. Status carries both, and
// keeping them would send an agent.updated per animation frame.
var codexTitleChrome = regexp.MustCompile(`^(?:[\x{2800}-\x{28FF}⁖⁘⁙⁚⁛⸪⸫⸬⸭]\s+|\[ [!.] \] Action Required \|\s*)+`)

func stableTitle(t string) string {
	t = strings.TrimSpace(codexTitleChrome.ReplaceAllString(t, ""))
	// A codex session with no name yet: `<spinner> | <cwd>`.
	return strings.TrimSpace(strings.TrimPrefix(t, "| "))
}

// firstSeen is when the bridge first saw this pane's agent (bridge start for agents that were
// already running).
func (s *Service) firstSeen(a herdr.Agent) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.seen[a.PaneID]; ok {
		return st.first
	}
	now := time.Now()
	s.seen[a.PaneID] = seenState{seq: a.StateChangeSeq, first: now}
	return now
}

// updatedAt: herdr gives a state sequence number, not a time. Use the transcript's mtime, or the
// time we saw the sequence change; before either is known, the time the bridge first saw the agent.
func (s *Service) updatedAt(a herdr.Agent, transcriptModified time.Time) time.Time {
	s.mu.Lock()
	now := time.Now()
	st, ok := s.seen[a.PaneID]
	var changed, first time.Time
	switch {
	case !ok:
		s.seen[a.PaneID] = seenState{seq: a.StateChangeSeq, first: now}
		first = now
	case st.seq != a.StateChangeSeq:
		s.seen[a.PaneID] = seenState{seq: a.StateChangeSeq, changed: now, first: st.first}
		changed, first = now, st.first
	default:
		changed, first = st.changed, st.first
	}
	s.mu.Unlock()
	switch {
	case !transcriptModified.IsZero() && !changed.IsZero():
		if transcriptModified.After(changed) {
			return transcriptModified
		}
		return changed
	case !transcriptModified.IsZero():
		return transcriptModified
	case !changed.IsZero():
		return changed
	}
	return first
}

func (s *Service) Workspaces(ctx context.Context) ([]api.Workspace, error) {
	var (
		agents []herdr.Agent
		errA   error
		wg     sync.WaitGroup
	)
	wg.Add(1)
	go func() { defer wg.Done(); agents, errA = s.herdr.Agents(ctx) }()
	ws, errW := s.herdr.Workspaces(ctx)
	wg.Wait()
	if errA != nil {
		return nil, errA
	}
	if errW != nil {
		return nil, errW
	}
	counts := map[string]int{}
	for _, a := range agents {
		counts[a.WorkspaceID]++
	}
	out := make([]api.Workspace, len(ws))
	for i, w := range ws {
		out[i] = api.Workspace{ID: w.WorkspaceID, Name: w.Label, AgentCount: counts[w.WorkspaceID]}
	}
	return out, nil
}

// MARK: Messages

func (s *Service) Messages(ctx context.Context, id string, before *string, limit int) (api.MessagePage, error) {
	sn, err := s.SnapshotOf(ctx, id)
	if err != nil {
		return api.MessagePage{}, err
	}
	var all []api.Message
	switch sn.Agent.TranscriptState {
	case api.TranscriptReady:
		if sn.Transcript != nil {
			if all, err = s.transcriptMessages(*sn.Transcript); err != nil {
				return api.MessagePage{}, err
			}
		}
	case api.TranscriptPending:
		// A new agent before its first message: its screen is a startup/trust screen, not chat.
		// Dialogs there reach the app as status=blocked + /approval.
	default:
		if all, err = s.screenMessages(ctx, sn.Raw); err != nil {
			return api.MessagePage{}, err
		}
	}
	return page(all, before, limit)
}

func page(all []api.Message, before *string, limit int) (api.MessagePage, error) {
	end := len(all)
	if before != nil {
		i := slices.IndexFunc(all, func(m api.Message) bool { return m.ID == *before })
		if i < 0 {
			return api.MessagePage{}, api.NotFound("no message " + *before)
		}
		end = i
	}
	start := max(0, end-limit)
	return api.MessagePage{Messages: all[start:end], HasMore: start > 0}, nil
}

func (s *Service) transcriptMessages(ref transcript.Ref) ([]api.Message, error) {
	fi, err := os.Stat(ref.Path)
	if err != nil {
		return nil, err
	}
	size, mtime := fi.Size(), fi.ModTime()
	s.mu.Lock()
	if e, ok := s.cache[ref.Path]; ok && e.size == size && e.mtime.Equal(mtime) {
		e.used = time.Now()
		s.mu.Unlock()
		return e.messages, nil
	}
	s.mu.Unlock()

	data, err := transcript.ReadBounded(ref.Path, size, transcript.MaxReadBytes)
	if err != nil {
		return nil, err
	}
	messages := transcript.Parse(data, ref.Format, ref.Cwd, s.uploads)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[ref.Path] = &cachedTranscript{size: size, mtime: mtime, messages: messages, used: time.Now()}
	if len(s.cache) > maxCachedTranscripts {
		oldest := ""
		for p, e := range s.cache {
			if oldest == "" || e.used.Before(s.cache[oldest].used) {
				oldest = p
			}
		}
		delete(s.cache, oldest)
	}
	return messages, nil
}

// screenMessages: no transcript, so the recent screen as one synthetic assistant message.
func (s *Service) screenMessages(ctx context.Context, a herdr.Agent) ([]api.Message, error) {
	read, err := s.herdr.Read(ctx, a.PaneID, herdr.SourceRecent, 200, false)
	if err != nil {
		return nil, err
	}
	text := strings.Trim(transcript.NewScrubber(a.Cwd).Scrub(read.Text), "\n\r  \v\f\u0085")
	if text == "" {
		return []api.Message{}, nil
	}
	return []api.Message{{
		ID:        "screen:" + a.PaneID,
		Role:      api.RoleAssistant,
		CreatedAt: api.FormatTime(s.updatedAt(a, time.Time{})),
		Blocks:    []api.Block{api.TextBlock("```\n" + text + "\n```")},
	}}, nil
}

// MARK: Approval

func (s *Service) Approval(ctx context.Context, id string) (*api.Approval, error) {
	a, err := s.herdr.Agent(ctx, id)
	if err != nil {
		return nil, err
	}
	if a.AgentStatus != api.StatusBlocked {
		return nil, nil
	}
	sc := transcript.NewScrubber(a.Cwd)
	cwdName := transcript.CwdName(a.CwdOrForeground())
	kind := s.kindOf(a)
	detection, err := s.herdr.Read(ctx, a.PaneID, herdr.SourceDetection, 0, false)
	if err != nil {
		return nil, err
	}
	screen := detection.Text
	ap := approval.Parse(screen, a.PaneID, sc, cwdName, kind)
	if ap == nil {
		visible, err := s.herdr.Read(ctx, a.PaneID, herdr.SourceVisible, 0, false)
		if err != nil {
			return nil, err
		}
		screen = visible.Text
		ap = approval.Parse(screen, a.PaneID, sc, cwdName, kind)
	}
	if ap == nil {
		return approval.Fallback(detection.Text, a.PaneID, sc), nil
	}
	if strings.Contains(screen, "←") {
		// The active tab is only visible as a highlight, so read with colours.
		ansi := ""
		if r, err := s.herdr.Read(ctx, a.PaneID, herdr.SourceVisible, 0, true); err == nil {
			ansi = r.Text
		}
		ap.Step = approval.Step(screen, ansi)
	}
	if kind == "claude" {
		ap.Plan = s.pendingPlan(a, ap.Question)
	}
	return ap, nil
}

// pendingPlan is the plan an ExitPlanMode prompt asks about; see api.md "Plans".
func (s *Service) pendingPlan(a herdr.Agent, question string) string {
	ref := s.locator.Locate(a, s.firstSeen(a).Add(-5*time.Second))
	if ref == nil {
		return ""
	}
	messages, err := s.transcriptMessages(*ref)
	if err != nil {
		return ""
	}
	if plan, ok := transcript.PendingPlan(messages); ok {
		return plan
	}
	if !transcript.IsPlanQuestion(question) {
		return ""
	}
	fi, err := os.Stat(ref.Path)
	if err != nil {
		return ""
	}
	data, err := transcript.ReadBounded(ref.Path, fi.Size(), transcript.MaxReadBytes)
	if err != nil {
		return ""
	}
	return transcript.LatestPlan(data, ref.Format, ref.Cwd)
}

// MARK: Actions

// Prompt always goes into an empty input box. Attachments become a final
// `Attached files: <paths>` line that Claude reads from.
func (s *Service) Prompt(ctx context.Context, id, text string, attachments []string) error {
	if len(attachments) > uploads.MaxPerMessage {
		return api.BadRequest(fmt.Sprintf("at most %d attachments per message", uploads.MaxPerMessage))
	}
	paths := make([]string, 0, len(attachments))
	for _, att := range attachments {
		p, ok := s.uploads.Find(att)
		if !ok {
			return api.BadRequest("unknown attachment " + att)
		}
		paths = append(paths, p)
	}
	if err := s.clearInput(ctx, id, 0); err != nil {
		return err
	}
	if len(paths) > 0 {
		a, err := s.herdr.Agent(ctx, id)
		if err != nil {
			return err
		}
		s.uploads.RecordSent(a.PaneID, text, paths)
	}
	return s.herdr.Prompt(ctx, id, uploads.Prompt(text, paths))
}

func (s *Service) Upload(ctx context.Context, id string, data []byte, filename string) (api.Attachment, error) {
	a, err := s.herdr.Agent(ctx, id)
	if err != nil {
		return api.Attachment{}, err
	}
	return s.uploads.Save(a.PaneID, data, filename)
}

func (s *Service) FindUpload(attachmentID string) (string, bool) { return s.uploads.Find(attachmentID) }

// File reads a file inside the agent's cwd; see api.md "Files".
func (s *Service) File(ctx context.Context, id, path string) (files.Result, error) {
	a, err := s.herdr.Agent(ctx, id)
	if err != nil {
		return files.Result{}, err
	}
	return files.Read(a.CwdOrForeground(), path)
}

// SendKeys: a stop (`["esc"]`) makes Claude put the interrupted prompt back in the input box;
// clear it.
func (s *Service) SendKeys(ctx context.Context, id string, keys []string) error {
	if err := s.herdr.SendKeys(ctx, id, keys); err != nil {
		return err
	}
	if len(keys) == 1 && (keys[0] == "esc" || keys[0] == "escape") {
		return s.clearInput(ctx, id, 1500*time.Millisecond)
	}
	return nil
}

// Text types literally into whatever is focused (e.g. a free-text answer field). No clearing, no Esc.
func (s *Service) Text(ctx context.Context, id, text string, submit bool) error {
	a, err := s.herdr.Agent(ctx, id)
	if err != nil {
		return err
	}
	if err := s.herdr.SendText(ctx, a.PaneID, text); err != nil {
		return err
	}
	if !submit {
		return nil
	}
	// Let the TUI take the pasted text before Enter, or Enter can land first.
	if err := sleep(ctx, 150*time.Millisecond); err != nil {
		return err
	}
	return s.herdr.SendKeys(ctx, a.PaneID, []string{"enter"})
}

// clearInput empties the agent's input box (claude, pi, codex) with ctrl+u until the screen shows
// it empty. It polls up to wait for text to appear (Claude puts the prompt back shortly after Esc).
// It never touches a blocked agent: keys there would answer a dialog.
func (s *Service) clearInput(ctx context.Context, id string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	cleared := false
	for range 12 {
		a, err := s.herdr.Agent(ctx, id)
		if err != nil {
			return err
		}
		kind := s.kindOf(a)
		if kind == "" {
			kind = "claude"
		}
		if a.AgentStatus == api.StatusBlocked || !clearableKinds[kind] {
			return nil
		}
		screen, ansi, err := s.inputScreen(ctx, a.PaneID, kind)
		if err != nil {
			return err
		}
		content, ok := approval.InputFor(kind, screen, ansi)
		if !ok {
			return nil
		}
		if content == "" {
			if cleared || !time.Now().Before(deadline) {
				return nil
			}
			if err := sleep(ctx, 150*time.Millisecond); err != nil {
				return err
			}
			continue
		}
		if err := s.herdr.SendKeys(ctx, a.PaneID, approval.ClearKeysFor(kind, content)); err != nil {
			return err
		}
		cleared = true
		if err := sleep(ctx, 120*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

var clearableKinds = map[string]bool{"claude": true, "pi": true, "codex": true}

// inputScreen reads what the kind's input detection needs. codex's empty composer shows a dim
// placeholder that only the colours tell apart from typed text, so it's read with ANSI too.
func (s *Service) inputScreen(ctx context.Context, pane, kind string) (screen, ansi string, err error) {
	if kind == "claude" {
		r, err := s.herdr.Read(ctx, pane, herdr.SourceDetection, 0, false)
		return r.Text, "", err
	}
	r, err := s.herdr.Read(ctx, pane, herdr.SourceVisible, 0, false)
	if err != nil || kind != "codex" {
		return r.Text, "", err
	}
	c, err := s.herdr.Read(ctx, pane, herdr.SourceVisible, 0, true)
	return r.Text, c.Text, err
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// MARK: Controls

func (s *Service) Catalog() api.Controls { return controls.Catalog() }

// KindControls is GET /controls?kind=: choices and saved defaults for a kind with no agent yet.
func (s *Service) KindControls(kind string) (api.AgentControls, error) {
	return s.controls.ForKind(kind)
}

func (s *Service) Controls(ctx context.Context, id string) (api.AgentControls, error) {
	return s.controls.ForAgent(ctx, id)
}

func (s *Service) Control(ctx context.Context, id string, r api.ControlRequest) (api.Agent, error) {
	pane, err := s.controls.Apply(ctx, id, r)
	if err != nil {
		return api.Agent{}, err
	}
	return s.Agent(ctx, pane)
}

// MARK: New agent

// Create opens a new tab in the workspace (cwd = its first pane's cwd), starts the agent with the
// kind's own launch flags for model/effort, then sends the optional prompt.
func (s *Service) Create(ctx context.Context, r server.CreateRequest) (api.Agent, error) {
	kind := r.Kind
	chose := r.Model != nil || r.Effort != nil
	var args []string
	if chose {
		// Validate against the kind's own lists before opening anything.
		if !s.controls.HasDriver(kind) {
			return api.Agent{}, api.NewError(http.StatusBadRequest, "unsupported", kind+" agents don't take a model or effort")
		}
		caps, err := s.controls.ForKind(kind)
		if err != nil {
			return api.Agent{}, err
		}
		if r.Model != nil && !hasChoice(caps.Models, *r.Model) {
			return api.Agent{}, api.BadRequest(fmt.Sprintf("%s isn't one of %s's models (GET /controls?kind=%s)", *r.Model, kind, kind))
		}
		if r.Effort != nil {
			target := r.Model
			if target == nil {
				target = caps.DefaultModel
			}
			allowed := caps.Efforts
			if target != nil {
				if list, ok := caps.EffortsByModel[*target]; ok {
					allowed = list
				}
			}
			if !hasChoice(allowed, *r.Effort) {
				what := kind
				if target != nil {
					what = *target
				}
				ids := make([]string, len(allowed))
				for i, c := range allowed {
					ids[i] = c.ID
				}
				return api.Agent{}, api.BadRequest(fmt.Sprintf("%s isn't an effort %s supports (%s)", *r.Effort, what, strings.Join(ids, ", ")))
			}
		}
		args = s.controls.LaunchArgs(kind, deref(r.Model), deref(r.Effort))
	}

	panes, err := s.herdr.Panes(ctx, r.WorkspaceID)
	if err != nil {
		return api.Agent{}, err
	}
	if len(panes) == 0 {
		return api.Agent{}, api.NotFound("no workspace " + r.WorkspaceID)
	}
	cwd := panes[0].CwdOrForeground()
	if r.CwdFromPane != nil {
		all, err := s.herdr.Panes(ctx, "")
		if err != nil {
			return api.Agent{}, err
		}
		i := slices.IndexFunc(all, func(p herdr.Pane) bool { return p.PaneID == *r.CwdFromPane })
		if i < 0 {
			return api.Agent{}, api.NotFound("no pane " + *r.CwdFromPane)
		}
		cwd = all[i].CwdOrForeground()
	}
	name := deref(r.Name)
	if r.Name == nil {
		name = defaultName(kind)
	}
	pane, err := s.herdr.CreateTab(ctx, r.WorkspaceID, cwd, deref(r.Name))
	if err != nil {
		return api.Agent{}, err
	}
	s.rememberCreated(pane.PaneID, kind)

	// The new shell may need a moment before herdr considers it available (agent_pane_busy);
	// that's the only failure worth retrying.
	var startErr error
	for attempt := range startAttempts {
		if _, startErr = s.herdr.StartAgent(ctx, name, kind, pane.PaneID, args, 0); !isRemote(startErr, "agent_pane_busy") {
			break
		}
		if attempt < startAttempts-1 {
			if err := sleep(ctx, time.Duration(300*(attempt+1))*time.Millisecond); err != nil {
				startErr = err
				break
			}
		}
	}
	switch {
	case startErr == nil:
		if r.Prompt != nil && *r.Prompt != "" {
			if err := s.herdr.Prompt(ctx, pane.PaneID, *r.Prompt); err != nil {
				return api.Agent{}, err
			}
		}
	case isRemote(startErr, "agent_not_ready"):
		// Blocked during startup (e.g. a trust dialog): the agent exists; skip the prompt.
	default:
		// Nothing started: don't leave an empty shell tab behind.
		s.closeTab(pane.TabID)
		return api.Agent{}, startErr
	}
	hasDriver := s.controls.HasDriver(kind)
	var chosenModel *string
	if r.Model != nil && hasDriver {
		chosenModel = api.Str(s.controls.AgentModel(kind, *r.Model))
	}
	// The footer/banner can lag the launch; report what the flags asked for until it shows.
	if hasDriver && chose {
		s.controls.Hold(pane.PaneID, controls.State{Model: chosenModel, Effort: r.Effort})
	}
	created, err := s.Agent(ctx, pane.PaneID)
	if err != nil {
		return api.Agent{}, err
	}
	// herdr may not have classified the new agent yet.
	if created.Kind == "unknown" {
		created.Kind = kind
		created.TranscriptState = api.TranscriptStateOf(kind, created.HasTranscript)
		if hasDriver && chose {
			if chosenModel != nil {
				created.Model = chosenModel
			}
			created.ModelLabel = nil
			if created.Model != nil {
				created.ModelLabel = s.controls.LabelForKind(kind, *created.Model)
			}
			if r.Effort != nil {
				created.Effort = r.Effort
			}
		}
	}
	return created, nil
}

const startAttempts = 6

func isRemote(err error, code string) bool {
	var he *herdr.Error
	return errors.As(err, &he) && he.Kind == herdr.ErrRemote && he.Code == code
}

// closeTab removes a tab this bridge opened for an agent that never started. Best effort, and
// not tied to the request: the client may already have gone.
func (s *Service) closeTab(tabID string) {
	if tabID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), herdr.ActionTimeout)
	defer cancel()
	_ = s.herdr.CloseTab(ctx, tabID)
}

// defaultName is `<kind letters and digits>-<4 hex>`, e.g. `claude-3f9a`.
func defaultName(kind string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(kind) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	var suffix [2]byte
	_, _ = rand.Read(suffix[:])
	return b.String() + "-" + hex.EncodeToString(suffix[:])
}

func hasChoice(list []api.ControlChoice, id string) bool {
	return slices.ContainsFunc(list, func(c api.ControlChoice) bool { return c.ID == id })
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// sameExceptTime: an agent.updated is only worth sending when something but updatedAt changed.
func sameExceptTime(a, b api.Agent) bool {
	b.UpdatedAt = a.UpdatedAt
	return reflect.DeepEqual(a, b)
}
