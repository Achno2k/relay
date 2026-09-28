package live

import (
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"relay/internal/api"
)

// Ported from LiveReplyTrackerTests.swift (LiveReplyTrackerTests and LiveReplySequenceTests),
// same cases and names.

// testClock is a settable clock.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock { return &testClock{now: time.Now()} }

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

var (
	ping        = api.NewLiveTool("Bash", "Ran ping -c 8 127.0.0.1")
	genericBash = api.NewLiveTool("Bash", "Ran a command")
)

func toolPtr(t api.LiveTool) *api.LiveTool { return &t }

func liveEvent(text *string, tool *api.LiveTool, seq int) *api.ServerEvent {
	ev := api.ReplyLive("a1", text, tool, seq)
	return &ev
}

func liveFor(agent string, text *string, seq int) *api.ServerEvent {
	ev := api.ReplyLive(agent, text, nil, seq)
	return &ev
}

func noEvent(t *testing.T, ev *api.ServerEvent) {
	t.Helper()
	if ev != nil {
		t.Fatalf("got a frame %s, want none", frame(ev))
	}
}

func frame(ev *api.ServerEvent) string {
	if ev == nil {
		return "nil"
	}
	b, _ := api.Marshal(*ev)
	return string(b)
}

func expectEvent(t *testing.T, got, want *api.ServerEvent) {
	t.Helper()
	if frame(got) != frame(want) {
		t.Fatalf("got  %s\nwant %s", frame(got), frame(want))
	}
}

func offer(tr *Tracker, text string) *api.ServerEvent {
	return tr.Offer("a1", strPtr(text), nil, false)
}

func TestFirstOfferSendsRightAway(t *testing.T) {
	tr := NewTracker(250*time.Millisecond, nil)
	expectEvent(t, offer(tr, "Hello"), liveEvent(strPtr("Hello"), nil, 1))
}

func TestUnchangedTextIsNotResent(t *testing.T) {
	tr := NewTracker(0, nil)
	offer(tr, "Hello")
	noEvent(t, offer(tr, "Hello"))
}

func TestThrottlesWithinTheMinInterval(t *testing.T) {
	clock := newTestClock()
	tr := NewTracker(250*time.Millisecond, clock.Now)
	if offer(tr, "Hello") == nil {
		t.Fatal("first offer sent nothing")
	}
	clock.advance(100 * time.Millisecond)
	noEvent(t, offer(tr, "Hello there")) // throttled, even though the text changed
	clock.advance(200 * time.Millisecond)
	expectEvent(t, offer(tr, "Hello there"), liveEvent(strPtr("Hello there"), nil, 2))
}

func TestLandedTextClearsAndSuppressesAMatchingOffer(t *testing.T) {
	tr := NewTracker(0, nil)
	offer(tr, "Hello there")
	expectEvent(t, tr.Landed("a1", []string{"Hello there, world"}, nil), liveEvent(nil, nil, 2))
	// Now that the transcript has caught up, offering the same (or a shorter) tail is suppressed.
	noEvent(t, offer(tr, "Hello there"))
	noEvent(t, offer(tr, "Hello there, world"))
}

func TestLandedMarkdownStillMatchesTheRenderedScreen(t *testing.T) {
	// codex/claude render markdown: the screen has no backticks or `**`, the transcript does.
	tr := NewTracker(0, nil)
	offer(tr, "The command waited six seconds, then printed relay-slow-ok.")
	cleared := tr.Landed("a1", []string{"The command waited six seconds, then printed `relay-slow-ok`."}, nil)
	expectEvent(t, cleared, liveEvent(nil, nil, 2))
	// The screen still shows it until the agent goes idle: it must not come back.
	noEvent(t, offer(tr, "The command waited six seconds, then printed relay-slow-ok."))
}

func TestAToolOnlyMessageDoesNotBringBackLandedText(t *testing.T) {
	// The grouped assistant message grows with a toolCall: the earlier landed text stays landed.
	tr := NewTracker(0, nil)
	offer(tr, "Let me check.")
	tr.Landed("a1", []string{"Let me check."}, nil)
	tr.Landed("a1", []string{"Let me check."}, []LandedCall{{ID: "t1", Summary: "Ran ls"}})
	noEvent(t, offer(tr, "Let me check."))
}

func TestLandedWithNothingLiveSendsNoEvent(t *testing.T) {
	tr := NewTracker(0, nil)
	noEvent(t, tr.Landed("a1", []string{"some text"}, nil))
}

func TestStoppedClearsTextAndTool(t *testing.T) {
	tr := NewTracker(0, nil)
	tr.Offer("a1", strPtr("Hello"), toolPtr(ping), false)
	tr.Offer("a1", strPtr("Hello"), toolPtr(ping), false)
	expectEvent(t, tr.Stopped("a1"), liveEvent(nil, nil, 3))
	// A second stop is a no-op: nothing to clear.
	noEvent(t, tr.Stopped("a1"))
}

