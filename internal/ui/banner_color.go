package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Banner colour: settled letters run a gradient from the purple accent on the
// left to the orange loader colour on the right; a letter still glitching is
// drawn dim so the motion reads as "not yet".
var (
	bannerFromDark, bannerToDark   = "#A78BFA", "#FB923C"
	bannerFromLight, bannerToLight = "#6D28D9", "#C2410C"
)

// letterStyle is the colour of settled letter i of n.
func letterStyle(i, n int) lipgloss.Style {
	t := 0.0
	if n > 1 {
		t = float64(i) / float64(n-1)
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{
		Light: mixHex(bannerFromLight, bannerToLight, t),
		Dark:  mixHex(bannerFromDark, bannerToDark, t),
	})
}

// mixHex linearly interpolates two #rrggbb colours.
func mixHex(a, b string, t float64) string {
	var ar, ag, ab, br, bg, bb int
	fmt.Sscanf(a, "#%02x%02x%02x", &ar, &ag, &ab)
	fmt.Sscanf(b, "#%02x%02x%02x", &br, &bg, &bb)
	mix := func(x, y int) int { return int(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", mix(ar, br), mix(ag, bg), mix(ab, bb))
}

// styledFrame renders the wordmark rows with each letter coloured: settled
// letters by their gradient position, unsettled ones dim.
func (a *bannerAnim) styledFrame() []string {
	n := len(a.glyphs)
	out := make([]string, bannerRows)
	for r := 0; r < bannerRows; r++ {
		var b strings.Builder
		for i, g := range a.glyphs {
			if i > 0 {
				b.WriteString(strings.Repeat(" ", bannerGap))
			}
			cell := g[r]
			if i < a.shown && g == realGlyph(i) {
				b.WriteString(letterStyle(i, n).Render(cell))
			} else {
				b.WriteString(mutedStyle().Render(cell))
			}
		}
		out[r] = b.String()
	}
	return out
}

// styledBannerLines is the finished, coloured wordmark.
func styledBannerLines() []string {
	a := newBannerAnim(0)
	for i := range a.glyphs {
		a.settle(i)
	}
	return a.styledFrame()
}
