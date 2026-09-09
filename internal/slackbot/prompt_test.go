package slackbot

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildPromptTranscriptAndInstruction(t *testing.T) {
	got := BuildPrompt(PromptInput{
		Messages: []Message{
			{TS: "1.1", UserID: "U1", Text: "<@UBOT> the login page 500s on an empty password"},
			{TS: "1.2", UserID: "UBOT", BotID: "B1", Text: "Started `agents-cli-a3f2`."},
			{TS: "1.3", UserID: "U2", Text: "only since the deploy this morning"},
			{TS: "1.4", UserID: "U2", Text: "   "},
		},
		Names:        map[string]string{"U1": "aman", "U2": "priya"},
		Instruction:  "fix it",
		WorktreePath: "/work/agents-cli/a3f2",
	})

	for _, want := range []string{
		"Slack thread:",
		"aman: the login page 500s on an empty password",
		"priya: only since the deploy this morning",
		"Instruction:\n\nfix it",
		"/work/agents-cli/a3f2/" + ReplyRelPath,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Started `agents-cli-a3f2`") {
		t.Fatalf("bot messages must not reach the agent:\n%s", got)
	}
	if strings.Contains(got, "<@UBOT>") {
		t.Fatalf("mentions should be stripped from the transcript:\n%s", got)
	}
	if strings.Count(got, "priya:") != 1 {
		t.Fatalf("blank messages should be dropped:\n%s", got)
	}
}

func TestBuildPromptDefaultsTheInstruction(t *testing.T) {
	got := BuildPrompt(PromptInput{
		Messages: []Message{{TS: "1.1", UserID: "U1", Text: "have a look at this"}},
		Names:    map[string]string{"U1": "aman"},
	})
	if !strings.Contains(got, defaultInstruction) {
		t.Fatalf("empty instruction should fall back to the default:\n%s", got)
	}
}

func TestBuildPromptKeepsOnlyMessagesNewerThanSince(t *testing.T) {
	msgs := []Message{
		{TS: "1700000000.000100", UserID: "U1", Text: "first"},
		{TS: "1700000001.000100", UserID: "U1", Text: "second"},
		{TS: "1700000002.000100", UserID: "U1", Text: "third"},
	}
	got := BuildPrompt(PromptInput{Messages: msgs, Since: "1700000001.000100"})

	if strings.Contains(got, "first") || strings.Contains(got, "second") {
		t.Fatalf("messages at or before Since must be dropped:\n%s", got)
	}
	if !strings.Contains(got, "third") {
		t.Fatalf("the new message is missing:\n%s", got)
	}
	if !strings.Contains(got, "new messages since your last turn") {
		t.Fatalf("a follow up should say so:\n%s", got)
	}
}

func TestBuildPromptCapsTheTranscript(t *testing.T) {
	var msgs []Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs, Message{
			TS:     fmt.Sprintf("17000000%02d.000100", i),
			UserID: "U1",
			Text:   fmt.Sprintf("message %d", i),
		})
	}
	got := BuildPrompt(PromptInput{Messages: msgs, Instruction: "go"})

	if n := strings.Count(got, "U1: message"); n != maxThreadMessages {
		t.Fatalf("kept %d messages, want %d", n, maxThreadMessages)
	}
	if strings.Contains(got, "message 19\n") {
		t.Fatalf("the oldest messages should have been dropped:\n%s", got)
	}
	if !strings.Contains(got, "message 59") {
		t.Fatalf("the newest message must survive:\n%s", got)
	}
}

func TestBuildPromptAlwaysEndsWithTheReplyContract(t *testing.T) {
	got := BuildPrompt(PromptInput{
		Messages:     []Message{{TS: "1.1", UserID: "U1", Text: "hi"}},
		Instruction:  "do it",
		WorktreePath: "/work/aura/9c1d",
	})
	if !strings.Contains(got, "Overwrite that file every turn") {
		t.Fatalf("the reply contract is missing:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "narration.") {
		t.Fatalf("the contract should be the last thing the agent reads:\n%s", got)
	}
}

func TestLatestTSIgnoresBotMessages(t *testing.T) {
	msgs := []Message{
		{TS: "1700000000.000100", UserID: "U1"},
		{TS: "1700000009.000100", UserID: "UBOT", BotID: "B1"},
		{TS: "1700000002.000100", UserID: "U1"},
	}
	if got := LatestTS(msgs); got != "1700000002.000100" {
		t.Fatalf("got %q", got)
	}
}

func TestTSLess(t *testing.T) {
	if !tsLess("1700000000.000100", "1700000000.000200") {
		t.Fatal("micros should order")
	}
	if tsLess("1700000001.000000", "1700000000.999999") {
		t.Fatal("seconds should order")
	}
}
