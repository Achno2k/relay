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
	"io"
)

// Step is one unit of work shown as "[n/N] <Name>" with a live status glyph.
type Step struct {
	Name string
	Run  func(ctx context.Context, log io.Writer) error
}

// RunSteps renders the numbered checklist and executes steps in order.
// On failure it prints the tail of that step's log and returns the error.
// Steps after the failed one are shown as skipped.
func RunSteps(ctx context.Context, title string, steps []Step) error { panic("TODO ui") }

// Check renders a verification list: "✓ Go" / "✗ Redis (not on PATH)".
type Check struct {
	Name string
	Run  func(ctx context.Context) error
}

func RunChecks(ctx context.Context, title string, checks []Check) (failed int, err error) {
	panic("TODO ui")
}

// Select shows a single-choice picker. Returns the chosen index.
func Select(title string, options []string) (int, error) { panic("TODO ui") }

// MultiSelect shows a multi-choice picker. Returns chosen indices.
func MultiSelect(title string, options []string, preselected []int) ([]int, error) {
	panic("TODO ui")
}

// Input asks for a line of text. Empty placeholder means none.
func Input(title, placeholder string) (string, error) { panic("TODO ui") }

// Confirm asks yes/no.
func Confirm(title string, def bool) (bool, error) { panic("TODO ui") }

// Spinner runs fn while showing "<label> ●" animation. Returns fn's error.
func Spinner(ctx context.Context, label string, fn func(ctx context.Context) error) error {
	panic("TODO ui")
}

// Plain text helpers. All write to stdout unless noted.
func Title(s string)     { panic("TODO ui") } // section heading, accent
func Info(s string)      { panic("TODO ui") }
func Success(s string)   { panic("TODO ui") } // "✓ s"
func Warn(s string)      { panic("TODO ui") } // "! s" amber
func Fail(s string)      { panic("TODO ui") } // "✗ s" red, stderr
func Muted(s string)     { panic("TODO ui") } // dim
func KV(pairs ...string) { panic("TODO ui") } // aligned key/value rows
func Code(s string)      { panic("TODO ui") } // command the user can copy
