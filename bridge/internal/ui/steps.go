package ui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// logTailLines is how many lines of a failed step's log we show.
const logTailLines = 15

// glyphGap is the minimum run of spaces between the longest label and the
// status column.
const glyphGap = 2

// minGlyphCol keeps short runs from collapsing the status column onto the text.
const minGlyphCol = 32

// liveTailLines is how many log lines show under the step that is running.
// Three is enough to see movement without burying the checklist.
const liveTailLines = 3

type stepState int

const (
	statePending stepState = iota
	stateActive
	stateDone
	stateFailed
	stateSkipped
)

// stepLabel is the left half of a step line: "[3/8] Installing Go".
func stepLabel(n, total int, name string) string {
	return fmt.Sprintf("[%d/%d] %s", n, total, name)
}

// glyphColumn is the column every status glyph in a run is drawn at. It is
// computed once from the widest label so the glyphs line up.
func glyphColumn(names []string) int {
	total := len(names)
	widest := 0
	for i, name := range names {
		if w := lipgloss.Width(stepLabel(i+1, total, name)); w > widest {
			widest = w
		}
	}
	col := widest + glyphGap
	if col < minGlyphCol {
		col = minGlyphCol
	}
	if max := termWidth() - 2; max > minGlyphCol && col > max {
		col = max
	}
	return col
}

// stepGlyph is the bracketed status badge. frame only matters while the step
// is running, where it holds the loader.
func stepGlyph(state stepState, frame int) string {
	switch state {
	case stateDone:
		return badgeOK()
	case stateFailed:
		return badgeFail()
	case stateActive:
		return badgeActive(frame)
	default:
		// Pending and skipped read the same: nothing happened here.
		return badgeEmpty()
	}
}

// timeColWidth is the column the elapsed times are right-aligned in. Five
// digits holds "1m02s", which is longer than any step should take.
const timeColWidth = 5

// renderStepLine lays out one step: label on the left, glyph at col, and the
// time it took right-aligned after the glyph. Steps under a second show no
// time, since the number would be noise.
func renderStepLine(col, n, total int, name string, state stepState, frame int, elapsed time.Duration) string {
	label := stepLabel(n, total, name)
	gap := col - lipgloss.Width(label)
	if gap < 1 {
		gap = 1
	}
	style := plainStyle()
	if state == statePending || state == stateSkipped {
		style = mutedStyle()
	}
	return style.Render(label) + pad(gap) + stepGlyph(state, frame) + renderElapsed(elapsed)
}

// renderElapsed is the dim, right-aligned time column. It is empty, column and
// all, when nothing has run long enough to be worth reporting.
func renderElapsed(d time.Duration) string {
	s := shortDuration(d)
	if s == "" {
		return ""
	}
	return "  " + pad(timeColWidth-lipgloss.Width(s)) + mutedStyle().Render(s)
}

// tailWriter keeps the last n lines written to it.
type tailWriter struct {
	mu    sync.Mutex
	n     int
	lines []string
	part  strings.Builder
}

func newTailWriter(n int) *tailWriter { return &tailWriter{n: n} }

func (t *tailWriter) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			t.push(t.part.String())
			t.part.Reset()
			continue
		}
		t.part.WriteByte(b)
	}
	return len(p), nil
}

func (t *tailWriter) push(s string) {
	t.lines = append(t.lines, lastDraw(s))
	if len(t.lines) > t.n {
		t.lines = t.lines[len(t.lines)-t.n:]
	}
}

// lastDraw keeps only what a terminal would still be showing: a carriage
// return means the writer redrew the line over itself, which is how npm, git
// and friends render progress.
func lastDraw(s string) string {
	if i := strings.LastIndex(s, "\r"); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// tail returns the whole buffer, including any unterminated last line.
func (t *tailWriter) tail() []string { return t.lastLines(t.n) }

// lastLines returns up to n buffered lines, oldest first, including the line
// currently being written. It is what the live tail renders each frame.
func (t *tailWriter) lastLines(n int) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n <= 0 {
		return nil
	}
	out := append([]string(nil), t.lines...)
	if part := lastDraw(t.part.String()); part != "" {
		out = append(out, part)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// renderLogLine is one line of live output: dim, indented two spaces, and cut
// to the terminal so it can never wrap and break the redraw.
func renderLogLine(s string, width int) string {
	max := width - 3
	if max < 8 {
		max = 8
	}
	return "  " + mutedStyle().Render(truncateWidth(sanitizeLog(s), max))
}

// sanitizeLog drops the escape sequences and control characters a build tool
// sprays at a terminal, which would otherwise corrupt the frame.
func sanitizeLog(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0x1b {
			i += escapeLen(s[i:])
			continue
		}
		if c == '\t' {
			b.WriteString("    ")
			i++
			continue
		}
		if c < 0x20 || c == 0x7f {
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return strings.TrimRight(b.String(), " ")
}

// escapeLen is the length of the escape sequence starting at s[0], or 1 when
// it is a lone ESC.
func escapeLen(s string) int {
	if len(s) < 2 {
		return 1
	}
	switch s[1] {
	case '[': // CSI: parameters, then a final byte in @ to ~
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
		return len(s)
	case ']': // OSC: runs to BEL or ST
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
		return len(s)
	default:
		return 2
	}
}

// truncateWidth cuts s to max display columns, marking the cut with an
// ellipsis. The rune count bounds the loop so wide characters cost a few
// extra passes rather than one per character.
func truncateWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		r = r[:max]
	}
	for len(r) > 0 && lipgloss.Width(string(r))+1 > max {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// printLogTail dumps a failed step's log indented two spaces. It goes through
// line so that inside a phase it lands under that phase rather than at the
// left margin.
func printLogTail(lines []string) {
	if len(lines) == 0 {
		return
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, "")
	for _, l := range lines {
		out = append(out, "  "+mutedStyle().Render(l))
	}
	line(strings.Join(out, "\n"))
}

// runStep executes one step, turning a panic into an error so a bad step
// cannot take the whole run down mid-render, and applying StepTimeout when the
// caller has set one.
func runStep(ctx context.Context, s Step, log io.Writer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	if s.Run == nil {
		return nil
	}

	timeout := StepTimeout
	if timeout <= 0 {
		return s.Run(ctx, log)
	}

	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	err = s.Run(stepCtx, log)

	// A step stopped by our own deadline reports the deadline, whatever it
	// happened to return on the way out, including nil. A step stopped because
	// the caller's context ended is not a timeout, so leave that error alone.
	if ctx.Err() == nil && stepCtx.Err() == context.DeadlineExceeded {
		return timedOut(timeout, log)
	}
	return err
}

// timedOut builds the deadline error and leaves a copy in the step's log, so
// the reason appears in the failure tail on screen rather than only in the
// error the caller gets back.
func timedOut(d time.Duration, log io.Writer) error {
	err := fmt.Errorf("timed out after %s", d)
	fmt.Fprintln(log, err.Error())
	return err
}
