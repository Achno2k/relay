package approval

import "strings"

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
