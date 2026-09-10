package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// badgeIndex is the column the bracketed status badge starts at.
func badgeIndex(t *testing.T, line string) int {
	t.Helper()
	i := strings.Index(line, "[")
	if i < 0 {
		t.Fatalf("no status badge in %q", line)
	}
	return len([]rune(line[:i]))
}

func TestStepLabel(t *testing.T) {
	if got, want := stepLabel(3, 8, "Installing Go"), "[3/8] Installing Go"; got != want {
		t.Fatalf("stepLabel = %q, want %q", got, want)
	}
}

// The loader is copied from cli-loaders dots_1 and has to stay byte for byte
// what the package ships, otherwise the attribution in theme.go is a lie.
func TestLoaderIsCliLoadersDots1(t *testing.T) {
	want := []string{"\u280B", "\u2819", "\u2839", "\u2838", "\u283C", "\u2834", "\u2826", "\u2827", "\u2807", "\u280F"}
	if len(loaderFrames) != len(want) {
		t.Fatalf("got %d frames, want %d", len(loaderFrames), len(want))
	}
	for i := range want {
		if loaderFrames[i] != want[i] {
			t.Fatalf("frame %d = %q, want %q", i, loaderFrames[i], want[i])
		}
	}
	if loaderInterval.Milliseconds() != 80 {
		t.Fatalf("interval is %v, want the package's 80ms", loaderInterval)
	}
}

func TestGlyphColumnAtLeastMinimum(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	if got := glyphColumn([]string{"Git", "Go"}); got != minGlyphCol {
		t.Fatalf("glyphColumn(short) = %d, want %d", got, minGlyphCol)
	}

	long := "Installing a harness with a very long descriptive name"
	want := len("[1/1] "+long) + glyphGap
	if got := glyphColumn([]string{long}); got != want {
		t.Fatalf("glyphColumn(long) = %d, want %d", got, want)
	}
}

func TestBadgesAreBracketedAndPlainOffATerminal(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	cases := map[string]string{
		badgeOK():       "[✓]",
		badgeFail():     "[☠]",
		badgeWarn():     "[!]",
		badgeEmpty():    "[ ]",
		badgeActive(0):  "[⠋]",
		badgeActive(2):  "[⠹]",
		badgeActive(10): "[⠋]", // frames wrap
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("badge = %q, want %q", got, want)
		}
	}
	if badgeWidth() != len([]rune(badgeOK())) {
		t.Fatalf("badgeWidth is %d but a badge renders %d columns", badgeWidth(), len([]rune(badgeOK())))
	}
}

func TestRenderStepLineAlignment(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	names := []string{"Git", "Installing Go 1.25", "Installing a rather long tool name here"}
	col := glyphColumn(names)
	states := []stepState{stateDone, stateFailed, statePending}

	first := -1
	for i, name := range names {
		line := renderStepLine(col, i+1, len(names), name, states[i], 0)
		if !strings.HasPrefix(line, stepLabel(i+1, len(names), name)) {
			t.Fatalf("line %d = %q, want prefix %q", i, line, stepLabel(i+1, len(names), name))
		}
		idx := badgeIndex(t, line[len(stepLabel(i+1, len(names), name)):])
		idx += len([]rune(stepLabel(i+1, len(names), name)))
		if first == -1 {
			first = idx
		}
		if idx != first {
			t.Fatalf("badge column drifted: line %d at %d, first at %d\n%q", i, idx, first, line)
		}
		if idx != col {
			t.Fatalf("badge at column %d, want %d (%q)", idx, col, line)
		}
	}
}

func TestRenderStepLineNeverCollidesWithLabel(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	// col deliberately narrower than the label.
	line := renderStepLine(4, 1, 1, "a very long step name", stateDone, 0)
	if !strings.Contains(line, "step name [✓]") {
		t.Fatalf("expected a single space before the badge, got %q", line)
	}
}

func TestStepGlyphPerState(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	cases := []struct {
		state stepState
		want  string
	}{
		{stateDone, "[✓]"},
		{stateFailed, "[☠]"},
		{statePending, "[ ]"},
		{stateSkipped, "[ ]"},
		{stateActive, "[⠋]"},
	}
	for _, tc := range cases {
		if got := stepGlyph(tc.state, 0); got != tc.want {
			t.Fatalf("state %v: got %q, want %q", tc.state, got, tc.want)
		}
	}
}

