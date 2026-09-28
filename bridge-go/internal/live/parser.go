package live

import (
	"slices"
	"strings"

	"relay/internal/transcript"
)

// ToolCall is a tool call as it shows on screen, before the transcript has it.
type ToolCall struct {
	// Name is the name the transcript's `toolCall` will carry for this kind (`Bash`, `Shell`, `bash`, …).
	Name string
	// Input is what the screen shows of the arguments, keyed like the real tool input (`command`, `file_path`, …).
	Input map[string]string
	// FixedSummary is a summary that doesn't come from transcript.Summary (codex builds its own, e.g. `Ran <cmd>`).
	FixedSummary *string
	// Generic means the screen doesn't show the arguments yet, so the summary is only the generic one for Name.
	Generic bool
}

func newTool(name string, input map[string]string) *ToolCall {
	if input == nil {
		input = map[string]string{}
	}
	return &ToolCall{Name: name, Input: input}
}

func fixedTool(name string, input map[string]string, summary string, generic bool) *ToolCall {
	t := newTool(name, input)
	t.FixedSummary = &summary
	t.Generic = generic
	return t
}

func genericTool(name string) *ToolCall {
	t := newTool(name, nil)
	t.Generic = true
	return t
}

// Summary is built and scrubbed the same way as the transcript's `toolCall.summary`.
func (t ToolCall) Summary(s transcript.Scrubber) string {
	if t.FixedSummary != nil {
		return transcript.Truncate(s.Scrub(*t.FixedSummary), 120)
	}
	input := make(map[string]any, len(t.Input))
	for k, v := range t.Input {
		input[k] = v
	}
	return transcript.Summary(t.Name, input, s)
}

// Screen is what a pane's visible screen shows of the current turn: the assistant prose being
// written and the tool that's running. Tool text is never part of Text.
type Screen struct {
	Text *string
	Tool *ToolCall
}

// Parse pulls the in-progress assistant text and the running tool out of a pane's visible
// screen, per agent kind. Input is already ANSI-stripped (see ScreenText). Only the current turn
// counts: anything above the user's last prompt on screen is ignored. Paths are not scrubbed
// here; callers do that.
func Parse(screen, kind string) Screen {
	lines := strings.Split(screen, "\n")
	for i, l := range lines {
		lines[i] = stripInlineBanner(l)
	}
	switch kind {
	case "claude":
		return claude(lines)
	case "codex":
		return codex(lines)
	case "pi":
		return pi(lines)
	}
	return Screen{}
}

// Extract is just the prose, for callers that don't care about tools.
func Extract(screen, kind string) *string { return Parse(screen, kind).Text }

// MARK: Claude

var claudeSpinners = map[string]bool{"✢": true, "✻": true, "✽": true, "✳": true, "✶": true, "✺": true, "✵": true}

type blockKind int

const (
	kindText blockKind = iota
	kindTool
)

type block struct {
	kind  blockKind
	lines []string
}

func claude(lines []string) Screen {
	for i, l := range lines {
		lines[i] = claudeOverlay.ReplaceAllString(l, "")
	}
	region := claudeTurn(lines)
	var blocks []block
	for _, chunk := range chunks(region) {
		first := trimWS(chunk[0])
		marked := strings.HasPrefix(first, "⏺")
		content := slices.Clone(chunk)
		if marked {
			content[0] = trimWS(dropChars(first, 1))
		} else if len(blocks) == 0 {
			// Output of a tool whose header scrolled off the top: indented like `⎿` output, and
			// neither prose nor something to name the tool by.
			for len(content) > 0 && indent(content[0]) >= toolOutputIndent {
				content = content[1:]
			}
			// What's left may start at the `⎿` result line, still without a header. Only a
			// `⎿  $ command` line says which tool it is; anything else can't be named, and may
			// well be a tool that already finished.
			if len(content) == 0 || strings.HasPrefix(trimWS(content[0]), "⎿") && !hasCommandLine(content) {
				continue
			}
		}
		tool := isClaudeTool(content)
		if marked || tool || len(blocks) == 0 {
			// A running tool's `⏺` blinks, so a tool block can start without it.
			ls := content
			if !marked && !tool {
				ls = nil
				for _, l := range content {
					if !isClaudeChrome(l) {
						ls = append(ls, l)
					}
				}
			}
			if len(ls) == 0 {
				continue
			}
			k := kindText
			if tool {
				k = kindTool
			}
			blocks = append(blocks, block{kind: k, lines: ls})
		} else {
			// A later paragraph of the same text block, or more output of the same tool.
			last := &blocks[len(blocks)-1]
			last.lines = append(append(last.lines, ""), content...)
		}
	}
	var out Screen
	out.Text = lastTextBlock(blocks)
	if n := len(blocks); n > 0 && blocks[n-1].kind == kindTool {
		out.Tool = claudeToolCall(blocks[n-1].lines)
	}
	return out
}

