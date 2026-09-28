// Package approval reads a blocked agent's screen into an api.Approval, and Claude's input box
// and pi/codex pickers for the control drivers.
package approval

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"relay/internal/api"
	"relay/internal/transcript"
)

// Parse reads a blocked agent's screen (herdr `agent.read` detection/visible text) into an
// Approval, or nil when it shows no menu. cwdName "" = unknown; kind "" = unknown.
//
// Two menu shapes:
//   - Numbered: the last run of `❯ 1. Yes`, `2. ...`, `3. No, ... (esc)`. Option N maps to keys
//     `["N"]`; a trailing "No" rendered with an `(esc)` hint maps to `["esc"]`. Claude's "Type
//     something." row is FreeText and is reached with arrow keys, because pressing its number also
//     types the digit.
//   - Cursor: unnumbered lines around a `❯` line (e.g. Claude's folder-trust prompt). Keys are
//     arrow moves relative to the `❯` line, then Enter.
func Parse(screen, agentID string, sc transcript.Scrubber, cwdName, kind string) *api.Approval {
	if kind == "codex" {
		return codex(screen, agentID, sc)
	}
	lines := frameLines(screen)
	if a := numbered(lines, measure(screen), agentID, sc); a != nil {
		return a
	}
	return cursorMenu(lines, agentID, sc, cwdName)
}

var (
	optionRow  = icu(`^([❯›>▶]\s*)?(\d{1,2})[.)]\s+(\S.*?)\s*$`)
	cursorLine = icu(`^[❯›▶]\s+(\S.*?)\s*$`)
	hint       = icu(`(?i)\s*\((esc|tab|shift\+tab|ctrl\+[a-z])\)\s*$`)
)

const (
	frameChars = "│┃║|╭╮╰╯┌┐└┘"
	ruleChars  = "─━═-–—╌┄ "
	// FreeTextLabel is Claude's free-text row.
	FreeTextLabel = "Type something."
)

// frameLines splits the screen into lines trimmed of whitespace and box-drawing frames.
func frameLines(screen string) []string {
	lines := strings.Split(screen, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimFunc(l, func(r rune) bool { return isWS(r) || strings.ContainsRune(frameChars, r) })
	}
	return lines
}

// MARK: Numbered menus

type row struct {
	line   int
	n      int
	label  string
	cursor bool
}

func numbered(lines []string, m widths, agentID string, sc transcript.Scrubber) *api.Approval {
	var parsed []row
	for i, line := range lines {
		m := optionRow.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(line[m[4]:m[5]])
		if err != nil {
			continue
		}
		parsed = append(parsed, row{line: i, n: n, label: line[m[6]:m[7]], cursor: m[2] >= 0})
	}

	// Walk back from the last option: N, N-1, ..., 1.
	if len(parsed) == 0 || parsed[len(parsed)-1].n < 1 {
		return nil
	}
	run := []row{parsed[len(parsed)-1]}
	for i := len(parsed) - 2; i >= 0; i-- {
		expected := run[len(run)-1].n - 1
		if expected < 1 || parsed[i].n != expected {
			break
		}
		run = append(run, parsed[i])
	}
	if run[len(run)-1].n != 1 {
		return nil
	}
	slices.Reverse(run)
	cursor := slices.IndexFunc(run, func(r row) bool { return r.cursor })

	rows := map[int]bool{}
	for _, o := range parsed {
		rows[o.line] = true
	}
	options := []api.ApprovalOption{}
	for idx, o := range run {
		label, escHint := stripHint(wrappedLabel(o, lines, rows, m))
		if label == FreeTextLabel && cursor >= 0 {
			// Selecting it by number would also type the digit into the field.
			keys := arrows(cursor, idx)
			if len(keys) == 0 {
				keys = []string{"up", "down"}
			}
			options = append(options, api.ApprovalOption{Label: label, Keys: keys, FreeText: boolPtr(true)})
			continue
		}
		isTrailingNo := idx == len(run)-1 && strings.HasPrefix(strings.ToLower(label), "no")
		keys := []string{strconv.Itoa(o.n)}
		if isTrailingNo && escHint {
			keys = []string{"esc"}
		}
		opt := api.ApprovalOption{Label: sc.Scrub(label), Keys: keys}
		if label == FreeTextLabel {
			opt.FreeText = boolPtr(true)
		}
		options = append(options, opt)
	}
	return &api.Approval{AgentID: agentID, Question: sc.Scrub(question(run[0].line, lines)), Options: options}
}

// wrappedLabel is the option's label with the lines it wrapped onto (R8-14). Descriptions under
// an AskUserQuestion option sit at the same indent as a wrapped label, so a line only continues
// the label when the line before it couldn't have fit its first word (the terminal wrapped it),
// or when it ends with the hint the label is missing (`… (shift+tab)`).
func wrappedLabel(o row, lines []string, rows map[int]bool, m widths) string {
	label := o.label
	for i := o.line + 1; i < len(lines); i++ {
		next := lines[i]
		if next == "" || rows[i] || allIn(next, ruleChars) || isFooter(next) || isTabBar(next) {
			break
		}
		word, _, _ := strings.Cut(next, " ")
		wrapped := m.lens[i-1]+1+len([]rune(word)) > m.width-wrapMargin
		if !wrapped && !(hasHint(next) && !hasHint(label)) {
			break
		}
		label += " " + next
	}
	return label
}

