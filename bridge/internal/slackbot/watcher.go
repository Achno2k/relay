package slackbot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Achno2k/agents-cli/internal/herdr"
	"github.com/Achno2k/agents-cli/internal/state"
	"github.com/Achno2k/agents-cli/internal/worktree"
)

// Reactions on the thread root, one per agent state.
const (
	reactionWorking = "hourglass_flowing_sand"
	reactionBlocked = "eyes"
	reactionIdle    = "white_check_mark"
	reactionDead    = "x"
)

// maxReplyChars is how much of an agent's answer goes inline in Slack. The
// rest is available through "full".
const maxReplyChars = 1500

// watchPost is what a state transition asks the watcher to say in the thread.
type watchPost int

const (
	postNothing  watchPost = iota
	postReply              // the agent finished a turn we started
	postApproval           // the agent is waiting on a permission prompt
	postPickup             // somebody drove the pane from a terminal
)

// step is one transition's output: the reaction the thread root should carry
// and the message, if any, to post.
type step struct {
	Reaction string
	Post     watchPost
}

// machine is the watcher's state machine, kept free of herdr, Slack and the
// clock so the transition rules can be tested on their own.
type machine struct {
	prev      herdr.AgentState
	started   bool
	expecting bool // we sent a prompt and owe the thread an answer
	quiet     bool // a human took the pane over; say nothing until next mention
	reaction  string
}

// prompted records that the bot just sent this session a prompt. It also ends
// a quiet period: the thread is driving again.
func (m *machine) prompted() {
	m.expecting = true
	m.quiet = false
}

// next advances the machine and returns what the watcher should do.
func (m *machine) next(s herdr.AgentState) step {
	if m.started && s == m.prev {
		return step{}
	}
	m.started, m.prev = true, s

	var out step
	switch s {
	case herdr.StateWorking:
		out.Reaction = reactionWorking
		// Work we did not start means somebody is typing in the pane.
		if !m.expecting && !m.quiet {
			m.quiet = true
			out.Post = postPickup
		}
	case herdr.StateBlocked:
		out.Reaction = reactionBlocked
		if !m.quiet {
			out.Post = postApproval
		}
	case herdr.StateIdle, herdr.StateDone:
		out.Reaction = reactionIdle
		if !m.quiet && m.expecting {
			m.expecting = false
			out.Post = postReply
		}
	default:
		// Unknown: leave the thread showing whatever it last showed.
		return step{}
	}
	if out.Reaction == m.reaction {
		out.Reaction = ""
	} else if out.Reaction != "" {
		m.reaction = out.Reaction
	}
	return out
}

// dead marks the agent gone. It is separate from next because pane death
// arrives as an error from herdr, not as a state.
func (m *machine) dead() step {
	if m.reaction == reactionDead {
		return step{}
	}
	m.reaction = reactionDead
	m.started, m.prev = true, herdr.StateUnknown
	return step{Reaction: reactionDead}
}

// waitStates are the states worth waking up for, given where we are now.
// Asking herdr to wait for the state we are already in returns immediately and
// spins the loop.
func waitStates(prev herdr.AgentState, started bool) []herdr.AgentState {
	all := []herdr.AgentState{herdr.StateWorking, herdr.StateBlocked, herdr.StateIdle, herdr.StateDone}
	if !started {
		return all
	}
	out := make([]herdr.AgentState, 0, len(all))
	for _, s := range all {
		if s != prev {
			out = append(out, s)
		}
	}
	return out
}

// replyReader gets an agent's final answer for a turn.
//
// The agent is asked to write it to <worktree>/.agents/reply.md. There is no
// fallback to the pane: herdr's scrollback is empty for full screen agents
// like Claude Code, and its viewport holds a screen of TUI rather than an
// answer. A turn that does not touch the file has not produced a reply, and
// saying so is more use than pasting a terminal.
type replyReader struct {
	worktree string
	lastMod  time.Time
	lastSize int64
}

func (r *replyReader) path() string { return filepath.Join(r.worktree, ReplyRelPath) }

// read returns the reply, and false when the file is missing, empty or
// unchanged since the previous turn.
func (r *replyReader) read() (string, bool) {
	if r.worktree == "" {
		return "", false
	}
	fi, err := os.Stat(r.path())
	if err != nil {
		return "", false
	}
	if fi.ModTime().Equal(r.lastMod) && fi.Size() == r.lastSize {
		return "", false // the agent never wrote this turn
	}
	b, err := os.ReadFile(r.path())
	if err != nil || strings.TrimSpace(string(b)) == "" {
		return "", false
	}
	r.lastMod, r.lastSize = fi.ModTime(), fi.Size()
	return strings.TrimSpace(string(b)), true
}

