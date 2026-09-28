package approval

import (
	"strings"
	"testing"
)

// Ported from InputBoxTests.swift, same cases and names.

func TestReadsRestoredPrompt(t *testing.T) {
	text, ok := InputBoxContent(fixture(t, "input-restored.txt"))
	expectEqual(t, ok, true)
	expectEqual(t, text, "Write a 400 word essay about rivers.\nSecond line of the prompt.")
	text, ok = InputBoxContent(fixture(t, "input-empty.txt"))
	expectEqual(t, ok, true)
	expectEqual(t, text, "")
}

func TestNoBoxOnDialogs(t *testing.T) {
	_, ok := InputBoxContent(fixture(t, "approval-trust.txt"))
	expectEqual(t, ok, false)
	_, ok = InputBoxContent("plain output")
	expectEqual(t, ok, false)
}

func TestClearKeysCoverEveryLineAndBreak(t *testing.T) {
	expectEqual(t, ClearKeys("one line"), []string{"ctrl+u", "ctrl+u"})
	expectEqual(t, len(ClearKeys("a\nb\nc")), 6)
}

// MARK: R8-11: pi and codex input boxes (screens from go-server, synthetic, 60 columns)

func TestInputForPi(t *testing.T) {
	for _, c := range []struct{ file, want string }{
		{"pi-input-empty.txt", ""},
		{"pi-input-typed.txt", "hello world"},
		{"pi-input-multi.txt", "line one\nline two\nline three"},
		{"pi-input-wrapped.txt", strings.Repeat("x", 60) + "\n" + strings.Repeat("x", 20)},
	} {
		text, ok := InputFor("pi", fixture(t, c.file), "")
		expectEqual(t, ok, true)
		expectEqual(t, text, c.want)
	}
}

func TestInputForCodex(t *testing.T) {
	for _, c := range []struct{ file, ansi, want string }{
		{"codex-input-empty.txt", "codex-input-empty.ansi", ""},
		{"codex-input-typed.txt", "codex-input-typed.ansi", "hello world"},
		{"codex-input-multi.txt", "", "line one\nline two\nline three"},
		{"codex-input-wrapped.txt", "", strings.Repeat("y", 58) + "\n" + strings.Repeat("y", 20)},
	} {
		ansi := ""
		if c.ansi != "" {
			ansi = fixture(t, c.ansi)
		}
		text, ok := InputFor("codex", fixture(t, c.file), ansi)
		expectEqual(t, ok, true)
		expectEqual(t, text, c.want)
	}
	// Without colours the placeholder can't be told from typed text; clearing it is harmless.
	text, _ := InputFor("codex", fixture(t, "codex-input-empty.txt"), "")
	expectEqual(t, text, "Ask Codex to do anything")
}

func TestInputForClaudeAndOthers(t *testing.T) {
	text, ok := InputFor("claude", fixture(t, "input-restored.txt"), "")
	expectEqual(t, ok, true)
	expectEqual(t, text, "Write a 400 word essay about rivers.\nSecond line of the prompt.")
	_, ok = InputFor("gemini", fixture(t, "input-restored.txt"), "")
	expectEqual(t, ok, false)
	_, ok = InputFor("pi", "plain output", "")
	expectEqual(t, ok, false)
	_, ok = InputFor("codex", "plain output", "")
	expectEqual(t, ok, false)
	expectEqual(t, ClearKeysFor("pi", "a\nb"), []string{"ctrl+u", "ctrl+u", "ctrl+u", "ctrl+u"})
}
