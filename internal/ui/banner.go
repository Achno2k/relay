package ui

import (
	"io"
	"math/rand"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// bannerGlyphs is the AGENTS wordmark, drawn here rather than pulled from a
// font package: six letters at five rows each is less code than a dependency,
// and it never changes. Every glyph is exactly bannerGlyphWidth columns so the
// letters can be animated independently.
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

// Animation timings. Six letters at bannerLetterGap plus the flicker and the
// fade keeps the whole sequence comfortably under two seconds.
const (
	bannerLetterGap   = 180 * time.Millisecond // from one letter to the next
	bannerGlitchFrame = 45 * time.Millisecond  // one glitch frame
	bannerGlitchCount = 3                      // glitch frames before a letter snaps
	bannerFlickerHold = 60 * time.Millisecond  // the final word wide flicker
	bannerFadeStep    = 60 * time.Millisecond  // one step of the version fade
	bannerFlickerN    = 2                      // letters that flicker at the end
)

// bannerGlitchChars are the block shades a letter cycles through before it
// settles. Light to heavy, so a run of them reads as something resolving
// rather than as noise.
var bannerGlitchChars = []rune{'░', '▒', '▓', '█'}

// bannerFadeShades are the version line materialising underneath.
var bannerFadeShades = []rune{'░', '▒', '▓'}

// bannerSeed is where the randomness comes from. Real runs vary; tests pin it.
var bannerSeed = func() int64 { return time.Now().UnixNano() }

// bannerAnim holds the per-letter state, so any point in the sequence can be
// rendered as a whole frame. Keeping the state here rather than in Banner is
// what makes the animation testable: given a seed, the frames are fixed.
type bannerAnim struct {
	rng    *rand.Rand
	glyphs [][bannerRows]string
	shown  int // letters that have finished resolving
}

func newBannerAnim(seed int64) *bannerAnim {
	a := &bannerAnim{rng: rand.New(rand.NewSource(seed))}
	a.glyphs = make([][bannerRows]string, len(bannerWord))
	for i := range a.glyphs {
		a.glyphs[i] = blankGlyph()
	}
	return a
}

func blankGlyph() [bannerRows]string {
	var g [bannerRows]string
	for r := range g {
		g[r] = strings.Repeat(" ", bannerGlyphWidth)
	}
	return g
}

// realGlyph is the finished shape of letter i.
func realGlyph(i int) [bannerRows]string {
	if g, ok := bannerGlyphs[rune(bannerWord[i])]; ok {
		return g
	}
	return blankGlyph()
}

// glitchGlyph is one frame of a letter still resolving: random block shades
// over the cells the real letter uses, shifted a column left or right so the
// letter jitters before it snaps into place.
func (a *bannerAnim) glitchGlyph(i int) [bannerRows]string {
	real := realGlyph(i)
	jitter := a.rng.Intn(3) - 1 // -1, 0 or +1 columns

	var g [bannerRows]string
	for r := 0; r < bannerRows; r++ {
		cells := []rune(strings.Repeat(" ", bannerGlyphWidth))
		for c, ch := range []rune(real[r]) {
			if ch == ' ' {
				continue
			}
			at := c + jitter
			if at < 0 || at >= bannerGlyphWidth {
				continue
			}
			cells[at] = bannerGlitchChars[a.rng.Intn(len(bannerGlitchChars))]
		}
		g[r] = string(cells)
	}
	return g
}

// shadeGlyph is letter i drawn entirely in one shade, used for the flicker.
func shadeGlyph(i int, shade rune) [bannerRows]string {
	real := realGlyph(i)
	var g [bannerRows]string
	for r := 0; r < bannerRows; r++ {
		cells := []rune(strings.Repeat(" ", bannerGlyphWidth))
		for c, ch := range []rune(real[r]) {
			if ch != ' ' {
				cells[c] = shade
			}
		}
		g[r] = string(cells)
	}
	return g
}

func (a *bannerAnim) set(i int, g [bannerRows]string) { a.glyphs[i] = g }

// settle puts letter i into its real shape and counts it as arrived.
func (a *bannerAnim) settle(i int) {
	a.glyphs[i] = realGlyph(i)
	if i+1 > a.shown {
		a.shown = i + 1
	}
}

// frame renders the wordmark as it currently stands, one string per row.
func (a *bannerAnim) frame() []string {
	out := make([]string, bannerRows)
	for r := 0; r < bannerRows; r++ {
		var b strings.Builder
		for i := range a.glyphs {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", bannerGap))
			}
			b.WriteString(a.glyphs[i][r])
		}
		out[r] = b.String()
	}
	return out
}

