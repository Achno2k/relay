package live

import "strings"

// Claude Code draws its "Update available! Run: …" banner right-aligned at a fixed row. Once the
// alternate screen scrolls, that row can be a reply row: the banner overwrites the right part of
// the text, and later text writes can overwrite part of the banner. A plain-text read then holds
// both interleaved, and nothing in the text says which characters are whose.
//
// The colours do. The monitor reads the screen with ANSI, and ScreenText blanks every cell drawn
// in the banner's colour on a banner row before stripping the escapes. What's left is the text
// herdr's plain read would give, minus the banner's cells: the reply's own characters stay in
// their columns, and the ones the banner covered are gone (they're not on screen to recover).

// defaultBannerColors is Claude Code's warning colour in its dark themes, used when the screen
// shows no intact "Update available!" to learn the colour from.
var defaultBannerColors = []string{"38;2;255;193;7"}

const bannerMarker = "Update available"

// run is text drawn with one foreground colour. fg is the SGR parameters that set it
// ("38;2;255;193;7", "33", "38;5;214"), "" for the default.
type run struct {
	fg   string
	text string
}

// ScreenText turns an ANSI screen read into the plain text herdr's `format: text` read gives
// (escapes stripped, trailing blanks trimmed per line, a final newline), with the "Update
// available!" banner's cells blanked out of any row it sits on.
func ScreenText(ansi string) string {
	lines := splitRuns(ansi)
	colors := map[string]bool{}
	for _, c := range defaultBannerColors {
		colors[c] = true
	}
	for _, runs := range lines {
		for _, r := range runs {
			if r.fg != "" && strings.Contains(r.text, bannerMarker) {
				colors[r.fg] = true
			}
		}
	}
	var b strings.Builder
	for i, runs := range lines {
		banner := isBannerRow(runs, colors)
		var line strings.Builder
		for _, r := range runs {
			if banner && colors[r.fg] {
				for range r.text {
					line.WriteByte(' ')
				}
				continue
			}
			line.WriteString(r.text)
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimRightFunc(line.String(), isWS))
	}
	out := b.String()
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// isBannerRow: the row shows the banner's marker, or its last visible run is in the banner's
// colour (the banner is right-aligned, so on its row it's what the row ends with). The footer's
// "⏵⏵ auto mode on" shares the colour but ends in grey, so it's left alone.
func isBannerRow(runs []run, colors map[string]bool) bool {
	for _, r := range runs {
		if strings.Contains(r.text, bannerMarker) {
			return true
		}
	}
	for i := len(runs) - 1; i >= 0; i-- {
		if strings.TrimFunc(runs[i].text, isWS) == "" {
			continue
		}
		return colors[runs[i].fg]
	}
	return false
}

// splitRuns splits an ANSI screen into lines of runs, tracking the foreground colour across SGR
// sequences (and lines). Other escape sequences are dropped.
func splitRuns(s string) [][]run {
	var lines [][]run
	var cur []run
	fg := ""
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			cur = append(cur, run{fg: fg, text: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\n':
			flush()
			lines = append(lines, cur)
			cur = nil
			i++
		case c == 0x1b && i+1 < len(s) && s[i+1] == '[':
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				next := applySGR(fg, s[i+2:j])
				if next != fg {
					flush()
					fg = next
				}
			}
			i = j + 1
		case c == 0x1b && i+1 < len(s) && s[i+1] == ']':
			// OSC (e.g. hyperlinks): up to BEL or ESC \.
			j := i + 2
			for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
				j++
			}
			if j < len(s) && s[j] == 0x1b {
				j++
			}
			i = j + 1
		case c == 0x1b:
			i += 2
		default:
			text.WriteByte(c)
			i++
		}
	}
	flush()
	return append(lines, cur)
}

// applySGR returns the foreground colour after an SGR sequence's parameters.
func applySGR(fg, params string) string {
	ps := strings.FieldsFunc(params, func(r rune) bool { return r == ';' || r == ':' })
	if params == "" {
		return ""
	}
	for i := 0; i < len(ps); i++ {
		p := strings.TrimLeft(ps[i], "0")
		switch {
		case p == "":
			fg = "" // 0: reset
		case p == "39":
			fg = ""
		case len(p) == 2 && (p[0] == '3' || p[0] == '9') && p[1] >= '0' && p[1] <= '7':
			fg = p
		case p == "38" || p == "48" || p == "58":
			n := 0
			if i+1 < len(ps) {
				switch ps[i+1] {
				case "5":
					n = 2
				case "2":
					n = 4
				}
			}
			end := min(i+1+n, len(ps))
			if p == "38" {
				fg = strings.Join(append([]string{"38"}, ps[i+1:end]...), ";")
			}
			i = end - 1
		}
	}
	return fg
}
