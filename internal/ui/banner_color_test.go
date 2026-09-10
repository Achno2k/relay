package ui

import "testing"

func TestMixHex(t *testing.T) {
	if got := mixHex("#000000", "#ffffff", 0.5); got != "#808080" {
		t.Fatalf("got %s", got)
	}
	if got := mixHex("#A78BFA", "#FB923C", 0); got != "#a78bfa" {
		t.Fatalf("got %s", got)
	}
	if got := mixHex("#A78BFA", "#FB923C", 1); got != "#fb923c" {
		t.Fatalf("got %s", got)
	}
}

func TestStyledFrameKeepsShape(t *testing.T) {
	// Stripped of styling, the coloured frame must equal the plain frame.
	a := newBannerAnim(1)
	for i := range a.glyphs {
		a.settle(i)
	}
	plain, styled := a.frame(), a.styledFrame()
	for r := range plain {
		if sanitizeLog(styled[r]) != plain[r] {
			t.Fatalf("row %d differs:\n%q\n%q", r, sanitizeLog(styled[r]), plain[r])
		}
	}
}
