package controls

import (
	"context"
	"net/http"
	"strings"
	"time"

	"relay/internal/api"
	"relay/internal/approval"
	"relay/internal/herdr"
)

// titleMatches: "" matches any picker.
func titleMatches(p approval.Picker, title string) bool {
	return title == "" || (p.Title != nil && strings.Contains(*p.Title, title))
}

func (c *Controls) picker(ctx context.Context, a herdr.Agent) (approval.Picker, bool, error) {
	s, err := c.screen(ctx, a)
	if err != nil {
		return approval.Picker{}, false, err
	}
	p, ok := approval.ParsePicker(s)
	return p, ok, nil
}

func pickerTitle(p approval.Picker) string {
	if p.Title == nil {
		return ""
	}
	return *p.Title
}

// drivePicker waits for a picker whose title contains title, moves its cursor to the row pick
// matches one key at a time (re-reading the screen after each press), then presses key.
func (c *Controls) drivePicker(ctx context.Context, a herdr.Agent, title string, pick func(string) bool, key, what string) error {
	var p approval.Picker
	found := false
	deadline := time.Now().Add(5 * time.Second)
	for !found {
		if !time.Now().Before(deadline) {
			return timeoutError("the " + title + " picker never opened")
		}
		if err := sleep(ctx, 150*time.Millisecond); err != nil {
			return err
		}
		next, ok, err := c.picker(ctx, a)
		if err != nil {
			return err
		}
		if ok && titleMatches(next, title) {
			p, found = next, true
		}
	}
	for range 24 {
		target := -1
		for i, l := range p.Labels {
			if pick(l) {
				target = i
				break
			}
		}
		if target < 0 {
			if err := c.keys(ctx, a, "esc"); err != nil {
				return err
			}
			return api.NewError(http.StatusBadRequest, "unsupported", what+" isn't in the agent's "+title+" picker ("+strings.Join(p.Labels, ", ")+")")
		}
		cursor := p.Cursor
		if cursor < 0 {
			return timeoutError("can't see the cursor in the " + title + " picker")
		}
		if cursor == target {
			return c.keys(ctx, a, key)
		}
		dir := "up"
		if target > cursor {
			dir = "down"
		}
		if err := c.keys(ctx, a, dir); err != nil {
			return err
		}
		// Re-read until the cursor moves.
		for range 10 {
			if err := sleep(ctx, 80*time.Millisecond); err != nil {
				return err
			}
			next, ok, err := c.picker(ctx, a)
			if err != nil {
				return err
			}
			if ok && next.Cursor != cursor {
				p = next
				break
			}
		}
	}
	_ = c.keys(ctx, a, "esc")
	return timeoutError("couldn't reach " + what + " in the " + title + " picker")
}

// waitForPicker waits up to ~4.5 s for a picker whose title contains title.
func (c *Controls) waitForPicker(ctx context.Context, a herdr.Agent, title string) (approval.Picker, error) {
	for range 30 {
		p, ok, err := c.picker(ctx, a)
		if err != nil {
			return approval.Picker{}, err
		}
		if ok && titleMatches(p, title) {
			return p, nil
		}
		if err := sleep(ctx, 150*time.Millisecond); err != nil {
			return approval.Picker{}, err
		}
	}
	return approval.Picker{}, timeoutError("the " + title + " picker never opened")
}
