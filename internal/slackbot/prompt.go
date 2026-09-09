package slackbot

import (
	"path/filepath"
	"strings"
)

// maxThreadMessages caps how much of a thread reaches the agent. Slack threads
// grow without bound; the newest 40 human messages are what a fresh turn needs.
const maxThreadMessages = 40

// ReplyRelPath is where an agent writes the reply the bot forwards to Slack.
//
// herdr can only read a pane's scrollback, and Claude Code draws on the
// alternate screen, so its output never lands there. Rather than scrape a
// viewport, we ask the agent to leave its answer in a file the bot can read.
const ReplyRelPath = ".agents/reply.md"

// defaultInstruction is used when the mention carries no words of its own,
// e.g. someone types just "@agents" on a thread full of context.
const defaultInstruction = "Read the thread above and do what is being asked. " +
	"If the thread does not ask for anything concrete, summarise what you would do and stop."

// PromptInput is everything the prompt builder needs. It holds no clients so
// the builder stays a pure function.
type PromptInput struct {
	// Messages is the thread, oldest first.
	Messages []Message
	// Names maps user ids to display names. Missing ids fall back to the id.
	Names map[string]string
	// Instruction is the user's ask, mention already stripped.
	Instruction string
	// Since keeps only messages newer than this ts. Empty means the whole
	// thread, which is what a first turn wants.
	Since string
	// WorktreePath is the session's checkout, used to name the reply file.
	WorktreePath string
}

// BuildPrompt renders the transcript block and the instruction block that get
// handed to the agent.
func BuildPrompt(in PromptInput) string {
	msgs := selectMessages(in.Messages, in.Since)

	var b strings.Builder
	if len(msgs) > 0 {
		b.WriteString("Slack thread")
		if in.Since != "" {
			b.WriteString(" (new messages since your last turn)")
		}
		b.WriteString(":\n\n")
		for _, m := range msgs {
			b.WriteString(displayName(in.Names, m.UserID))
			b.WriteString(": ")
			b.WriteString(cleanText(m.Text))
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	instruction := strings.TrimSpace(in.Instruction)
	if instruction == "" {
		instruction = defaultInstruction
	}
	b.WriteString("Instruction:\n\n")
	b.WriteString(instruction)
	b.WriteString("\n\n")
	b.WriteString(replyContract(in.WorktreePath))
	return b.String()
}

// replyContract is appended to every prompt. It has to be repeated each turn
// because agents forget file conventions between turns.
func replyContract(worktree string) string {
	path := ReplyRelPath
	if worktree != "" {
		path = filepath.Join(worktree, ReplyRelPath)
	}
	return "When you finish this turn, write your complete final reply as Markdown to " + path + ".\n" +
		"Overwrite that file every turn: it is the only thing the person in Slack sees, " +
		"and they cannot see your terminal. Keep it self contained and skip the tool by tool narration."
}

// selectMessages drops bot posts, keeps only messages newer than since, and
// trims to the newest maxThreadMessages.
func selectMessages(msgs []Message, since string) []Message {
	out := make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if m.FromBot() {
			continue
		}
		if strings.TrimSpace(m.Text) == "" {
			continue
		}
		if since != "" && !tsLess(since, m.TS) {
			continue
		}
		out = append(out, m)
	}
	if len(out) > maxThreadMessages {
		out = out[len(out)-maxThreadMessages:]
	}
	return out
}

func displayName(names map[string]string, id string) string {
	if n := names[id]; n != "" {
		return n
	}
	if id == "" {
		return "someone"
	}
	return id
}

// cleanText flattens a Slack message for the transcript: mentions become plain
// names, and newlines are indented so one message stays one visual block.
func cleanText(s string) string {
	s = strings.TrimSpace(mentionRE.ReplaceAllString(s, ""))
	s = strings.ReplaceAll(s, "\n", "\n    ")
	return strings.TrimSpace(s)
}

// LatestTS returns the newest ts among human messages, which becomes the
// session's LastSentTS after a prompt goes out.
func LatestTS(msgs []Message) string {
	latest := ""
	for _, m := range msgs {
		if m.FromBot() {
			continue
		}
		if latest == "" || tsLess(latest, m.TS) {
			latest = m.TS
		}
	}
	return latest
}
