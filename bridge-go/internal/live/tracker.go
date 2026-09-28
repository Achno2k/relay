package live

import (
	"strings"
	"sync"
	"time"
	"unicode"

	"relay/internal/api"
)

// Tracker turns raw per-poll screen reads into a stable `reply.live` stream per agent. Pure
// logic, no I/O, so it's cheap to test.
//
// Screen reads flicker (claude blinks a running tool's `⏺`, spinners animate, a read can catch
// a half-drawn frame), so nothing here trusts a single read:
//   - text only grows; a different text needs two reads in a row that agree, and a text that was
//     replaced or landed in the transcript is never sent again this turn;
//   - the tool appears, changes or clears only after two reads in a row agree, except a generic
//     summary being refined;
//   - text and tool clear at once when the transcript has them, and both clear when the agent stops.
type Tracker struct {
	mu          sync.Mutex
	state       map[string]*trackState
	minInterval time.Duration
	clock       func() time.Time
}

type trackState struct {
	seq        int
	lastSentAt time.Time // zero = never
	// What the client currently shows.
	text        *string
	tool        *api.LiveTool
	toolGeneric bool

	// Per turn, reset when the agent stops.
	// landedTexts are folded (see fold) text blocks the transcript already has.
	landedTexts map[string]bool
	// retiredTexts are folded texts that were shown and then replaced or cleared; never shown again.
	retiredTexts    []string
	landedToolIDs   map[string]bool
	landedSummaries map[string]bool
	// clearedSummaries are specific summaries already cleared this turn, so a re-read of the
	// same block stays cleared.
	clearedSummaries map[string]bool
	// toolSince is len(landedToolIDs) when the tool block with this summary first showed up.
	toolSince map[string]int
	// The previous read's tool, before any suppression.
	lastRawTool    *api.LiveTool
	lastRawGeneric bool
	// A change seen once, waiting for a second read that agrees. hasPendingTool with a nil
	// pendingTool is a pending clear.
	pendingText    *string
	pendingTool    *api.LiveTool
	hasPendingTool bool
}

func newTrackState() *trackState {
	return &trackState{
		landedTexts:      map[string]bool{},
		landedToolIDs:    map[string]bool{},
		landedSummaries:  map[string]bool{},
		clearedSummaries: map[string]bool{},
		toolSince:        map[string]int{},
	}
}

// NewTracker throttles frames to one per minInterval per agent. clock nil = time.Now.
func NewTracker(minInterval time.Duration, clock func() time.Time) *Tracker {
	if clock == nil {
		clock = time.Now
	}
	return &Tracker{state: map[string]*trackState{}, minInterval: minInterval, clock: clock}
}

// LandedCall is a transcript tool call the tracker compares the screen's tool against.
type LandedCall struct {
	ID      string
	Summary string
}

func (t *Tracker) get(agentID string) *trackState {
	s, ok := t.state[agentID]
	if !ok {
		s = newTrackState()
		t.state[agentID] = s
	}
	return s
}

