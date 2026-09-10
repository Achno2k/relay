package ui

import (
	"errors"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled is returned when the user aborts a picker or prompt (ctrl-c,
// esc). Callers should treat it as "user said no" and exit quietly.
var ErrCancelled = errors.New("cancelled")

// formTheme is the huh theme: no borders, monochrome, one accent.
func formTheme() *huh.Theme {
	t := huh.ThemeBase()

	// ThemeBase draws a thick left border on the focused field. We want none,
	// so both states get the same two-space indent and nothing else.
	indent := renderer.NewStyle().PaddingLeft(2)
	t.Form.Base = renderer.NewStyle()
	t.Group.Base = renderer.NewStyle()
	t.Focused.Base = indent
	t.Blurred.Base = indent
	t.Focused.Card = indent
	t.Blurred.Card = indent

	// The question text stays in the default colour. The accent is spent on the
	// "?" alone, which askTitle has already rendered into the string.
	t.Focused.Title = plainStyle()
	t.Focused.NoteTitle = plainStyle()
	t.Focused.Description = mutedStyle()
	t.Blurred.Title = mutedStyle()
	t.Blurred.NoteTitle = mutedStyle()
	t.Blurred.Description = mutedStyle()

	t.Focused.ErrorIndicator = failStyle().SetString(" " + markFail)
	t.Focused.ErrorMessage = failStyle().SetString(" " + markFail)
	t.Blurred.ErrorIndicator = t.Focused.ErrorIndicator
	t.Blurred.ErrorMessage = t.Focused.ErrorMessage

	// Options are a plain vertical list: a filled circle on the row that is
	// chosen or highlighted, a hollow one on the rest, and the text in the
	// default colour. No cursor arrow, no brackets.
	//
	// huh reuses SelectedOption and UnselectedOption for both Select and
	// MultiSelect, and in both the "selected" style is the one we want the
	// filled circle on: the highlighted row in a Select, the checked rows in a
	// MultiSelect. So the circles live there, and the selectors and prefixes
	// are emptied out of the way.
	chosen := renderer.NewStyle().SetString(accentStyle().Render(markChosen))
	unchosen := renderer.NewStyle().SetString(mutedStyle().Render(markUnchosen))

	t.Focused.SelectSelector = renderer.NewStyle()
	t.Blurred.SelectSelector = renderer.NewStyle()
	t.Focused.SelectedPrefix = renderer.NewStyle()
	t.Focused.UnselectedPrefix = renderer.NewStyle()
	t.Blurred.SelectedPrefix = renderer.NewStyle()
	t.Blurred.UnselectedPrefix = renderer.NewStyle()

	t.Focused.Option = plainStyle()
	t.Blurred.Option = mutedStyle()
	t.Focused.SelectedOption = chosen
	t.Focused.UnselectedOption = unchosen
	t.Blurred.SelectedOption = chosen
	t.Blurred.UnselectedOption = unchosen

	// A MultiSelect has a cursor as well as check marks, and huh gives the
	// cursor row no text style of its own: the selector is the only hook. A
	// bar marks it without reintroducing an arrow.
	t.Focused.MultiSelectSelector = accentStyle().Bold(true).SetString(markCursor)
	t.Blurred.MultiSelectSelector = renderer.NewStyle().SetString(" ")

	t.Focused.NextIndicator = mutedStyle().MarginLeft(1).SetString("→")
	t.Focused.PrevIndicator = mutedStyle().MarginRight(1).SetString("←")
	t.Blurred.NextIndicator = renderer.NewStyle()
	t.Blurred.PrevIndicator = renderer.NewStyle()

	button := renderer.NewStyle().Padding(0, 2).MarginRight(1)
	t.Focused.FocusedButton = button.Foreground(colorAccent).Underline(true)
	t.Focused.BlurredButton = button.Foreground(colorMuted)
	t.Blurred.FocusedButton = t.Focused.BlurredButton
	t.Blurred.BlurredButton = t.Focused.BlurredButton

	t.Focused.TextInput.Prompt = mutedStyle().SetString("› ")
	t.Focused.TextInput.Placeholder = mutedStyle()
	t.Focused.TextInput.Cursor = accentStyle()
	t.Focused.TextInput.Text = plainStyle()
	t.Blurred.TextInput = t.Focused.TextInput

	t.Help.ShortKey = mutedStyle()
	t.Help.ShortDesc = mutedStyle()
	t.Help.ShortSeparator = mutedStyle()
	t.Help.FullKey = mutedStyle()
	t.Help.FullDesc = mutedStyle()
	t.Help.FullSeparator = mutedStyle()
	t.Help.Ellipsis = mutedStyle()

	t.FieldSeparator = lipgloss.NewStyle().SetString("\n")
	return t
}

// promptKeys trims huh's help line down to the keys that matter, and names the
// ones we actually tell people to press. A binding with no help text is left
// out of the line entirely, which is how the filter and select-all keys stay
// available without cluttering it.
func promptKeys() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()

	// A binding with no keys is disabled, and bubbles' help leaves disabled
	// bindings out of the line entirely. Blanking only the help text would
	// leave the separators behind: "↑ up •   •   • enter confirm".
	off := key.NewBinding()

	km.MultiSelect.Toggle = key.NewBinding(
		key.WithKeys(" ", "x"), key.WithHelp("space", "toggle"))
	km.MultiSelect.Submit = key.NewBinding(
		key.WithKeys("enter"), key.WithHelp("enter", "confirm"))
	// A MultiSelect list is short enough that filtering is clutter.
	km.MultiSelect.Filter = off
	km.MultiSelect.SetFilter = off
	km.MultiSelect.ClearFilter = off
	km.MultiSelect.SelectAll = off
	km.MultiSelect.Next = off
	km.MultiSelect.Prev = off

	// Select keeps its filter: an instance picker can be long.
	km.Select.Submit = key.NewBinding(
		key.WithKeys("enter"), key.WithHelp("enter", "confirm"))
	km.Select.Next = off
	km.Select.Prev = off

	return km
}

// runForm runs a one-field form, falling back to huh's numbered stdin prompts
// when we are not on a terminal.
func runForm(field huh.Field) error {
	form := huh.NewForm(huh.NewGroup(field)).
		WithTheme(formTheme()).
		WithKeyMap(promptKeys()).
		WithShowHelp(true).
		WithShowErrors(true).
		WithInput(os.Stdin).
		WithOutput(os.Stdout)
	if !interactive() {
		form = form.WithAccessible(true)
	}

	// huh drives its own bubbletea program. Hand it the terminal for the
	// duration, or the two renderers fight over the same rows.
	restore := lv.suspend()
	err := form.Run()
	restore()

	if errors.Is(err, huh.ErrUserAborted) {
		return ErrCancelled
	}
	return err
}

// askTitle is the question as huh shows it: a teal "?" and then the question
// in the default colour.
func askTitle(title string) string { return glyphAsk() + " " + title }

// answered collapses a finished prompt to one line, so the scrollback reads as
// a list of decisions rather than a graveyard of widgets. huh's own view is
// empty once the form quits, so this line lands where the widget was.
func answered(title, answer string) {
	line(glyphAsk() + " " + plainStyle().Render(title) + "  " + plainStyle().Render(answer))
}

// maskSecret is what a secret looks like in the transcript: the length is a
// useful hint that something was typed, the value never appears.
func maskSecret(v string) string {
	const maxDots = 12
	n := len([]rune(v))
	if n == 0 {
		return "(empty)"
	}
	if n > maxDots {
		n = maxDots
	}
	return strings.Repeat("•", n)
}

func indexOptions(options []string, selected map[int]bool) []huh.Option[int] {
	opts := make([]huh.Option[int], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, i).Selected(selected[i])
	}
	return opts
}
