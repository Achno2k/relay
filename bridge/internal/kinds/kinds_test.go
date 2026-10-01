package kinds

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"relay/internal/api"
)

type machine struct {
	home       string
	env        map[string]string
	installed  map[string]bool
	claudeAuth []byte
	authCalls  int
	now        time.Time
}

func newMachine(t *testing.T) *machine {
	return &machine{home: t.TempDir(), env: map[string]string{}, installed: map[string]bool{}, now: time.Unix(1_800_000_000, 0)}
}

func (m *machine) checker() *Checker {
	return New(Env{
		Home:     m.home,
		Getenv:   func(k string) string { return m.env[k] },
		LookPath: func(n string) (string, bool) { return "/x/" + n, m.installed[n] },
		ClaudeAuth: func(context.Context) []byte {
			m.authCalls++
			return m.claudeAuth
		},
		Now: func() time.Time { return m.now },
	})
}

func (m *machine) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(m.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func byKind(all []api.KindStatus) map[string]api.KindStatus {
	out := map[string]api.KindStatus{}
	for _, s := range all {
		out[s.Kind] = s
	}
	return out
}

func TestNothingInstalled(t *testing.T) {
	m := newMachine(t)
	all := m.checker().All(context.Background())
	if len(all) != 3 || all[0].Kind != "claude" || all[1].Kind != "codex" || all[2].Kind != "pi" {
		t.Fatalf("kinds %+v", all)
	}
	for _, s := range all {
		if s.Installed || s.SignedIn || s.SignInHint != "`"+s.Kind+"` isn't installed on this machine." {
			t.Errorf("%+v", s)
		}
	}
}

func TestInstalledNotSignedIn(t *testing.T) {
	m := newMachine(t)
	m.installed = map[string]bool{"claude": true, "codex": true, "pi": true}
	m.claudeAuth = []byte(`{"loggedIn":false}`)
	m.write(t, ".pi/agent/auth.json", `{}`)
	got := byKind(m.checker().All(context.Background()))
	for kind, want := range map[string]string{"claude": "claude auth login", "codex": "codex login", "pi": "/login"} {
		s := got[kind]
		if !s.Installed || s.SignedIn || !strings.Contains(s.SignInHint, want) {
			t.Errorf("%s: %+v", kind, s)
		}
	}
}

func TestSignedIn(t *testing.T) {
	m := newMachine(t)
	m.installed = map[string]bool{"claude": true, "codex": true, "pi": true}
	m.write(t, ".claude/.credentials.json", `{}`)
	m.write(t, ".codex/auth.json", `{}`)
	m.write(t, ".pi/agent/auth.json", `{"anthropic":{"type":"oauth"}}`)
	for _, s := range m.checker().All(context.Background()) {
		if !s.SignedIn || s.SignInHint != "" {
			t.Errorf("%+v", s)
		}
	}
	if m.authCalls != 0 {
		t.Errorf("claude auth status ran with a credentials file present")
	}
}

func TestOtherSignInSources(t *testing.T) {
	m := newMachine(t)
	m.installed = map[string]bool{"claude": true, "codex": true, "pi": true}
	m.claudeAuth = []byte(`{"loggedIn":true,"authMethod":"claude.ai"}`) // the macOS keychain
	m.env["OPENAI_API_KEY"] = "sk-test"
	m.env["PI_CODING_AGENT_DIR"] = filepath.Join(m.home, "pi")
	m.write(t, "pi/auth.json", `{"openai":{"type":"api_key"}}`)
	for _, s := range m.checker().All(context.Background()) {
		if !s.SignedIn {
			t.Errorf("%+v", s)
		}
	}
	m2 := newMachine(t)
	m2.installed = map[string]bool{"codex": true}
	m2.env["CODEX_HOME"] = filepath.Join(m2.home, "ch")
	m2.write(t, "ch/auth.json", `{}`)
	if s := m2.checker().Check(context.Background(), "codex"); !s.SignedIn {
		t.Errorf("CODEX_HOME: %+v", s)
	}
}

func TestCacheLastsAtMostTTL(t *testing.T) {
	m := newMachine(t)
	m.installed = map[string]bool{"codex": true}
	c := m.checker()
	if byKind(c.All(context.Background()))["codex"].SignedIn {
		t.Fatal("signed in before login")
	}
	m.write(t, ".codex/auth.json", `{}`)
	m.now = m.now.Add(CacheTTL - time.Second)
	if byKind(c.All(context.Background()))["codex"].SignedIn {
		t.Fatal("cache should still answer")
	}
	m.now = m.now.Add(time.Second)
	if !byKind(c.All(context.Background()))["codex"].SignedIn {
		t.Fatal("cache outlived its TTL")
	}
}

func TestStartableIgnoresTheCache(t *testing.T) {
	m := newMachine(t)
	m.installed = map[string]bool{"codex": true}
	c := m.checker()
	c.All(context.Background())

	var apiErr *api.Error
	if err := c.Startable(context.Background(), "codex"); !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Code != "not_signed_in" || !strings.Contains(apiErr.Message, "codex login") {
		t.Fatalf("codex: %v", err)
	}
	m.write(t, ".codex/auth.json", `{}`)
	if err := c.Startable(context.Background(), "codex"); err != nil {
		t.Fatalf("after login: %v", err)
	}
	if err := c.Startable(context.Background(), "pi"); !errors.As(err, &apiErr) || apiErr.Code != "not_installed" {
		t.Fatalf("pi: %v", err)
	}
	if err := c.Startable(context.Background(), "shell"); err != nil {
		t.Fatalf("kinds outside /kinds are not gated: %v", err)
	}
}
