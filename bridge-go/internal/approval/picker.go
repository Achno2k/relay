package approval

import (
	"slices"
	"strconv"
	"strings"
)

// Picker is a numbered picker as pi and codex draw it: `› 2. GPT-5.6-Terra   Older balanced model…`.
type Picker struct {
	Title  *string // the nearest non-blank line above row 1, nil when none
	Labels []string
	Cursor int // index into Labels of the `›` row, -1 when none shows
}

var pickerRow = icu(`^\s*([›❯>▶→]\s*)?(\d{1,2})\.\s+(\S.*?)\s*$`)

// ParsePicker finds the last run of numbered rows 1…N on screen.
func ParsePicker(screen string) (Picker, bool) {
	lines := strings.Split(screen, "\n")
	var rows []row
	for i, l := range lines {
		m := pickerRow.FindStringSubmatchIndex(l)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(l[m[4]:m[5]])
		if err != nil {
			continue
		}
		rows = append(rows, row{line: i, n: n, label: CleanPickerLabel(l[m[6]:m[7]]), cursor: m[2] >= 0})
	}
	if len(rows) == 0 {
		return Picker{}, false
	}
	run := []row{rows[len(rows)-1]}
	for i := len(rows) - 2; i >= 0; i-- {
		if rows[i].n != run[len(run)-1].n-1 {
			break
		}
		run = append(run, rows[i])
	}
	if run[len(run)-1].n != 1 {
		return Picker{}, false
	}
	slices.Reverse(run)
	var title *string
	for i := run[0].line - 1; i >= 0; i-- {
		if t := trimWS(lines[i]); t != "" {
			title = &t
			break
		}
	}
	p := Picker{Title: title, Cursor: slices.IndexFunc(run, func(r row) bool { return r.cursor })}
	for _, r := range run {
		p.Labels = append(p.Labels, r.label)
	}
	return p, true
}

// CleanPickerLabel turns `GPT-5.6-Terra         Older balanced…` into `GPT-5.6-Terra`, and
// drops `(default)`/`(current)`.
func CleanPickerLabel(raw string) string {
	s, _, _ := strings.Cut(raw, "  ")
	for _, tag := range []string{"(default)", "(current)"} {
		s = strings.ReplaceAll(s, tag, "")
	}
	return trimWS(s)
}
