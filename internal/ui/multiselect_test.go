package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func newTestSelect(t *testing.T, options, marks []string, checked []bool) *markedSelect {
	t.Helper()
	if checked == nil {
		checked = make([]bool, len(options))
	}
	return newMarkedSelect("Harnesses", options, marks, checked)
}

// The four states the widget exists to tell apart. Off a terminal the styles
// render as plain text, so what is asserted here is the glyph; the colours are
// covered by TestRowStylesByCursorAndChecked.
func TestRowGlyphs(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	m := newTestSelect(t, []string{"claude", "codex", "gemini"}, nil,
		[]bool{true, false, true})
	m.cursor = 1

	cases := map[int]string{
		0: markChosen + " claude",  // checked, no cursor
		1: markUnchosen + " codex", // cursor, unchecked
		2: markChosen + " gemini",  // checked, no cursor
	}
	for i, want := range cases {
		if got := m.row(i); got != want {
			t.Fatalf("row %d = %q, want %q", i, got, want)
		}
	}
}

// The cursor is shown by the colour of the circle, not by an extra glyph, so
// every row has to be the same width.
func TestRowsAreTheSameWidthWhereverTheCursorIs(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	m := newTestSelect(t, []string{"claude", "codex"}, nil, []bool{true, true})

	m.cursor = 0
	a := m.row(0)
	m.cursor = 1
	b := m.row(0)

	if a != b {
		t.Fatalf("moving the cursor changed the row text:\n%q\n%q", a, b)
	}
	if strings.Contains(a, "▌") || strings.Contains(a, ">") || strings.Contains(a, "›") {
		t.Fatalf("no cursor glyph should appear: %q", a)
	}
}

// With colour on, the circle is the only thing that changes between rows.
func TestRowStylesByCursorAndChecked(t *testing.T) {
	m := newTestSelect(t, []string{"a", "b", "c"}, nil, []bool{true, false, true})
	m.cursor = 1

	// The style each row should use for its circle.
	want := []func() lipgloss.Style{
		plainStyle,  // checked, elsewhere
		accentStyle, // cursor
		plainStyle,  // checked, elsewhere
	}
	for i, wantStyle := range want {
		glyph := markUnchosen
		if m.checked[i] {
			glyph = markChosen
		}
		expect := wantStyle().Render(glyph) + " " + plainStyle().Render(m.options[i])
		if got := m.row(i); got != expect {
			t.Fatalf("row %d = %q, want %q", i, got, expect)
		}
	}

	// An unchecked row away from the cursor is the dim case.
	m.cursor = 0
	expect := mutedStyle().Render(markUnchosen) + " " + plainStyle().Render("b")
	if got := m.row(1); got != expect {
		t.Fatalf("row 1 = %q, want %q", got, expect)
	}
}

func TestMarksRenderAndAlign(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	m := newTestSelect(t,
		[]string{"claude", "a-much-longer-harness"},
		[]string{"installed", "installed"},
		nil)

	rows := []string{m.row(0), m.row(1)}
	for i, r := range rows {
		if !strings.Contains(r, markOK+" installed") {
			t.Fatalf("row %d has no mark: %q", i, r)
		}
	}
	// The marks line up in a column, so the widest option sets the gutter.
	at := func(s string) int { return strings.Index(s, markOK) }
	if at(rows[0]) != at(rows[1]) {
		t.Fatalf("marks are not aligned:\n%q\n%q", rows[0], rows[1])
	}
}

func TestMarksAreOptionalPerRow(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	m := newTestSelect(t, []string{"claude", "codex", "gemini"},
		[]string{"installed", "", "  "}, nil)

	if !strings.Contains(m.row(0), "installed") {
		t.Fatalf("row 0 should be marked: %q", m.row(0))
	}
	for _, i := range []int{1, 2} {
		if got := m.row(i); got != markUnchosen+" "+m.options[i] {
			t.Fatalf("row %d should carry no mark, got %q", i, got)
		}
	}
}

// A short or nil marks slice must not panic; it just marks nothing.
func TestMarksShorterThanOptions(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	m := newTestSelect(t, []string{"a", "b", "c"}, []string{"installed"}, nil)
	if !strings.Contains(m.row(0), "installed") {
		t.Fatalf("row 0 = %q", m.row(0))
	}
	if got := m.row(2); got != markUnchosen+" c" {
		t.Fatalf("row 2 = %q", got)
	}

	nilMarks := newTestSelect(t, []string{"a"}, nil, nil)
	if got := nilMarks.row(0); got != markUnchosen+" a" {
		t.Fatalf("got %q", got)
	}
}

