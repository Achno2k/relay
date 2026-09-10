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

func TestReplyReaderReadsTheFileOncePerWrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ReplyRelPath)
	if err := os.WriteFile(path, []byte("# Done\n\nFixed the 500."), 0o644); err != nil {
		t.Fatal(err)
	}

	r := replyReader{worktree: dir}

	text, ok := r.read()
	if !ok || !strings.Contains(text, "Fixed the 500.") {
		t.Fatalf("first read: got %q, ok=%v", text, ok)
	}

	// Unchanged file: the turn wrote nothing, and there is no pane to fall
	// back to any more.
	if text, ok := r.read(); ok {
		t.Fatalf("second read should report no reply, got %q", text)
	}

	// A rewrite is picked up again.
	later := time.Now().Add(2 * time.Second)
	if err := os.WriteFile(path, []byte("# Done again"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	text, ok = r.read()
	if !ok || !strings.Contains(text, "Done again") {
		t.Fatalf("third read: got %q, ok=%v", text, ok)
	}
}

func TestReplyReaderReportsNothingWithoutAFile(t *testing.T) {
	if _, ok := (&replyReader{}).read(); ok {
		t.Fatal("no worktree means no reply")
	}
	if _, ok := (&replyReader{worktree: t.TempDir()}).read(); ok {
		t.Fatal("a missing file means no reply")
	}

	// An empty file is not a reply either.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ReplyRelPath), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := (&replyReader{worktree: dir}).read(); ok {
		t.Fatal("a blank file means no reply")
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
