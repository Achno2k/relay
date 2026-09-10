package ui

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// maxVisibleOptions is how many rows a list shows before it scrolls.
const maxVisibleOptions = 10

// markedSelect is the multi-choice picker.
//
// It is written here rather than driven through huh because of one rule: the
// cursor row is shown by colouring its circle. huh's theme keys the circle on
// whether a row is checked and only lets the cursor reach a separate prefix,
// so the two cannot meet. Four states have to be told apart, and only a model
// that knows both facts at once can do it:
//
//	cursor,   checked   accent ●
//	cursor,   unchecked accent ○
//	elsewhere checked   default ●
//	elsewhere unchecked dim ○
type markedSelect struct {
	title   string
	options []string
	marks   []string
	checked []bool

	cursor  int
	offset  int // first visible row
	labelW  int // widest option, so the marks line up
	done    bool
	aborted bool
}

func newMarkedSelect(title string, options, marks []string, checked []bool) *markedSelect {
	m := &markedSelect{title: title, options: options, marks: marks, checked: checked}
	for _, o := range options {
		if w := lipgloss.Width(o); w > m.labelW {
			m.labelW = w
		}
	}
	return m
}

func (m *markedSelect) Init() tea.Cmd { return nil }

func (m *markedSelect) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "ctrl+c", "esc":
		m.aborted, m.done = true, true
		return m, tea.Quit
	case "enter":
		m.done = true
		return m, tea.Quit
	case "up", "k", "ctrl+p":
		m.move(-1)
	case "down", "j", "ctrl+n":
		m.move(1)
	case "home":
		m.cursor = 0
		m.scroll()
	case "end":
		m.cursor = len(m.options) - 1
		m.scroll()
	case " ", "x":
		m.checked[m.cursor] = !m.checked[m.cursor]
	}
	return m, nil
}

// move walks the cursor, stopping at the ends rather than wrapping: a list
// that jumps from bottom to top is easy to overshoot.
func (m *markedSelect) move(by int) {
	m.cursor += by
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.options) {
		m.cursor = len(m.options) - 1
	}
	m.scroll()
}

// scroll keeps the cursor inside the visible window.
func (m *markedSelect) scroll() {
	h := m.visible()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	if max := len(m.options) - h; m.offset > max {
		m.offset = max
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *markedSelect) visible() int {
	if len(m.options) < maxVisibleOptions {
		return len(m.options)
	}
	return maxVisibleOptions
}

func (m *markedSelect) View() string {
	// An empty final frame is what leaves the collapsed answer line in place
	// of the widget, the same way huh's own fields behave.
	if m.done {
		return ""
	}
	return m.render()
}

// render draws the whole widget. It is separate from View so tests can look at
// a frame without running a program.
func (m *markedSelect) render() string {
	var b strings.Builder
	b.WriteString("  " + askTitle(m.title) + "\n")

	h := m.visible()
	for i := m.offset; i < m.offset+h && i < len(m.options); i++ {
		b.WriteString("  " + m.row(i) + "\n")
	}
	if len(m.options) > h {
		b.WriteString("  " + mutedStyle().Render(
			strconv.Itoa(m.offset+h)+"/"+strconv.Itoa(len(m.options))) + "\n")
	}
	b.WriteString(mutedStyle().Render("space toggle • ↑ up • ↓ down • enter confirm"))
	return b.String()
}

// row is one option: its circle, its text, and whatever mark it carries.
func (m *markedSelect) row(i int) string {
	circle := markUnchosen
	if m.checked[i] {
		circle = markChosen
	}

	style := mutedStyle()
	switch {
	case i == m.cursor:
		style = accentStyle()
	case m.checked[i]:
		style = plainStyle()
	}

	out := style.Render(circle) + " " + plainStyle().Render(m.options[i])
	if suffix := markSuffix(m.marks, i); suffix != "" {
		out += pad(m.labelW-lipgloss.Width(m.options[i])) + "  " + suffix
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

// chosen returns the checked indices, ascending.
func (m *markedSelect) chosen() []int {
	out := []int{}
	for i, on := range m.checked {
		if on {
			out = append(out, i)
		}
	}
	return out
}

// runMarkedSelect drives the picker and returns the chosen indices.
func runMarkedSelect(title string, options, marks []string, checked []bool) ([]int, error) {
	if !interactive() {
		return markedSelectPlain(title, options, marks, checked)
	}

	m := newMarkedSelect(title, options, marks, checked)

	// The live tree owns the terminal while a phase is running; hand it over
	// for the duration, exactly as the huh backed prompts do.
	restore := lv.suspend()
	_, err := tea.NewProgram(m, tea.WithOutput(stdoutFile())).Run()
	restore()

	if err != nil {
		return nil, err
	}
	if m.aborted {
		return nil, ErrCancelled
	}
	return m.chosen(), nil
}

// markedSelectPlain is the picker off a terminal: numbered lines and a prompt,
// toggled one number at a time, 0 to finish. It mirrors what huh's accessible
// mode does for the other fields.
func markedSelectPlain(title string, options, marks []string, checked []bool) ([]int, error) {
	in := bufio.NewReader(os.Stdin)

	for {
		line(askTitle(title))
		for i, o := range options {
			box := " "
			if checked[i] {
				box = markOK
			}
			row := strconv.Itoa(i+1) + ". [" + box + "] " + o
			if m := strings.TrimSpace(markAt(marks, i)); m != "" {
				row += " (" + m + ")"
			}
			line(row)
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

func markAt(marks []string, i int) string {
	if i >= len(marks) {
		return ""
	}
	return marks[i]
}
