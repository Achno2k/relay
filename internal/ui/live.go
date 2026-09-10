package ui

import (
	"context"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The live renderer.
//
// Every animated construct here used to own a bubbletea program. That cannot
// express the shape the CLI wants:
//
//	[⠋] Setting up dev environment
//	    [⠋] Inferring steps
//	        [1/3] Installing Go     [✓]
//
// because the outer loaders have to keep ticking while the inner checklist
// runs, and two bubbletea programs cannot share a screen. So there is one
// program per process. It owns a tree of nodes, and every construct attaches a
// node rather than starting a program of its own. Whichever construct is
// outermost starts the program and stops it, which is also what puts the final
// frame into scrollback.
const indentWidth = 4

var indentStr = strings.Repeat(" ", indentWidth)

// node is one renderable thing in the tree. lines returns its rendered rows at
// the given animation frame, given the width it has to work with.
type node interface {
	lines(frame, width int) []string
}

// runState is the three-way state a phase or spinner can be in.
type runState int

const (
	runActive runState = iota
	runDone
	runFailed
)

// phaseNode is a titled group. Its children are indented one level.
type phaseNode struct {
	title    string
	state    runState
	children []node
}

func (p *phaseNode) lines(frame, width int) []string {
	out := []string{stateBadge(p.state, frame) + " " + p.title}
	for _, c := range p.children {
		for _, l := range c.lines(frame, width-indentWidth) {
			out = append(out, indentStr+l)
		}
	}
	return out
}

// stepsNode is one RunSteps checklist, including the live tail under whichever
// step is running.
type stepsNode struct {
	names   []string
	states  []stepState
	logs    []*tailWriter
	elapsed []time.Duration
	col     int
}

// elapsedOf is zero for steps that have not finished, so the time column only
// appears once there is something to report.
func (s *stepsNode) elapsedOf(i int) time.Duration {
	if s.elapsed == nil || i >= len(s.elapsed) {
		return 0
	}
	return s.elapsed[i]
}

func (s *stepsNode) lines(frame, width int) []string {
	var out []string
	for i, name := range s.names {
		out = append(out, renderStepLine(s.col, i+1, len(s.names), name, s.states[i], frame, s.elapsedOf(i)))
		if s.states[i] != stateActive {
			continue
		}
		for _, l := range s.logs[i].lastLines(liveTailLines) {
			out = append(out, renderLogLine(l, width))
		}
	}
	return out
}

// spinnerNode is one Spinner: the label with the loader trailing it, then a
// badge once it settles.
type spinnerNode struct {
	label string
	state runState
}

func (s *spinnerNode) lines(frame, _ int) []string {
	if s.state == runActive {
		return []string{s.label + " " + accentStyle().Render(loaderFrames[frame%len(loaderFrames)])}
	}
	return []string{stateBadge(s.state, frame) + " " + s.label}
}

// textNode is a line from one of the text helpers, captured so it renders in
// its place in the tree instead of being written straight at the terminal,
// which would tear the frame.
type textNode struct{ text string }

func (t *textNode) lines(int, int) []string { return strings.Split(t.text, "\n") }

func stateBadge(s runState, frame int) string {
	switch s {
	case runDone:
		return badgeOK()
	case runFailed:
		return badgeFail()
	default:
		return badgeActive(frame)
	}
}

// live owns the one program and the tree it draws.
type live struct {
	mu sync.Mutex

	root  []node
	stack []*phaseNode // open phases, innermost last

	prog *tea.Program
	done chan struct{}
	// owned is set once a construct has started the program, so nested
	// constructs know they are not the owner.
	owned bool
	frame int

	// cancels are the per-construct cancel funcs ctrl-c triggers.
	cancels []context.CancelFunc

	// plainDepth is the nesting level used when there is no program, so the
	// non-tty path indents to match.
	plainDepth int
}

var lv = &live{}

// begin starts the program unless one is already running. It reports whether
// the caller now owns it and must call end.
func (l *live) begin() bool {
	l.mu.Lock()
	if l.owned {
		l.mu.Unlock()
		return false
	}
	l.owned = true
	l.frame = 0
	l.root = nil
	l.stack = nil
	l.cancels = nil
	prog := tea.NewProgram(&treeModel{}, tea.WithOutput(stdoutFile()))
	l.prog, l.done = prog, make(chan struct{})
	done := l.done
	l.mu.Unlock()

	go func() {
		_, _ = prog.Run()
		close(done)
	}()
	return true
}

// end stops the program and waits for it, so the finished tree is flushed to
// the terminal before anything else prints.
func (l *live) end() {
	l.mu.Lock()
	prog, done := l.prog, l.done
	l.mu.Unlock()
	if prog == nil {
		return
	}
	prog.Quit()
	<-done

	l.mu.Lock()
	l.prog, l.done, l.owned = nil, nil, false
	l.root, l.stack, l.cancels = nil, nil, nil
	l.mu.Unlock()
}

// running reports whether a program currently owns the terminal.
func (l *live) running() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.prog != nil
}

