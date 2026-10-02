package service

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"relay/internal/api"
	"relay/internal/herdr"
	"relay/internal/herdr/herdrtest"
	"relay/internal/server"
)

// GET /agents/:id/changes… end to end: w1:p1 works in a temp git repo, w1:p2 in a plain folder.
func TestRoutes_Changes(t *testing.T) {
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com",
			"-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("one\n"), 0o644)
	git("add", "a.txt")
	git("commit", "-q", "-m", "first")
	sha := git("rev-parse", "HEAD")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("two\n"), 0o644)

	fake := herdrtest.New(t, func(method string, params map[string]any) any {
		agents := []map[string]any{
			herdrtest.AgentJSON("w1:p1", str("api"), "idle", repo, nil, ""),
			herdrtest.AgentJSON("w1:p2", str("docs"), "idle", plain, nil, ""),
		}
		if method == "agent.get" {
			for _, a := range agents {
				if a["pane_id"] == params["target"] {
					return map[string]any{"type": "agent_info", "agent": a}
				}
			}
			return herdrtest.Error{Code: "agent_not_found", Message: "no agent"}
		}
		return herdrtest.Error{Code: "unknown_method", Message: method}
	})
	ts := httptest.NewServer(server.New(server.Options{Backend: New(Deps{Herdr: herdr.NewClient(fake.SocketPath)}),
		Hub: server.NewHub(), Token: routesToken, Machine: testMachine}))
	t.Cleanup(ts.Close)
	app := routesApp{ts: ts}

	r := app.get(t, "/agents/w1%3Ap1/changes")
	c := decodeInto[api.Changes](t, r)
	if r.status != 200 || !c.Repo || len(c.Files) != 1 || c.Files[0].Path != "a.txt" || len(c.Commits) != 1 || c.Commits[0].SHA != sha {
		t.Fatalf("%d %s", r.status, r.body)
	}
	r = app.get(t, "/agents/w1%3Ap1/changes/diff?path=a.txt")
	if d := decodeInto[api.FileDiffText](t, r); r.status != 200 || !strings.Contains(d.Diff, "-one\n+two\n") {
		t.Errorf("diff %d %s", r.status, r.body)
	}
	r = app.get(t, "/agents/w1%3Ap1/changes/commits/"+sha[:7])
	if d := decodeInto[api.CommitDetail](t, r); r.status != 200 || d.SHA != sha || len(d.Files) != 1 || !strings.Contains(d.Files[0].Diff, "+one\n") {
		t.Errorf("commit %d %s", r.status, r.body)
	}
	if r := app.get(t, "/agents/w1%3Ap2/changes"); r.status != 200 || string(r.body) != `{"repo":false}` {
		t.Errorf("plain %d %s", r.status, r.body)
	}

	for uri, want := range map[string]int{
		"/agents/w1%3Ap1/changes/diff?path=..%2Fx":                   http.StatusForbidden,
		"/agents/w1%3Ap1/changes/diff?path=%2Fetc%2Fhost":            http.StatusBadRequest,
		"/agents/w1%3Ap1/changes/diff":                               http.StatusBadRequest,
		"/agents/w1%3Ap1/changes/diff?path=nope.txt":                 http.StatusNotFound,
		"/agents/w1%3Ap1/changes/commits/xyz":                        http.StatusNotFound,
		"/agents/w1%3Ap1/changes/commits/" + strings.Repeat("f", 40): http.StatusNotFound,
		"/agents/w1%3Ap2/changes/diff?path=a.txt":                    http.StatusNotFound,
		"/agents/w1%3Ap2/changes/commits/" + sha:                     http.StatusNotFound,
		"/agents/w9%3Ap9/changes":                                    http.StatusNotFound,
	} {
		r := app.get(t, uri)
		if r.status != want {
			t.Errorf("%s: want %d, got %d %s", uri, want, r.status, r.body)
		}
		for _, p := range []string{repo, plain, "/Users/", "/private/"} {
			if bytes.Contains(r.body, []byte(p)) {
				t.Errorf("%s leaked a path: %s", uri, r.body)
			}
		}
	}
	if r := app.do(t, "GET", "/agents/w1%3Ap1/changes", nil, nil); r.status != http.StatusUnauthorized {
		t.Errorf("no token: %d", r.status)
	}
}
