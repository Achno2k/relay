package approval

import (
	"strings"
	"unicode/utf8"
)

// Claude Code's prompt input: the lines between the last two horizontal rules, starting with `❯`.

const inputRuleChars = "─━═╌┄ "

func isInputRule(l string) bool {
	t := trimWS(l)
	return len(chars(t)) >= 10 && allIn(t, inputRuleChars)
}

// InputBoxContent is the text in the input box, "" when empty; ok is false when the screen has
// no recognisable box.
func InputBoxContent(screen string) (text string, ok bool) {
	lines := strings.Split(screen, "\n")
	bottom := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if isInputRule(lines[i]) {
			bottom = i
			break
		}
	}
	top := -1
	for i := bottom - 1; i >= 0; i-- {
		if isInputRule(lines[i]) {
			top = i
			break
		}
	}
	if bottom < 0 || top < 0 || bottom-top < 2 {
		return "", false
	}
	box := lines[top+1 : bottom]
	first := box[0]
	if !strings.HasPrefix(first, "❯") && !strings.HasPrefix(first, ">") {
		return "", false
	}
	parts := []string{trimWS(dropFirstChar(first))}
	for _, l := range box[1:] {
		parts = append(parts, trimWS(l))
	}
	return strings.TrimFunc(strings.Join(parts, "\n"), isWSOrNewline), true
}

// LastPromptLine is the text on the last `❯` line of the screen: the input line, even while an
// autocomplete list covers the footer. "" when the input is empty; ok is false with no `❯` line.
func LastPromptLine(screen string) (string, bool) {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "❯") {
			return trimWS(dropFirstChar(lines[i])), true
		}
	}
	return "", false
}

// ClearKeys is enough `ctrl+u` presses to empty text: one per line plus one per line break.
func ClearKeys(text string) []string {
	n := min(len(strings.Split(text, "\n"))*2, 60)
	keys := make([]string, n)
	for i := range keys {
		keys[i] = "ctrl+u"
	}
	return keys
}

// InputFor is the text in kind's input box: "" when empty; ok is false when the screen shows no
// box or the kind has none. ansi is a visible read with colours, used by codex only ("" = none).
//   - claude: InputBoxContent.
//   - pi: the lines between the last two full-width `─` rules (above the cwd and stats footer).
//   - codex: the `› ` composer line and its 2-space continuation lines, down to a blank line.
//     An empty composer shows a dim placeholder ("Ask Codex to do anything"), so with ansi a
//     composer whose text is all dim is empty.
func InputFor(kind, screen, ansi string) (string, bool) {
	switch kind {
	case "claude":
		return InputBoxContent(screen)
	case "pi":
		return piInput(screen)
	case "codex":
		return codexInput(screen, ansi)
	}
	return "", false
}

// ClearKeysFor is enough keys to empty text from kind's input box. `ctrl+u` works the same way
// in claude, pi and codex: it clears the current line, then joins the empty line to the one above.
func ClearKeysFor(kind, text string) []string { return ClearKeys(text) }

func isPiRule(l string) bool {
	t := trimWS(l)
	return len([]rune(t)) >= 10 && allIn(t, "─")
}

func piInput(screen string) (string, bool) {
	lines := strings.Split(screen, "\n")
	bottom, top := -1, -1
	for i := len(lines) - 1; i >= 0; i-- {
		if isPiRule(lines[i]) {
			if bottom < 0 {
				bottom = i
			} else {
				top = i
				break
			}
		}
	}
	if top < 0 || bottom-top < 2 {
		return "", false
	}
	var parts []string
	for _, l := range lines[top+1 : bottom] {
		parts = append(parts, trimWS(l))
	}
	return strings.TrimFunc(strings.Join(parts, "\n"), isWSOrNewline), true
}

func codexInput(screen, ansi string) (string, bool) {
	lines := strings.Split(screen, "\n")
	start := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "›") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", false
	}
	if ansi != "" && codexPlaceholder(ansi) {
		return "", true
	}
	parts := []string{trimWS(dropFirstChar(lines[start]))}
	for _, l := range lines[start+1:] {
		if trimWS(l) == "" || !strings.HasPrefix(l, "  ") {
			break
		}
		parts = append(parts, trimWS(l))
	}
	return strings.TrimFunc(strings.Join(parts, "\n"), isWSOrNewline), true
}

// codexPlaceholder: the last `›` line of an ANSI read has text, and all of it is dim (SGR 2).
func codexPlaceholder(ansi string) bool {
	lines := strings.Split(strings.ReplaceAll(ansi, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		plain, dim := sgrText(lines[i])
		if !strings.HasPrefix(plain, "›") {
			continue
		}
		seen := false
		for j, r := range []rune(plain) {
			if j == 0 || isWS(r) {
				continue
			}
			seen = true
			if !dim[j] {
				return false
			}
		}
		return seen
	}
	return false
}

// sgrText strips escape sequences from line and reports, per rune of the result, whether it was
// drawn dim.
func sgrText(line string) (string, []bool) {
	var b strings.Builder
	var dim []bool
	on := false
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j < len(line) && line[j] == 'm' {
				ps := strings.Split(line[i+2:j], ";")
				for k := 0; k < len(ps); k++ {
					switch strings.TrimLeft(ps[k], "0") {
					case "":
						on = false // 0 / empty: reset
					case "2":
						on = true
					case "22":
						on = false
					case "38", "48", "58": // colours: skip their `5;n` / `2;r;g;b` arguments
						if k+1 < len(ps) && ps[k+1] == "5" {
							k += 2
						} else if k+1 < len(ps) && ps[k+1] == "2" {
							k += 4
						}
					}
				}
			}
			i = j + 1
			continue
		}
		r, n := utf8.DecodeRuneInString(line[i:])
		b.WriteRune(r)
		dim = append(dim, on)
		i += n
	}
	return b.String(), dim
}
