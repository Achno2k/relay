package ui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
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

	steps := []Step{{Name: "First"}, {Name: "Second"}, {Name: "Third"}}
	logs := []*tailWriter{newTailWriter(logTailLines), newTailWriter(logTailLines), newTailWriter(logTailLines)}
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(logs[0], "done line %d\n", i)
		fmt.Fprintf(logs[1], "live line %d\n", i)
	}

	m := &stepsModel{
		steps:  steps,
		states: []stepState{stateDone, stateActive, statePending},
		logs:   logs,
		col:    glyphColumn([]string{"First", "Second", "Third"}),
		failed: -1,
	}

	out := m.View()
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
	after := strings.Split(strings.TrimRight(m.View(), "\n"), "\n")
	if len(after) != 3 {
		t.Fatalf("the tail should collapse, got %d lines:\n%s", len(after), m.View())
	}
	if strings.Contains(m.View(), "live line") {
		t.Fatalf("no log should survive the step finishing:\n%s", m.View())
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