func TestEmptyTextNeverOffersAFrame(t *testing.T) {
	tr := NewTracker(0, nil)
	noEvent(t, offer(tr, ""))
}

func TestSequenceKeepsIncreasingAcrossOffersAndClears(t *testing.T) {
	tr := NewTracker(0, nil)
	events := []*api.ServerEvent{offer(tr, "One"), offer(tr, "One two"), tr.Stopped("a1"), offer(tr, "Next turn")}
	var seqs []int
	for _, ev := range events {
		if ev != nil {
			seqs = append(seqs, ev.Seq)
		}
	}
	expectEqual(t, seqs, []int{1, 2, 3, 4})
}

func TestDifferentAgentsAreIndependent(t *testing.T) {
	tr := NewTracker(0, nil)
	a := tr.Offer("a1", strPtr("Hello"), nil, false)
	b := tr.Offer("a2", strPtr("Hello"), nil, false)
	expectEvent(t, a, liveFor("a1", strPtr("Hello"), 1))
	expectEvent(t, b, liveFor("a2", strPtr("Hello"), 1))
}

// MARK: Stability

func TestAnEmptyOrShorterReadKeepsTheText(t *testing.T) {
	tr := NewTracker(0, nil)
	offer(tr, "The answer starts here")
	noEvent(t, tr.Offer("a1", nil, nil, false))
	noEvent(t, offer(tr, "The answer"))
	expectEvent(t, offer(tr, "The answer starts here and grows"), liveEvent(strPtr("The answer starts here and grows"), nil, 2))
}

func TestADifferentTextNeedsTwoReadsThenNeverFlipsBack(t *testing.T) {
	tr := NewTracker(0, nil)
	offer(tr, "First block of prose.")
	noEvent(t, offer(tr, "Second block")) // seen once: pending
	expectEvent(t, offer(tr, "Second block grows"), liveEvent(strPtr("Second block grows"), nil, 2))
	// A glitchy read of the old block never brings it back, even twice in a row.
	noEvent(t, offer(tr, "First block of prose."))
	noEvent(t, offer(tr, "First block of prose."))
}

func TestAOneOffDifferentReadIsIgnored(t *testing.T) {
	tr := NewTracker(0, nil)
	offer(tr, "Real reply text")
	noEvent(t, offer(tr, "garbage from a half-drawn frame"))
	expectEvent(t, offer(tr, "Real reply text, more"), liveEvent(strPtr("Real reply text, more"), nil, 2))
}

func TestAScrolledReadExtendsTheText(t *testing.T) {
	// Once the reply is taller than the viewport, the read only has its tail.
	tr := NewTracker(0, nil)
	offer(tr, "Paragraph one is here.\n\nParagraph two is being written")
	ev := offer(tr, "Paragraph two is being written right now.\n\nThree")
	expectEvent(t, ev, liveEvent(strPtr("Paragraph one is here.\n\nParagraph two is being written right now.\n\nThree"), nil, 2))
}

func TestToolNeedsTwoReadsToAppear(t *testing.T) {
	tr := NewTracker(0, nil)
	noEvent(t, tr.Offer("a1", nil, toolPtr(ping), false))
	expectEvent(t, tr.Offer("a1", nil, toolPtr(ping), false), liveEvent(nil, toolPtr(ping), 1))
}

func TestGenericToolRefinesAtOnce(t *testing.T) {
	tr := NewTracker(0, nil)
	tr.Offer("a1", nil, toolPtr(genericBash), true)
	tr.Offer("a1", nil, toolPtr(genericBash), true)
	expectEvent(t, tr.Offer("a1", nil, toolPtr(ping), false), liveEvent(nil, toolPtr(ping), 2))
}

func TestLandedToolCallClearsTheToolAndItStaysCleared(t *testing.T) {
	tr := NewTracker(0, nil)
	tr.Offer("a1", nil, toolPtr(ping), false)
	tr.Offer("a1", nil, toolPtr(ping), false)
	cleared := tr.Landed("a1", nil, []LandedCall{{ID: "toolu_1", Summary: "Ran ping -c 8 127.0.0.1"}})
	expectEvent(t, cleared, liveEvent(nil, nil, 2))
	// The block is still on screen while the command runs: no frame, twice in a row or not.
	noEvent(t, tr.Offer("a1", nil, toolPtr(ping), false))
	noEvent(t, tr.Offer("a1", nil, toolPtr(ping), false))
}