func lastTextBlock(blocks []block) *string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].kind == kindText {
			return nonEmpty(joinBlock(blocks[i].lines))
		}
	}
	return nil
}

// claudeTurn is the lines of the current turn: after the user's last prompt echo (and its
// wrapped lines), before the spinner and the input box. The whole screen when the echo has
// scrolled off.
func claudeTurn(lines []string) []string {
	end := len(lines)
	// The input box: a rule with the `❯` prompt line right under it.
outer:
	for i := len(lines) - 1; i >= 0; i-- {
		if !isRule(trimWS(lines[i])) {
			continue
		}
		for _, next := range lines[i+1:] {
			if t := trimWS(next); t != "" {
				if strings.HasPrefix(t, "❯") {
					end = i
					break outer
				}
				break
			}
		}
	}
	start := 0
	for i := end - 1; i >= 0; i-- {
		// A dialog's `❯ 1. Yes` row is its menu cursor, not the user's prompt.
		if t := trimWS(lines[i]); strings.HasPrefix(t, "❯") && !menuCursor.MatchString(t) {
			start = i + 1
			for start < end && trimWS(lines[start]) != "" {
				start++
			}
			break
		}
	}
	if start >= end {
		return nil
	}
	region := lines[start:end]
	if i := slices.IndexFunc(region, isClaudeSpinner); i >= 0 {
		region = region[:i]
	}
	if i := slices.IndexFunc(region, func(l string) bool { return isRule(trimWS(l)) }); i >= 0 {
		region = region[:i]
	}
	return region
}

var (
	menuCursor = icu(`^❯\s*\d{1,2}[.)]\s`)
	// claudeOverlay is fullscreen Claude's scroll hint, drawn over whatever row is at the bottom
	// of the transcript view while the pane is scrolled up.
	claudeOverlay = icu(`\s*(?:\d+ new messages?|Jump to bottom) \(click\)(?: ↓)?`)
)

// toolOutputIndent is where Claude's tool output text starts (`  ⎿  `); reply prose sits at 2.
const toolOutputIndent = 5

// indent counts a line's leading blanks.
func indent(line string) int {
	n := 0
	for _, r := range line {
		if !isWS(r) {
			break
		}
		n++
	}
	return n
}

// isClaudeSpinner matches spinner/status lines: `✻ Crafting… (3s)`, `· Crafting…`, `✻ Cooked for 3s · done`.
func isClaudeSpinner(line string) bool {
	t := trimWS(line)
	first := firstChar(t)
	if first == "" || firstChar(t[len(first):]) != " " {
		return false
	}
	if claudeSpinners[first] {
		return true
	}
	return (first == "·" || first == "*") && strings.Contains(t, "…")
}

func isClaudeTool(content []string) bool {
	for _, l := range content {
		if strings.HasPrefix(trimWS(l), "⎿") {
			return true
		}
	}
	header := claudeHeader(content)
	if _, _, ok := callSignature(header); ok {
		return true
	}
	_, ok := claudeGroupLabel(header)
	return ok
}

var claudeTimer = icu(`\s+·\s+\d+(?:m\s*\d+)?s$`)

// claudeHeader is the block's first lines up to its `⎿` output, rejoined, with a trailing `· 3s`
// timer dropped.
func claudeHeader(content []string) string {
	var head []string
	for _, l := range content {
		t := trimWS(l)
		if strings.HasPrefix(t, "⎿") || t == "" {
			break
		}
		head = append(head, t)
	}
	return claudeTimer.ReplaceAllString(strings.Join(head, " "), "")
}

