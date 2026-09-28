package approval

import "testing"

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
