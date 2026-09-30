package transcript

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"relay/internal/api"
)

// Caps on ToolEdit strings; see api.md "Tool call files and edits".
const (
	EditStringLimit = 64 << 10
	EditTotalLimit  = 256 << 10
)

// fileKeys: the input key naming the file, per tool name (claude and pi).
var fileKeys = map[string]string{
	"Read": "file_path", "Write": "file_path", "Edit": "file_path", "MultiEdit": "file_path",
	"NotebookEdit": "notebook_path",
	"read":         "path", "write": "path", "edit": "path",
}

// WithFile adds `path` and `edit` to a claude or pi toolCall from its raw input.
func WithFile(b api.Block, input map[string]any, s Scrubber) api.Block {
	key, ok := fileKeys[b.Name]
	if !ok {
		return b
	}
	if p, ok := str(input[key]); ok {
		b.Path = RelativePath(p, s)
	}
	var changes []api.ToolEditChange
	change := func(e map[string]any, oldKey, newKey string) {
		o, _ := str(e[oldKey])
		n, _ := str(e[newKey])
		all, _ := boolean(e["replace_all"])
		changes = append(changes, api.ToolEditChange{Old: s.Scrub(o), New: s.Scrub(n), ReplaceAll: all})
	}
	switch b.Name {
	case "Write", "write":
		if c, ok := str(input["content"]); ok {
			b.Edit = capEdit(&api.ToolEdit{Kind: api.ToolEditWrite, Content: s.Scrub(c)})
		}
		return b
	case "Edit":
		change(input, "old_string", "new_string")
	case "MultiEdit":
		edits, _ := objects(input["edits"])
		for _, e := range edits {
			change(e, "old_string", "new_string")
		}
	case "edit":
		if edits, ok := objects(input["edits"]); ok {
			for _, e := range edits {
				change(e, "oldText", "newText")
			}
		} else if _, ok := input["oldText"]; ok {
			change(input, "oldText", "newText")
		}
	}
	if len(changes) > 0 {
		b.Edit = capEdit(&api.ToolEdit{Kind: api.ToolEditEdit, Changes: changes})
	}
	return b
}

