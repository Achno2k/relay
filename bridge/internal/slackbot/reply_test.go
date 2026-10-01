package slackbot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/Achno2k/agents-cli/internal/state"
)

func TestParseReplyPlainText(t *testing.T) {
	got := parseReply("Fixed the 500 on empty passwords.\n\nOne test added.")
	if got.Text != "Fixed the 500 on empty passwords.\n\nOne test added." {
		t.Fatalf("text = %q", got.Text)
	}
	if len(got.Artifacts) != 0 || len(got.Decision) != 0 {
		t.Fatalf("nothing should have been parsed off: %+v", got)
	}
}

func TestParseReplyTakesArtifactLines(t *testing.T) {
	raw := strings.Join([]string{
		"Wrote the migration plan.",
		"",
		"artifact: migration-plan.md",
		"artifact: rollback-notes.md",
	}, "\n")

	got := parseReply(raw)
	if got.Text != "Wrote the migration plan." {
		t.Fatalf("text = %q", got.Text)
	}
	if want := []string{"migration-plan.md", "rollback-notes.md"}; !equalStrings(got.Artifacts, want) {
		t.Fatalf("artifacts = %v, want %v", got.Artifacts, want)
	}
}

// An artifact line in the middle of a reply is still an artifact line, and
// still noise in the posted text.
func TestParseReplyTakesArtifactLinesAnywhere(t *testing.T) {
	got := parseReply("Here is the plan.\nartifact: plan.md\nTell me what you think.")
	if got.Text != "Here is the plan.\nTell me what you think." {
		t.Fatalf("text = %q", got.Text)
	}
	if !equalStrings(got.Artifacts, []string{"plan.md"}) {
		t.Fatalf("artifacts = %v", got.Artifacts)
	}
}

// A reply that quotes the contract must not be eaten by it.
func TestParseReplyIgnoresArtifactAndDecisionInsideCodeBlocks(t *testing.T) {
	raw := strings.Join([]string{
		"The contract says to write:",
		"```",
		"artifact: plan.md",
		"decision:",
		"1. one",
		"2. two",
		"```",
		"That is all.",
	}, "\n")

	got := parseReply(raw)
	if len(got.Artifacts) != 0 {
		t.Fatalf("artifacts = %v, want none", got.Artifacts)
	}
	if len(got.Decision) != 0 {
		t.Fatalf("decision = %v, want none", got.Decision)
	}
	if !strings.Contains(got.Text, "artifact: plan.md") {
		t.Fatalf("the quoted block should survive:\n%s", got.Text)
	}
}

func TestParseReplyTakesABareDecisionBlock(t *testing.T) {
	raw := strings.Join([]string{
		"Two ways to do this.",
		"",
		"decision:",
		"1. Rewrite the query",
		"2. Add an index",
		"3. Leave it",
	}, "\n")

	got := parseReply(raw)
	if got.Text != "Two ways to do this." {
		t.Fatalf("text = %q", got.Text)
	}
	want := []string{"Rewrite the query", "Add an index", "Leave it"}
	if !equalStrings(got.Decision, want) {
		t.Fatalf("decision = %v, want %v", got.Decision, want)
	}
}

// The instruction file shows the block fenced, so agents will send it fenced.
func TestParseReplyTakesAFencedDecisionBlock(t *testing.T) {
	raw := strings.Join([]string{
		"Two ways to do this.",
		"",
		"```",
		"decision:",
		"1. Rewrite the query",
		"2. Add an index",
		"```",
	}, "\n")

	got := parseReply(raw)
	if got.Text != "Two ways to do this." {
		t.Fatalf("text = %q", got.Text)
	}
	if !equalStrings(got.Decision, []string{"Rewrite the query", "Add an index"}) {
		t.Fatalf("decision = %v", got.Decision)
	}
}

func TestParseReplyCapsDecisionOptions(t *testing.T) {
	raw := "Pick.\n\ndecision:\n1. a\n2. b\n3. c\n4. d\n5. e\n6. f"
	got := parseReply(raw)
	if len(got.Decision) != maxDecisionOptions {
		t.Fatalf("got %d options, want %d: %v", len(got.Decision), maxDecisionOptions, got.Decision)
	}
	if got.Decision[0] != "a" || got.Decision[3] != "d" {
		t.Fatalf("wrong options kept: %v", got.Decision)
	}
}

