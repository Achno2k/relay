// Package state is the sqlite store on the box (~/.agents/state.db).
// Owner: session "core". Use modernc.org/sqlite, no cgo. Migrations are
// embedded SQL files in this package.
package state

import (
	"context"
	"time"
)

type Status string

const (
	StatusStarting Status = "starting"
	StatusWorking  Status = "working"
	StatusIdle     Status = "idle"
	StatusBlocked  Status = "blocked"
	StatusParked   Status = "parked" // agent killed by TTL, worktree kept
	StatusDone     Status = "done"
)

type Session struct {
	ID           string // short id, e.g. a3f2
	AgentName    string // <repo>-<id>
	Kind         string // claude | codex
	Repo         string
	Branch       string
	WorktreePath string
	SlackChannel string
	ThreadTS     string
	LastSentTS   string // last slack message ts forwarded to the agent
	Status       Status
	AttachedBy   string // "aman@amans-mbp" or ""
	AttachedAt   *time.Time
	CreatedAt    time.Time
	LastActive   time.Time
}

type Store interface {
	Create(ctx context.Context, s Session) error
	Get(ctx context.Context, id string) (Session, error)
	ByThread(ctx context.Context, channel, threadTS string) (Session, error)
	ByAgent(ctx context.Context, agentName string) (Session, error)
	List(ctx context.Context) ([]Session, error)
	Update(ctx context.Context, s Session) error
	Delete(ctx context.Context, id string) error
	Close() error
}

func Open(path string) (Store, error) { panic("TODO state") }

// DefaultPath is ~/.agents/state.db (respects AGENTS_HOME).
func DefaultPath() string { panic("TODO state") }
