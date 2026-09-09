package slackbot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Achno2k/agents-cli/internal/herdr"
)

// event is one input to the state machine: either a herdr state or the bot
// sending a prompt.
type event struct {
	state    herdr.AgentState
	prompted bool
}

func TestMachineNormalTurn(t *testing.T) {
	var m machine
	m.prompted()

	if got := m.next(herdr.StateWorking); got != (step{Reaction: reactionWorking}) {
		t.Fatalf("working: got %+v", got)
	}
	if got := m.next(herdr.StateIdle); got != (step{Reaction: reactionIdle, Post: postReply}) {
		t.Fatalf("idle: got %+v", got)
	}
	// A second idle is the same state, so it says nothing.
	if got := m.next(herdr.StateIdle); got != (step{}) {
		t.Fatalf("repeat idle: got %+v", got)
	}
}

func TestMachineBlockedAsksForApproval(t *testing.T) {
	var m machine
	m.prompted()
	m.next(herdr.StateWorking)

	if got := m.next(herdr.StateBlocked); got != (step{Reaction: reactionBlocked, Post: postApproval}) {
		t.Fatalf("blocked: got %+v", got)
	}
	// Answering the prompt puts it back to work; the turn is still ours.
	if got := m.next(herdr.StateWorking); got != (step{Reaction: reactionWorking}) {
		t.Fatalf("resumed: got %+v", got)
	}
	if got := m.next(herdr.StateIdle); got != (step{Reaction: reactionIdle, Post: postReply}) {
		t.Fatalf("finished: got %+v", got)
	}
}

func TestMachineTerminalPickupGoesQuiet(t *testing.T) {
	var m machine

	// Work nobody asked for in Slack: somebody is at the keyboard.
	if got := m.next(herdr.StateWorking); got != (step{Reaction: reactionWorking, Post: postPickup}) {
		t.Fatalf("pickup: got %+v", got)
	}
	// From here the thread only tracks reactions, it does not narrate.
	if got := m.next(herdr.StateIdle); got != (step{Reaction: reactionIdle}) {
		t.Fatalf("quiet idle: got %+v", got)
	}
	if got := m.next(herdr.StateBlocked); got != (step{Reaction: reactionBlocked}) {
		t.Fatalf("quiet blocked: got %+v", got)
	}
	// A second stretch of terminal work must not announce itself again.
	if got := m.next(herdr.StateWorking); got != (step{Reaction: reactionWorking}) {
		t.Fatalf("second pickup should be silent: got %+v", got)
	}

	// The next mention takes the session back.
	m.prompted()
	if got := m.next(herdr.StateIdle); got != (step{Reaction: reactionIdle, Post: postReply}) {
		t.Fatalf("after a new prompt: got %+v", got)
	}
}

func TestMachineIdleWithoutAPromptSaysNothing(t *testing.T) {
	var m machine
	if got := m.next(herdr.StateIdle); got != (step{Reaction: reactionIdle}) {
		t.Fatalf("got %+v", got)
	}
}

func TestMachineDoneCountsAsIdle(t *testing.T) {
	var m machine
	m.prompted()
	m.next(herdr.StateWorking)
	if got := m.next(herdr.StateDone); got != (step{Reaction: reactionIdle, Post: postReply}) {
		t.Fatalf("got %+v", got)
	}
}

func TestMachineUnknownLeavesTheThreadAlone(t *testing.T) {
	var m machine
	m.prompted()
	m.next(herdr.StateWorking)
	if got := m.next(herdr.StateUnknown); got != (step{}) {
		t.Fatalf("got %+v", got)
	}
}

func TestMachineDeadIsPostedOnce(t *testing.T) {
	var m machine
	m.prompted()
	m.next(herdr.StateWorking)
	if got := m.dead(); got != (step{Reaction: reactionDead}) {
		t.Fatalf("got %+v", got)
	}
	if got := m.dead(); got != (step{}) {
		t.Fatalf("dead twice should be silent: got %+v", got)
	}
}

