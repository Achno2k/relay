// Package ui is the single source of terminal styling for the CLI.
// Owner: session "ui". Everyone else calls these functions and never uses
// lipgloss directly. Signatures below are the contract; bodies are TODO.
//
// Look: minimal, monochrome base, one accent. No boxes or borders.
// Reference screenshot (init step runner):
//
//	[1/8] Installing Git            ✓
//	[2/8] Installing Go 1.25        ✓
//	...
//	Running verification...
//	✓ Go
//	✓ Node
//	Environment ready.
package ui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// StepTimeout bounds how long a single step in RunSteps may run. Zero, the
// default, means no limit. It is a package variable rather than an argument
// because the signatures in this file are a fixed contract; callers set it
// once before the run.
//
// When it fires, the step is marked failed and reports "timed out after <d>",
// which is also written into that step's log so it shows in the failure tail.
var StepTimeout time.Duration

// Step is one unit of work shown as "[n/N] <Name>" with a live status glyph.
type Step struct {
	Name string
	Run  func(ctx context.Context, log io.Writer) error
}

// RunSteps renders the numbered checklist and executes steps in order.
// On failure it prints the tail of that step's log and returns the error.
// Steps after the failed one are shown as skipped.
func RunSteps(ctx context.Context, title string, steps []Step) error {
	if len(steps) == 0 {
		return nil
	}
	if title != "" {
		Title(title)
	}

	names := make([]string, len(steps))
	logs := make([]*tailWriter, len(steps))
	for i, s := range steps {
		names[i] = s.Name
		logs[i] = newTailWriter(logTailLines)
	}
	col := glyphColumn(names)

	var (
		runErr error
		failed = -1
	)

	if !interactive() {
		for i, s := range steps {
			state := stateSkipped
			if runErr == nil {
				if err := runStep(ctx, s, logs[i]); err != nil {
					runErr, failed, state = err, i, stateFailed
				} else {
					state = stateDone
				}
			}
			line(renderStepLine(col, i+1, len(steps), s.Name, state, 0))
		}
	} else {
		runCtx, cancel := context.WithCancel(ctx)
		m := &stepsModel{
			steps:  steps,
			states: make([]stepState, len(steps)),
			logs:   logs,
			col:    col,
			failed: -1,
			ctx:    runCtx,
			cancel: cancel,
		}
		_, err := tea.NewProgram(m, tea.WithOutput(stdoutFile())).Run()
		cancel()
		if err != nil {
			return err
		}
		runErr, failed = m.err, m.failed
	}

	if runErr != nil && failed >= 0 {
		printLogTail(stdout, logs[failed].tail())
	}
	return runErr
}

// Check renders a verification list: "✓ Go" / "✗ Redis (not on PATH)".
type Check struct {
	Name string
	Run  func(ctx context.Context) error
}

func RunChecks(ctx context.Context, title string, checks []Check) (failed int, err error) {
	if title != "" {
		Title(title)
	}
	for _, c := range checks {
		if err := ctx.Err(); err != nil {
			return failed, err
		}
		var runErr error
		if c.Run != nil {
			runErr = c.Run(ctx)
		}
		if runErr != nil {
			failed++
			line(badgeFail() + " " + c.Name + "  " + mutedStyle().Render(runErr.Error()))
			continue
		}
		line(badgeOK() + " " + c.Name)
	}
	return failed, nil
}

// Select shows a single-choice picker. Returns the chosen index.
func Select(title string, options []string) (int, error) {
	if len(options) == 0 {
		return -1, fmt.Errorf("ui: Select %q has no options", title)
	}
	choice := 0
	field := huh.NewSelect[int]().
		Title(title).
		Options(indexOptions(options, nil)...).
		Height(pickerHeight(len(options))).
		Value(&choice)
	if err := runForm(field); err != nil {
		return -1, err
	}
	return choice, nil
}

// MultiSelect shows a multi-choice picker. Returns chosen indices.
func MultiSelect(title string, options []string, preselected []int) ([]int, error) {
	if len(options) == 0 {
		return nil, fmt.Errorf("ui: MultiSelect %q has no options", title)
	}
	sel := make(map[int]bool, len(preselected))
	for _, i := range preselected {
		if i >= 0 && i < len(options) {
			sel[i] = true
		}
	}
	chosen := []int{}
	field := huh.NewMultiSelect[int]().
		Title(title).
		Options(indexOptions(options, sel)...).
		Height(pickerHeight(len(options))).
		Value(&chosen)
	if err := runForm(field); err != nil {
		return nil, err
	}
	return chosen, nil
}

// Input asks for a line of text. Empty placeholder means none.
func Input(title, placeholder string) (string, error) {
	var v string
	field := huh.NewInput().Title(title).Value(&v)
	if placeholder != "" {
		field = field.Placeholder(placeholder)
	}
	if err := runForm(field); err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}

// Secret asks for a line of text without echoing it (tokens, passwords).
func Secret(title string) (string, error) {
	var v string
	field := huh.NewInput().Title(title).EchoMode(huh.EchoModePassword).Value(&v)
	if err := runForm(field); err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}

// Confirm asks yes/no.
func Confirm(title string, def bool) (bool, error) {
	v := def
	field := huh.NewConfirm().Title(title).Affirmative("Yes").Negative("No").Value(&v)
	if err := runForm(field); err != nil {
		return false, err
	}
	return v, nil
}

// Spinner runs fn while showing "<label> ●" animation. Returns fn's error.
func Spinner(ctx context.Context, label string, fn func(ctx context.Context) error) error {
	if fn == nil {
		return nil
	}
	if !interactive() {
		err := fn(ctx)
		if err != nil {
			line(badgeFail() + " " + label)
		} else {
			line(badgeOK() + " " + label)
		}
		return err
	}
	m := &spinnerModel{label: label, fn: fn, ctx: ctx}
	if _, err := tea.NewProgram(m, tea.WithOutput(stdoutFile())).Run(); err != nil {
		return err
	}
	return m.err
}

// Plain text helpers. All write to stdout unless noted.
func Title(s string) { line("\n" + accentStyle().Bold(true).Render(s)) } // section heading, accent
func Info(s string)  { line(plainStyle().Render(s)) }
func Success(s string) { // "[✓] s"
	line(badgeOK() + " " + s)
}
func Warn(s string) { // "[!] s" amber
	line(badgeWarn() + " " + s)
}
func Fail(s string) { // "[☠] s" red, stderr
	errLine(badgeFail() + " " + s)
}
func Muted(s string) { line(mutedStyle().Render(s)) } // dim

// KV prints aligned key/value rows. Odd trailing keys get an empty value.
func KV(pairs ...string) {
	widest := 0
	for i := 0; i < len(pairs); i += 2 {
		if w := lipgloss.Width(pairs[i]); w > widest {
			widest = w
		}
	}
	for i := 0; i < len(pairs); i += 2 {
		k := pairs[i]
		v := ""
		if i+1 < len(pairs) {
			v = pairs[i+1]
		}
		line(mutedStyle().Render(k) + pad(widest-lipgloss.Width(k)+2) + plainStyle().Render(v))
	}
}

// Code prints a command the user can copy, indented and in the accent colour.
func Code(s string) {
	for _, l := range strings.Split(s, "\n") {
		line("  " + accentStyle().Render(l))
	}
}

// pickerHeight keeps pickers compact but scrollable for long lists.
func pickerHeight(n int) int {
	const maxRows = 10
	if n > maxRows {
		n = maxRows
	}
	return n + 2 // title + padding
}