func TestCursorMovesWithTheArrowKeys(t *testing.T) {
	m := newTestSelect(t, []string{"a", "b", "c"}, nil, nil)

	m.move(1)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	m.move(1)
	m.move(1) // past the end
	if m.cursor != 2 {
		t.Fatalf("cursor should stop at the last row, got %d", m.cursor)
	}
	m.move(-5) // past the start
	if m.cursor != 0 {
		t.Fatalf("cursor should stop at the first row, got %d", m.cursor)
	}
}

func TestTogglingTracksTheCursor(t *testing.T) {
	m := newTestSelect(t, []string{"a", "b", "c"}, nil, nil)

	m.checked[m.cursor] = !m.checked[m.cursor]
	m.move(2)
	m.checked[m.cursor] = !m.checked[m.cursor]

	if got := m.chosen(); len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Fatalf("chosen = %v, want [0 2]", got)
	}

	// Toggling again clears it, and the result stays ascending.
	m.checked[0] = false
	if got := m.chosen(); len(got) != 1 || got[0] != 2 {
		t.Fatalf("chosen = %v, want [2]", got)
	}
}

func TestChosenIsEmptyNotNil(t *testing.T) {
	m := newTestSelect(t, []string{"a"}, nil, nil)
	if got := m.chosen(); got == nil || len(got) != 0 {
		t.Fatalf("chosen = %v, want an empty slice", got)
	}
}

func TestPreselectionShowsAsChecked(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	m := newTestSelect(t, []string{"a", "b"}, nil, []bool{false, true})
	m.cursor = 0
	if got := m.row(1); got != markChosen+" b" {
		t.Fatalf("a preselected row should be filled, got %q", got)
	}
}

func TestLongListsScrollWithTheCursor(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	options := make([]string, 25)
	for i := range options {
		options[i] = "option-" + string(rune('a'+i%26))
	}
	m := newTestSelect(t, options, nil, nil)

	if m.visible() != maxVisibleOptions {
		t.Fatalf("visible = %d, want %d", m.visible(), maxVisibleOptions)
	}
	for i := 0; i < len(options)-1; i++ {
		m.move(1)
		if m.cursor < m.offset || m.cursor >= m.offset+m.visible() {
			t.Fatalf("cursor %d fell outside the window [%d,%d)",
				m.cursor, m.offset, m.offset+m.visible())
		}
	}
	if max := len(options) - m.visible(); m.offset > max {
		t.Fatalf("offset %d scrolled past the end %d", m.offset, max)
	}
	for i := len(options) - 1; i > 0; i-- {
		m.move(-1)
		if m.cursor < m.offset || m.cursor >= m.offset+m.visible() {
			t.Fatalf("cursor %d fell outside the window on the way back", m.cursor)
		}
	}
	if m.offset != 0 {
		t.Fatalf("offset = %d, want 0 back at the top", m.offset)
	}
}

func TestRenderShowsTitleRowsAndKeys(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	m := newTestSelect(t, []string{"claude", "codex"}, []string{"installed", ""}, []bool{true, false})
	got := m.render()

	for _, want := range []string{
		"? Harnesses",
		markChosen + " claude",
		markUnchosen + " codex",
		"installed",
		"space toggle",
		"enter confirm",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("render is missing %q:\n%s", want, got)
		}
	}
	// Title and options are indented; the key line sits at the margin.
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], "  ?") {
		t.Fatalf("title should be indented: %q", lines[0])
	}
	if strings.HasPrefix(lines[len(lines)-1], " ") {
		t.Fatalf("the key line should not be indented: %q", lines[len(lines)-1])
	}
}

// The widget has to leave nothing behind, so the collapsed answer line takes
// its place.
func TestViewIsEmptyOnceDone(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	m := newTestSelect(t, []string{"a"}, nil, nil)
	if m.View() == "" {
		t.Fatal("a live widget should render something")
	}
	m.done = true
	if got := m.View(); got != "" {
		t.Fatalf("a finished widget should render nothing, got %q", got)
	}
}

func TestRenderCountsHiddenRows(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	options := make([]string, 20)
	for i := range options {
		options[i] = "opt"
	}
	m := newTestSelect(t, options, nil, nil)
	if !strings.Contains(m.render(), "/20") {
		t.Fatalf("a scrolling list should say how long it is:\n%s", m.render())
	}

	short := newTestSelect(t, []string{"a", "b"}, nil, nil)
	if strings.Contains(short.render(), "/2") {
		t.Fatalf("a list that fits needs no counter:\n%s", short.render())
	}
}