func TestAToolCallWithADifferentSummaryStillClearsTheBlock(t *testing.T) {
	// The screen's summary can't always match (a wrapped command); any toolCall that lands
	// after the block showed up is its toolCall.
	tr := NewTracker(0, nil)
	partial := api.NewLiveTool("Bash", "Ran for i in 1 2 3; do echo")
	tr.Offer("a1", nil, toolPtr(partial), false)
	tr.Offer("a1", nil, toolPtr(partial), false)
	cleared := tr.Landed("a1", nil, []LandedCall{{ID: "toolu_1", Summary: "Ran for i in 1 2 3; do echo tick $i; done"}})
	expectEvent(t, cleared, liveEvent(nil, nil, 2))
	noEvent(t, tr.Offer("a1", nil, toolPtr(partial), false))
	noEvent(t, tr.Offer("a1", nil, toolPtr(partial), false))
}

func TestToolCallsLandedBeforeTheBlockDontClearIt(t *testing.T) {
	tr := NewTracker(0, nil)
	tr.Landed("a1", nil, []LandedCall{{ID: "toolu_0", Summary: "Read README.md"}})
	tr.Offer("a1", nil, toolPtr(ping), false)
	expectEvent(t, tr.Offer("a1", nil, toolPtr(ping), false), liveEvent(nil, toolPtr(ping), 1))
}

func TestTracksTextAndToolIndependently(t *testing.T) {
	tr := NewTracker(0, nil)
	tr.Offer("a1", strPtr("Let me ping it."), toolPtr(ping), false)
	tr.Offer("a1", strPtr("Let me ping it."), toolPtr(ping), false)
	// The text lands first; the tool stays.
	expectEvent(t, tr.Landed("a1", []string{"Let me ping it."}, nil), liveEvent(nil, toolPtr(ping), 3))
}

// MARK: Wire shape

func TestEncodesToolAndNulls(t *testing.T) {
	// Swift's test sorts keys; the Go encoder writes them in api.md's order.
	withTool, _ := api.Marshal(api.ReplyLive("w1:p1", nil, toolPtr(ping), 3))
	expectEqual(t, string(withTool), `{"type":"reply.live","agentId":"w1:p1","text":null,"tool":{"name":"Bash","summary":"Ran ping -c 8 127.0.0.1","state":"running"},"seq":3}`)
	cleared, _ := api.Marshal(api.ReplyLive("w1:p1", strPtr("Hi"), nil, 4))
	expectEqual(t, string(cleared), `{"type":"reply.live","agentId":"w1:p1","text":"Hi","tool":null,"seq":4}`)
}

// MARK: Sequences

// Real screen sequences, through the parser and the tracker together: whatever the screen does
// between reads, the client never sees a state come back once it was replaced.

const sequenceBottom = `─────────────────────────────────────────────────────────────
❯
─────────────────────────────────────────────────────────────
  82.9k/1.0M · in 82.9k out 148 · wk 11%(4d12h)`

func sequenceScreen(turn string) string {
	return `⏺ You chose: Left.

✻ Cooked for 3s · done 2:15 AM

❯ Run ping -c 8 127.0.0.1 then say one sentence about it.

` + turn + `

✻ Crafting… (3s · ↓ 23 tokens)
` + sequenceBottom
}

type seen struct {
	text *string
	tool *api.LiveTool
}

// replay feeds screens through parser + tracker the way Monitor does, interleaving transcript
// landings at the given read indexes. Returns what the client saw.
func replay(screens []string, landings map[int][]api.Block) []seen {
	tr := NewTracker(0, nil)
	var events []*api.ServerEvent
	for i, s := range screens {
		if blocks, ok := landings[i]; ok {
			events = append(events, tr.LandedBlocks("a1", blocks))
		}
		parsed := Parse(s, "claude")
		var tool *api.LiveTool
		generic := false
		if parsed.Tool != nil {
			tool = toolPtr(api.NewLiveTool(parsed.Tool.Name, parsed.Tool.Summary(noCwd)))
			generic = parsed.Tool.Generic
		}
		events = append(events, tr.Offer("a1", parsed.Text, tool, generic))
	}
	events = append(events, tr.Stopped("a1"))
	var out []seen
	for _, ev := range events {
		if ev != nil {
			out = append(out, seen{ev.Text, ev.Tool})
		}
	}
	return out
}