// pickFlicker chooses n settled letters to blink, without repeats.
func (a *bannerAnim) pickFlicker(n int) []int {
	if a.shown == 0 || n <= 0 {
		return nil
	}
	if n > a.shown {
		n = a.shown
	}
	return a.rng.Perm(a.shown)[:n]
}

// bannerLines is the finished wordmark, one string per row.
func bannerLines() []string {
	a := newBannerAnim(0)
	for i := range a.glyphs {
		a.settle(i)
	}
	return a.frame()
}

// bannerWidth is the wordmark's column count.
func bannerWidth() int {
	return len(bannerWord)*bannerGlyphWidth + (len(bannerWord)-1)*bannerGap
}

// fadeSub is the version line part way through materialising: step 0 is the
// lightest shade, the last step is the real text.
func fadeSub(sub string, step int) string {
	if step >= len(bannerFadeShades) {
		return sub
	}
	return strings.Repeat(string(bannerFadeShades[step]), len([]rune(sub)))
}

// Banner prints the AGENTS wordmark with the version under it.
//
// On a terminal the letters arrive one at a time from the left, each one
// glitching through a few frames of block shade before it snaps into shape;
// then two letters flicker once and the version line fades in. Anywhere else
// the finished banner is printed at once, because an animation nobody watches
// is just noise in a log.
func Banner(version string) {
	sub := "agents " + version

	if !interactive() {
		line("")
		for _, r := range bannerLines() {
			line(plainStyle().Render(strings.TrimRight(r, " ")))
		}
		line(mutedStyle().Render(sub))
		line("")
		return
	}

	a := newBannerAnim(bannerSeed())
	rawLine("")
	draw := func(subLine string) {
		for _, r := range a.frame() {
			rawLine(plainStyle().Render(r))
		}
		rawLine(mutedStyle().Render(subLine))
	}
	redraw := func(subLine string) {
		cursorUp(bannerRows + 1)
		draw(subLine)
	}

	blankSub := strings.Repeat(" ", len([]rune(sub)))
	draw(blankSub)

	// Letters arrive left to right, each glitching before it settles.
	for i := range a.glyphs {
		for f := 0; f < bannerGlitchCount; f++ {
			a.set(i, a.glitchGlyph(i))
			redraw(blankSub)
			time.Sleep(bannerGlitchFrame)
		}
		a.settle(i)
		redraw(blankSub)
		if rest := bannerLetterGap - bannerGlitchCount*bannerGlitchFrame; rest > 0 {
			time.Sleep(rest)
		}
	}

	// One flicker across the finished word.
	flickered := a.pickFlicker(bannerFlickerN)
	for _, i := range flickered {
		a.set(i, shadeGlyph(i, '░'))
	}
	redraw(blankSub)
	time.Sleep(bannerFlickerHold)
	for _, i := range flickered {
		a.settle(i)
	}
	redraw(blankSub)

	// The version line fades in underneath.
	for step := 0; step <= len(bannerFadeShades); step++ {
		redraw(fadeSub(sub, step))
		if step < len(bannerFadeShades) {
			time.Sleep(bannerFadeStep)
		}
	}
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
