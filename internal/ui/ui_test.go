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

// glyphIndex is where the status mark sits on a rendered line.
func glyphIndex(t *testing.T, line string) int {
	t.Helper()
	for _, g := range []string{glyphOK, glyphFail, glyphSkipped, glyphPending} {
		if i := strings.Index(line, g); i >= 0 {
			return len([]rune(line[:i]))
		}
	}
	t.Fatalf("no status glyph in %q", line)
	return -1
}

func TestStepLabel(t *testing.T) {
	if got, want := stepLabel(3, 8, "Installing Go"), "[3/8] Installing Go"; got != want {
		t.Fatalf("stepLabel = %q, want %q", got, want)
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

func TestRenderStepLineAlignment(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	names := []string{"Git", "Installing Go 1.25", "Installing a rather long tool name here"}
	col := glyphColumn(names)
	states := []stepState{stateDone, stateFailed, statePending}

	var first = -1
	for i, name := range names {
		line := renderStepLine(col, i+1, len(names), name, states[i], 0)
		if !strings.HasPrefix(line, stepLabel(i+1, len(names), name)) {
			t.Fatalf("line %d = %q, want prefix %q", i, line, stepLabel(i+1, len(names), name))
		}
		idx := glyphIndex(t, line)
		if first == -1 {
			first = idx
		}
		if idx != first {
			t.Fatalf("glyph column drifted: line %d at %d, first at %d\n%q", i, idx, first, line)
		}
		if idx != col {
			t.Fatalf("glyph at column %d, want %d (%q)", idx, col, line)
		}
	}
}

func TestRenderStepLineNeverCollidesWithLabel(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	// col deliberately narrower than the label.
	line := renderStepLine(4, 1, 1, "a very long step name", stateDone, 0)
	if !strings.Contains(line, "step name "+glyphOK) {
		t.Fatalf("expected a single space before the glyph, got %q", line)
	}
}

func TestActiveStepAnimatesDot(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	seen := map[string]bool{}
	for f := 0; f < len(dotFrames); f++ {
		seen[stepGlyph(stateActive, f)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("active glyph does not animate: %v", seen)
	}
}

func TestRunStepsPlainOutput(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	err := RunSteps(context.Background(), "", []Step{
		{Name: "Installing Git", Run: func(context.Context, io.Writer) error { return nil }},
		{Name: "Installing Go 1.25", Run: func(context.Context, io.Writer) error { return nil }},
	})
	if err != nil {
		t.Fatalf("RunSteps: %v", err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
	a, b := glyphIndex(t, lines[0]), glyphIndex(t, lines[1])
	if a != b {
		t.Fatalf("columns differ: %d vs %d\n%s", a, b, buf.String())
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, glyphOK) {
			t.Fatalf("line %q should end in %s", l, glyphOK)
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
	if !strings.HasSuffix(lines[0], glyphOK) {
		t.Fatalf("step 1 should have passed: %q", lines[0])
	}
	if !strings.HasSuffix(lines[1], glyphFail) {
		t.Fatalf("step 2 should have failed: %q", lines[1])
	}
	if !strings.HasSuffix(lines[2], glyphSkipped) {
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
	want := glyphOK + " Go\n" + glyphFail + " Redis  not on PATH\n"
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
		glyphOK + " done",
		glyphWarn + " careful",
		glyphFail + " broken",
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