// Offer is a fresh screen read for agentID: its prose and its running tool (already scrubbed).
// toolGeneric means the screen didn't show the tool's arguments, so tool.Summary is the generic
// one. Returns the frame to broadcast, if anything the client sees should change now.
func (t *Tracker) Offer(agentID string, text *string, tool *api.LiveTool, toolGeneric bool) *api.ServerEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.get(agentID)

	candidateText := filterText(text, s)
	candidateTool := filterTool(tool, toolGeneric, s)

	// Text: grows at once, a shorter or empty read keeps it, anything else waits for a second read.
	newText := s.text
	var pendingText *string
	if candidateText != nil {
		tx := *candidateText
		if s.text != nil {
			cur := *s.text
			ft, fc := fold(tx), fold(cur)
			if strings.HasPrefix(tx, cur) || (strings.HasPrefix(ft, fc) && charCount(ft) > charCount(fc)) {
				newText = &tx
			} else if merged, ok := extend(cur, tx); ok {
				newText = &merged // the reply scrolled: the read is a later window of it
			} else if strings.HasPrefix(fc, ft) || (charCount(ft) >= 12 && strings.Contains(fc, ft)) {
				// A shorter, re-rendered or scrolled read of the same text: keep what's shown.
			} else if p := s.pendingText; p != nil && (strings.HasPrefix(ft, fold(*p)) || extends(*p, tx)) {
				if merged, ok := extend(*p, tx); ok {
					newText = &merged
				} else {
					newText = &tx
				}
			} else {
				pendingText = &tx
			}
		} else {
			newText = &tx
		}
	}

	// Tool: a generic summary refines at once, anything else waits for a second read.
	newTool := s.tool
	newToolGeneric := s.toolGeneric
	var pendingTool *api.LiveTool
	hasPendingTool := false
	if !sameTool(candidateTool, s.tool) {
		if s.tool != nil && candidateTool != nil && s.toolGeneric && s.tool.Name == candidateTool.Name {
			newTool = candidateTool
			newToolGeneric = toolGeneric
		} else if s.hasPendingTool && sameTool(s.pendingTool, candidateTool) {
			newTool = candidateTool
			newToolGeneric = candidateTool != nil && toolGeneric
		} else {
			pendingTool, hasPendingTool = candidateTool, true
		}
	}

	if sameText(newText, s.text) && sameTool(newTool, s.tool) {
		s.pendingText = pendingText
		s.pendingTool, s.hasPendingTool = pendingTool, hasPendingTool
		return nil
	}
	now := t.clock()
	if !s.lastSentAt.IsZero() && now.Sub(s.lastSentAt) < t.minInterval {
		// Throttled: keep the accepted change pending so the next read confirms it again.
		if !sameText(newText, s.text) {
			s.pendingText = newText
		} else {
			s.pendingText = pendingText
		}
		if !sameTool(newTool, s.tool) {
			s.pendingTool, s.hasPendingTool = newTool, true
		} else {
			s.pendingTool, s.hasPendingTool = pendingTool, hasPendingTool
		}
		return nil
	}
	s.pendingText = pendingText
	s.pendingTool, s.hasPendingTool = pendingTool, hasPendingTool
	ev := send(s, agentID, newText, newTool, newToolGeneric, now)
	return &ev
}

// Landed tells the tracker the transcript's assistant message grew (or landed) with these text
// blocks and tool calls. It remembers them so the preview never repeats them, and clears
// whichever part the client is showing that the transcript now has, right away, bypassing the
// throttle.
func (t *Tracker) Landed(agentID string, texts []string, toolCalls []LandedCall) *api.ServerEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.get(agentID)
	for _, tx := range texts {
		if f := fold(tx); f != "" {
			s.landedTexts[f] = true
		}
	}
	for _, c := range toolCalls {
		s.landedToolIDs[c.ID] = true
		s.landedSummaries[c.Summary] = true
	}

	text, tool := s.text, s.tool
	if text != nil && matchesLanded(fold(*text), s.landedTexts) {
		text = nil
	}
	if tool != nil {
		since, ok := s.toolSince[tool.Summary]
		if !ok {
			since = len(s.landedToolIDs)
		}
		if s.landedSummaries[tool.Summary] || len(s.landedToolIDs) > since {
			if !s.toolGeneric {
				s.clearedSummaries[tool.Summary] = true
			}
			tool = nil
		}
	}
	if sameText(text, s.text) && sameTool(tool, s.tool) {
		return nil
	}
	generic := tool != nil && s.toolGeneric
	ev := send(s, agentID, text, tool, generic, t.clock())
	return &ev
}

// LandedBlocks is Landed for a message's blocks.
func (t *Tracker) LandedBlocks(agentID string, blocks []api.Block) *api.ServerEvent {
	var texts []string
	var calls []LandedCall
	for _, b := range blocks {
		switch b.Type {
		case api.BlockText:
			texts = append(texts, b.Text)
		case api.BlockToolCall:
			calls = append(calls, LandedCall{ID: b.ID, Summary: b.Summary})
		}
	}
	return t.Landed(agentID, texts, calls)
}

// Stopped means the agent stopped working (or closed): clear the preview right away and forget
// the turn.
func (t *Tracker) Stopped(agentID string) *api.ServerEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	old := t.get(agentID)
	s := newTrackState()
	s.seq, s.lastSentAt, s.text, s.tool = old.seq, old.lastSentAt, old.text, old.tool
	t.state[agentID] = s
	if s.text == nil && s.tool == nil {
		return nil
	}
	ev := send(s, agentID, nil, nil, false, t.clock())
	s.retiredTexts = nil
	return &ev
}

// Remove forgets agentID.
func (t *Tracker) Remove(agentID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.state, agentID)
}

