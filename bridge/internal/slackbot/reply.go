package slackbot

import (
	"regexp"
	"strconv"
	"strings"
)

// ArtifactsRelPath is where an agent leaves anything too long for a Slack
// reply. The bot uploads these as files instead of pasting them.
const ArtifactsRelPath = ".agents/artifacts"

// maxDecisionOptions is what Slack's action row and the contract both allow.
const maxDecisionOptions = 4

// noReplyMessage is the whole post when a turn ends without touching
// reply.md. There is no fallback to the pane: scraping a terminal is what the
// reply file exists to avoid.
const noReplyMessage = "Finished without a reply. Say `full` for the screen."

// reply is a parsed agent reply: the prose that gets posted, plus the two
// trailing sections the contract lets the agent add.
type reply struct {
	Text      string   // what goes in the thread, already stripped
	Artifacts []string // file names under .agents/artifacts, in order
	Decision  []string // option lines, first is the agent's recommendation
}

var (
	// artifactLine is a whole line that is only an artifact reference.
	artifactLine = regexp.MustCompile(`^artifact:\s*([A-Za-z0-9._-]+\.md)\s*$`)
	// decisionOption is "1. do the thing".
	decisionOption = regexp.MustCompile(`^(\d+)[.)]\s+(.*\S)\s*$`)
	// fenceLine is a triple backtick line, with or without a language tag.
	fenceLine = regexp.MustCompile("^```\\s*([A-Za-z0-9_+-]*)\\s*$")
)

// parseReply pulls the artifact references and the decision block out of a raw
// reply and hands back the text that should actually be posted.
//
// Both sections are recognised only outside fenced code blocks, so a reply
// that quotes the contract does not get eaten by it. The decision block is
// accepted fenced or bare, because the instruction file shows it fenced and
// agents copy what they see.
func parseReply(raw string) reply {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	lines, decision := takeDecision(lines)
	lines, artifacts := takeArtifacts(lines)

	return reply{
		Text:      strings.TrimSpace(strings.Join(lines, "\n")),
		Artifacts: artifacts,
		Decision:  decision,
	}
}

// takeDecision removes a trailing decision block and returns its options.
func takeDecision(lines []string) ([]string, []string) {
	end := lastContentLine(lines)
	if end < 0 {
		return lines, nil
	}

	// A fenced block at the end: find its opening fence and check the first
	// line inside it.
	if fenceLine.MatchString(strings.TrimSpace(lines[end])) {
		for i := end - 1; i >= 0; i-- {
			if !fenceLine.MatchString(strings.TrimSpace(lines[i])) {
				continue
			}
			if opts, ok := decisionOptions(lines[i+1 : end]); ok {
				return lines[:i], opts
			}
			break
		}
		return lines, nil
	}

	// Bare block: walk back to the "decision:" header, which has to be at the
	// top level, not inside a fence.
	for i := end; i >= 0; i-- {
		if !isDecisionHeader(lines[i]) {
			continue
		}
		if insideFence(lines[:i]) {
			return lines, nil
		}
		if opts, ok := decisionOptions(lines[i : end+1]); ok {
			return lines[:i], opts
		}
		return lines, nil
	}
	return lines, nil
}

// decisionOptions validates a candidate block and returns its options. The
// block must start with the header and hold nothing but numbered lines.
func decisionOptions(block []string) ([]string, bool) {
	if len(block) == 0 || !isDecisionHeader(block[0]) {
		return nil, false
	}
	var opts []string
	for _, l := range block[1:] {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		m := decisionOption.FindStringSubmatch(t)
		if m == nil {
			return nil, false // prose in the block: not a decision after all
		}
		opts = append(opts, m[2])
	}
	if len(opts) < 2 {
		return nil, false
	}
	if len(opts) > maxDecisionOptions {
		opts = opts[:maxDecisionOptions]
	}
	return opts, true
}

func isDecisionHeader(l string) bool {
	return strings.EqualFold(strings.TrimSpace(l), "decision:")
}

// takeArtifacts removes every standalone artifact line outside a fence and
// returns the names in the order they appeared.
func takeArtifacts(lines []string) ([]string, []string) {
	var (
		kept  []string
		names []string
		fence bool
	)
	for _, l := range lines {
		if fenceLine.MatchString(strings.TrimSpace(l)) {
			fence = !fence
			kept = append(kept, l)
			continue
		}
		if !fence {
			if m := artifactLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
				names = append(names, m[1])
				continue
			}
		}
		kept = append(kept, l)
	}
	return kept, names
}

// lastContentLine is the index of the last non-blank line, or -1.
func lastContentLine(lines []string) int {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return i
		}
	}
	return -1
}

// insideFence reports whether the lines so far leave a fence open.
func insideFence(lines []string) bool {
	open := false
	for _, l := range lines {
		if fenceLine.MatchString(strings.TrimSpace(l)) {
			open = !open
		}
	}
	return open
}

var (
	boldRun   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	mdLink    = regexp.MustCompile(`\[([^\]\n]*)\]\(([^)\s]+)\)`)
	mdHeading = regexp.MustCompile(`^\s{0,3}#{1,6}\s+(.*\S)\s*$`)
)

// toMrkdwn is the safety net for agents that slip back into GitHub markdown.
// Slack's mrkdwn is close enough to markdown that the mistakes are always the
// same four, and all four are cheap to undo.
//
// Only the text outside fenced code blocks is rewritten. Inside a block the
// characters are the content, and a fence's own language tag is dropped
// because Slack renders it as the first line of the code.
func toMrkdwn(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	fence := false

	for i, l := range lines {
		if m := fenceLine.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			fence = !fence
			lines[i] = "```"
			continue
		}
		if fence {
			continue
		}
		out := l
		if m := mdHeading.FindStringSubmatch(out); m != nil {
			out = "*" + strings.TrimSpace(m[1]) + "*"
		}
		out = boldRun.ReplaceAllString(out, "*$1*")
		out = mdLink.ReplaceAllStringFunc(out, rewriteLink)
		lines[i] = out
	}
	return strings.Join(lines, "\n")
}

// rewriteLink turns [label](url) into Slack's <url|label>, or plain <url> when
// the label adds nothing.
func rewriteLink(m string) string {
	parts := mdLink.FindStringSubmatch(m)
	if parts == nil {
		return m
	}
	label, url := strings.TrimSpace(parts[1]), parts[2]
	if label == "" || label == url {
		return "<" + url + ">"
	}
	return "<" + url + "|" + label + ">"
}

// lastQuestionLine is what a blocked harness is actually asking. The pane
// holds a whole screen of TUI; the thread only needs the question.
func lastQuestionLine(screen string) string {
	lines := strings.Split(strings.TrimRight(screen, "\n"), "\n")

	// Strip the box drawing and cursor glyphs the harness TUIs frame prompts
	// with, then look for the last line that reads as a question.
	var clean []string
	for _, l := range lines {
		t := strings.TrimSpace(strings.Trim(l, "│┃|╭╮╰╯─━┌┐└┘ >❯›*"))
		if t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return "(the pane is empty; check the box)"
	}
	for i := len(clean) - 1; i >= 0; i-- {
		if strings.HasSuffix(clean[i], "?") {
			return clean[i]
		}
	}
	return clean[len(clean)-1]
}

// decisionPrompt is what a clicked button sends back to the agent.
func decisionPrompt(n int, option string) string {
	return strconv.Itoa(n) + ". " + option
}
