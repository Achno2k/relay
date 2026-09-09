// Package herdr wraps the herdr CLI on the box. v1 shells out to `herdr ...`
// and parses JSON. Owner: session "core". Run `herdr agent`, `herdr pane`,
// and `herdr api schema` for the real shapes.
package herdr

import "context"

type AgentState string

const (
	StateIdle    AgentState = "idle"
	StateWorking AgentState = "working"
	StateBlocked AgentState = "blocked"
	StateDone    AgentState = "done"
	StateUnknown AgentState = "unknown"
)

type Agent struct {
	Name   string
	Kind   string // "claude" | "codex"
	PaneID string
	State  AgentState
	CWD    string
}

type Client interface {
	ListAgents(ctx context.Context) ([]Agent, error)
	GetAgent(ctx context.Context, name string) (Agent, error)
	// NewPane creates a shell pane in the bot's workspace/tab with cwd and
	// returns its pane id.
	NewPane(ctx context.Context, cwd string) (paneID string, err error)
	StartAgent(ctx context.Context, name, kind, paneID string, args ...string) error
	Prompt(ctx context.Context, name, text string) error
	// Wait blocks until the agent reaches one of the given states or ctx ends.
	Wait(ctx context.Context, name string, until ...AgentState) (AgentState, error)
	// Read returns recent unwrapped output, last n lines.
	Read(ctx context.Context, name string, lines int) (string, error)
	SendKeys(ctx context.Context, name string, keys ...string) error
	KillAgent(ctx context.Context, name string) error
	// Notify shows a herdr notification in the agent's pane.
	Notify(ctx context.Context, name, text string) error
}

func New() Client { panic("TODO herdr") }