// wrapMargin allows for the dialog's own padding inside the terminal's width.
const wrapMargin = 2

func hasHint(s string) bool { return hint.MatchString(s) }

// widths: the width the screen's text wraps at (its widest line; a box's inner width when the
// menu is boxed), and each line's length measured the same way.
type widths struct {
	width int
	lens  []int
}

func measure(screen string) widths {
	raw := strings.Split(screen, "\n")
	boxed := false
	for _, l := range raw {
		if lt := trimWS(l); strings.HasPrefix(lt, "│") || strings.HasPrefix(lt, "╭") {
			boxed = true
			break
		}
	}
	m := widths{lens: make([]int, len(raw))}
	for i, l := range raw {
		t := strings.TrimRightFunc(l, isWS)
		full := len([]rune(t))
		m.width = max(m.width, full)
		if boxed {
			// `│ text   │`: the text, without the frame and its padding.
			content := strings.TrimRightFunc(strings.TrimRight(t, frameChars), isWS)
			m.lens[i] = max(len([]rune(content))-2, 0)
		} else {
			m.lens[i] = full
		}
	}
	if boxed {
		m.width -= 4
	}
	return m
}

// MARK: codex

var (
	shortcut   = icu(`\s*\((esc|[a-z])\)\s*$`)
	codexFirst = icu(`^([›❯]\s*)?1\.\s`)
)

// codex: `› 1. Yes, proceed (y)` … `3. No, and tell Codex what to do differently (esc)`.
// A digit only moves codex's cursor, so keys move to the row and press Enter. The question is
// codex's heading ("Would you like to run the following command?") plus the `$ command`.
func codex(screen, agentID string, sc transcript.Scrubber) *api.Approval {
	picker, ok := ParsePicker(screen)
	if !ok || picker.Cursor < 0 {
		return nil
	}
	lines := strings.Split(screen, "\n")
	for i, l := range lines {
		lines[i] = trimWS(l)
	}
	// The run the picker found is the last one on screen, so its row 1 is the last "1." line.
	firstRow := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if codexFirst.MatchString(lines[i]) {
			firstRow = i
			break
		}
	}
	if firstRow < 0 {
		return nil
	}

	options := []api.ApprovalOption{}
	for i, raw := range picker.Labels {
		label := raw
		esc := false
		if m := shortcut.FindStringSubmatchIndex(label); m != nil {
			esc = label[m[2]:m[3]] == "esc"
			label = label[:m[0]]
		}
		keys := append(arrows(picker.Cursor, i), "enter")
		if esc && i == len(picker.Labels)-1 {
			keys = []string{"esc"}
		}
		options = append(options, api.ApprovalOption{Label: sc.Scrub(label), Keys: keys})
	}

	// Heading: the nearest "…?" line above the options, skipping codex's "Reason: …?" line.
	var heading, command *string
	for i := firstRow - 1; i >= 0 && firstRow-i <= 20; i-- {
		l := lines[i]
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "$ ") && command == nil {
			c := l[2:]
			command = &c
		}
		if strings.HasSuffix(l, "?") && !strings.HasPrefix(l, "Reason:") {
			heading = &l
			break
		}
	}
	q := "Codex is waiting for your answer"
	if heading != nil {
		q = *heading
	} else if picker.Title != nil {
		q = *picker.Title
	}
	if command != nil {
		q += "\n`" + *command + "`"
	}
	return &api.Approval{AgentID: agentID, Question: sc.Scrub(q), Options: options}
}

// MARK: Cursor menus

func cursorMenu(lines []string, agentID string, sc transcript.Scrubber, cwdName string) *api.Approval {
	// The last `❯ label` line; a bare `❯` is an empty input box, not a menu.
	c := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if cursorLine.MatchString(lines[i]) {
			c = i
			break
		}
	}
	if c < 0 {
		return nil
	}
	isItem := func(l string) bool { return l != "" && !allIn(l, ruleChars) && !isFooter(l) }
	start, end := c, c
	for start > 0 && isItem(lines[start-1]) {
		start--
	}
	for end < len(lines)-1 && isItem(lines[end+1]) {
		end++
	}
	if end <= start {
		return nil
	}
	// Text between two rules is the input box (a multi-line prompt), not a menu.
	isRule := func(i int) bool { return i >= 0 && i < len(lines) && lines[i] != "" && allIn(lines[i], ruleChars) }
	if isRule(start-1) && isRule(end+1) {
		return nil
	}

	cursor := c - start
	options := []api.ApprovalOption{}
	for i := start; i <= end; i++ {
		item := lines[i]
		if i == c {
			item = strings.TrimLeftFunc(strings.TrimLeft(item, "❯›▶"), unicode.IsSpace)
		}
		label, _ := stripHint(item)
		options = append(options, api.ApprovalOption{Label: sc.Scrub(label), Keys: append(arrows(cursor, i-start), "enter")})
	}

	screen := strings.ToLower(strings.Join(lines, "\n"))
	var q string
	if strings.Contains(screen, "trust this folder") || strings.Contains(screen, "quick safety check") {
		q = "Trust this folder?"
		if cwdName != "" {
			q = "Trust this folder? " + cwdName
		}
	} else {
		q = question(start, lines)
	}
	return &api.Approval{AgentID: agentID, Question: sc.Scrub(q), Options: options}
}