func TestMachineSequence(t *testing.T) {
	events := []event{
		{prompted: true},
		{state: herdr.StateWorking},
		{state: herdr.StateBlocked},
		{state: herdr.StateWorking},
		{state: herdr.StateIdle},
	}
	want := []watchPost{postNothing, postNothing, postApproval, postNothing, postReply}

	var m machine
	for i, e := range events {
		var got step
		if e.prompted {
			m.prompted()
		} else {
			got = m.next(e.state)
		}
		if got.Post != want[i] {
			t.Fatalf("event %d: got post %v, want %v", i, got.Post, want[i])
		}
	}
}

func TestWaitStatesExcludeTheCurrentOne(t *testing.T) {
	all := waitStates(herdr.StateUnknown, false)
	if len(all) != 4 {
		t.Fatalf("a fresh watcher should wait for every state, got %v", all)
	}
	got := waitStates(herdr.StateWorking, true)
	for _, s := range got {
		if s == herdr.StateWorking {
			t.Fatalf("waiting for the state we are in spins the loop: %v", got)
		}
	}
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}

func TestReplyReaderPrefersTheFileThenFallsBackToThePane(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ReplyRelPath)
	if err := os.WriteFile(path, []byte("# Done\n\nFixed the 500."), 0o644); err != nil {
		t.Fatal(err)
	}

	h := newFakeHerdr()
	h.readText = "pane scrollback"
	r := replyReader{worktree: dir}

	text, source := r.read(context.Background(), h, "agents-cli-a3f2")
	if source != "reply.md" || !strings.Contains(text, "Fixed the 500.") {
		t.Fatalf("first read: got %q from %s", text, source)
	}

	// Unchanged file: the turn produced nothing new, so use the pane.
	text, source = r.read(context.Background(), h, "agents-cli-a3f2")
	if source != "pane" || text != "pane scrollback" {
		t.Fatalf("second read: got %q from %s", text, source)
	}

	// A rewrite is picked up again.
	later := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(path, []byte("# Done again"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	text, source = r.read(context.Background(), h, "agents-cli-a3f2")
	if source != "reply.md" || !strings.Contains(text, "Done again") {
		t.Fatalf("third read: got %q from %s", text, source)
	}
}

func TestReplyReaderWithoutAWorktreeUsesThePane(t *testing.T) {
	h := newFakeHerdr()
	h.readText = "only the pane"
	r := replyReader{}
	text, source := r.read(context.Background(), h, "a")
	if source != "pane" || text != "only the pane" {
		t.Fatalf("got %q from %s", text, source)
	}
}

func TestTrimReply(t *testing.T) {
	if got := trimReply("   "); !strings.Contains(got, "without writing a reply") {
		t.Fatalf("got %q", got)
	}
	if got := trimReply("short"); got != "short" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("a line of the reply\n", 200)
	got := trimReply(long)
	if len(got) > maxReplyChars+64 {
		t.Fatalf("trimmed reply is %d chars, want about %d", len(got), maxReplyChars)
	}
	if !strings.Contains(got, "say `full`") {
		t.Fatalf("truncation should say how to get the rest: %q", got[len(got)-80:])
	}
}

func TestSummaryOf(t *testing.T) {
	stat := " internal/ui/ui.go | 12 ++++--\n internal/ui/theme.go | 3 +-\n 2 files changed, 13 insertions(+), 2 deletions(-)\n"
	if got := summaryOf(stat); got != "2 files changed, 13 insertions(+), 2 deletions(-)" {
		t.Fatalf("got %q", got)
	}
	if got := summaryOf(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestStatsLineWithoutAWorktree(t *testing.T) {
	got := statsLine(context.Background(), "", 92*time.Second)
	if !strings.Contains(got, "no changes") || !strings.Contains(got, "1m32s") {
		t.Fatalf("got %q", got)
	}
}

func TestLastScreenKeepsTheTail(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 100; i++ {
		b.WriteString("line\n")
	}
	b.WriteString("Do you want to proceed?")
	got := lastScreen(b.String())
	if !strings.HasSuffix(got, "Do you want to proceed?") {
		t.Fatalf("the prompt should be the last thing kept: %q", got)
	}
	if strings.Count(got, "\n") > 25 {
		t.Fatalf("kept too many lines: %d", strings.Count(got, "\n"))
	}
}
