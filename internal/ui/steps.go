package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// logTailLines is how many lines of a failed step's log we show.
const logTailLines = 15

// glyphGap is the minimum run of spaces between the longest label and the
// status column.
const glyphGap = 2

// minGlyphCol keeps short runs from collapsing the status column onto the text.
const minGlyphCol = 32

// tickInterval drives the active-step pulse.
const tickInterval = 130 * time.Millisecond

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

// stepGlyph is the status mark. frame only matters for the active step.
func stepGlyph(state stepState, frame int) string {
	switch state {
	case stateDone:
		return okStyle().Render(glyphOK)
	case stateFailed:
		return failStyle().Render(glyphFail)
	case stateSkipped:
		return mutedStyle().Render(glyphSkipped)
	case stateActive:
		return accentStyle().Render(dotFrames[frame%len(dotFrames)])
	default:
		return mutedStyle().Render(glyphPending)
	}
}

// renderStepLine lays out one step: label on the left, glyph at col.
func renderStepLine(col, n, total int, name string, state stepState, frame int) string {
	label := stepLabel(n, total, name)
	gap := col - lipgloss.Width(label)
	if gap < 1 {
		gap = 1
	}
	style := plainStyle()
	if state == statePending || state == stateSkipped {
		style = mutedStyle()
	}
	return style.Render(label) + pad(gap) + stepGlyph(state, frame)
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
	t.lines = append(t.lines, strings.TrimRight(s, "\r"))
	if len(t.lines) > t.n {
		t.lines = t.lines[len(t.lines)-t.n:]
	}
}

// tail returns the buffered lines, including any unterminated last line.
func (t *tailWriter) tail() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := append([]string(nil), t.lines...)
	if t.part.Len() > 0 {
		out = append(out, t.part.String())
	}
	if len(out) > t.n {
		out = out[len(out)-t.n:]
	}
	return out
}

// printLogTail dumps a failed step's log indented two spaces.
func printLogTail(w io.Writer, lines []string) {
	if len(lines) == 0 {
		return
	}
	outMu.Lock()
	defer outMu.Unlock()
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw)
	for _, l := range lines {
		fmt.Fprintln(bw, "  "+mutedStyle().Render(l))
	}
	bw.Flush()
}

// runStep executes one step, turning a panic into an error so a bad step
// cannot take the whole run down mid-render.
func runStep(ctx context.Context, s Step, log io.Writer) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	if s.Run == nil {
		return nil
	}
	return s.Run(ctx, log)
}

// stepsModel is the animated (tty) renderer for RunSteps.
type stepsModel struct {
	steps  []Step
	states []stepState
	logs   []*tailWriter
	col    int
	cur    int
	frame  int
	err    error
	failed int
	ctx    context.Context
	cancel context.CancelFunc
}

type stepDoneMsg struct {
	idx int
	err error
}

type tickMsg struct{}

func (m *stepsModel) Init() tea.Cmd {
	return tea.Batch(m.start(0), tick())
}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *stepsModel) start(i int) tea.Cmd {
	m.cur = i
	m.states[i] = stateActive
	return func() tea.Msg {
		return stepDoneMsg{idx: i, err: runStep(m.ctx, m.steps[i], m.logs[i])}
	}
}

func (m *stepsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			m.cancel()
		}
		return m, nil
	case tickMsg:
		m.frame++
		return m, tick()
	case stepDoneMsg:
		if msg.err != nil {
			m.states[msg.idx] = stateFailed
			m.err = msg.err
			m.failed = msg.idx
			for i := msg.idx + 1; i < len(m.states); i++ {
				m.states[i] = stateSkipped
			}
			return m, tea.Quit
		}
		m.states[msg.idx] = stateDone
		if msg.idx+1 >= len(m.steps) {
			return m, tea.Quit
		}
		return m, m.start(msg.idx + 1)
	}
	return m, nil
}

func (m *stepsModel) View() string {
	var b strings.Builder
	total := len(m.steps)
	for i, s := range m.steps {
		b.WriteString(renderStepLine(m.col, i+1, total, s.Name, m.states[i], m.frame))
		b.WriteByte('\n')
	}
	return b.String()
}