func TestParseReplyRejectsMalformedDecisionBlocks(t *testing.T) {
	cases := map[string]string{
		"one option":     "Pick.\n\ndecision:\n1. only one",
		"prose inside":   "Pick.\n\ndecision:\n1. a\nactually never mind\n2. b",
		"no options":     "Pick.\n\ndecision:",
		"not at the end": "decision:\n1. a\n2. b\n\nAnd then I carried on.",
		"no header":      "Pick.\n\n1. a\n2. b",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseReply(raw)
			if len(got.Decision) != 0 {
				t.Fatalf("decision = %v, want none; text = %q", got.Decision, got.Text)
			}
			if strings.TrimSpace(got.Text) == "" {
				t.Fatalf("the text should have been left alone, got %q", got.Text)
			}
		})
	}
}

func TestParseReplyTakesBothSections(t *testing.T) {
	raw := strings.Join([]string{
		"Drafted the plan.",
		"",
		"artifact: plan.md",
		"",
		"decision:",
		"1. Ship it",
		"2. Revise",
	}, "\n")

	got := parseReply(raw)
	if got.Text != "Drafted the plan." {
		t.Fatalf("text = %q", got.Text)
	}
	if !equalStrings(got.Artifacts, []string{"plan.md"}) {
		t.Fatalf("artifacts = %v", got.Artifacts)
	}
	if !equalStrings(got.Decision, []string{"Ship it", "Revise"}) {
		t.Fatalf("decision = %v", got.Decision)
	}
}

func TestParseReplyAcceptsNumberedOptionsWithABracket(t *testing.T) {
	got := parseReply("Pick.\n\ndecision:\n1) a\n2) b")
	if !equalStrings(got.Decision, []string{"a", "b"}) {
		t.Fatalf("decision = %v", got.Decision)
	}
}

// --- mrkdwn ---

