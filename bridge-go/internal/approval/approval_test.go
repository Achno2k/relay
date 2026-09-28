package approval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"relay/internal/api"
	"relay/internal/transcript"
)

// Ported from ApprovalParserTests.swift, same cases and names.

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func expectEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func must(t *testing.T, a *api.Approval) *api.Approval {
	t.Helper()
	if a == nil {
		t.Fatal("no approval")
	}
	return a
}

var none = transcript.NewScrubber("")

func opt(label string, keys ...string) api.ApprovalOption {
	return api.ApprovalOption{Label: label, Keys: keys}
}

func freeText(label string, keys ...string) api.ApprovalOption {
	o := opt(label, keys...)
	o.FreeText = boolPtr(true)
	return o
}

func labels(a *api.Approval) []string {
	var out []string
	for _, o := range a.Options {
		out = append(out, o.Label)
	}
	return out
}

func keys(a *api.Approval) [][]string {
	var out [][]string
	for _, o := range a.Options {
		out = append(out, o.Keys)
	}
	return out
}

func title(s string) *string { return &s }

func TestEditPrompt(t *testing.T) {
	a := Parse(fixture(t, "approval-edit.txt"), "w1:p2", none, "", "")
	expectEqual(t, a, &api.Approval{AgentID: "w1:p2", Question: "Do you want to make this edit to api.md?", Options: []api.ApprovalOption{
		opt("Yes", "1"),
		opt("Yes, allow all edits during this session", "2"),
		opt("No, and tell Claude what to do differently", "esc"),
	}})
}

func TestBoxedBashPromptScrubsPaths(t *testing.T) {
	a := must(t, Parse(fixture(t, "approval-bash-boxed.txt"), "w1:p2", transcript.NewScrubber("/Users/dev/shop-api"), "", ""))
	expectEqual(t, a.Question, "Do you want to proceed?")
	expectEqual(t, labels(a), []string{"Yes", "Yes, and don't ask again for npm test commands in .", "No, tell Claude what to do differently"})
	expectEqual(t, keys(a), [][]string{{"1"}, {"2"}, {"esc"}})
}

func TestAskUserQuestionIgnoresEarlierListAndDescriptions(t *testing.T) {
	a := must(t, Parse(fixture(t, "approval-question.txt"), "w2:p1", none, "", ""))
	expectEqual(t, a.Question, "Which invalidation strategy should I use?")
	expectEqual(t, a.Options, []api.ApprovalOption{
		opt("SIGHUP", "1"),
		opt("mtime", "2"),
		freeText("Type something.", "down", "down"),
	})
}

func TestFreeTextKeysAreRelativeToCursor(t *testing.T) {
	a := must(t, Parse(fixture(t, "approval-multi.txt"), "x", none, "", ""))
	expectEqual(t, a.Question, "When do you focus best?")
	expectEqual(t, a.Options, []api.ApprovalOption{
		opt("Morning", "1"),
		opt("Night", "2"),
		freeText("Type something.", "down"),
		opt("Chat about this", "4"),
	})
	// Cursor already on the row: a no-op move still gives the app something to send.
	on := must(t, Parse("Pick?\n  1. A\n❯ 2. Type something.", "x", none, "", ""))
	expectEqual(t, on.Options[len(on.Options)-1], freeText("Type something.", "up", "down"))
	// Without a visible cursor, fall back to the number.
	noCursor := must(t, Parse("Pick?\n  1. A\n  2. Type something.", "x", none, "", ""))
	expectEqual(t, noCursor.Options[len(noCursor.Options)-1], freeText("Type something.", "2"))
}

func TestCodexCommandApproval(t *testing.T) {
	a := must(t, Parse(fixture(t, "codex-approval.txt"), "w14:p5", none, "", "codex"))
	expectEqual(t, a.Question, "Would you like to run the following command?\n`curl -sI https://example.com | head -1`")
	expectEqual(t, a.Options, []api.ApprovalOption{
		opt("Yes, proceed", "enter"),
		opt("Yes, and don't ask again for commands that start with `curl -sI https://example.com`", "down", "enter"),
		opt("No, and tell Codex what to do differently", "esc"),
	})
}

func TestCodexEditApprovalWithCursorOnSecondRow(t *testing.T) {
	a := must(t, Parse(fixture(t, "codex-approval-edit.txt"), "w14:p5", transcript.NewScrubber("/Users/dev/project"), "", "codex"))
	expectEqual(t, a.Question, "Would you like to make the following edits?")
	expectEqual(t, keys(a), [][]string{{"up", "enter"}, {"enter"}, {"esc"}})
	expectEqual(t, a.Options[1].Label, "Yes, and don't ask again for these files")
}

func TestClaudeParsingUnchangedWithoutKind(t *testing.T) {
	// The same codex screen parsed the Claude way keeps digit keys (regression guard for other kinds).
	a := must(t, Parse(fixture(t, "codex-approval.txt"), "x", none, "", ""))
	expectEqual(t, a.Options[0].Keys, []string{"1"})
}

