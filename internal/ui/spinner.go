package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type spinnerModel struct {
	label string
	frame int
	err   error
	done  bool
	fn    func(ctx context.Context) error
	ctx   context.Context
}

type spinnerDoneMsg struct{ err error }

func (m *spinnerModel) Init() tea.Cmd {
	return tea.Batch(tick(), func() tea.Msg { return spinnerDoneMsg{err: m.fn(m.ctx)} })
}

func (m *spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.done {
			return m, nil
		}
		m.frame++
		return m, tea.Tick(loaderInterval, func(time.Time) tea.Msg { return tickMsg{} })
	case spinnerDoneMsg:
		m.done, m.err = true, msg.err
		return m, tea.Quit
	}
	return m, nil
}

func (m *spinnerModel) View() string {
	if m.done {
		if m.err != nil {
			return badgeFail() + " " + m.label + "\n"
		}
		return badgeOK() + " " + m.label + "\n"
	}
	// While it runs the label leads and the loader trails, which is the shape
	// the Spinner contract documents.
	return m.label + " " + accentStyle().Render(loaderFrames[m.frame%len(loaderFrames)]) + "\n"
}
