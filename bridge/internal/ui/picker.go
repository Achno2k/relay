package ui

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// maxVisibleOptions is how many rows a list shows before it scrolls.
const maxVisibleOptions = 10

// pickerMode says whether a picker takes one answer or many.
type pickerMode int

const (
	pickOne pickerMode = iota
	pickMany
)

// picker is the list widget behind Select, MultiSelect and Confirm.
//
// It is written here rather than driven through huh for two reasons.
//
// The cursor row is shown by colouring its circle, and huh's theme keys the
// circle on whether a row is checked while only letting the cursor reach a
// separate prefix, so the two can never meet. In a multi-select four states
// have to be told apart and only a model that knows both facts at once can:
//
//	cursor,   checked   accent ●
//	cursor,   unchecked accent ○
//	elsewhere checked   default ●
//	elsewhere unchecked dim ○
//
// And huh's Select scrolls its viewport under a stationary cursor once a list
// passes its height, so pressing down slides the whole list instead of moving
// the circle. Here the list holds still and the cursor moves, and the window
// shifts by exactly one row only when the cursor would leave it.
type picker struct {
	mode    pickerMode
	title   string
	options []string
	marks   []string
	checked []bool

	// view holds the option indices currently shown, which is every option
	// until a filter narrows it.
	view []int

	cursor int // index into view
	offset int // index into view of the first visible row
	labelW int // widest option, so the marks line up

	filtering bool
	filter    string

	done    bool
	aborted bool
}

func newPicker(mode pickerMode, title string, options, marks []string, checked []bool) *picker {
	p := &picker{
		mode:    mode,
		title:   title,
		options: options,
		marks:   marks,
		checked: checked,
	}
	for _, o := range options {
		if w := lipgloss.Width(o); w > p.labelW {
			p.labelW = w
		}
	}
	p.refilter()
	return p
}

// filterable reports whether this picker takes a "/" filter. Only single
// choice lists do: in a multi-select the space bar toggles a row, so it cannot
// also type into a filter.
func (p *picker) filterable() bool { return p.mode == pickOne }

func (p *picker) Init() tea.Cmd { return nil }

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	if p.filtering {
		return p, p.filterKey(k)
	}

	switch k.String() {
	case "ctrl+c", "esc":
		p.aborted, p.done = true, true
		return p, tea.Quit
	case "enter":
		// An empty list has nothing to pick.
		if len(p.view) == 0 {
			return p, nil
		}
		p.done = true
		return p, tea.Quit
	case "up", "k", "ctrl+p":
		p.move(-1)
	case "down", "j", "ctrl+n":
		p.move(1)
	case "home":
		p.cursor = 0
		p.scroll()
	case "end":
		p.cursor = len(p.view) - 1
		p.scroll()
	case "/":
		if p.filterable() {
			p.filtering = true
		}
	case " ", "x":
		p.toggle()
	}
	return p, nil
}

// filterKey handles a key while the filter line is open.
func (p *picker) filterKey(k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "ctrl+c":
		p.aborted, p.done = true, true
		return tea.Quit
	case "esc":
		// Esc clears the filter and puts the whole list back.
		p.filtering, p.filter = false, ""
		p.refilter()
	case "enter":
		if len(p.view) == 0 {
			return nil
		}
		p.done = true
		return tea.Quit
	case "up", "ctrl+p":
		p.move(-1)
	case "down", "ctrl+n":
		p.move(1)
	case "backspace":
		if r := []rune(p.filter); len(r) > 0 {
			p.filter = string(r[:len(r)-1])
			p.refilter()
		}
	default:
		if r := k.Runes; len(r) > 0 && !unicode.IsControl(r[0]) {
			p.filter += string(r)
			p.refilter()
		}
	}
	return nil
}