// MARK: Multi-question tabs

const tabMarks = "☐☒✔☑✓"

// Step reads progress from a tab bar like `←  ☒ Delivery  ☐ Focus  ✔ Submit  →`, or nil.
// The active tab is the highlighted one in ansi (a visible read with colours, "" = none);
// without it, the first unanswered tab. Index is 1-based and Count includes the Submit tab.
func Step(screen, ansi string) *api.ApprovalStep {
	bar := ""
	found := false
	for _, l := range strings.Split(screen, "\n") {
		if t := trimWS(l); isTabBar(t) {
			bar, found = t, true
		}
	}
	if !found {
		return nil
	}
	tabs := tabItems(bar)
	if len(tabs) < 2 {
		return nil
	}
	active := -1
	if title, ok := highlightedTab(ansi); ansi != "" && ok {
		active = slices.IndexFunc(tabs, func(t tab) bool { return t.title == title })
	}
	if active < 0 {
		active = slices.IndexFunc(tabs, func(t tab) bool { return t.mark == "☐" })
	}
	if active < 0 {
		active = len(tabs) - 1
	}
	title := tabs[active].title
	return &api.ApprovalStep{Index: active + 1, Count: len(tabs), Title: &title}
}

func isTabMark(ch string) bool { return strings.Contains(tabMarks, ch) && len([]rune(ch)) == 1 }

func isTabBar(l string) bool {
	return strings.HasPrefix(l, "←") && strings.HasSuffix(l, "→") && slices.ContainsFunc(chars(l), isTabMark)
}

type tab struct {
	mark  string
	title string
}

func tabItems(bar string) []tab {
	cs := chars(bar)
	if len(cs) < 2 {
		return nil
	}
	var out []tab
	mark := ""
	var title strings.Builder
	for _, ch := range cs[1 : len(cs)-1] {
		if isTabMark(ch) {
			if mark != "" {
				out = append(out, tab{mark, trimWS(title.String())})
			}
			mark = ch
			title.Reset()
		} else if mark != "" {
			title.WriteString(ch)
		}
	}
	if mark != "" {
		out = append(out, tab{mark, trimWS(title.String())})
	}
	return slices.DeleteFunc(out, func(t tab) bool { return t.title == "" })
}

var highlighted = icu(`\x{1B}\[[0-9;]*\b48;[0-9;]*m\s*[☐☒✔☑✓]\s+([^\x{1B}]+?)\s*\x{1B}`)

func highlightedTab(ansi string) (string, bool) {
	for _, line := range strings.Split(ansi, "\n") {
		if !strings.Contains(line, "←") || !strings.Contains(line, "→") {
			continue
		}
		if m := highlighted.FindStringSubmatch(line); m != nil {
			return m[1], true
		}
	}
	return "", false
}

// MARK: Helpers

func arrows(from, to int) []string {
	key, n := "down", to-from
	if to < from {
		key, n = "up", from-to
	}
	out := make([]string, n)
	for i := range out {
		out[i] = key
	}
	return out
}

func stripHint(raw string) (label string, esc bool) {
	m := hint.FindStringSubmatchIndex(raw)
	if m == nil {
		return raw, false
	}
	return raw[:m[0]], strings.ToLower(raw[m[2]:m[3]]) == "esc"
}

func isFooter(l string) bool {
	lower := strings.ToLower(l)
	return strings.Contains(lower, "esc to cancel") || (strings.Contains(lower, " · ") && strings.Contains(lower, "enter to"))
}

// question is the nearest question above index, else the nearest meaningful line.
func question(index int, lines []string) string {
	fallback := ""
	looked := 0
	for i := index - 1; i >= 0 && looked < 8; i-- {
		l := lines[i]
		if l == "" || allIn(l, ruleChars) || isFooter(l) || isTabBar(l) {
			continue
		}
		looked++
		if strings.HasSuffix(l, "?") {
			return l
		}
		if fallback == "" {
			fallback = l
		}
	}
	if fallback == "" {
		return "Waiting for your input"
	}
	return fallback
}

// Fallback is used when a blocked agent shows no menu at all: the last meaningful screen line.
func Fallback(screen, agentID string, sc transcript.Scrubber) *api.Approval {
	lines := frameLines(screen)
	return &api.Approval{AgentID: agentID, Question: sc.Scrub(question(len(lines), lines)), Options: []api.ApprovalOption{}}
}

func boolPtr(b bool) *bool { return &b }