func opt(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// expectNoFlipFlop: no state (text or tool) shows up again after the client moved away from it.
func expectNoFlipFlop(t *testing.T, got []seen) {
	t.Helper()
	texts := make([]*string, len(got))
	tools := make([]*string, len(got))
	for i, s := range got {
		texts[i] = s.text
		if s.tool != nil {
			tools[i] = strPtr(s.tool.Summary)
		}
	}
	for _, c := range []struct {
		label  string
		values []*string
	}{{"text", texts}, {"tool", tools}} {
		left := map[string]bool{}
		var previous *string
		started := false
		for _, v := range c.values {
			if started && !sameText(previous, v) && previous != nil {
				left[*previous] = true
			}
			if v != nil && left[*v] {
				t.Errorf("%s came back: %s", c.label, *v)
			}
			previous, started = v, true
		}
	}
	for _, s := range got {
		if s.text == nil {
			continue
		}
		if strings.Contains(*s.text, "Running") || strings.Contains(*s.text, "ping -c") || *s.text == "You chose: Left." {
			t.Errorf("tool or previous-turn text leaked: %q", *s.text)
		}
	}
}

func TestClaudeToolRunWithBlinkingDot(t *testing.T) {
	// What round 6 captured live: the grouped label, then the described block with a blinking
	// `⏺`, the tool_use landing ~5s in, then the reply.
	dotOn := sequenceScreen("⏺ Pinging localhost 8 times · 2s\n  ⎿  $ ping -c 8 127.0.0.1")
	dotOff := sequenceScreen("  Pinging localhost 8 times · 2s\n  ⎿  $ ping -c 8 127.0.0.1")
	screens := []string{
		sequenceScreen(""),
		sequenceScreen("⏺ Running 1 shell command…"),
		sequenceScreen("  Running 1 shell command…"),
		sequenceScreen("⏺ Running 1 shell command…"),
		dotOff, dotOn, dotOff, dotOn, dotOff, dotOn, // index 9: tool_use lands before this read
		dotOff, dotOn, dotOff,
		sequenceScreen("⏺ Ran 1 shell command\n  ⎿  $ ping -c 8 127.0.0.1\n\n⏺ All 8 packets got"),
		sequenceScreen("⏺ Ran 1 shell command\n  ⎿  $ ping -c 8 127.0.0.1\n\n⏺ All 8 packets got through with no loss."),
	}
	toolUse := api.ToolCallBlock("toolu_1", "Bash", "Ran ping -c 8 127.0.0.1", "{}")
	got := replay(screens, map[int][]api.Block{9: {toolUse}})
	expectNoFlipFlop(t, got)

	var tools, texts []string
	for _, s := range got {
		if s.tool != nil {
			tools = append(tools, s.tool.Summary)
		} else {
			tools = append(tools, "<nil>")
		}
		texts = append(texts, opt(s.text))
	}
	expectEqual(t, tools, []string{"Ran a command", "Ran ping -c 8 127.0.0.1", "<nil>", "<nil>", "<nil>", "<nil>"})
	expectEqual(t, texts, []string{"<nil>", "<nil>", "<nil>", "All 8 packets got", "All 8 packets got through with no loss.", "<nil>"})
}

func TestClaudeProseThenToolThenProse(t *testing.T) {
	screens := []string{
		sequenceScreen("⏺ Let me check the"),
		sequenceScreen("⏺ Let me check the loopback first."),
		sequenceScreen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  Running…"),
		sequenceScreen("⏺ Let me check the loopback first.\n\n  Bash(ping -c 8 127.0.0.1)\n  ⎿  Running…"),
		sequenceScreen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  Running…"),
		sequenceScreen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  PING 127.0.0.1\n\n⏺ It works"),
		sequenceScreen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  PING 127.0.0.1\n\n⏺ It works fine."),
		sequenceScreen("⏺ Let me check the loopback first.\n\n⏺ Bash(ping -c 8 127.0.0.1)\n  ⎿  PING 127.0.0.1\n\n⏺ It works fine."),
	}
	landedFirst := []api.Block{
		api.TextBlock("Let me check the loopback first."),
		api.ToolCallBlock("toolu_1", "Bash", "Ran ping -c 8 127.0.0.1", "{}"),
	}
	got := replay(screens, map[int][]api.Block{4: landedFirst})
	expectNoFlipFlop(t, got)
	if !slices.ContainsFunc(got, func(s seen) bool { return sameTool(s.tool, toolPtr(ping)) }) {
		t.Error("never showed the ping tool")
	}
	if last := got[len(got)-1]; last.text != nil || last.tool != nil {
		t.Errorf("last frame didn't clear: %s / %v", opt(last.text), last.tool)
	}
	if !slices.ContainsFunc(got, func(s seen) bool { return opt(s.text) == "It works fine." }) {
		t.Error("never showed the final text")
	}
}

func TestAlternatingGlitchReadsNeverFlipTheText(t *testing.T) {
	// Every other read catches a half-drawn frame with nothing parseable, or an old block.
	var screens []string
	words := []string{"The", "The answer", "The answer is", "The answer is forty", "The answer is forty-two."}
	for _, w := range words {
		screens = append(screens, sequenceScreen("⏺ "+w), sequenceScreen(""), sequenceScreen("⏺ Some older block that was on screen"))
	}
	got := replay(screens, nil)
	expectNoFlipFlop(t, got)
	var texts []string
	for _, s := range got {
		if s.text != nil {
			texts = append(texts, *s.text)
		}
	}
	expectEqual(t, texts, words)
}

