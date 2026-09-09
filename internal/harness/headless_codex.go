package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// codexEvent covers the JSONL events `codex exec --json` prints. The shape has
// moved between versions, so both the thread/item events and the older msg
// envelope are decoded.
type codexEvent struct {
	Type string `json:"type"`
	Item *struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Message string `json:"message"`
	} `json:"item"`
	Msg *struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Message string `json:"message"`
	} `json:"msg"`
	Message string `json:"message"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// HeadlessJSON runs one non-interactive Codex prompt in dir with
// `codex exec --json <prompt>` and returns the last agent message.
func (Codex) HeadlessJSON(ctx context.Context, dir, prompt, schema string) (string, error) {
	cmd := exec.CommandContext(ctx, "codex", "exec", "--json", "--skip-git-repo-check", withSchema(prompt, schema))
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	out, err := lastAgentMessage(stdout.String())
	if err != nil {
		if runErr != nil {
			return "", fmt.Errorf("codex: %w: %s", runErr, tail(strings.TrimSpace(stderr.String()), 500))
		}
		return "", err
	}
	return answer(out, schema)
}

// lastAgentMessage scans codex JSONL events and returns the final agent
// message. Non-JSON log lines are ignored.
func lastAgentMessage(out string) (string, error) {
	var last, lastErr string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev codexEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if t, text := agentText(ev); t {
			last = text
		}
		switch ev.Type {
		case "error", "turn.failed":
			if ev.Error != nil && ev.Error.Message != "" {
				lastErr = ev.Error.Message
			} else if ev.Message != "" {
				lastErr = ev.Message
			}
		}
	}
	if strings.TrimSpace(last) == "" {
		if lastErr != "" {
			return "", fmt.Errorf("codex: %s", strings.TrimSpace(lastErr))
		}
		return "", fmt.Errorf("codex: no agent message in output")
	}
	return last, nil
}

// agentText reports whether ev carries an agent message and returns its text.
func agentText(ev codexEvent) (bool, string) {
	isAgent := func(t string) bool {
		return t == "agent_message" || t == "assistant_message"
	}
	if ev.Item != nil && isAgent(ev.Item.Type) {
		if ev.Item.Text != "" {
			return true, ev.Item.Text
		}
		return true, ev.Item.Message
	}
	if ev.Msg != nil && isAgent(ev.Msg.Type) {
		if ev.Msg.Message != "" {
			return true, ev.Msg.Message
		}
		return true, ev.Msg.Text
	}
	return false, ""
}