func TestActiveStepCyclesTheLoader(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	seen := map[string]bool{}
	for f := 0; f < len(loaderFrames); f++ {
		seen[stepGlyph(stateActive, f)] = true
	}
	if len(seen) != len(loaderFrames) {
		t.Fatalf("expected %d distinct frames, saw %d", len(loaderFrames), len(seen))
	}
}

// --- live tail ---

func TestTailWriterLastLines(t *testing.T) {
	w := newTailWriter(logTailLines)
	fmt.Fprintln(w, "one")
	fmt.Fprintln(w, "two")
	fmt.Fprintln(w, "three")

	if got := w.lastLines(2); strings.Join(got, "|") != "two|three" {
		t.Fatalf("got %v", got)
	}
	if got := w.lastLines(0); got != nil {
		t.Fatalf("got %v, want nothing", got)
	}

	// A line still being written counts, so the tail moves as bytes arrive.
	fmt.Fprint(w, "four in progress")
	if got := w.lastLines(1); strings.Join(got, "|") != "four in progress" {
		t.Fatalf("got %v", got)
	}
}

func TestTailWriterKeepsTheLastDrawOfARedrawnLine(t *testing.T) {
	w := newTailWriter(logTailLines)
	fmt.Fprint(w, "Receiving objects:  10%\rReceiving objects:  90%\n")
	if got := w.lastLines(1); strings.Join(got, "") != "Receiving objects:  90%" {
		t.Fatalf("a carriage return should overwrite, got %v", got)
	}

	// Same again for the partial line the live tail reads.
	fmt.Fprint(w, "step 1\rstep 2")
	if got := w.lastLines(1); strings.Join(got, "") != "step 2" {
		t.Fatalf("got %v", got)
	}
}