// RelativePath is p made cwd-relative, or "" when it isn't inside the cwd. Decided before
// scrubbing: the scrubber keeps an outside path's last component, which would look relative.
func RelativePath(p string, s Scrubber) string {
	p = strings.TrimPrefix(p, "file://")
	if p == "" || strings.HasPrefix(p, "~") || strings.ContainsRune(p, 0) {
		return ""
	}
	if filepath.IsAbs(p) {
		cwd := s.Cwd()
		if cwd == "" {
			return ""
		}
		rel, err := filepath.Rel(cwd, filepath.Clean(p))
		if err != nil {
			return ""
		}
		p = rel
	}
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// codexEdit is the ToolEdit for a FileChange's `changes` map (path -> {type, unified_diff, content}).
func codexEdit(changes map[string]any, s Scrubber) *api.ToolEdit {
	paths := make([]string, 0, len(changes))
	for k := range changes {
		paths = append(paths, k)
	}
	sort.Strings(paths)
	if len(paths) == 1 {
		c, _ := obj(changes[paths[0]])
		if _, hasDiff := str(c["unified_diff"]); !hasDiff {
			if content, ok := str(c["content"]); ok {
				return capEdit(&api.ToolEdit{Kind: api.ToolEditWrite, Content: s.Scrub(content)})
			}
		}
	}
	var b strings.Builder
	for _, path := range paths {
		c, ok := obj(changes[path])
		if !ok {
			continue
		}
		name := s.Scrub(strings.TrimPrefix(path, "file://"))
		body, hasDiff := str(c["unified_diff"])
		if !hasDiff {
			content, ok := str(c["content"])
			if !ok {
				continue
			}
			body = addedDiff(content, strOr(c["type"], "") == "delete")
		}
		body = s.Scrub(body)
		if !strings.HasPrefix(body, "--- ") && !strings.HasPrefix(body, "diff ") {
			old, new := "a/"+name, "b/"+name
			switch strOr(c["type"], "") {
			case "add":
				old = "/dev/null"
			case "delete":
				new = "/dev/null"
			}
			b.WriteString("--- " + old + "\n+++ " + new + "\n")
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return nil
	}
	return capEdit(&api.ToolEdit{Kind: api.ToolEditDiff, Diff: b.String()})
}

// addedDiff is a one-hunk diff adding (or, for a delete, removing) every line of content.
func addedDiff(content string, deleted bool) string {
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	sign, hunk := "+", "@@ -0,0 +1,%d @@\n"
	if deleted {
		sign, hunk = "-", "@@ -1,%d +0,0 @@\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, hunk, len(lines))
	for _, l := range lines {
		b.WriteString(sign + l)
		if !strings.HasSuffix(l, "\n") {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// capEdit applies the per-string and total caps, emptying later strings first.
func capEdit(e *api.ToolEdit) *api.ToolEdit {
	budget := EditTotalLimit
	take := func(s *string) {
		limit := min(EditStringLimit, budget)
		if len(*s) > limit {
			*s = cutLine(*s, limit)
			e.Truncated = true
		}
		budget -= len(*s)
	}
	take(&e.Content)
	take(&e.Diff)
	for i := range e.Changes {
		take(&e.Changes[i].Old)
		take(&e.Changes[i].New)
	}
	return e
}

// cutLine cuts s to at most n bytes, at the last newline if there is one, else on a rune boundary.
func cutLine(s string, n int) string {
	if n <= 0 {
		return ""
	}
	s = s[:n]
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[:i+1]
	}
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// PlanLimit caps a plan (toolCall.plan, Approval.plan).
const PlanLimit = 256 << 10

// PlansDir is where Claude Code writes plan-mode plans. A var so tests can point it elsewhere.
var PlansDir = filepath.Join(homeDir(), ".claude", "plans")

// IsPlanFile: p is a file in PlansDir.
func IsPlanFile(p string) bool {
	return filepath.IsAbs(p) && filepath.Dir(filepath.Clean(p)) == filepath.Clean(PlansDir)
}

// ReadPlan reads a plan file, capped. ok is false when it can't be read or is empty.
func ReadPlan(p string) (string, bool) {
	f, err := os.Open(p)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, PlanLimit+1))
	if err != nil || len(bytes.TrimSpace(data)) == 0 {
		return "", false
	}
	return string(data), true
}

// FinishPlan scrubs and caps a plan for the wire.
func FinishPlan(plan string, s Scrubber) string {
	plan = s.Scrub(plan)
	if len(plan) > PlanLimit {
		plan = cutLine(plan, PlanLimit)
	}
	return plan
}

// claudePlan remembers the session's latest plan-file write, and gives ExitPlanMode its plan:
// input.plan, else the plan file as it is on disk, else the last Write's content.
func (p *Parser) claudePlan(b api.Block, input map[string]any) api.Block {
	switch b.Name {
	case "Write", "Edit", "MultiEdit":
		if f, ok := str(input["file_path"]); ok && IsPlanFile(f) {
			p.planFile = f
			p.planWrite, _ = str(input["content"])
		}
	case "ExitPlanMode":
		plan, _ := str(input["plan"])
		if strings.TrimSpace(plan) == "" && p.planFile != "" {
			var ok bool
			if plan, ok = ReadPlan(p.planFile); !ok {
				plan = p.planWrite
			}
		}
		if strings.TrimSpace(plan) != "" {
			b.Plan = FinishPlan(plan, p.scrubber)
		}
	}
	return b
}

// PendingPlan is the plan of an ExitPlanMode call that has no toolResult yet (the one an
// approval prompt is asking about), if the transcript has it.
func PendingPlan(messages []api.Message) (string, bool) {
	answered := map[string]bool{}
	for i := len(messages) - 1; i >= 0; i-- {
		blocks := messages[i].Blocks
		for j := len(blocks) - 1; j >= 0; j-- {
			b := blocks[j]
			switch {
			case b.Type == api.BlockToolResult:
				answered[b.ToolCallID] = true
			case b.Type == api.BlockToolCall && b.Name == "ExitPlanMode":
				if answered[b.ID] || b.Plan == "" {
					return "", false
				}
				return b.Plan, true
			}
		}
	}
	return "", false
}

// LatestPlan is the session's latest plan file (on disk, else its Write content), for a plan
// prompt whose ExitPlanMode isn't in the transcript yet.
func LatestPlan(data []byte, format Format, cwd string) string {
	if format != FormatClaude {
		return ""
	}
	p := NewParser(format, cwd, nil)
	p.ConsumeData(data)
	if p.planFile == "" {
		return ""
	}
	plan, ok := ReadPlan(p.planFile)
	if !ok {
		plan = p.planWrite
	}
	if strings.TrimSpace(plan) == "" {
		return ""
	}
	return FinishPlan(plan, p.scrubber)
}

// IsPlanQuestion: the approval question is Claude's plan-mode exit prompt.
func IsPlanQuestion(q string) bool {
	q = strings.ToLower(q)
	return strings.Contains(q, "plan") && (strings.Contains(q, "ready to execute") || strings.Contains(q, "proceed"))
}