// claudeDisplayNames are Claude's display names that differ from the transcript's tool name.
var claudeDisplayNames = map[string]string{
	"Update": "Edit", "Search": "Grep", "Fetch": "WebFetch", "Web Search": "WebSearch", "Task": "Agent",
}

var claudePathTools = map[string]bool{"Read": true, "Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true}

var claudeCommandTimer = icu(`\s+\(\d+(?:m\s*\d+)?s(?:\s+·\s+\d+ lines?)?\)$`)

func isCommandLine(l string) bool {
	t := trimWS(l)
	return strings.HasPrefix(t, "⎿  $ ") || strings.HasPrefix(t, "⎿ $ ")
}

func hasCommandLine(content []string) bool { return slices.ContainsFunc(content, isCommandLine) }

func claudeToolCall(content []string) *ToolCall {
	// `⎿  $ ping -c 8 127.0.0.1 (3s · 5 lines)`: the command itself, the most specific source.
	for _, l := range content {
		if !isCommandLine(l) {
			continue
		}
		t := trimWS(l)
		rest := dropChars(trimWS(dropChars(t, 1)), 2)
		if command := claudeCommandTimer.ReplaceAllString(rest, ""); command != "" {
			return newTool("Bash", map[string]string{"command": command})
		}
		break
	}
	header := claudeHeader(content)
	if display, args, ok := callSignature(header); ok {
		name := display
		if n, ok := claudeDisplayNames[display]; ok {
			name = n
		}
		arg := unquote(args)
		switch {
		case name == "Bash":
			return newTool(name, map[string]string{"command": arg})
		case claudePathTools[name]:
			key := "file_path"
			if name == "NotebookEdit" {
				key = "notebook_path"
			}
			return newTool(name, map[string]string{key: arg})
		case name == "Grep", name == "Glob":
			pattern := arg
			if p, ok := keyed(args); ok {
				pattern = p
			}
			return newTool(name, map[string]string{"pattern": pattern})
		case name == "WebFetch":
			return newTool(name, map[string]string{"url": arg})
		case name == "WebSearch":
			return newTool(name, map[string]string{"query": arg})
		case name == "Agent":
			return newTool(name, map[string]string{"description": arg})
		case name == "Skill":
			return newTool(name, map[string]string{"skill": arg})
		default:
			return genericTool(name)
		}
	}
	if name, ok := claudeGroupLabel(header); ok {
		return genericTool(name)
	}
	summary := header
	if summary == "" {
		summary = "Tool"
	}
	return fixedTool("Tool", nil, summary, true)
}

var claudeGroup = icu(`^(Running|Ran|Reading|Read|Searching for|Searched for|Searching|Searched|Listing|Listed|Editing|Edited|Updating|Updated|Writing|Wrote|Creating|Created|Fetching|Fetched) \d+ (?:shell )?(?:commands?|files?|patterns?|director(?:y|ies)|pages?|searches|queries|agents?|tasks?|tools?)\b[^\n]*$`)

// claudeGroupLabel reads Claude's grouped tool labels, e.g. `Running 1 shell command…`,
// `Read 3 files`, `Searched for 2 patterns, read 1 file`. Returns the transcript name of the
// first tool.
func claudeGroupLabel(header string) (string, bool) {
	m := groups(claudeGroup, header)
	if m == nil {
		return "", false
	}
	switch m[0] {
	case "Running", "Ran":
		return "Bash", true
	case "Reading", "Read":
		return "Read", true
	case "Searching for", "Searched for", "Searching", "Searched":
		return "Grep", true
	case "Listing", "Listed":
		return "Glob", true
	case "Editing", "Edited", "Updating", "Updated":
		return "Edit", true
	case "Writing", "Wrote", "Creating", "Created":
		return "Write", true
	}
	return "WebFetch", true
}

var claudeFooterMarkers = []string{"Update available!", "auto mode on", "Esc to cancel"}

func isClaudeChrome(line string) bool {
	t := trimWS(line)
	if t == "" {
		return false // blank lines are paragraph separators, not chrome
	}
	if isRule(t) || strings.HasPrefix(t, "❯") || strings.HasPrefix(t, "⎿") || isClaudeSpinner(t) {
		return true
	}
	for _, m := range claudeFooterMarkers {
		if strings.Contains(t, m) {
			return true
		}
	}
	return isTokenStats(t)
}

// MARK: Codex

// codexToolHeads are the headers of codex's tool cells (`• Ran ping …`, `• Explored`,
// `• Edited App.swift (+2 -1)`).
var codexToolHeads = []string{
	"Running ", "Ran ", "Explored", "Exploring", "Edited ", "Editing ", "Added ", "Deleted ", "Called ",
	"Calling ", "Waiting", "Waited", "Searched ", "Searching ", "Updated Plan", "Viewed Image", "Read ",
}

func codex(lines []string) Screen {
	region := codexTurn(lines)
	var blocks []block
	inBlock := false
	for _, line := range region {
		t := trimWS(line)
		switch {
		case strings.HasPrefix(t, "• ") || strings.HasPrefix(t, "◦ "):
			first := dropChars(t, 2)
			if strings.HasPrefix(first, "Working (") || strings.HasPrefix(first, "Model changed") {
				inBlock = false
				continue
			}
			k := kindText
			if slices.ContainsFunc(codexToolHeads, func(h string) bool { return strings.HasPrefix(first, h) }) {
				k = kindTool
			}
			blocks = append(blocks, block{kind: k, lines: []string{first}})
			inBlock = true
		case isCodexStop(t):
			inBlock = false
		case inBlock:
			last := &blocks[len(blocks)-1]
			last.lines = append(last.lines, line)
			if strings.HasPrefix(t, "└") {
				last.kind = kindTool
			}
		}
	}
	var out Screen
	out.Text = lastTextBlock(blocks)
	if n := len(blocks); n > 0 && blocks[n-1].kind == kindTool {
		out.Tool = codexToolCall(blocks[n-1].lines[0])
	}
	return out
}

// codexTurn is after the user's last prompt echo, before the input line. The input is the
// last `›` line; the echo is the `›` line above it.
func codexTurn(lines []string) []string {
	var prompts []int
	for i, l := range lines {
		if strings.HasPrefix(trimWS(l), "›") {
			prompts = append(prompts, i)
		}
	}
	if len(prompts) == 0 {
		return lines
	}
	input := prompts[len(prompts)-1]
	start := 0
	if len(prompts) >= 2 {
		start = prompts[len(prompts)-2] + 1
	}
	if start < input {
		return lines[start:input]
	}
	return nil
}

var codexDiffStat = icu(`\s+\(\+\d+ -\d+\)$`)

func codexToolCall(header string) *ToolCall {
	rest := func(prefix string) string { return trimWS(header[len(prefix):]) }
	for _, verb := range []string{"Running ", "Ran "} {
		if strings.HasPrefix(header, verb) {
			return fixedTool("Shell", map[string]string{"command": rest(verb)}, "Ran "+rest(verb), false)
		}
	}
	for _, verb := range []string{"Edited ", "Editing ", "Added ", "Deleted "} {
		if strings.HasPrefix(header, verb) {
			path := codexDiffStat.ReplaceAllString(rest(verb), "")
			return fixedTool("Edit", map[string]string{"path": path}, "Edited "+path, false)
		}
	}
	for _, verb := range []string{"Called ", "Calling "} {
		if strings.HasPrefix(header, verb) {
			name := rest(verb)
			if i := strings.Index(name, "("); i >= 0 {
				name = name[:i]
			}
			return fixedTool(name, nil, "Called "+name, false)
		}
	}
	for _, verb := range []string{"Searched ", "Searching "} {
		if strings.HasPrefix(header, verb) {
			return fixedTool("WebSearch", map[string]string{"query": rest(verb)}, "Searched the web for "+rest(verb), false)
		}
	}
	for _, p := range []string{"Explored", "Exploring", "Waiting", "Waited", "Read "} {
		if strings.HasPrefix(header, p) {
			return genericTool("Shell")
		}
	}
	return fixedTool("Tool", nil, header, true)
}

func isCodexStop(t string) bool {
	switch {
	case strings.HasPrefix(t, "›"):
		return true
	case strings.HasPrefix(t, "✗") || strings.HasPrefix(t, "✔") || strings.HasPrefix(t, "■"):
		return true
	case isRule(t), isTimestamp(t):
		return true
	case strings.Contains(t, "background terminal") && strings.Contains(t, "/ps"):
		return true // status line, not reply text
	}
	return false
}

// MARK: pi

func pi(lines []string) Screen {
	working := slices.ContainsFunc(lines, isPiSpinner)
	var content []string
	for _, l := range lines {
		if !isPiChrome(l) {
			content = append(content, l)
		}
	}
	lastTool := -1
	for i := len(content) - 1; i >= 0; i-- {
		if piToolCall(content[i]) != nil {
			lastTool = i
			break
		}
	}
	took := func() int {
		for i := lastTool + 1; i < len(content); i++ {
			if strings.HasPrefix(trimWS(content[i]), "Took ") {
				return i
			}
		}
		return -1
	}

	var tool *ToolCall
	if working && lastTool >= 0 && took() < 0 {
		// Still running unless a `Took …` line closed its box.
		tool = piToolCall(content[lastTool])
	}
	// pi renders its reply in one piece, and never while its own `Working` spinner is up.
	if working {
		return Screen{Tool: tool}
	}
	// Prose comes after the last tool box: after its `Took …` line for bash, else after its
	// header (a read/edit box has no footer, so its output can't be told apart; the reply's last
	// paragraph is still prose in practice).
	from := 0
	if lastTool >= 0 {
		if t := took(); t >= 0 {
			from = t + 1
		} else if piToolCall(content[lastTool]).Name == "bash" {
			return Screen{}
		} else {
			from = lastTool + 1
		}
	}
	var prose []string
	for _, l := range content[from:] {
		if !isPiToolFooter(l) {
			prose = append(prose, l)
		}
	}
	if p := lastParagraph(prose); p != nil {
		return Screen{Text: nonEmpty(joinBlock(p))}
	}
	return Screen{}
}

var (
	piTimeout  = icu(`\s+\(timeout \d+s\)$`)
	piGrep     = icu(`^grep /(.*)/ in (\S+)`)
	piFind     = icu(`^find (\S+) in (\S+)`)
	piPathTool = icu(`^(read|write|edit|ls) (\S+)$`)
	piReadLine = icu(`:\d+(?:-\d*)?$`)
)

// piToolCall reads pi's tool box headers: `$ cmd`, `read path:1-20`, `write path`, `edit path`,
// `ls path`, `grep /pattern/ in path`, `find pattern in path`.
func piToolCall(line string) *ToolCall {
	t := trimWS(line)
	if strings.HasPrefix(t, "$ ") {
		command := piTimeout.ReplaceAllString(dropChars(t, 2), "")
		if command == "" {
			return nil
		}
		return newTool("bash", map[string]string{"command": command})
	}
	if m := groups(piGrep, t); m != nil {
		return newTool("grep", map[string]string{"pattern": m[0], "path": m[1]})
	}
	if m := groups(piFind, t); m != nil {
		return newTool("find", map[string]string{"pattern": m[0], "path": m[1]})
	}
	if m := groups(piPathTool, t); m != nil {
		path := m[1]
		if m[0] == "read" {
			path = piReadLine.ReplaceAllString(path, "")
		}
		return newTool(m[0], map[string]string{"path": path})
	}
	return nil
}

func isPiToolFooter(line string) bool {
	t := trimWS(line)
	return strings.HasPrefix(t, "Took ") || strings.HasPrefix(t, "Elapsed ") || strings.HasPrefix(t, "... (")
}

func isPiSpinner(line string) bool {
	t := trimWS(line)
	return strings.Contains(t, "Working") && strings.Contains(t, "─")
}

var piChromePrefixes = []string{
	"Update Available", "New version", "Changelog:", "Model:", "Warning:", "Thinking level:",
	"Manage extra usage",
}

func isPiChrome(line string) bool {
	t := trimWS(line)
	switch {
	case t == "":
		return false // blank lines are paragraph separators, not chrome
	case isRule(t), isPiSpinner(t):
		return true
	case slices.ContainsFunc(piChromePrefixes, func(p string) bool { return strings.HasPrefix(t, p) }):
		return true
	case strings.HasPrefix(t, "$") && strings.Contains(t, "(") && !strings.HasPrefix(t, "$ "):
		return true // cost/token footer chip
	case strings.Contains(t, "(auto)") || strings.Contains(t, "(sub)"):
		return true
	case strings.HasPrefix(t, "~"):
		return true // cwd + git branch footer line
	}
	return false
}

// MARK: Shared helpers

var signature = icu(`^([A-Za-z][A-Za-z0-9_.]*|Web Search)\((.*)\)$`)

// callSignature reads a tool-call signature, e.g. `Write(answer.txt)`,
// `Bash(ping -c 45 127.0.0.1)`, `Web Search("swift actors")`: the display name and the raw
// arguments.
func callSignature(text string) (name, args string, ok bool) {
	m := groups(signature, text)
	if m == nil {
		return "", "", false
	}
	return m[0], m[1], true
}

var keyedPattern = icu(`pattern: "([^"]*)"`)

// keyed reads `pattern: "x", path: "y"` → the value for `pattern`.
func keyed(args string) (string, bool) {
	m := groups(keyedPattern, args)
	if m == nil {
		return "", false
	}
	return m[0], true
}

func unquote(s string) string {
	if charCount(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s[1 : len(s)-1]
	}
	return s
}

// chunks splits lines into runs of non-blank lines.
func chunks(lines []string) [][]string {
	var out [][]string
	var current []string
	for _, l := range lines {
		if trimWS(l) == "" {
			if len(current) > 0 {
				out = append(out, current)
				current = nil
			}
		} else {
			current = append(current, l)
		}
	}
	if len(current) > 0 {
		out = append(out, current)
	}
	return out
}

func isRule(line string) bool {
	if line == "" {
		return false
	}
	for _, r := range line {
		if r != '─' && r != '-' {
			return false
		}
	}
	return true
}

var (
	timestamp  = icu(`^(\d{1,2}:\d{2}\s?(?:AM|PM))$`)
	tokenStats = icu(`^([\d.]+[kM]?/[\d.]+[kM]?)\s*·`)
)

func isTimestamp(line string) bool { return timestamp.MatchString(line) }

// isTokenStats matches Claude's footer token/usage line, e.g.
// `72.2k/1.0M · in 72.2k out 977 · 5h 10%(2h13m) · wk 11%(4d15h)`.
func isTokenStats(line string) bool { return tokenStats.MatchString(line) }

// lastParagraph is the last run of non-blank lines. It's fine to show only the tail of a
// scrolled-off reply.
func lastParagraph(lines []string) []string {
	end := len(lines)
	for end > 0 && trimWS(lines[end-1]) == "" {
		end--
	}
	if end == 0 {
		return nil
	}
	start := end
	for start > 0 && trimWS(lines[start-1]) != "" {
		start--
	}
	return lines[start:end]
}

// joinBlock rejoins wrapped lines into flowing paragraphs; a blank line is kept as a paragraph break.
func joinBlock(lines []string) string {
	var paragraphs, current []string
	for _, raw := range lines {
		line := trimWS(raw)
		if line == "" {
			if len(current) > 0 {
				paragraphs = append(paragraphs, strings.Join(current, " "))
				current = nil
			}
		} else {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		paragraphs = append(paragraphs, strings.Join(current, " "))
	}
	return strings.Join(paragraphs, "\n\n")
}

// stripInlineBanner is the plain-text fallback for Claude Code's "Update available!" banner
// sitting on a content row: drop everything from the banner on. ScreenText removes the banner by
// its colour before this runs, which also handles a banner that text partly overwrote.
func stripInlineBanner(line string) string {
	if i := strings.Index(line, "Update available!"); i >= 0 {
		return line[:i]
	}
	return line
}