// refilter rebuilds the visible list and keeps the cursor on something real.
func (p *picker) refilter() {
	want := strings.ToLower(strings.TrimSpace(p.filter))
	p.view = p.view[:0]
	for i, o := range p.options {
		if want == "" || strings.Contains(strings.ToLower(o), want) {
			p.view = append(p.view, i)
		}
	}
	if p.cursor >= len(p.view) {
		p.cursor = len(p.view) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	p.offset = 0
	p.scroll()
}

// toggle flips the row under the cursor. Only a multi-select has anything to
// flip; a single choice list is answered by moving the cursor.
func (p *picker) toggle() {
	if p.mode != pickMany || len(p.view) == 0 {
		return
	}
	i := p.view[p.cursor]
	p.checked[i] = !p.checked[i]
}

// move walks the cursor, stopping at the ends rather than wrapping: a list
// that jumps from bottom to top is easy to overshoot.
func (p *picker) move(by int) {
	if len(p.view) == 0 {
		return
	}
	p.cursor += by
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.view) {
		p.cursor = len(p.view) - 1
	}
	p.scroll()
}

// scroll keeps the cursor inside the window, moving the window by the least it
// can: one row, only when the cursor would otherwise step outside it.
func (p *picker) scroll() {
	h := p.visible()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+h {
		p.offset = p.cursor - h + 1
	}
	if max := len(p.view) - h; p.offset > max {
		p.offset = max
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

// visible is how many rows fit on screen.
func (p *picker) visible() int {
	if len(p.view) < maxVisibleOptions {
		return len(p.view)
	}
	return maxVisibleOptions
}

func (p *picker) View() string {
	// An empty final frame is what leaves the collapsed answer line in place
	// of the widget, the same way huh's own fields behave.
	if p.done {
		return ""
	}
	return p.render()
}

// render draws the whole widget. It is separate from View so tests can look at
// a frame without running a program.
func (p *picker) render() string {
	var b strings.Builder
	b.WriteString("  " + askTitle(p.title) + "\n")

	if p.filtering {
		b.WriteString("  " + mutedStyle().Render("/") + p.filter +
			accentStyle().Render("▏") + "\n")
	}

	if len(p.view) == 0 {
		b.WriteString("  " + mutedStyle().Render("no matches") + "\n")
	}
	h := p.visible()
	for i := p.offset; i < p.offset+h && i < len(p.view); i++ {
		b.WriteString("  " + p.row(i) + "\n")
	}
	if len(p.view) > h {
		b.WriteString("  " + mutedStyle().Render(
			strconv.Itoa(p.offset+h)+"/"+strconv.Itoa(len(p.view))) + "\n")
	}
	b.WriteString(mutedStyle().Render(p.keyHelp()))
	return b.String()
}

// keyHelp is the one dim line under the list.
func (p *picker) keyHelp() string {
	if p.filtering {
		return "type to filter • ↑ up • ↓ down • esc clear • enter confirm"
	}
	if p.mode == pickMany {
		return "space toggle • ↑ up • ↓ down • enter confirm"
	}
	return "↑ up • ↓ down • / filter • enter confirm"
}

// row is one visible line: its circle, its text, and whatever mark it carries.
// The argument indexes view, not options.
func (p *picker) row(at int) string {
	i := p.view[at]

	filled := at == p.cursor
	if p.mode == pickMany {
		filled = p.checked[i]
	}
	circle := markUnchosen
	if filled {
		circle = markChosen
	}

	style := mutedStyle()
	switch {
	case at == p.cursor:
		style = accentStyle()
	case p.mode == pickMany && p.checked[i]:
		style = plainStyle()
	}

	out := style.Render(circle) + " " + plainStyle().Render(p.options[i])
	if suffix := markSuffix(p.marks, i); suffix != "" {
		out += pad(p.labelW-lipgloss.Width(p.options[i])) + "  " + suffix
	}
	return out
}

// markSuffix is the "✓ installed" tail on an option that carries a mark: a
// green tick so it reads at a glance, and the word itself dim because it is
// context, not a choice.
func markSuffix(marks []string, i int) string {
	if i >= len(marks) || strings.TrimSpace(marks[i]) == "" {
		return ""
	}
	return okStyle().Render(markOK) + " " + mutedStyle().Render(strings.TrimSpace(marks[i]))
}

// chosen returns the checked option indices, ascending.
func (p *picker) chosen() []int {
	out := []int{}
	for i, on := range p.checked {
		if on {
			out = append(out, i)
		}
	}
	return out
}

// picked is the option the cursor is on, for a single choice list.
func (p *picker) picked() int {
	if len(p.view) == 0 {
		return -1
	}
	return p.view[p.cursor]
}

// run drives the picker on a terminal.
func (p *picker) run() error {
	// The live tree owns the terminal while a phase is running; hand it over
	// for the duration, exactly as the huh backed prompts do.
	restore := lv.suspend()
	_, err := tea.NewProgram(p, tea.WithOutput(stdoutFile())).Run()
	restore()

	if err != nil {
		return err
	}
	if p.aborted {
		return ErrCancelled
	}
	return nil
}

// runPickOne shows a single choice list and returns the chosen index.
func runPickOne(title string, options, marks []string, start int) (int, error) {
	if !interactive() {
		return pickOnePlain(title, options, marks, start)
	}
	p := newPicker(pickOne, title, options, marks, make([]bool, len(options)))
	if start > 0 && start < len(options) {
		p.cursor = start
		p.scroll()
	}
	if err := p.run(); err != nil {
		return -1, err
	}
	return p.picked(), nil
}

// runPickMany shows a multi-choice list and returns the checked indices.
func runPickMany(title string, options, marks []string, checked []bool) ([]int, error) {
	if !interactive() {
		return pickManyPlain(title, options, marks, checked)
	}
	p := newPicker(pickMany, title, options, marks, checked)
	if err := p.run(); err != nil {
		return nil, err
	}
	return p.chosen(), nil
}

// pickOnePlain is the single choice list off a terminal: numbered lines and
// one answer.
func pickOnePlain(title string, options, marks []string, start int) (int, error) {
	in := bufio.NewReader(os.Stdin)
	for {
		line(askTitle(title))
		for i, o := range options {
			line(strconv.Itoa(i+1) + ". " + o + plainMark(marks, i))
		}
		prompt := "Enter a number between 1 and " + strconv.Itoa(len(options))
		if start >= 0 && start < len(options) {
			prompt += " (default " + strconv.Itoa(start+1) + ")"
		}
		line(prompt + ":")

		text, err := in.ReadString('\n')
		trimmed := strings.TrimSpace(text)
		if trimmed == "" && start >= 0 && start < len(options) {
			return start, nil // bare enter takes the default
		}
		if err != nil && trimmed == "" {
			return -1, ErrCancelled
		}
		n, convErr := strconv.Atoi(trimmed)
		if convErr == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		line(mutedStyle().Render("Not one of the numbers above."))
		if err != nil {
			return -1, ErrCancelled
		}
	}
}

// pickManyPlain is the multi-choice list off a terminal: toggle one number at
// a time, 0 to finish. It mirrors what huh's accessible mode does.
func pickManyPlain(title string, options, marks []string, checked []bool) ([]int, error) {
	in := bufio.NewReader(os.Stdin)

	for {
		line(askTitle(title))
		for i, o := range options {
			box := " "
			if checked[i] {
				box = markOK
			}
			line(strconv.Itoa(i+1) + ". [" + box + "] " + o + plainMark(marks, i))
		}
		line("0. Confirm selection")
		line("Enter a number between 0 and " + strconv.Itoa(len(options)) + ":")

		text, err := in.ReadString('\n')
		if err != nil && strings.TrimSpace(text) == "" {
			return nil, ErrCancelled // stdin ended before anything was picked
		}
		n, convErr := strconv.Atoi(strings.TrimSpace(text))
		switch {
		case convErr != nil || n < 0 || n > len(options):
			line(mutedStyle().Render("Not one of the numbers above."))
		case n == 0:
			out := []int{}
			for i, on := range checked {
				if on {
					out = append(out, i)
				}
			}
			return out, nil
		default:
			checked[n-1] = !checked[n-1]
		}
		if err != nil {
			return nil, ErrCancelled
		}
	}
}

// plainMark is the mark suffix without styling, for the non-tty lists.
func plainMark(marks []string, i int) string {
	if m := strings.TrimSpace(markAt(marks, i)); m != "" {
		return " (" + m + ")"
	}
	return ""
}

func markAt(marks []string, i int) string {
	if i >= len(marks) {
		return ""
	}
	return marks[i]
}
