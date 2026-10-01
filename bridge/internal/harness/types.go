package harness

// Claude is the Claude Code harness. Install/login methods live in claude.go
// (bootstrap session); HeadlessJSON lives in headless_claude.go (env session).
type Claude struct{}

// Codex is the OpenAI Codex harness. Same file split as Claude.
type Codex struct{}

// Pi is the pi coding agent harness. Same file split as Claude.
type Pi struct{}

func (Claude) Name() string { return "claude" }
func (Codex) Name() string  { return "codex" }
func (Pi) Name() string     { return "pi" }

func init() {
	Register(Claude{})
	Register(Codex{})
	Register(Pi{})
}
