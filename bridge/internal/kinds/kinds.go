// Package kinds answers GET /kinds: whether each agent CLI (claude, codex, pi) is installed and
// signed in on this machine, so the app never starts an agent that would only show a login screen.
// Status is re-derived from the CLIs' own files; the cache only spares repeated calls.
package kinds

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"relay/internal/api"
	"relay/internal/controls/agentcli"
)

// Kinds are the agent kinds GET /kinds reports, in its order.
var Kinds = []string{"claude", "codex", "pi"}

// CacheTTL bounds how stale GET /kinds may be.
const CacheTTL = 15 * time.Second

// Env is what a check reads. The zero value is filled from the real machine by New.
type Env struct {
	Home     string
	Getenv   func(string) string
	LookPath func(string) (string, bool)
	// ClaudeAuth runs `claude auth status --json`; nil output reads as signed out.
	ClaudeAuth func(ctx context.Context) []byte
	Now        func() time.Time
}

// Checker derives KindStatus values, caching the full list for CacheTTL.
type Checker struct {
	env Env

	mu   sync.Mutex
	at   time.Time
	last []api.KindStatus
}

// New returns a Checker for this machine; env fields left zero use the real ones.
func New(env Env) *Checker {
	if env.Home == "" {
		env.Home, _ = os.UserHomeDir()
	}
	if env.Getenv == nil {
		env.Getenv = os.Getenv
	}
	if env.LookPath == nil {
		env.LookPath = agentcli.LookPath
	}
	if env.ClaudeAuth == nil {
		env.ClaudeAuth = func(ctx context.Context) []byte {
			return agentcli.Output(ctx, []string{"claude", "auth", "status", "--json"}, "", 8*time.Second, false)
		}
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	return &Checker{env: env}
}

// All is GET /kinds, cached for at most CacheTTL.
func (c *Checker) All(ctx context.Context) []api.KindStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last != nil && c.env.Now().Sub(c.at) < CacheTTL {
		return append([]api.KindStatus(nil), c.last...)
	}
	out := make([]api.KindStatus, 0, len(Kinds))
	for _, k := range Kinds {
		out = append(out, c.Check(ctx, k))
	}
	c.last, c.at = out, c.env.Now()
	return append([]api.KindStatus(nil), out...)
}

// Known reports whether kind is one GET /kinds covers.
func Known(kind string) bool {
	for _, k := range Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// Check derives one kind's status now, without the cache.
func (c *Checker) Check(ctx context.Context, kind string) api.KindStatus {
	s := api.KindStatus{Kind: kind}
	if _, ok := c.env.LookPath(kind); !ok {
		s.SignInHint = "`" + kind + "` isn't installed on this machine."
		return s
	}
	s.Installed = true
	switch kind {
	case "claude":
		s.SignedIn = c.claudeSignedIn(ctx)
	case "codex":
		s.SignedIn = c.codexSignedIn()
	case "pi":
		s.SignedIn = c.piSignedIn()
	}
	if !s.SignedIn {
		s.SignInHint = signInHint(kind)
	}
	return s
}

// Startable is the POST /agents gate: nil, or the 409 for a kind that can't start. Kinds outside
// GET /kinds are left to the rest of the create path.
func (c *Checker) Startable(ctx context.Context, kind string) error {
	if !Known(kind) {
		return nil
	}
	s := c.Check(ctx, kind)
	switch {
	case !s.Installed:
		return api.NewError(http.StatusConflict, "not_installed", s.SignInHint)
	case !s.SignedIn:
		return api.NewError(http.StatusConflict, "not_signed_in", s.SignInHint)
	}
	return nil
}

func signInHint(kind string) string {
	switch kind {
	case "claude":
		return "Run `claude auth login` on this machine, then try again."
	case "codex":
		return "Run `codex login` on this machine, then try again."
	case "pi":
		return "Run `pi` on this machine and sign in with `/login`, then try again."
	}
	return ""
}

// dir is $<env> when set, else ~/<rel>.
func (c *Checker) dir(env, rel string) string {
	if d := c.env.Getenv(env); d != "" {
		return d
	}
	return filepath.Join(c.env.Home, rel)
}

// claudeSignedIn: the credentials file Claude Code uses on Linux, else `claude auth status` for
// credentials kept elsewhere (the macOS keychain).
func (c *Checker) claudeSignedIn(ctx context.Context) bool {
	if fileExists(filepath.Join(c.dir("CLAUDE_CONFIG_DIR", ".claude"), ".credentials.json")) {
		return true
	}
	var st struct {
		LoggedIn bool `json:"loggedIn"`
	}
	out := c.env.ClaudeAuth(ctx)
	return out != nil && json.Unmarshal(out, &st) == nil && st.LoggedIn
}

// codexSignedIn: codex's auth.json, or an API key in the bridge's environment.
func (c *Checker) codexSignedIn() bool {
	if c.env.Getenv("CODEX_API_KEY") != "" || c.env.Getenv("OPENAI_API_KEY") != "" {
		return true
	}
	return fileExists(filepath.Join(c.dir("CODEX_HOME", ".codex"), "auth.json"))
}

// piSignedIn: pi's auth.json holds at least one provider.
func (c *Checker) piSignedIn() bool {
	b, err := os.ReadFile(filepath.Join(c.dir("PI_CODING_AGENT_DIR", ".pi/agent"), "auth.json"))
	if err != nil {
		return false
	}
	var providers map[string]json.RawMessage
	return json.Unmarshal(b, &providers) == nil && len(providers) > 0
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}