func send(s *trackState, agentID string, text *string, tool *api.LiveTool, toolGeneric bool, now time.Time) api.ServerEvent {
	if s.text != nil && !sameText(text, s.text) {
		fo := fold(*s.text)
		fn := ""
		if text != nil {
			fn = fold(*text)
		}
		if text == nil || !strings.HasPrefix(fn, fo) {
			s.retiredTexts = append(s.retiredTexts, fo)
		}
	}
	s.text = copyText(text)
	s.tool = copyTool(tool)
	s.toolGeneric = toolGeneric
	s.seq++
	s.lastSentAt = now
	return api.ReplyLive(agentID, copyText(text), copyTool(tool), s.seq)
}

// filterText is nil for empty text, text the transcript already has, or text shown before and
// retired.
func filterText(text *string, s *trackState) *string {
	if text == nil || *text == "" {
		return nil
	}
	f := fold(*text)
	if f == "" || matchesLanded(f, s.landedTexts) {
		return nil
	}
	for _, r := range s.retiredTexts {
		if r == f || strings.HasPrefix(r, f) || (charCount(f) >= 12 && strings.Contains(r, f)) {
			return nil
		}
	}
	return text
}

// filterTool is nil once the transcript has this tool call: same summary, or any tool call that
// landed after this tool block showed up. Tracks which block each read belongs to.
func filterTool(tool *api.LiveTool, generic bool, s *trackState) *api.LiveTool {
	defer func() {
		s.lastRawTool = copyTool(tool)
		s.lastRawGeneric = generic
	}()
	if tool == nil {
		return nil
	}
	since := -1
	if prev := s.lastRawTool; prev != nil && (prev.Summary == tool.Summary || (s.lastRawGeneric && prev.Name == tool.Name)) {
		if known, ok := s.toolSince[prev.Summary]; ok {
			since = known // the same block as the previous read (possibly refined)
		}
	}
	if since < 0 && !generic {
		if known, ok := s.toolSince[tool.Summary]; ok {
			since = known // a specific block seen earlier this turn, back after a glitchy read
		}
	}
	if since < 0 {
		since = len(s.landedToolIDs)
	}
	s.toolSince[tool.Summary] = since
	if s.landedSummaries[tool.Summary] || s.clearedSummaries[tool.Summary] || len(s.landedToolIDs) > since {
		if !generic {
			s.clearedSummaries[tool.Summary] = true
		}
		return nil
	}
	return copyTool(tool)
}

// extend is cur plus what t adds after it, when t is a later window of the same text: t starts
// somewhere inside cur and carries on past its end (the top of a long reply scrolled off the
// viewport as it grew).
func extend(cur, t string) (string, bool) {
	head := charPrefix(t, 24)
	if charCount(head) < 12 {
		return "", false
	}
	searchEnd := len(cur)
	for {
		r := strings.LastIndex(cur[:searchEnd], head)
		if r < 0 {
			break
		}
		rest := cur[r:]
		if r > 0 && strings.HasPrefix(t, rest) && charCount(t) > charCount(rest) {
			return cur[:r] + t, true
		}
		// Look again for a match ending before this one's last character.
		searchEnd = len(dropLastChar(cur[:r+len(head)]))
		if searchEnd <= 0 {
			break
		}
	}
	return "", false
}

func extends(cur, t string) bool {
	_, ok := extend(cur, t)
	return ok
}

// fold keeps lowercased letters and digits only, so a rendered screen (no `**`, backticks,
// rewrapped lines, curly quotes) compares equal to the transcript's markdown.
func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// matchesLanded: the screen text is (part of) a landed block: equal, inside it (the visible
// tail of a scrolled reply), or sharing a long prefix with it (markdown the fold couldn't line up).
func matchesLanded(f string, landed map[string]bool) bool {
	for l := range landed {
		if l == f || (charCount(f) >= 12 && strings.Contains(l, f)) || strings.HasPrefix(l, f) || commonPrefix(l, f) >= 40 {
			return true
		}
	}
	return false
}

// commonPrefix counts the Characters a and b start with in common.
func commonPrefix(a, b string) int {
	n := 0
	for a != "" && b != "" {
		ca, cb := firstChar(a), firstChar(b)
		if ca != cb {
			break
		}
		a, b = a[len(ca):], b[len(cb):]
		n++
	}
	return n
}

func sameText(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameTool(a, b *api.LiveTool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func copyText(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}

func copyTool(t *api.LiveTool) *api.LiveTool {
	if t == nil {
		return nil
	}
	v := *t
	return &v
}
