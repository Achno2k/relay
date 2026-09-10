package ui

import (
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// bannerGlyphs is the AGENTS wordmark, drawn here rather than pulled from a
// font package: six letters at five rows each is less code than a dependency,
// and it never changes. Every glyph is exactly bannerGlyphWidth columns so the
// wordmark can be revealed a column at a time.
const (
	bannerRows       = 5
	bannerGlyphWidth = 5
	bannerGap        = 1
)

var bannerGlyphs = map[rune][bannerRows]string{
	'A': {
		" ███ ",
		"█   █",
		"█████",
		"█   █",
		"█   █",
	},
	'G': {
		" ████",
		"█    ",
		"█  ██",
		"█   █",
		" ████",
	},
	'E': {
		"█████",
		"█    ",
		"████ ",
		"█    ",
		"█████",
	},
	'N': {
		"█   █",
		"██  █",
		"█ █ █",
		"█  ██",
		"█   █",
	},
	'T': {
		"█████",
		"  █  ",
		"  █  ",
		"  █  ",
		"  █  ",
	},
	'S': {
		" ████",
		"█    ",
		" ███ ",
		"    █",
		"████ ",
	},
}

// bannerWord is what the wordmark spells.
const bannerWord = "AGENTS"

// bannerRevealTime is how long the left to right reveal takes.
const bannerRevealTime = 500 * time.Millisecond

// bannerLines assembles the wordmark, one string per row.
func bannerLines() []string {
	out := make([]string, bannerRows)
	for row := 0; row < bannerRows; row++ {
		var b strings.Builder
		for i, r := range bannerWord {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", bannerGap))
			}
			g, ok := bannerGlyphs[r]
			if !ok {
				b.WriteString(strings.Repeat(" ", bannerGlyphWidth))
				continue
			}
			b.WriteString(g[row])
		}
		out[row] = b.String()
	}
	return out
}

// bannerWidth is the wordmark's column count.
func bannerWidth() int {
	return len(bannerWord)*bannerGlyphWidth + (len(bannerWord)-1)*bannerGap
}

// revealTo cuts every row to the first n columns, keeping the rows the same
// length so the block does not jitter as it fills in.
func revealTo(rows []string, n, width int) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		runes := []rune(r)
		if n < len(runes) {
			runes = runes[:n]
		}
		out[i] = string(runes) + strings.Repeat(" ", width-len(runes))
	}
	return out
}

// Banner prints the AGENTS wordmark with the version under it. On a terminal
// the letters are revealed left to right over about half a second; anywhere
// else the whole thing is printed at once, because an animation nobody watches
// is just noise in a log.
func Banner(version string) {
	rows := bannerLines()
	width := bannerWidth()
	sub := "agents " + version

	if !interactive() {
		line("")
		for _, r := range rows {
			line(plainStyle().Render(strings.TrimRight(r, " ")))
		}
		line(mutedStyle().Render(sub))
		line("")
		return
	}

	rawLine("")
	for _, r := range revealTo(rows, 0, width) {
		rawLine(r)
	}

	step := bannerRevealTime / time.Duration(width)
	for n := 1; n <= width; n++ {
		cursorUp(bannerRows)
		for _, r := range revealTo(rows, n, width) {
			rawLine(plainStyle().Render(r))
		}
		if n < width {
			time.Sleep(step)
		}
	}

	rawLine(mutedStyle().Render(sub))
	rawLine("")
}

// rawLine writes straight at the terminal, bypassing the tree renderer. Banner
// runs before anything else, so there is no tree to disturb, and it needs to
// redraw its own rows in place.
func rawLine(s string) {
	outMu.Lock()
	defer outMu.Unlock()
	io.WriteString(stdout, s+"\n")
}

// cursorUp moves back over n rows so they can be redrawn.
func cursorUp(n int) {
	if n <= 0 {
		return
	}
	outMu.Lock()
	defer outMu.Unlock()
	io.WriteString(stdout, "\x1b["+itoa(n)+"A\r")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// bannerVisualWidth exists so tests can assert the rows line up.
func bannerVisualWidth(rows []string) int {
	w := 0
	for _, r := range rows {
		if n := lipgloss.Width(r); n > w {
			w = n
		}
	}
	return w
}
