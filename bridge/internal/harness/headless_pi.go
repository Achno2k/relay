package harness

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// HeadlessJSON runs one non-interactive pi prompt in dir with
// `pi --print --no-session <prompt>`, which writes only the final assistant
// text to stdout.
func (Pi) HeadlessJSON(ctx context.Context, dir, prompt, schema string) (string, error) {
	cmd := exec.CommandContext(ctx, "pi", "--print", "--no-session", "--", withSchema(prompt, schema))
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pi: %w: %s", err, tail(strings.TrimSpace(stderr.String()), 500))
	}
	return answer(stdout.String(), schema)
}
