package ui

import (
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/muesli/termenv"
)

// Palette. Monochrome base with a single accent; status colours only on the
// three glyphs. Every colour is adaptive so it stays legible on light and dark
// terminals.
var (
	colorAccent = lipgloss.AdaptiveColor{Light: "#B85A2E", Dark: "#E07A4F"}
	colorOK     = lipgloss.AdaptiveColor{Light: "#2E7D32", Dark: "#5FBF6A"}
	colorFail   = lipgloss.AdaptiveColor{Light: "#C0392B", Dark: "#E05C4B"}
	colorWarn   = lipgloss.AdaptiveColor{Light: "#A9701A", Dark: "#E0B34F"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#767676", Dark: "#8A8A8A"}
)

// Status marks. They are always shown inside dim brackets, the way
// `flutter doctor` reports: the bracket pair holds the column, the mark inside
// carries the colour.
const (
	markOK    = "✓"
	markFail  = "☠"
	markWarn  = "!"
	markEmpty = " " // pending, and steps skipped after a failure
)

// Rendered badges. Off a terminal these come out as plain "[✓]", "[☠]", "[!]"
// and "[ ]", which is still readable in a log.
func badgeOK() string    { return badge(markOK, okStyle()) }
func badgeFail() string  { return badge(markFail, failStyle()) }
func badgeWarn() string  { return badge(markWarn, warnStyle()) }
func badgeEmpty() string { return badge(markEmpty, mutedStyle()) }
func badgeWidth() int    { return 3 }
func badgeActive(frame int) string {
	return badge(loaderFrames[frame%len(loaderFrames)], accentStyle())
}

// badge wraps one mark in dim brackets.
func badge(mark string, style lipgloss.Style) string {
	dim := mutedStyle()
	return dim.Render("[") + style.Render(mark) + dim.Render("]")
}

// loaderFrames is the "dots_1" loader from the npm package cli-loaders,
// copied frame for frame instead of depended on: this is Go, and the whole
// asset is ten strings and a number.
//
//	package  cli-loaders v3.0.0   https://www.npmjs.com/package/cli-loaders
//	source   https://github.com/cbmongithub/cli-loaders
//	licence  MIT, so copying the frames with attribution is fine
//	upstream {speed: 80, keyframes: ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"]}
//
// Verified against both the ESM and CJS builds in the published tarball, which
// agree. dots_1 turns out to be the classic braille spinner, so the fallback
// the brief allowed for and the real thing are the same ten frames either way.
var loaderFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// loaderInterval is cli-loaders' dots_1 speed of 80ms per frame.
const loaderInterval = 80 * time.Millisecond

var (
	outMu    sync.Mutex
	stdout   io.Writer = os.Stdout
	stderr   io.Writer = os.Stderr
	renderer           = newRenderer(os.Stdout)
)

func newRenderer(w io.Writer) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(w)
	if !colorEnabled(w) {
		r.SetColorProfile(termenv.Ascii)
	}
	return r
}

// setOutput redirects every ui write. Test hook; production always uses the
// process stdout/stderr set at init.
func setOutput(w io.Writer) func() {
	outMu.Lock()
	prevOut, prevErr, prevRenderer := stdout, stderr, renderer
	stdout, stderr, renderer = w, w, newRenderer(w)
	outMu.Unlock()
	return func() {
		outMu.Lock()
		stdout, stderr, renderer = prevOut, prevErr, prevRenderer
		outMu.Unlock()
	}
}

// colorEnabled reports whether styling should be emitted at all. NO_COLOR wins
// over everything, then the writer has to be a real terminal.
func colorEnabled(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return isTTY(w)
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(f.Fd())
}

// interactive reports whether we can drive an animated UI: stdout has to be a
// terminal we may redraw, stdin has to be readable by a human, and the user
// must not have asked for plain output with AGENTS_UI_PLAIN or a dumb TERM.
// Everything else gets static lines and huh's numbered stdin prompts.
//
// NO_COLOR deliberately plays no part here. It asks for no colour, not for no
// motion. Routing it through colorEnabled meant anyone with NO_COLOR in their
// profile lost the loader entirely, which is how this function used to answer
// "no animation" in a perfectly capable terminal.
func interactive() bool {
	if !envAllowsAnimation() {
		return false
	}
	outMu.Lock()
	w := stdout
	outMu.Unlock()
	return isTTY(w) && isTTY(os.Stdin)
}

// envAllowsAnimation is the environment half of interactive(), split out so the
// rules can be tested without a pty. It consults AGENTS_UI_PLAIN and TERM, and
// nothing else: NO_COLOR governs colour, not motion.
func envAllowsAnimation() bool {
	if os.Getenv("AGENTS_UI_PLAIN") == "1" {
		return false
	}
	switch os.Getenv("TERM") {
	case "", "dumb":
		return false
	}
	return true
}

// termWidth is the usable width, with a sane default off a terminal.
func termWidth() int {
	outMu.Lock()
	w := stdout
	outMu.Unlock()
	f, ok := w.(*os.File)
	if !ok {
		return 100
	}
	cols, _, err := term.GetSize(f.Fd())
	if err != nil || cols <= 0 {
		return 100
	}
	return cols
}

func accentStyle() lipgloss.Style { return renderer.NewStyle().Foreground(colorAccent) }
func okStyle() lipgloss.Style     { return renderer.NewStyle().Foreground(colorOK) }
func failStyle() lipgloss.Style   { return renderer.NewStyle().Foreground(colorFail) }
func warnStyle() lipgloss.Style   { return renderer.NewStyle().Foreground(colorWarn) }
func mutedStyle() lipgloss.Style  { return renderer.NewStyle().Foreground(colorMuted) }
func plainStyle() lipgloss.Style  { return renderer.NewStyle() }

// line writes one styled line. While a live tree is on screen the text becomes
// a node in it, so it lands in the right place under its phase instead of
// tearing the frame. Otherwise it goes straight to stdout, indented to the
// phase nesting the non-tty path is tracking.
func line(s string) {
	if lv.emit(s) {
		return
	}
	outMu.Lock()
	defer outMu.Unlock()
	io.WriteString(stdout, indentLines(s, lv.indent())+"\n")
}

// errLine is line for stderr. Inside a live tree it has to join the tree like
// everything else: a stray write to stderr would land in the middle of the
// frame the renderer is repainting.
func errLine(s string) {
	if lv.emit(s) {
		return
	}
	outMu.Lock()
	defer outMu.Unlock()
	io.WriteString(stderr, indentLines(s, lv.indent())+"\n")
}

// indentLines puts prefix in front of every non-empty line of s. Blank lines
// stay blank rather than becoming runs of trailing spaces.
func indentLines(s, prefix string) string {
	if prefix == "" {
		return s
	}
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		if p != "" {
			parts[i] = prefix + p
		}
	}
	return strings.Join(parts, "\n")
}

// stdoutFile returns stdout as a file for bubbletea, or nil when it is not one.
func stdoutFile() *os.File {
	outMu.Lock()
	defer outMu.Unlock()
	f, _ := stdout.(*os.File)
	return f
}

func pad(n int) string {
	if n < 0 {
		n = 0
	}
	return strings.Repeat(" ", n)
}