// attach adds n under the innermost open phase, or at the top level.
func (l *live) attach(n node) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attachLocked(n)
}

func (l *live) attachLocked(n node) {
	if len(l.stack) > 0 {
		p := l.stack[len(l.stack)-1]
		p.children = append(p.children, n)
		return
	}
	l.root = append(l.root, n)
}

// push attaches a phase and makes it the parent for whatever comes next.
func (l *live) push(p *phaseNode) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.attachLocked(p)
	l.stack = append(l.stack, p)
}

// pop closes the innermost phase with its result.
func (l *live) pop(p *phaseNode, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p.state = resultState(err)
	for i := len(l.stack) - 1; i >= 0; i-- {
		if l.stack[i] == p {
			l.stack = l.stack[:i]
			return
		}
	}
}

// emit captures a line of text as a node. It reports false when no program is
// running, in which case the caller writes to the terminal itself.
func (l *live) emit(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.prog == nil {
		return false
	}
	l.attachLocked(&textNode{text: s})
	return true
}

// setStep records a step's state and is the only writer the renderer races
// with, so it takes the same lock View does.
func (l *live) setStep(n *stepsNode, i int, s stepState, elapsed time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n.states[i] = s
	n.elapsed[i] = elapsed
}

func (l *live) setSpinner(n *spinnerNode, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n.state = resultState(err)
}

// withCancel derives a context ctrl-c can cancel while the tree is on screen.
func (l *live) withCancel(ctx context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(ctx)
	l.mu.Lock()
	l.cancels = append(l.cancels, cancel)
	l.mu.Unlock()
	return c, cancel
}

// interrupt cancels every construct currently on screen.
func (l *live) interrupt() {
	l.mu.Lock()
	cancels := append([]context.CancelFunc(nil), l.cancels...)
	l.mu.Unlock()
	for _, c := range cancels {
		c()
	}
}

// indent is the prefix the non-tty path puts in front of every line.
func (l *live) indent() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Repeat(indentStr, l.plainDepth)
}

func (l *live) pushPlain() {
	l.mu.Lock()
	l.plainDepth++
	l.mu.Unlock()
}

func (l *live) popPlain() {
	l.mu.Lock()
	if l.plainDepth > 0 {
		l.plainDepth--
	}
	l.mu.Unlock()
}

// render draws the whole tree. It is the program's View.
func (l *live) render() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	width := termWidth()
	var b strings.Builder
	for _, n := range l.root {
		for _, ln := range n.lines(l.frame, width) {
			b.WriteString(ln)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func resultState(err error) runState {
	if err != nil {
		return runFailed
	}
	return runDone
}

// treeModel is the one bubbletea model. It holds no state of its own: the tree
// lives on live, which the constructs mutate from their own goroutine.
type treeModel struct{}

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(loaderInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *treeModel) Init() tea.Cmd { return tick() }

func (m *treeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			lv.interrupt()
		}
		return m, nil
	case tickMsg:
		lv.mu.Lock()
		lv.frame++
		lv.mu.Unlock()
		return m, tick()
	}
	return m, nil
}

func (m *treeModel) View() string { return lv.render() }

// suspend hands the terminal back so another full screen program, such as a
// huh picker, can use it. It returns a function that takes it back.
func (l *live) suspend() func() {
	l.mu.Lock()
	prog := l.prog
	l.mu.Unlock()
	if prog == nil {
		return func() {}
	}
	if err := prog.ReleaseTerminal(); err != nil {
		return func() {}
	}
	return func() { _ = prog.RestoreTerminal() }
}
