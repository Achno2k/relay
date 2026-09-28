package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"path/filepath"
	"time"

	"relay/internal/api"
	"relay/internal/config"
	"relay/internal/controls/agentcli"
)

// defaultProbeDir is `<relay home>/usage-probe`: a dedicated cwd so `claude -p /usage` never shows
// up among the user's real project sessions. `--no-session-persistence` keeps it from writing a
// transcript at all.
func defaultProbeDir() string { return filepath.Join(config.Home(), "usage-probe") }

// LiveCodex spawns `codex app-server`, asks `account/rateLimits/read` once over JSON-RPC/stdio and
// returns the raw response line, or nil. The child is always reaped: SIGTERM after 6 s, SIGKILL
// 2 s later.
func LiveCodex(ctx context.Context) []byte {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmd, ok := agentcli.Command(ctx, "", nil, "codex", "app-server")
	if !ok {
		return nil
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil
	}
	if cmd.Start() != nil {
		return nil
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()
	send := func(v any) {
		b, err := json.Marshal(v)
		if err == nil {
			_, _ = stdin.Write(append(b, '\n'))
		}
	}
	send(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{"clientInfo": map[string]any{"name": "relay-bridge", "version": api.Version}}})
	send(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "account/rateLimits/read", "params": map[string]any{}})

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var o map[string]any
		if json.Unmarshal(line, &o) != nil {
			continue
		}
		if id, ok := o["id"].(float64); ok && id == 2 {
			return append([]byte(nil), line...)
		}
	}
	return nil // EOF: exited on its own, or the timeout killed it
}

// LiveClaudeUsage runs `claude -p "/usage"` (structured usage_report) from dir.
func LiveClaudeUsage(ctx context.Context, dir string) []byte {
	return agentcli.Output(ctx, []string{"claude", "-p", "/usage", "--output-format", "stream-json", "--verbose", "--no-session-persistence"},
		dir, 20*time.Second, false)
}

// LiveClaudeAuth runs `claude auth status --json` (plan name).
func LiveClaudeAuth(ctx context.Context) []byte {
	return agentcli.Output(ctx, []string{"claude", "auth", "status", "--json"}, "", 8*time.Second, false)
}

// LivePiAuth runs `pi auth check --provider <id> --json`: whether pi has a valid stored credential,
// never the credential itself.
func LivePiAuth(ctx context.Context, provider string) []byte {
	return agentcli.Output(ctx, []string{"pi", "auth", "check", "--provider", provider, "--json"}, "", 8*time.Second, false)
}

// PiReady is true only for `{"status":"ready"}`. Any failure (pi missing, timeout, non-JSON) is
// "not ready", never "ready".
func PiReady(out []byte) bool {
	var o map[string]any
	if out == nil || json.Unmarshal(out, &o) != nil {
		return false
	}
	s, _ := o["status"].(string)
	return s == "ready"
}