func TestSanitizeLog(t *testing.T) {
	cases := map[string]string{
		"plain":                      "plain",
		"\x1b[31mred\x1b[0m":         "red",
		"\x1b]0;a title\x07after":    "after",
		"a\tb":                       "a    b",
		"bell\x07and\x00nul":         "bellandnul",
		"trailing   ":                "trailing",
		"\x1b[2K\x1b[1Gprogress 40%": "progress 40%",
	}
	for in, want := range cases {
		if got := sanitizeLog(in); got != want {
			t.Fatalf("sanitizeLog(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateWidth(t *testing.T) {
	if got := truncateWidth("short", 20); got != "short" {
		t.Fatalf("got %q", got)
	}
	got := truncateWidth("abcdefghij", 5)
	if len([]rune(got)) != 5 || !strings.HasSuffix(got, "…") {
		t.Fatalf("got %q, want 5 columns ending in an ellipsis", got)
	}
	if got := truncateWidth("anything", 0); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderLogLineIsDimIndentedAndCut(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	got := renderLogLine("npm install", 100)
	if got != "  npm install" {
		t.Fatalf("got %q, want two spaces of indent", got)
	}

	long := renderLogLine(strings.Repeat("x", 200), 40)
	if !strings.HasPrefix(long, "  ") {
		t.Fatalf("got %q", long)
	}
	if w := len([]rune(long)); w > 40 {
		t.Fatalf("line is %d columns wide on a 40 column terminal: %q", w, long)
	}
}

func TestViewStreamsTheActiveStepAndCollapsesTheRest(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	logs := []*tailWriter{newTailWriter(logTailLines), newTailWriter(logTailLines), newTailWriter(logTailLines)}
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(logs[0], "done line %d\n", i)
		fmt.Fprintf(logs[1], "live line %d\n", i)
	}

	m := &stepsNode{
		names:  []string{"First", "Second", "Third"},
		states: []stepState{stateDone, stateActive, statePending},
		logs:   logs,
		col:    glyphColumn([]string{"First", "Second", "Third"}),
	}
	view := func() string { return strings.Join(m.lines(0, 100), "\n") + "\n" }

	out := view()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3+liveTailLines {
		t.Fatalf("got %d lines, want 3 steps plus %d live lines:\n%s", len(lines), liveTailLines, out)
	}
	// The finished step's log is gone; only the running one streams.
	if strings.Contains(out, "done line") {
		t.Fatalf("a finished step must collapse its log:\n%s", out)
	}
	for i, want := range []string{"  live line 3", "  live line 4", "  live line 5"} {
		if lines[2+i] != want {
			t.Fatalf("live line %d = %q, want %q", i, lines[2+i], want)
		}
	}

	// Finishing the step drops the tail without touching the checklist.
	m.states[1] = stateDone
	after := strings.Split(strings.TrimRight(view(), "\n"), "\n")
	if len(after) != 3 {
		t.Fatalf("the tail should collapse, got %d lines:\n%s", len(after), view())
	}
	if strings.Contains(view(), "live line") {
		t.Fatalf("no log should survive the step finishing:\n%s", view())
	}
}

// --- plain, non-tty rendering ---

func TestRunStepsPlainOutput(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	err := RunSteps(context.Background(), "", []Step{
		{Name: "Installing Git", Run: func(_ context.Context, log io.Writer) error {
			fmt.Fprintln(log, "this never shows on success")
			return nil
		}},
		{Name: "Installing Go 1.25", Run: func(context.Context, io.Writer) error { return nil }},
	})
	if err != nil {
		t.Fatalf("RunSteps: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
	a, b := badgeIndex(t, lines[0]), badgeIndex(t, lines[1])
	if a != b {
		t.Fatalf("columns differ: %d vs %d\n%s", a, b, buf.String())
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, "[✓]") {
			t.Fatalf("line %q should end in [✓]", l)
		}
	}
}

func TestRunStepsFailureSkipsRestAndPrintsLogTail(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	boom := errors.New("exit status 1")
	err := RunSteps(context.Background(), "", []Step{
		{Name: "First", Run: func(context.Context, io.Writer) error { return nil }},
		{Name: "Second", Run: func(_ context.Context, log io.Writer) error {
			for i := 1; i <= 40; i++ {
				fmt.Fprintf(log, "log line %d\n", i)
			}
			return boom
		}},
		{Name: "Third", Run: func(context.Context, io.Writer) error {
			t.Fatal("step after a failure must not run")
			return nil
		}},
	})
	if !errors.Is(err, boom) {
		t.Fatalf("RunSteps err = %v, want %v", err, boom)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3+1+logTailLines {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), 3+1+logTailLines, out)
	}
	if !strings.HasSuffix(lines[0], "[✓]") {
		t.Fatalf("step 1 should have passed: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "[☠]") {
		t.Fatalf("step 2 should have failed: %q", lines[1])
	}
	if !strings.HasSuffix(lines[2], "[ ]") {
		t.Fatalf("step 3 should be skipped: %q", lines[2])
	}
	if lines[3] != "" {
		t.Fatalf("expected a blank line before the log tail, got %q", lines[3])
	}

	tail := lines[4:]
	for i, l := range tail {
		want := fmt.Sprintf("  log line %d", 40-logTailLines+1+i)
		if l != want {
			t.Fatalf("log tail line %d = %q, want %q", i, l, want)
		}
	}
}

func TestRunChecksOutput(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	failed, err := RunChecks(context.Background(), "", []Check{
		{Name: "Go", Run: func(context.Context) error { return nil }},
		{Name: "Redis", Run: func(context.Context) error { return errors.New("not on PATH") }},
	})
	if err != nil {
		t.Fatalf("RunChecks: %v", err)
	}
	if failed != 1 {
		t.Fatalf("failed = %d, want 1", failed)
	}
	want := "[✓] Go\n[☠] Redis  not on PATH\n"
	if buf.String() != want {
		t.Fatalf("output = %q, want %q", buf.String(), want)
	}
}

func TestTextHelpersAreUnstyledOffATerminal(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	Info("plain")
	Success("done")
	Warn("careful")
	Fail("broken")
	Muted("quiet")
	KV("region", "us-east-1", "instance", "i-0abc")
	Code("agents attach web-3")

	want := strings.Join([]string{
		"plain",
		"[✓] done",
		"[!] careful",
		"[☠] broken",
		"quiet",
		"region    us-east-1",
		"instance  i-0abc",
		"  agents attach web-3",
		"",
	}, "\n")
	if buf.String() != want {
		t.Fatalf("output =\n%q\nwant\n%q", buf.String(), want)
	}
}

// --- environment gating ---

// NO_COLOR asks for no colour, not for no motion. Coupling the two is what
// silently removed the loader for anyone who sets it in their profile.
func TestNoColorDropsColourButNotAnimation(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("AGENTS_UI_PLAIN", "")

	t.Setenv("NO_COLOR", "1")
	if !envAllowsAnimation() {
		t.Fatal("NO_COLOR must not disable the animation")
	}
	if colorEnabled(os.Stdout) {
		t.Fatal("NO_COLOR must still disable colour")
	}

	os.Unsetenv("NO_COLOR")
	if !envAllowsAnimation() {
		t.Fatal("a plain terminal should allow animation")
	}
}

func TestEnvAllowsAnimation(t *testing.T) {
	cases := []struct {
		name  string
		term  string
		plain string
		want  bool
	}{
		{"normal terminal", "xterm-256color", "", true},
		{"screen", "screen-256color", "", true},
		{"dumb terminal", "dumb", "", false},
		{"no TERM at all", "", "", false},
		{"user asked for plain", "xterm-256color", "1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERM", tc.term)
			t.Setenv("AGENTS_UI_PLAIN", tc.plain)
			if got := envAllowsAnimation(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// --- StepTimeout ---

func TestStepTimeoutIsOffByDefault(t *testing.T) {
	if StepTimeout != 0 {
		t.Fatalf("StepTimeout defaults to %v, want 0", StepTimeout)
	}
}

func TestStepTimeoutFailsTheStepAndSkipsTheRest(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	prev := StepTimeout
	StepTimeout = 60 * time.Millisecond
	defer func() { StepTimeout = prev }()

	ran := false
	err := RunSteps(context.Background(), "", []Step{
		{Name: "Quick", Run: func(context.Context, io.Writer) error { return nil }},
		{Name: "Hangs", Run: func(ctx context.Context, log io.Writer) error {
			fmt.Fprintln(log, "still working")
			<-ctx.Done() // a well behaved step notices the deadline
			return ctx.Err()
		}},
		{Name: "Never", Run: func(context.Context, io.Writer) error {
			ran = true
			return nil
		}},
	})

	if err == nil || err.Error() != "timed out after 60ms" {
		t.Fatalf("err = %v, want \"timed out after 60ms\"", err)
	}
	if ran {
		t.Fatal("a step after the timeout must not run")
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if !strings.HasSuffix(lines[0], "[✓]") {
		t.Fatalf("step 1: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], "[☠]") {
		t.Fatalf("a timed out step should render as failed: %q", lines[1])
	}
	if !strings.HasSuffix(lines[2], "[ ]") {
		t.Fatalf("step 3 should be skipped: %q", lines[2])
	}
	if !strings.Contains(buf.String(), "  timed out after 60ms") {
		t.Fatalf("the reason should reach the failure tail:\n%s", buf.String())
	}
}

// A step that ignores its context and returns success after the deadline is
// still a timeout: we promised the caller a bound, so we report the bound.
func TestStepTimeoutBeatsALateSuccess(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	prev := StepTimeout
	StepTimeout = 40 * time.Millisecond
	defer func() { StepTimeout = prev }()

	err := runStep(context.Background(), Step{
		Name: "Stubborn",
		Run: func(context.Context, io.Writer) error {
			time.Sleep(120 * time.Millisecond)
			return nil
		},
	}, io.Discard)

	if err == nil || !strings.Contains(err.Error(), "timed out after 40ms") {
		t.Fatalf("err = %v, want a timeout", err)
	}
}

func TestStepTimeoutLeavesAFastStepAlone(t *testing.T) {
	prev := StepTimeout
	StepTimeout = time.Second
	defer func() { StepTimeout = prev }()

	if err := runStep(context.Background(), Step{
		Name: "Quick",
		Run:  func(context.Context, io.Writer) error { return nil },
	}, io.Discard); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

// Cancelling the caller's context is not a timeout, so the step's own error
// has to survive.
func TestCancelledParentIsNotReportedAsATimeout(t *testing.T) {
	prev := StepTimeout
	StepTimeout = 10 * time.Second
	defer func() { StepTimeout = prev }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runStep(ctx, Step{
		Name: "Interrupted",
		Run:  func(ctx context.Context, _ io.Writer) error { return ctx.Err() },
	}, io.Discard)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// --- Phase ---

func TestPhaseNestingAndIndentationInPlainMode(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	err := Phase(context.Background(), "Setting up dev environment", func(ctx context.Context) error {
		Info("checking the box")
		return Phase(ctx, "Inferring steps", func(ctx context.Context) error {
			Muted("asking the harness")
			return RunSteps(ctx, "", []Step{
				{Name: "Installing Go", Run: func(context.Context, io.Writer) error { return nil }},
				{Name: "go mod download", Run: func(context.Context, io.Writer) error { return nil }},
			})
		})
	})
	if err != nil {
		t.Fatalf("Phase: %v", err)
	}

	want := []string{
		"Setting up dev environment",
		"    checking the box",
		"    Inferring steps",
		"        asking the harness",
		"        [1/2] Installing Go             [✓]",
		"        [2/2] go mod download           [✓]",
		"    [✓] Inferring steps",
		"[✓] Setting up dev environment",
	}
	got := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), buf.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d:\n got %q\nwant %q\nfull output:\n%s", i, got[i], want[i], buf.String())
		}
	}
}

func TestPhaseIndentsFourSpacesPerLevel(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	_ = Phase(context.Background(), "one", func(ctx context.Context) error {
		Info("at one")
		return Phase(ctx, "two", func(ctx context.Context) error {
			Info("at two")
			return Phase(ctx, "three", func(context.Context) error {
				Info("at three")
				return nil
			})
		})
	})

	for depth, needle := range []string{"at one", "at two", "at three"} {
		want := strings.Repeat(" ", indentWidth*(depth+1)) + needle
		if !strings.Contains(buf.String(), want+"\n") {
			t.Fatalf("expected %q at depth %d:\n%s", want, depth+1, buf.String())
		}
	}
}

func TestPhaseFailureFlipsTheTitleAndReturnsTheError(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	boom := errors.New("no network")
	err := Phase(context.Background(), "Cloning", func(context.Context) error { return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}

	got := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	want := []string{"Cloning", "[☠] Cloning"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// An inner failure must not be swallowed, and the levels above it have to show
// they failed too.
func TestPhaseFailurePropagatesOutwards(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	boom := errors.New("exit status 1")
	err := Phase(context.Background(), "outer", func(ctx context.Context) error {
		return Phase(ctx, "inner", func(context.Context) error { return boom })
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	out := buf.String()
	if !strings.Contains(out, "    [☠] inner\n") {
		t.Fatalf("inner should be marked failed and indented:\n%s", out)
	}
	if !strings.Contains(out, "[☠] outer\n") {
		t.Fatalf("outer should be marked failed:\n%s", out)
	}
}

func TestPhaseWithNilFuncIsANoOp(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	if err := Phase(context.Background(), "nothing", nil); err != nil {
		t.Fatalf("err = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestPhaseRestoresTheIndentAfterItReturns(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	_ = Phase(context.Background(), "inside", func(context.Context) error { return nil })
	Info("back at the margin")

	if !strings.HasSuffix(buf.String(), "\nback at the margin\n") {
		t.Fatalf("indent leaked past the phase:\n%s", buf.String())
	}
}

// --- the node tree the animated renderer draws ---

func TestPhaseNodeIndentsChildrenOneLevel(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	inner := &phaseNode{title: "Inferring steps", children: []node{
		&stepsNode{
			names:  []string{"Installing Go"},
			states: []stepState{stateActive},
			logs:   []*tailWriter{newTailWriter(logTailLines)},
			col:    minGlyphCol,
		},
	}}
	outer := &phaseNode{title: "Setting up dev environment", children: []node{inner}}

	got := outer.lines(0, 100)
	want := []string{
		"[⠋] Setting up dev environment",
		"    [⠋] Inferring steps",
		"        [1/1] Installing Go             [⠋]",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("line %d:\n got %q\nwant %q", i, got[i], want[i])
		}
	}
}

func TestPhaseNodeBadgeFollowsState(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	for state, want := range map[runState]string{
		runActive: "[⠋]",
		runDone:   "[✓]",
		runFailed: "[☠]",
	} {
		p := &phaseNode{title: "t", state: state}
		if got := p.lines(0, 100)[0]; got != want+" t" {
			t.Fatalf("state %v: got %q, want %q", state, got, want+" t")
		}
	}
}

func TestSpinnerNodeAnimatesThenSettles(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	n := &spinnerNode{label: "Fetching"}
	if got := n.lines(1, 100)[0]; got != "Fetching ⠙" {
		t.Fatalf("got %q", got)
	}
	n.state = runFailed
	if got := n.lines(0, 100)[0]; got != "[☠] Fetching" {
		t.Fatalf("got %q", got)
	}
}
