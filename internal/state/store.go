// Package state is the sqlite store on the box (~/.agents/state.db).
// Owner: session "core". Use modernc.org/sqlite, no cgo. Migrations are
// embedded SQL files in this package.
package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Achno2k/agents-cli/internal/config"
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

// Live reports whether the session still holds a running agent.
func (s Session) Live() bool {
	switch s.Status {
	case StatusParked, StatusDone:
		return false
	default:
		return true
	}
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

// ErrNotFound is returned by the lookup methods when no session matches.
var ErrNotFound = errors.New("session not found")

// ErrExists is returned when a session would collide on id, agent name, or
// slack thread.
var ErrExists = errors.New("session already exists")

//go:embed migrations/*.sql
var migrations embed.FS

// NewID returns a fresh 4 hex character session id.
func NewID() string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; fall back to the clock so a
		// caller never gets an empty id.
		n := time.Now().UnixNano()
		b[0], b[1] = byte(n), byte(n>>8)
	}
	return hex.EncodeToString(b[:])
}

// DefaultPath is ~/.agents/state.db (respects AGENTS_HOME).
func DefaultPath() string { return filepath.Join(config.Dir(), "state.db") }

type store struct{ db *sql.DB }

// Open opens the store at path, creating the file and applying migrations.
func Open(path string) (Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// The bot and the CLI both touch this file; serialise writes in-process.
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &store{db: db}, nil
}

// migrate applies every embedded migration not yet recorded, in name order.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("state: migrations table: %w", err)
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var seen int
		if err := db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE name = ?`, name).Scan(&seen); err != nil {
			return err
		}
		if seen > 0 {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("state: migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`, name, time.Now().Unix()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

const columns = `id, agent_name, kind, repo, branch, worktree_path, slack_channel,
	thread_ts, last_sent_ts, status, attached_by, attached_at, created_at, last_active`

func (s *store) Create(ctx context.Context, sess Session) error {
	if sess.ID == "" {
		return errors.New("state: session id is required")
	}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = time.Now()
	}
	if sess.LastActive.IsZero() {
		sess.LastActive = sess.CreatedAt
	}
	if sess.Status == "" {
		sess.Status = StatusStarting
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (`+columns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.AgentName, sess.Kind, sess.Repo, sess.Branch, sess.WorktreePath,
		sess.SlackChannel, sess.ThreadTS, sess.LastSentTS, string(sess.Status),
		sess.AttachedBy, unixPtr(sess.AttachedAt), sess.CreatedAt.Unix(), sess.LastActive.Unix())
	if err != nil && isConstraint(err) {
		return fmt.Errorf("%w: %v", ErrExists, err)
	}
	return err
}

func (s *store) Get(ctx context.Context, id string) (Session, error) {
	return s.one(ctx, `SELECT `+columns+` FROM sessions WHERE id = ?`, id)
}

func (s *store) ByThread(ctx context.Context, channel, threadTS string) (Session, error) {
	return s.one(ctx, `SELECT `+columns+` FROM sessions WHERE slack_channel = ? AND thread_ts = ?`, channel, threadTS)
}

func (s *store) ByAgent(ctx context.Context, agentName string) (Session, error) {
	return s.one(ctx, `SELECT `+columns+` FROM sessions WHERE agent_name = ?`, agentName)
}

// List returns every session, newest first.
func (s *store) List(ctx context.Context) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+columns+` FROM sessions ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		sess, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

func (s *store) Update(ctx context.Context, sess Session) error {
	if sess.LastActive.IsZero() {
		sess.LastActive = time.Now()
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET
		agent_name = ?, kind = ?, repo = ?, branch = ?, worktree_path = ?,
		slack_channel = ?, thread_ts = ?, last_sent_ts = ?, status = ?,
		attached_by = ?, attached_at = ?, last_active = ?
		WHERE id = ?`,
		sess.AgentName, sess.Kind, sess.Repo, sess.Branch, sess.WorktreePath,
		sess.SlackChannel, sess.ThreadTS, sess.LastSentTS, string(sess.Status),
		sess.AttachedBy, unixPtr(sess.AttachedAt), sess.LastActive.Unix(), sess.ID)
	if err != nil {
		if isConstraint(err) {
			return fmt.Errorf("%w: %v", ErrExists, err)
		}
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *store) Close() error { return s.db.Close() }

func (s *store) one(ctx context.Context, query string, args ...any) (Session, error) {
	sess, err := scan(s.db.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

// scanner covers both *sql.Row and *sql.Rows.
type scanner interface{ Scan(dest ...any) error }

func scan(r scanner) (Session, error) {
	var (
		s          Session
		status     string
		attachedAt sql.NullInt64
		created    int64
		lastActive int64
	)
	err := r.Scan(&s.ID, &s.AgentName, &s.Kind, &s.Repo, &s.Branch, &s.WorktreePath,
		&s.SlackChannel, &s.ThreadTS, &s.LastSentTS, &status, &s.AttachedBy,
		&attachedAt, &created, &lastActive)
	if err != nil {
		return Session{}, err
	}
	s.Status = Status(status)
	s.CreatedAt = time.Unix(created, 0)
	s.LastActive = time.Unix(lastActive, 0)
	if attachedAt.Valid {
		t := time.Unix(attachedAt.Int64, 0)
		s.AttachedAt = &t
	}
	return s, nil
}

func unixPtr(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.Unix()
}

// isConstraint spots a UNIQUE or PRIMARY KEY violation without depending on the
// driver's error type.
func isConstraint(err error) bool {
	return strings.Contains(strings.ToUpper(err.Error()), "CONSTRAINT")
}