func TestTrustFolderCursorMenu(t *testing.T) {
	a := Parse(fixture(t, "approval-trust.txt"), "w15:p2", transcript.NewScrubber("/Users/dev/fresh-project"), "fresh-project", "")
	expectEqual(t, a, &api.Approval{AgentID: "w15:p2", Question: "Trust this folder? fresh-project", Options: []api.ApprovalOption{
		opt("No, exit", "enter"),
		opt("Yes, I trust this folder", "down", "enter"),
	}})
}

func TestGenericCursorMenuMovesUp(t *testing.T) {
	a := must(t, Parse(fixture(t, "approval-cursor-generic.txt"), "x", none, "", ""))
	expectEqual(t, a.Question, "Resume a previous session?")
	expectEqual(t, a.Options, []api.ApprovalOption{
		opt("Start fresh", "up", "up", "enter"),
		opt(`Resume "fix flaky tests"`, "up", "enter"),
		opt(`Resume "landing hero"`, "enter"),
	})
}

func TestInputBoxIsNotAMenu(t *testing.T) {
	expectEqual(t, Parse(fixture(t, "input-restored.txt"), "x", none, "", ""), (*api.Approval)(nil))
	expectEqual(t, Parse(fixture(t, "input-empty.txt"), "x", none, "", ""), (*api.Approval)(nil))
}

func TestStepFromHighlightedTab(t *testing.T) {
	screen := fixture(t, "approval-multi.txt")
	expectEqual(t, Step(screen, fixture(t, "approval-multi.ansi")), &api.ApprovalStep{Index: 2, Count: 3, Title: title("Focus")})
	// Without colours: the first unanswered tab.
	expectEqual(t, Step(screen, ""), &api.ApprovalStep{Index: 2, Count: 3, Title: title("Focus")})
	submit := "←  ☒ Delivery  ☒ Focus  ✔ Submit  →\nReady to submit your answers?"
	expectEqual(t, Step(submit, ""), &api.ApprovalStep{Index: 3, Count: 3, Title: title("Submit")})
	expectEqual(t, Step(fixture(t, "approval-edit.txt"), ""), (*api.ApprovalStep)(nil))
}

func TestTabBarIsNeverTheQuestion(t *testing.T) {
	a := must(t, Parse("←  ☐ A  ✔ Submit  →\n\n❯ 1. Yes\n  2. No", "x", none, "", ""))
	expectEqual(t, a.Question, "Waiting for your input")
}

func TestEncodesOptionalFieldsOnlyWhenSet(t *testing.T) {
	// Swift's test sorts keys; the Go encoder writes them in api.md's order.
	plain, _ := api.Marshal(api.Approval{AgentID: "a", Question: "q", Options: []api.ApprovalOption{opt("Yes", "1")}})
	expectEqual(t, string(plain), `{"agentId":"a","question":"q","options":[{"label":"Yes","keys":["1"]}]}`)
	full, _ := api.Marshal(api.Approval{AgentID: "a", Question: "q", Options: []api.ApprovalOption{freeText("Type something.", "down")},
		Step: &api.ApprovalStep{Index: 2, Count: 3, Title: title("Focus")}})
	expectEqual(t, string(full), `{"agentId":"a","question":"q","options":[{"label":"Type something.","keys":["down"],"freeText":true}],"step":{"index":2,"count":3,"title":"Focus"}}`)
}

func TestNoOptions(t *testing.T) {
	expectEqual(t, Parse(fixture(t, "approval-none.txt"), "x", none, "", ""), (*api.Approval)(nil))
	expectEqual(t, Parse("", "x", none, "", ""), (*api.Approval)(nil))
}

func TestBrokenSequenceIsNotAnApproval(t *testing.T) {
	expectEqual(t, Parse("Pick one?\n 2. B\n 3. C", "x", none, "", ""), (*api.Approval)(nil))
}

func TestTrailingNoWithoutEscHintKeepsNumber(t *testing.T) {
	a := must(t, Parse("Continue?\n❯ 1. Yes\n  2. No", "x", none, "", ""))
	expectEqual(t, keys(a), [][]string{{"1"}, {"2"}})
}

func TestFallbackUsesLastLine(t *testing.T) {
	a := Fallback("some output\nAllow network access?\n", "x", none)
	expectEqual(t, a.Question, "Allow network access?")
	expectEqual(t, len(a.Options), 0)
}

func TestDecodesContractFixture(t *testing.T) {
	b, err := os.ReadFile("../../../docs/fixtures/approval.json")
	if err != nil {
		t.Fatal(err)
	}
	var a api.Approval
	if err := json.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}
	expectEqual(t, a.Options[len(a.Options)-1].Keys, []string{"esc"})
}