// artifactPath resolves a name from a reply against the artifacts directory,
// refusing anything that would escape it.
func (r *replyReader) artifactPath(name string) (string, error) {
	return safeJoin(filepath.Join(r.worktree, ArtifactsRelPath), name)
}

// trimReply cuts a reply down to what belongs inline in a Slack thread.
func trimReply(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(the agent finished without writing a reply)"
	}
	if len(s) <= maxReplyChars {
		return s
	}
	cut := s[:maxReplyChars]
	// Prefer to break on a line end so a code fence is less likely to be split.
	if i := strings.LastIndex(cut, "\n"); i > maxReplyChars/2 {
		cut = cut[:i]
	}
	// Cutting inside a code block would leave Slack rendering the rest of the
	// message as code, so close the fence we opened.
	if insideFence(strings.Split(cut, "\n")) {
		cut += "\n```"
	}
	return cut + "\n… (truncated, say `full` for everything)"
}

// statsLine is the one line of bookkeeping under every reply.
func statsLine(ctx context.Context, path string, elapsed time.Duration) string {
	changed := "no changes"
	if path != "" {
		if out, err := worktree.DiffStat(ctx, path); err == nil {
			if s := summaryOf(out); s != "" {
				changed = s
			}
		}
	}
	return "_" + changed + " · " + roundDuration(elapsed) + "_"
}

// summaryOf pulls "3 files changed, 40 insertions(+)" off the end of a
// git diff --stat.
func summaryOf(diffStat string) string {
	lines := strings.Split(strings.TrimRight(diffStat, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.Contains(l, "changed") {
			return l
		}
	}
	return ""
}

func roundDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return "under a second"
	case d < time.Minute:
		return d.Round(time.Second).String()
	default:
		return d.Round(time.Second).String()
	}
}

// watcher mirrors one agent's state onto its Slack thread.
type watcher struct {
	bot     *Bot
	session state.Session

	m      machine
	reply  replyReader
	turnAt time.Time
}

func newWatcher(b *Bot, s state.Session) *watcher {
	return &watcher{
		bot:     b,
		session: s,
		reply:   replyReader{worktree: s.WorktreePath},
		turnAt:  time.Now(),
	}
}

// run follows the agent until the context ends or the pane dies.
func (w *watcher) run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		st, err := w.bot.Herdr.Wait(ctx, w.session.AgentName, waitStates(w.m.prev, w.m.started)...)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if herdr.IsNotFound(err) {
				w.apply(ctx, w.m.dead())
				w.markStatus(ctx, state.StatusDone)
				return
			}
			// A transient herdr error should not kill the watcher; back off.
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		w.apply(ctx, w.m.next(st))
		w.recordStatus(ctx, st)
	}
}

// apply carries out one transition: the reaction first, then the message.
func (w *watcher) apply(ctx context.Context, s step) {
	if s.Reaction != "" {
		w.setReaction(ctx, s.Reaction)
	}
	switch s.Post {
	case postReply:
		w.postReply(ctx)
	case postApproval:
		w.postApproval(ctx)
	case postPickup:
		w.postPickup(ctx)
	}
}

// setReaction swaps the thread root's reaction for the new one.
func (w *watcher) setReaction(ctx context.Context, name string) {
	for _, old := range []string{reactionWorking, reactionBlocked, reactionIdle, reactionDead} {
		if old != name {
			_ = w.bot.Slack.RemoveReaction(ctx, w.session.SlackChannel, w.session.ThreadTS, old)
		}
	}
	if err := w.bot.Slack.AddReaction(ctx, w.session.SlackChannel, w.session.ThreadTS, name); err != nil {
		w.bot.logf("reaction %s on %s: %v", name, w.session.ID, err)
	}
}

func (w *watcher) postReply(ctx context.Context) {
	raw, ok := w.reply.read()
	if !ok {
		w.say(ctx, noReplyMessage)
		return
	}

	r := parseReply(raw)
	body := trimReply(toMrkdwn(r.Text)) + "\n\n" +
		statsLine(ctx, w.session.WorktreePath, time.Since(w.turnAt))
	w.say(ctx, body)

	w.uploadArtifacts(ctx, r.Artifacts)
	w.postDecision(ctx, r.Decision)
}

