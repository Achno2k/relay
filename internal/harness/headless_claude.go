package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// jsonInstruction is appended to a prompt when the caller wants a JSON answer.
const jsonInstruction = `

Answer with ONLY a single JSON value that validates against this JSON schema.
No prose, no explanation, no markdown code fences.

JSON schema:
%s`

// withSchema appends the JSON-only instruction when schema is non-empty.
func withSchema(prompt, schema string) string {
	schema = strings.TrimSpace(schema)
	if schema == "" {
		return prompt
	}
	return prompt + fmt.Sprintf(jsonInstruction, schema)
}

// stripFences removes a surrounding markdown code fence, with or without a
// language tag, and any prose around it. Text without a fence is returned
// trimmed.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	i := strings.Index(s, "```")
	if i < 0 {
		return s
	}
	s = s[i+3:]
	// Drop a language tag: a bare word on the rest of the opening line.
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		if tag := strings.TrimSpace(s[:nl]); tag != "" && !strings.ContainsAny(tag, " \t{}[]\"") {
			s = s[nl+1:]
		}
	}
	if j := strings.LastIndex(s, "```"); j >= 0 {
		s = s[:j]
	}
	return strings.TrimSpace(s)
}

// answer post-processes harness output: strips fences and, when a schema was
// requested, checks that what is left actually parses as JSON.
func answer(out, schema string) (string, error) {
	text := stripFences(out)
	if strings.TrimSpace(schema) == "" {
		return text, nil
	}
	if text == "" {
		return "", fmt.Errorf("harness returned no JSON")
	}
	if !json.Valid([]byte(text)) {
		return "", fmt.Errorf("harness answer is not valid JSON: %s", tail(text, 300))
	}
	return text, nil
}

// tail returns at most n trailing bytes of s.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}

// claudeResult is the shape of `claude -p --output-format json`.
type claudeResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Error   string `json:"error"`
}

// HeadlessJSON runs one non-interactive Claude Code prompt in dir with
// `claude -p <prompt> --output-format json` and returns the result field.
func (Claude) HeadlessJSON(ctx context.Context, dir, prompt, schema string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", withSchema(prompt, schema), "--output-format", "json")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("claude: %w: %s", err, tail(strings.TrimSpace(stderr.String()), 500))
	}
	return parseClaudeJSON(stdout.String(), schema)
}

// parseClaudeJSON pulls the result field out of claude's JSON envelope.
func parseClaudeJSON(out, schema string) (string, error) {
	var r claudeResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &r); err != nil {
		return "", fmt.Errorf("claude: cannot parse --output-format json output: %w", err)
	}
	if r.IsError {
		msg := r.Error
		if msg == "" {
			msg = r.Result
		}
		return "", fmt.Errorf("claude: %s", strings.TrimSpace(msg))
	}
	return answer(r.Result, schema)
}
