package approval

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// Ported from FuzzTests.swift: the cases for this package's parsers. The property is "never
// panics, never hangs".

var fuzzPool = chars("abcXYZ012 \n\t\r❯›▶☐☒✔☑✓←→│┃║╭╮╰╯┌┐└┘─━═-–—╌┄()[]{}.,:;!?\"'`~@#$%^&*+=|\\/<>_🎉🙂👍️\x00\x1b\x7f\u200b\ufeff😀")

func randomScreen(maxLen int) string {
	var b strings.Builder
	for range rand.IntN(maxLen + 1) {
		b.WriteString(fuzzPool[rand.IntN(len(fuzzPool))])
	}
	return b.String()
}

func TestApprovalParserNeverCrashesOnRandomScreens(t *testing.T) {
	kinds := []string{"", "claude", "codex", "pi", "gemini"}
	for range 500 {
		screen := randomScreen(400)
		_ = Parse(screen, "w1:p1", none, "proj", kinds[rand.IntN(len(kinds))])
		_ = Fallback(screen, "w1:p1", none)
		_ = Step(screen, "")
	}
}

func TestPickerParserNeverCrashesOnRandomScreens(t *testing.T) {
	for range 500 {
		_, _ = ParsePicker(randomScreen(400))
	}
}

func TestInputBoxNeverCrashesOnRandomScreens(t *testing.T) {
	for range 500 {
		s := randomScreen(400)
		_, _ = InputBoxContent(s)
		_, _ = LastPromptLine(s)
	}
}