// uploadArtifacts posts each referenced file as an attachment. A name that
// does not resolve gets one line rather than silence, so the agent's mistake
// is visible in the thread.
func (w *watcher) uploadArtifacts(ctx context.Context, names []string) {
	for _, name := range names {
		path, err := w.reply.artifactPath(name)
		if err != nil {
			w.say(ctx, "artifact "+name+" not found")
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(body)) == "" {
			w.say(ctx, "artifact "+name+" not found")
			continue
		}
		if err := w.bot.Slack.UploadText(ctx,
			w.session.SlackChannel, w.session.ThreadTS, name, name, string(body)); err != nil {
			w.bot.logf("upload artifact %s for %s: %v", name, w.session.ID, err)
			w.say(ctx, "artifact "+name+" could not be uploaded")
		}
	}
}

// postDecision turns the agent's options into buttons. Clicking one sends it
// back as the next prompt, which is the whole point: the thread answers
// without anyone typing.
func (w *watcher) postDecision(ctx context.Context, options []string) {
	if len(options) == 0 {
		return
	}
	if _, err := w.bot.Slack.PostButtons(ctx, w.session.SlackChannel, w.session.ThreadTS,
		"Pick one:", decisionButtons(w.session.ID, options)); err != nil {
		w.bot.logf("post decision for %s: %v", w.session.ID, err)
	}
}

// say posts one line into the session's thread.
func (w *watcher) say(ctx context.Context, text string) {
	if _, err := w.bot.Slack.PostMessage(ctx, w.session.SlackChannel, w.session.ThreadTS, text); err != nil {
		w.bot.logf("post for %s: %v", w.session.ID, err)
	}
}

func (w *watcher) postApproval(ctx context.Context) {
	screen, err := w.bot.Herdr.Read(ctx, w.session.AgentName, 200)
	if err != nil {
		w.bot.logf("read pane for %s: %v", w.session.ID, err)
	}
	body := "*" + w.session.AgentName + "* needs a decision:\n> " + lastQuestionLine(screen)
	if _, err := w.bot.Slack.PostButtons(ctx, w.session.SlackChannel, w.session.ThreadTS,
		body, approvalButtons(w.session.ID)); err != nil {
		w.bot.logf("post approval for %s: %v", w.session.ID, err)
	}
}

func (w *watcher) postPickup(ctx context.Context) {
	where := "in a terminal"
	if by := w.currentAttachedBy(ctx); by != "" {
		where = "on " + by
	}
	if _, err := w.bot.Slack.PostMessage(ctx, w.session.SlackChannel, w.session.ThreadTS, "Picked up "+where); err != nil {
		w.bot.logf("post pickup for %s: %v", w.session.ID, err)
	}
}

// currentAttachedBy re-reads the row, since `agents attach` writes AttachedBy
// after the watcher started.
func (w *watcher) currentAttachedBy(ctx context.Context) string {
	s, err := w.bot.Store.Get(ctx, w.session.ID)
	if err != nil {
		return w.session.AttachedBy
	}
	return s.AttachedBy
}

// recordStatus keeps the sqlite row roughly in step with herdr.
func (w *watcher) recordStatus(ctx context.Context, st herdr.AgentState) {
	switch st {
	case herdr.StateWorking:
		w.markStatus(ctx, state.StatusWorking)
	case herdr.StateBlocked:
		w.markStatus(ctx, state.StatusBlocked)
	case herdr.StateIdle, herdr.StateDone:
		w.markStatus(ctx, state.StatusIdle)
	}
}

// markStatus does a read, modify, write so it does not clobber fields the
// router changed while the watcher was asleep.
func (w *watcher) markStatus(ctx context.Context, st state.Status) {
	w.bot.mu.Lock()
	defer w.bot.mu.Unlock()
	s, err := w.bot.Store.Get(ctx, w.session.ID)
	if err != nil {
		return
	}
	if s.Status == st {
		return
	}
	s.Status = st
	s.LastActive = time.Now()
	if err := w.bot.Store.Update(ctx, s); err != nil {
		w.bot.logf("status %s for %s: %v", st, w.session.ID, err)
	}
	w.session.Status = st
}

// noteTurnStart resets the elapsed clock and tells the machine we are owed a
// reply. Called when the bot sends the session a prompt.
func (w *watcher) noteTurnStart() {
	w.turnAt = time.Now()
	w.m.prompted()
}
