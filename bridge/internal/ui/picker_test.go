package ui

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

func newTestSelect(t *testing.T, options, marks []string, checked []bool) *picker {
	t.Helper()
	if checked == nil {
		checked = make([]bool, len(options))
	}
	return newPicker(pickMany, "Harnesses", options, marks, checked)
}

func newTestPickOne(t *testing.T, options []string) *picker {
	t.Helper()
	return newPicker(pickOne, "Region", options, nil, make([]bool, len(options)))
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

	m.toggle()
	m.move(2)
	m.toggle()

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

// --- single choice ---

// The bug this replaced: huh's Select scrolled the whole list under a
// stationary cursor. The list holds still and the circle moves until the
// cursor would leave the window, and only then does the window shift, by one
// row.
func TestSingleSelectScrollsOneRowAtTheBottomEdge(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	options := make([]string, 14) // more than maxVisibleOptions
	for i := range options {
		options[i] = "region-" + strconv.Itoa(i)
	}
	p := newTestPickOne(t, options)

	// Walking down to the last visible row must not move the window at all.
	for i := 0; i < maxVisibleOptions-1; i++ {
		p.move(1)
		if p.offset != 0 {
			t.Fatalf("the list moved early: cursor %d, offset %d", p.cursor, p.offset)
		}
	}
	if p.cursor != maxVisibleOptions-1 {
		t.Fatalf("cursor = %d, want %d", p.cursor, maxVisibleOptions-1)
	}

	// The next step past the edge shifts by exactly one row.
	for want := 1; want <= len(options)-maxVisibleOptions; want++ {
		p.move(1)
		if p.offset != want {
			t.Fatalf("step %d: offset = %d, want %d", want, p.offset, want)
		}
		// The cursor stays pinned to the last visible row.
		if p.cursor != p.offset+maxVisibleOptions-1 {
			t.Fatalf("cursor %d left the bottom edge (offset %d)", p.cursor, p.offset)
		}
	}

	// At the end the window stops rather than scrolling past.
	p.move(1)
	if want := len(options) - maxVisibleOptions; p.offset != want {
		t.Fatalf("offset = %d, want %d at the end of the list", p.offset, want)
	}
}

func TestSingleSelectScrollsOneRowAtTheTopEdge(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	options := make([]string, 14)
	for i := range options {
		options[i] = "region-" + strconv.Itoa(i)
	}
	p := newTestPickOne(t, options)

	p.cursor = len(options) - 1
	p.scroll()
	bottom := p.offset

	// Walking back up through the window must not move it.
	for i := 0; i < maxVisibleOptions-1; i++ {
		p.move(-1)
		if p.offset != bottom {
			t.Fatalf("the list moved early going up: cursor %d, offset %d", p.cursor, p.offset)
		}
	}

	// Then one row at a time, until the top.
	for want := bottom - 1; want >= 0; want-- {
		p.move(-1)
		if p.offset != want {
			t.Fatalf("offset = %d, want %d", p.offset, want)
		}
		if p.cursor != p.offset {
			t.Fatalf("cursor %d left the top edge (offset %d)", p.cursor, p.offset)
		}
	}

	p.move(-1)
	if p.offset != 0 || p.cursor != 0 {
		t.Fatalf("cursor %d offset %d, want both 0", p.cursor, p.offset)
	}
}

// Exactly one filled accent circle, on the cursor, and hollow dim elsewhere.
func TestSingleSelectMarksOnlyTheCursorRow(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	p := newTestPickOne(t, []string{"a", "b", "c"})
	p.cursor = 1

	if got, want := p.row(1), markChosen+" b"; got != want {
		t.Fatalf("cursor row = %q, want %q", got, want)
	}
	for _, at := range []int{0, 2} {
		if got, want := p.row(at), markUnchosen+" "+p.options[at]; got != want {
			t.Fatalf("row %d = %q, want %q", at, got, want)
		}
	}
}

func TestSingleSelectStylesCursorAccentAndRestDim(t *testing.T) {
	p := newTestPickOne(t, []string{"a", "b"})
	p.cursor = 0

	wantCursor := accentStyle().Render(markChosen) + " " + plainStyle().Render("a")
	if got := p.row(0); got != wantCursor {
		t.Fatalf("cursor row = %q, want %q", got, wantCursor)
	}
	wantOther := mutedStyle().Render(markUnchosen) + " " + plainStyle().Render("b")
	if got := p.row(1); got != wantOther {
		t.Fatalf("other row = %q, want %q", got, wantOther)
	}
}

func TestSingleSelectPickedFollowsTheCursor(t *testing.T) {
	p := newTestPickOne(t, []string{"a", "b", "c"})
	if got := p.picked(); got != 0 {
		t.Fatalf("picked = %d, want 0", got)
	}
	p.move(2)
	if got := p.picked(); got != 2 {
		t.Fatalf("picked = %d, want 2", got)
	}
}

// A multi-select has no filter, because space toggles a row there.
func TestOnlySingleChoiceListsFilter(t *testing.T) {
	if !newTestPickOne(t, []string{"a"}).filterable() {
		t.Fatal("a single choice list should filter")
	}
	if newTestSelect(t, []string{"a"}, nil, nil).filterable() {
		t.Fatal("a multi-select must not filter: space is the toggle")
	}
}

// --- filtering ---

func TestFilterNarrowsTheList(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	p := newTestPickOne(t, []string{"us-east-1", "us-west-2", "eu-west-1", "ap-south-1"})

	p.filter = "west"
	p.refilter()
	if len(p.view) != 2 {
		t.Fatalf("view = %v, want the two west regions", p.view)
	}
	if p.options[p.view[0]] != "us-west-2" || p.options[p.view[1]] != "eu-west-1" {
		t.Fatalf("wrong matches: %q, %q", p.options[p.view[0]], p.options[p.view[1]])
	}

	// The cursor indexes the filtered list, so picking returns the real index.
	p.cursor = 1
	if got := p.picked(); got != 2 {
		t.Fatalf("picked = %d, want 2 (eu-west-1)", got)
	}
}

func TestFilterIsCaseInsensitive(t *testing.T) {
	p := newTestPickOne(t, []string{"Claude", "codex"})
	p.filter = "CLAUD"
	p.refilter()
	if len(p.view) != 1 || p.options[p.view[0]] != "Claude" {
		t.Fatalf("view = %v", p.view)
	}
}

func TestFilterKeysTypeClearAndPick(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	p := newTestPickOne(t, []string{"us-east-1", "us-west-2", "eu-west-1"})

	// "/" opens the filter line.
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !p.filtering {
		t.Fatal("/ should open the filter")
	}

	for _, r := range "eu" {
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if p.filter != "eu" {
		t.Fatalf("filter = %q", p.filter)
	}
	if len(p.view) != 1 || p.options[p.view[0]] != "eu-west-1" {
		t.Fatalf("view = %v", p.view)
	}

	// Backspace widens it again: every region here has an "e" in it.
	p.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if p.filter != "e" || len(p.view) != 3 {
		t.Fatalf("filter = %q, view = %v", p.filter, p.view)
	}

	// Esc clears the filter and puts the whole list back.
	p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if p.filtering || p.filter != "" {
		t.Fatalf("esc should clear: filtering=%v filter=%q", p.filtering, p.filter)
	}
	if len(p.view) != 3 {
		t.Fatalf("view = %v, want the whole list back", p.view)
	}
	if p.aborted {
		t.Fatal("esc inside a filter clears it; it must not abort the picker")
	}

	// Enter picks the cursor row while filtering.
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	for _, r := range "west" {
		p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	p.Update(tea.KeyMsg{Type: tea.KeyDown})
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !p.done || p.aborted {
		t.Fatalf("enter should pick: done=%v aborted=%v", p.done, p.aborted)
	}
	if got := p.picked(); p.options[got] != "eu-west-1" {
		t.Fatalf("picked %q, want eu-west-1", p.options[got])
	}
}

func TestFilterWithNoMatchesPicksNothing(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	p := newTestPickOne(t, []string{"a", "b"})
	p.filtering = true
	p.filter = "zzz"
	p.refilter()

	if len(p.view) != 0 {
		t.Fatalf("view = %v, want empty", p.view)
	}
	if got := p.picked(); got != -1 {
		t.Fatalf("picked = %d, want -1", got)
	}
	if !strings.Contains(p.render(), "no matches") {
		t.Fatalf("render should say so:\n%s", p.render())
	}

	// Enter on an empty list does nothing rather than picking a ghost row.
	p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p.done {
		t.Fatal("enter with no matches should not finish the picker")
	}
}

// A filter that cuts the list shorter than the cursor must not leave the
// cursor pointing past the end.
func TestFilterKeepsTheCursorInRange(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	p := newTestPickOne(t, []string{"aa", "ab", "ac", "zz"})
	p.cursor = 3

	p.filter = "a"
	p.refilter()
	if p.cursor >= len(p.view) {
		t.Fatalf("cursor %d is past the end of %v", p.cursor, p.view)
	}
	if got := p.picked(); got < 0 {
		t.Fatalf("picked = %d", got)
	}
}

func TestFilteringChangesTheHelpLine(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	p := newTestPickOne(t, []string{"a", "b"})
	if !strings.Contains(p.render(), "/ filter") {
		t.Fatalf("a single choice list should advertise the filter:\n%s", p.render())
	}

	p.filtering = true
	got := p.render()
	if !strings.Contains(got, "esc clear") {
		t.Fatalf("the filter line should say how to clear it:\n%s", got)
	}
	if !strings.Contains(got, "type to filter") {
		t.Fatalf("help = %q", got)
	}
}

func TestMultiSelectHelpStillSaysSpace(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	p := newTestSelect(t, []string{"a", "b"}, nil, nil)
	got := p.render()
	if !strings.Contains(got, "space toggle") {
		t.Fatalf("help = %q", got)
	}
	if strings.Contains(got, "/ filter") {
		t.Fatalf("a multi-select should not advertise a filter:\n%s", got)
	}
}

// --- SelectOrOther ---

// pickScript replays a sequence of picker results, recording the cursor each
// call was opened with.
type pickScript struct {
	results []int
	errs    []error
	starts  []int
	n       int
}

func (p *pickScript) pick(start int) (int, error) {
	p.starts = append(p.starts, start)
	i := p.n
	p.n++
	if i >= len(p.results) {
		return -1, ErrCancelled
	}
	var err error
	if i < len(p.errs) {
		err = p.errs[i]
	}
	return p.results[i], err
}

type askScript struct {
	texts []string
	errs  []error
	n     int
}

func (a *askScript) ask() (string, error) {
	i := a.n
	a.n++
	var (
		text string
		err  error
	)
	if i < len(a.texts) {
		text = a.texts[i]
	}
	if i < len(a.errs) {
		err = a.errs[i]
	}
	return text, err
}

func TestSelectOrOtherPicksAnOption(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	options := []string{"us-east-1", "eu-west-1"}
	pick := &pickScript{results: []int{1}}
	ask := &askScript{}

	got, err := selectOrOther("Region", options, pick.pick, ask.ask)
	if err != nil || got != "eu-west-1" {
		t.Fatalf("got %q, %v", got, err)
	}
	if ask.n != 0 {
		t.Fatal("picking a listed option should not open the text field")
	}
	if buf.String() != "? Region  eu-west-1\n" {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestSelectOrOtherTakesTypedText(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	options := []string{"us-east-1"}
	pick := &pickScript{results: []int{1}} // the "other…" row
	ask := &askScript{texts: []string{"me-central-1"}}

	got, err := selectOrOther("Region", options, pick.pick, ask.ask)
	if err != nil || got != "me-central-1" {
		t.Fatalf("got %q, %v", got, err)
	}
	if buf.String() != "? Region  me-central-1\n" {
		t.Fatalf("output = %q", buf.String())
	}
	if strings.Contains(buf.String(), otherOption) {
		t.Fatalf("the answer must never be the row label: %q", buf.String())
	}
}

// Esc in the text field goes back to the list, it does not cancel the
// question, and the cursor lands back on the row that opened it.
func TestSelectOrOtherEscInTheInputGoesBack(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	options := []string{"us-east-1", "eu-west-1"}
	pick := &pickScript{results: []int{2, 0}} // other…, then the first option
	ask := &askScript{texts: []string{""}, errs: []error{ErrCancelled}}

	got, err := selectOrOther("Region", options, pick.pick, ask.ask)
	if err != nil || got != "us-east-1" {
		t.Fatalf("got %q, %v", got, err)
	}
	if pick.n != 2 {
		t.Fatalf("the list should have reopened, opened %d times", pick.n)
	}
	if want := []int{0, len(options)}; !equalInts(pick.starts, want) {
		t.Fatalf("cursor starts = %v, want %v (back on the last row)", pick.starts, want)
	}
	if buf.String() != "? Region  us-east-1\n" {
		t.Fatalf("exactly one answer line, got %q", buf.String())
	}
}

// An empty enter behaves the same as esc: back to the list.
func TestSelectOrOtherEmptyInputGoesBack(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	options := []string{"us-east-1"}
	pick := &pickScript{results: []int{1, 1}}
	ask := &askScript{texts: []string{"   ", "sa-east-1"}}

	got, err := selectOrOther("Region", options, pick.pick, ask.ask)
	if err != nil || got != "sa-east-1" {
		t.Fatalf("got %q, %v", got, err)
	}
	if pick.n != 2 || ask.n != 2 {
		t.Fatalf("picker ran %d times, input %d times, want 2 and 2", pick.n, ask.n)
	}
	if want := []int{0, 1}; !equalInts(pick.starts, want) {
		t.Fatalf("cursor starts = %v, want %v", pick.starts, want)
	}
	if buf.String() != "? Region  sa-east-1\n" {
		t.Fatalf("output = %q", buf.String())
	}
}

// Going back and forth still records exactly one answer.
func TestSelectOrOtherRecordsOneAnswerAfterSeveralTrips(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	options := []string{"a", "b"}
	pick := &pickScript{results: []int{2, 2, 2}}
	ask := &askScript{
		texts: []string{"", "", "typed"},
		errs:  []error{ErrCancelled, nil, nil},
	}

	got, err := selectOrOther("Region", options, pick.pick, ask.ask)
	if err != nil || got != "typed" {
		t.Fatalf("got %q, %v", got, err)
	}
	if n := strings.Count(buf.String(), "? Region"); n != 1 {
		t.Fatalf("got %d answer lines, want 1:\n%s", n, buf.String())
	}
}

// Esc in the list itself does cancel.
func TestSelectOrOtherEscInThePickerCancels(t *testing.T) {
	var buf bytes.Buffer
	restore := setOutput(&buf)
	defer restore()

	pick := &pickScript{results: []int{0}, errs: []error{ErrCancelled}}
	ask := &askScript{}

	got, err := selectOrOther("Region", []string{"a"}, pick.pick, ask.ask)
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("got %q, %v, want ErrCancelled", got, err)
	}
	if ask.n != 0 {
		t.Fatal("a cancelled list should not open the text field")
	}
	if buf.Len() != 0 {
		t.Fatalf("a cancelled question records nothing, got %q", buf.String())
	}
}

// A real failure from the text field is not a way back; it is an error.
func TestSelectOrOtherPropagatesAnInputError(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	boom := errors.New("tty gone")
	pick := &pickScript{results: []int{1}}
	ask := &askScript{errs: []error{boom}}

	if _, err := selectOrOther("Region", []string{"a"}, pick.pick, ask.ask); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestSelectOrOtherNeedsOptions(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	if _, err := SelectOrOther("Region", nil, ""); err == nil {
		t.Fatal("an empty list should error")
	}
}

// The list the user sees is the options plus one extra row.
func TestSelectOrOtherAddsTheOtherRow(t *testing.T) {
	restore := setOutput(&bytes.Buffer{})
	defer restore()

	options := []string{"us-east-1", "eu-west-1"}
	rows := append(append([]string(nil), options...), otherOption)
	p := newPicker(pickOne, "Region", rows, nil, make([]bool, len(rows)))

	if len(p.view) != 3 {
		t.Fatalf("view = %v, want three rows", p.view)
	}
	if got := p.options[p.view[2]]; got != otherOption {
		t.Fatalf("last row = %q, want %q", got, otherOption)
	}
	// Building the rows must not disturb the caller's slice.
	if len(options) != 2 {
		t.Fatalf("options grew to %v", options)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// esc has to reach the same "nothing was typed" branch an empty enter does.
// Routing it to huh's Quit instead would cancel with tea.Interrupt, which
// exits without a final render and leaves the abandoned field on screen.
func TestEscSubmitsKeysBindEscToSubmitNotQuit(t *testing.T) {
	km := escSubmitsKeys()

	if !hasKey(km.Input.Submit.Keys(), "esc") {
		t.Fatalf("submit keys = %v, want esc among them", km.Input.Submit.Keys())
	}
	if !hasKey(km.Input.Submit.Keys(), "enter") {
		t.Fatalf("enter must still submit: %v", km.Input.Submit.Keys())
	}
	if hasKey(km.Quit.Keys(), "esc") {
		t.Fatalf("esc must not be a quit key: %v", km.Quit.Keys())
	}

	// The default map leaves esc inert, which is what Input and Secret keep.
	if hasKey(huh.NewDefaultKeyMap().Input.Submit.Keys(), "esc") {
		t.Fatal("huh's default already submits on esc; this shim is redundant")
	}
}

func hasKey(keys []string, want string) bool {
	for _, k := range keys {
		if k == want {
			return true
		}
	}
	return false
}

// Enter on an untouched field takes the default, and the collapsed line shows
// it rather than nothing.
func TestInputDefaultShowsTheValueUsed(t *testing.T) {
	for _, c := range []struct{ typed, want string }{
		{"", "~/.ssh/demo.pem"},
		{"~/.ssh/other.pem", "~/.ssh/other.pem"},
	} {
		var buf bytes.Buffer
		restore := setOutput(&buf)
		ask := &askScript{texts: []string{c.typed}}
		got, err := inputDefault("SSH private key", "~/.ssh/demo.pem", ask.ask)
		restore()
		if err != nil || got != c.want {
			t.Fatalf("typed %q: got %q, %v", c.typed, got, err)
		}
		if buf.String() != "? SSH private key  "+c.want+"\n" {
			t.Fatalf("typed %q: output = %q", c.typed, buf.String())
		}
	}
}
