package live

import (
	"strings"
	"testing"
)

// The round 5 open item: Claude Code's "Update available!" banner bleeding into live reply text.
// These screens are synthetic, in the shape herdr's `format: ansi` visible read has.

const (
	reset = "\x1b[0m"
	grey  = "\x1b[38;2;153;153;153m"
	white = "\x1b[38;2;255;255;255m"
	amber = "\x1b[38;2;255;193;7m"
	bold  = "\x1b[1m"
	rule  = "\x1b[38;2;136;136;136m" + "────────────────────────────────────────" + reset
)

func banner(pad int) string {
	return strings.Repeat(" ", pad) + reset + amber + "Update available! Run: " + reset + bold + amber + "brew upgrade claude-code@latest" + reset
}

func claudeANSI(rows ...string) string {
	return strings.Join(append(rows,
		rule,
		"❯ "+reset,
		rule,
		"  "+reset+amber+"⏵⏵ auto mode on"+reset+grey+" (shift+tab to cycle) · ← 1 agent"+reset,
	), "\n")
}

func TestScreenTextMatchesThePlainRead(t *testing.T) {
	// herdr's text read: no escapes, trailing blanks (NBSP included) trimmed, a final newline.
	ansi := reset + white + "⏺ " + reset + "Hello there" + reset + "\x1b[48;2;55;55;55m      " + reset + "\n" +
		"❯ " + reset + "\n" +
		"\x1b[38;5;248m87.4k" + reset + "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\" + reset
	expectEqual(t, ScreenText(ansi), "⏺ Hello there\n❯\n87.4klink\n")
}

func TestScreenTextBlanksTheBannerOnItsOwnRow(t *testing.T) {
	text := ScreenText(claudeANSI(reset+white+"⏺ "+reset+"Hello there", banner(40)))
	if strings.Contains(text, "Update") || strings.Contains(text, "brew") {
		t.Fatalf("banner left in:\n%s", text)
	}
	expectEqual(t, Extract(text, "claude"), strPtr("Hello there"))
}

func TestScreenTextBlanksABannerConcatenatedOntoText(t *testing.T) {
	ansi := claudeANSI(
		reset+white+"⏺ "+reset+"If there's no cached answer, the query goes out to a recursive resolver, typically operated by",
		"  your ISP or a public service like 1.1.1.1 or 8."+banner(20),
		"  down the answer.",
	)
	expectEqual(t, Extract(ScreenText(ansi), "claude"), strPtr("If there's no cached answer, the query goes out to a recursive resolver, typically operated by your ISP or a public service like 1.1.1.1 or 8. down the answer."))
}

func TestScreenTextBlanksABannerThatTextPartlyOverwrote(t *testing.T) {
	// The reply's row was drawn over the banner's first cells, so "Update available!" is gone
	// and only its tail is left, glued to the reply: the plain-text strip can't see it.
	row := "  your ISP or a public service like 1.1.1.1 or 8.8.8.8, whi" + reset + amber + "able! Run: " + reset + bold + amber + "brew upgrade claude-code@latest" + reset
	ansi := claudeANSI(
		reset+white+"⏺ "+reset+"If there's no cached answer, the query goes out to a recursive resolver, typically operated by",
		row,
		"  ch tracks down the answer.",
	)
	plain := strings.NewReplacer(reset, "", amber, "", bold, "", white, "").Replace(row)
	if !strings.Contains(stripInlineBanner(plain), "brew upgrade") {
		t.Fatal("the plain-text strip was expected to miss this banner")
	}
	text := Extract(ScreenText(ansi), "claude")
	expectEqual(t, text, strPtr("If there's no cached answer, the query goes out to a recursive resolver, typically operated by your ISP or a public service like 1.1.1.1 or 8.8.8.8, whi ch tracks down the answer."))
}

func TestScreenTextBlanksBannerCellsInterleavedWithText(t *testing.T) {
	// Text and banner cells alternating on one row: only the banner's go.
	row := "  the answer is" + reset + amber + " Upd" + reset + " forty" + amber + "able! Run: " + reset + bold + amber + "brew upgrade claude-code@latest" + reset
	text := Extract(ScreenText(claudeANSI(reset+white+"⏺ "+reset+"So,", row)), "claude")
	expectEqual(t, text, strPtr("So, the answer is     forty"))
}

func TestScreenTextLearnsTheBannerColour(t *testing.T) {
	// Another theme: the colour comes from the intact banner in the footer, then blanks the
	// fragment on the reply row.
	other := "\x1b[38;2;150;108;30m"
	fragment := "  line of the reply" + reset + other + "ilable! Run: claude update" + reset
	intact := strings.Repeat(" ", 30) + reset + other + "Update available! Run: claude update" + reset
	text := ScreenText(claudeANSI(reset+white+"⏺ "+reset+"First", fragment, intact))
	if strings.Contains(text, "ilable") || strings.Contains(text, "claude update") {
		t.Fatalf("banner left in:\n%s", text)
	}
	expectEqual(t, Extract(text, "claude"), strPtr("First line of the reply"))
}

func TestScreenTextKeepsTheAutoModeFooter(t *testing.T) {
	// Same colour as the banner, but the row ends in grey: not a banner row.
	text := ScreenText(claudeANSI(reset + white + "⏺ " + reset + "Hi"))
	if !strings.Contains(text, "⏵⏵ auto mode on (shift+tab to cycle) · ← 1 agent") {
		t.Fatalf("footer changed:\n%s", text)
	}
}

func TestScreenTextKeepsAmberReplyText(t *testing.T) {
	// Amber that isn't at a row's end (a warning word mid-line) stays.
	row := "  a " + amber + "warning" + reset + " in the middle"
	expectEqual(t, Extract(ScreenText(claudeANSI(reset+white+"⏺ "+reset+"Here is", row)), "claude"), strPtr("Here is a warning in the middle"))
}