func TestToMrkdwn(t *testing.T) {
	cases := map[string]struct{ in, want string }{
		"double asterisk bold": {"**done** and **dusted**", "*done* and *dusted*"},
		"heading to bold":      {"# Summary", "*Summary*"},
		"deep heading to bold": {"### Deep heading", "*Deep heading*"},
		"indented heading":     {"  ## Indented", "*Indented*"},
		"markdown link":        {"see [the docs](https://x.dev/a)", "see <https://x.dev/a|the docs>"},
		"bare link":            {"[https://x.dev](https://x.dev)", "<https://x.dev>"},
		"already mrkdwn":       {"*bold* and _italic_", "*bold* and _italic_"},
		"hash mid line":        {"issue #42 is fixed", "issue #42 is fixed"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := toMrkdwn(tc.in); got != tc.want {
				t.Fatalf("toMrkdwn(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestToMrkdwnStripsFenceLanguageTags(t *testing.T) {
	in := "Run this:\n```go\nfmt.Println(\"hi\")\n```\ndone"
	want := "Run this:\n```\nfmt.Println(\"hi\")\n```\ndone"
	if got := toMrkdwn(in); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Inside a code block the characters are the content, not formatting.
func TestToMrkdwnLeavesCodeBlocksAlone(t *testing.T) {
	in := strings.Join([]string{
		"Before **bold**",
		"```bash",
		"echo **not bold** # not a heading",
		"curl [x](https://y)",
		"```",
		"After **bold**",
	}, "\n")
	got := toMrkdwn(in)

	if !strings.Contains(got, "echo **not bold** # not a heading") {
		t.Fatalf("code block was rewritten:\n%s", got)
	}
	if !strings.Contains(got, "curl [x](https://y)") {
		t.Fatalf("a link inside code was rewritten:\n%s", got)
	}
	if !strings.Contains(got, "Before *bold*") || !strings.Contains(got, "After *bold*") {
		t.Fatalf("text outside the block was not rewritten:\n%s", got)
	}
	if strings.Contains(got, "```bash") {
		t.Fatalf("the language tag survived:\n%s", got)
	}
}

// --- approval question ---

func TestLastQuestionLine(t *testing.T) {
	screen := strings.Join([]string{
		"● Reading src/auth.go",
		"● Editing src/auth.go",
		"╭──────────────────────────────╮",
		"│ Edit file                    │",
		"│ Do you want to proceed?      │",
		"│ ❯ 1. Yes                     │",
		"│   2. No                      │",
		"╰──────────────────────────────╯",
	}, "\n")

	if got := lastQuestionLine(screen); got != "Do you want to proceed?" {
		t.Fatalf("got %q", got)
	}
}

func TestLastQuestionLineFallsBackToTheLastLine(t *testing.T) {
	if got := lastQuestionLine("working\nstill working\nAllow Codex to run npm install"); got != "Allow Codex to run npm install" {
		t.Fatalf("got %q", got)
	}
	if got := lastQuestionLine("   \n\n"); !strings.Contains(got, "empty") {
		t.Fatalf("got %q", got)
	}
}

// --- decision buttons ---

func TestDecisionButtons(t *testing.T) {
	btns := decisionButtons("a3f2", []string{"Rewrite the query", "Add an index", "Leave it"})
	if len(btns) != 3 {
		t.Fatalf("got %d buttons", len(btns))
	}
	if btns[0].Style != "primary" {
		t.Fatal("the first option is the recommendation and should lead")
	}
	if btns[1].Style != "" {
		t.Fatalf("only the first button is styled, got %q", btns[1].Style)
	}
	for i, b := range btns {
		if !isDecisionAction(b.ActionID) {
			t.Fatalf("button %d has action id %q", i, b.ActionID)
		}
		if isApprovalAction(b.ActionID) {
			t.Fatalf("button %d collides with the approval row: %q", i, b.ActionID)
		}
	}
	if btns[0].Label != "1. Rewrite the query" {
		t.Fatalf("label = %q", btns[0].Label)
	}
}

func TestDecisionButtonsCapAtFour(t *testing.T) {
	btns := decisionButtons("a3f2", []string{"a", "b", "c", "d", "e"})
	if len(btns) != maxDecisionOptions {
		t.Fatalf("got %d buttons, want %d", len(btns), maxDecisionOptions)
	}
}

func TestDecisionButtonLabelIsTruncatedForSlack(t *testing.T) {
	long := strings.Repeat("a very long option ", 10)
	btns := decisionButtons("a3f2", []string{long, "short"})
	if n := len([]rune(btns[0].Label)); n > 75 {
		t.Fatalf("label is %d runes, Slack caps at 75", n)
	}
	if !strings.HasSuffix(btns[0].Label, "…") {
		t.Fatalf("a truncated label should say so: %q", btns[0].Label)
	}
}

func TestDecisionClickRoundTrips(t *testing.T) {
	const option = "Rewrite the query, then re-run the suite"
	id := decisionActionID("a3f2", 2)

	sess, n, opt, ok := decodeDecision(id, option)
	if !ok || sess != "a3f2" || n != 2 || opt != option {
		t.Fatalf("decode(%q, %q) = %q, %d, %q, %v", id, option, sess, n, opt, ok)
	}
	if got := decisionPrompt(n, opt); got != "2. "+option {
		t.Fatalf("prompt = %q", got)
	}
}

// The regression this replaced: Slack strips control characters out of a
// button value, so nothing structural may live in it.
func TestDecisionButtonValuesCarryNoControlCharacters(t *testing.T) {
	btns := decisionButtons("539c", []string{
		"Apply the doc changes on a feature branch (no PR yet)",
		"Open a PR",
	})
	for i, b := range btns {
		for _, r := range b.Value {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("button %d value holds %q, which Slack will strip: %q", i, r, b.Value)
			}
		}
		if b.Value != []string{
			"Apply the doc changes on a feature branch (no PR yet)",
			"Open a PR",
		}[i] {
			t.Fatalf("button %d value = %q, want the option text alone", i, b.Value)
		}
	}

	// A click reconstructed from what Slack sends back still decodes.
	sess, n, opt, ok := decodeDecision(btns[0].ActionID, btns[0].Value)
	if !ok || sess != "539c" || n != 1 || opt != btns[0].Value {
		t.Fatalf("decode = %q, %d, %q, %v", sess, n, opt, ok)
	}
}

func TestDecodeDecisionRejectsJunk(t *testing.T) {
	cases := []struct{ actionID, value string }{
		{"", "opt"},              // not a decision at all
		{"approve_allow", "opt"}, // the other row
		{"decide_", "opt"},       // nothing packed
		{"decide_a3f2", "opt"},   // no option number
		{"decide_a3f2_", "opt"},  // empty option number
		{"decide__1", "opt"},     // empty session
		{"decide_a3f2_x", "opt"}, // option number is not a number
		{"decide_a3f2_0", "opt"}, // options are one based
		{"decide_a3f2_1", ""},    // no option text
		{"decide_a3f2_1", "   "}, // blank option text
	}
	for _, tc := range cases {
		if _, _, _, ok := decodeDecision(tc.actionID, tc.value); ok {
			t.Fatalf("decodeDecision(%q, %q) should have failed", tc.actionID, tc.value)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- watcher, end to end over the fakes ---

func newTestWatcher(t *testing.T) (*watcher, *fakeSlack, *fakeHerdr, string) {
	t.Helper()
	b, sc, h, _ := testBot(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ArtifactsRelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	sess := liveSession("a3f2", "100.1")
	sess.WorktreePath = dir
	if err := b.Store.Create(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	return newWatcher(b, sess), sc, h, dir
}

func writeReply(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ReplyRelPath), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A turn that never wrote the file gets one line, and no stats line: there is
// nothing to report statistics about.
func TestPostReplyWithoutAFileSaysSo(t *testing.T) {
	w, sc, _, _ := newTestWatcher(t)
	w.postReply(context.Background())

	got := sc.texts()
	if len(got) != 1 || got[0] != noReplyMessage {
		t.Fatalf("got %q", got)
	}
}

func TestPostReplyPostsTextThenArtifactsThenButtons(t *testing.T) {
	w, sc, _, dir := newTestWatcher(t)
	if err := os.WriteFile(filepath.Join(dir, ArtifactsRelPath, "plan.md"),
		[]byte("# Plan\n\nStep one."), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReply(t, dir, strings.Join([]string{
		"Drafted the **plan**.",
		"",
		"artifact: plan.md",
		"",
		"decision:",
		"1. Ship it",
		"2. Revise",
	}, "\n"))

	w.postReply(context.Background())

	texts := sc.texts()
	if len(texts) != 2 {
		t.Fatalf("expected the reply and the button message, got %q", texts)
	}
	if !strings.Contains(texts[0], "Drafted the *plan*.") {
		t.Fatalf("the mrkdwn pass did not run: %q", texts[0])
	}
	if strings.Contains(texts[0], "artifact:") || strings.Contains(texts[0], "decision:") {
		t.Fatalf("the trailing sections should have been stripped: %q", texts[0])
	}
	if !strings.Contains(texts[0], "·") {
		t.Fatalf("the stats line is missing: %q", texts[0])
	}

	if len(sc.uploads) != 1 || sc.uploads[0].Filename != "plan.md" {
		t.Fatalf("uploads = %+v", sc.uploads)
	}
	if !strings.Contains(sc.uploads[0].Content, "Step one.") {
		t.Fatalf("wrong file uploaded: %q", sc.uploads[0].Content)
	}

	last := sc.posts[len(sc.posts)-1]
	if len(last.Buttons) != 2 {
		t.Fatalf("expected two decision buttons, got %+v", last.Buttons)
	}
	if !isDecisionAction(last.Buttons[0].ActionID) {
		t.Fatalf("wrong action id: %q", last.Buttons[0].ActionID)
	}
}

func TestPostReplyReportsAMissingArtifact(t *testing.T) {
	w, sc, _, dir := newTestWatcher(t)
	writeReply(t, dir, "Done.\n\nartifact: nope.md")

	w.postReply(context.Background())

	texts := sc.texts()
	if len(texts) != 2 || texts[1] != "artifact nope.md not found" {
		t.Fatalf("got %q", texts)
	}
	if len(sc.uploads) != 0 {
		t.Fatalf("nothing should have been uploaded: %+v", sc.uploads)
	}
}

// An artifact name is agent-supplied, so it must not be able to reach outside
// the artifacts directory.
func TestPostReplyRefusesArtifactPathEscapes(t *testing.T) {
	w, sc, _, dir := newTestWatcher(t)
	if err := os.WriteFile(filepath.Join(dir, "secret.md"), []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReply(t, dir, "Done.\n\nartifact: ../secret.md")

	w.postReply(context.Background())

	for _, u := range sc.uploads {
		if strings.Contains(u.Content, "private") {
			t.Fatalf("a path escape leaked a file: %+v", u)
		}
	}
}

func TestPostApprovalShowsOnlyTheQuestion(t *testing.T) {
	w, sc, h, _ := newTestWatcher(t)
	var screen strings.Builder
	for i := 0; i < 40; i++ {
		screen.WriteString("● doing something noisy\n")
	}
	screen.WriteString("│ Do you want to proceed? │\n│ ❯ 1. Yes │\n")
	h.readText = screen.String()

	w.postApproval(context.Background())

	if len(sc.posts) != 1 {
		t.Fatalf("got %d posts", len(sc.posts))
	}
	body := sc.posts[0].Text
	if !strings.Contains(body, "Do you want to proceed?") {
		t.Fatalf("the question is missing: %q", body)
	}
	if strings.Contains(body, "doing something noisy") {
		t.Fatalf("the screen leaked into the thread: %q", body)
	}
	if n := strings.Count(body, "\n"); n > 1 {
		t.Fatalf("an approval should be two lines, got %d:\n%s", n+1, body)
	}
}

// --- clicking a decision button ---

func TestDecisionClickSendsThePickAsTheNextPrompt(t *testing.T) {
	b, sc, h, _ := testBot(t)
	sess := liveSession("a3f2", "100.1")
	if err := b.Store.Create(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	sc.names = map[string]string{"U1": "aman"}

	b.HandleInteraction(context.Background(), slack.InteractionCallback{
		User:    slack.User{ID: "U1"},
		Message: slack.Message{Msg: slack.Msg{Timestamp: "ts-buttons"}},
		ActionCallback: slack.ActionCallbacks{
			BlockActions: []*slack.BlockAction{{
				ActionID: decisionActionID("a3f2", 2),
				Value:    "Add an index",
			}},
		},
	})

	if len(h.prompts) != 1 || h.prompts[0] != "2. Add an index" {
		t.Fatalf("prompts = %q", h.prompts)
	}
	if len(h.keys) != 0 {
		t.Fatalf("a decision is a prompt, not key presses: %v", h.keys)
	}
	got := sc.updates["ts-buttons"]
	if !strings.Contains(got, "2. Add an index") || !strings.Contains(got, "aman") {
		t.Fatalf("the button message should record the pick, got %q", got)
	}
}

func TestDecisionClickIgnoresStrangers(t *testing.T) {
	b, _, h, _ := testBot(t)
	sess := liveSession("a3f2", "100.1")
	if err := b.Store.Create(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	b.HandleInteraction(context.Background(), slack.InteractionCallback{
		User: slack.User{ID: "U9"},
		ActionCallback: slack.ActionCallbacks{
			BlockActions: []*slack.BlockAction{{
				ActionID: decisionActionID("a3f2", 1),
				Value:    "Ship it",
			}},
		},
	})
	if len(h.prompts) != 0 {
		t.Fatalf("a stranger must not drive the session: %q", h.prompts)
	}
}

func TestDecisionClickOnADeadSessionSaysSo(t *testing.T) {
	b, sc, h, _ := testBot(t)
	sess := liveSession("a3f2", "100.1")
	sess.Status = state.StatusDone
	if err := b.Store.Create(context.Background(), sess); err != nil {
		t.Fatal(err)
	}

	b.HandleInteraction(context.Background(), slack.InteractionCallback{
		User: slack.User{ID: "U1"},
		ActionCallback: slack.ActionCallbacks{
			BlockActions: []*slack.BlockAction{{
				ActionID: decisionActionID("a3f2", 1),
				Value:    "Ship it",
			}},
		},
	})
	if len(h.prompts) != 0 {
		t.Fatalf("prompts = %q", h.prompts)
	}
	if got := sc.texts(); len(got) != 1 || !strings.Contains(got[0], "done") {
		t.Fatalf("got %q", got)
	}
}

func TestEnsureArtifactsDir(t *testing.T) {
	dir := t.TempDir()
	if err := ensureArtifactsDir(dir); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, ArtifactsRelPath))
	if err != nil || !fi.IsDir() {
		t.Fatalf("artifacts dir missing: %v", err)
	}
	// Creating it twice is how a resumed session behaves.
	if err := ensureArtifactsDir(dir); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if err := ensureArtifactsDir(""); err == nil {
		t.Fatal("a session with no worktree should error")
	}
}

// Truncating inside a code block would leave Slack rendering everything after
// it as code.
func TestTrimReplyClosesAnOpenFence(t *testing.T) {
	body := "Here is the log:\n```\n" + strings.Repeat("a noisy log line\n", 200)
	got := trimReply(body)

	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncation:\n%s", got)
	}
	if insideFence(strings.Split(got, "\n")) {
		t.Fatalf("the fence was left open:\n%s", got[len(got)-120:])
	}
}

// Slack requires action ids to be unique within a message.
func TestDecisionButtonActionIDsAreDistinct(t *testing.T) {
	btns := decisionButtons("539c", []string{"a", "b", "c", "d"})
	seen := map[string]bool{}
	for i, b := range btns {
		if seen[b.ActionID] {
			t.Fatalf("button %d repeats action id %q", i, b.ActionID)
		}
		seen[b.ActionID] = true

		sess, n, _, ok := decodeDecision(b.ActionID, b.Value)
		if !ok || sess != "539c" || n != i+1 {
			t.Fatalf("button %d: decode(%q) = %q, %d, ok=%v", i, b.ActionID, sess, n, ok)
		}
	}
}
