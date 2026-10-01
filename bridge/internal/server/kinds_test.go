package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"relay/internal/api"
)

// fakeKinds says claude may start and everything else is blocked.
type fakeKinds struct{}

func (fakeKinds) All(context.Context) []api.KindStatus {
	return []api.KindStatus{{Kind: "claude", Installed: true, SignedIn: true},
		{Kind: "codex", Installed: true, SignInHint: "Run `codex login` on this machine, then try again."}}
}

func (fakeKinds) Startable(_ context.Context, kind string) error {
	switch kind {
	case "claude":
		return nil
	case "codex":
		return api.NewError(http.StatusConflict, "not_signed_in", "Run `codex login` on this machine, then try again.")
	}
	return api.NewError(http.StatusConflict, "not_installed", "`"+kind+"` isn't installed on this machine.")
}

// createCounter records whether Create reached the backend.
type createCounter struct {
	stubBackend
	calls *int
}

func (c createCounter) Create(ctx context.Context, r CreateRequest) (api.Agent, error) {
	*c.calls++
	return api.Agent{ID: "w1:p9", Kind: r.Kind}, nil
}

func TestKindsRoute(t *testing.T) {
	ts := newTestServer(t, Options{Kinds: fakeKinds{}})
	r := do(t, ts, "GET", "/kinds", "", auth)
	if r.status != http.StatusOK {
		t.Fatalf("status %d %s", r.status, r.body)
	}
	var got []api.KindStatus
	if err := json.Unmarshal([]byte(r.body), &got); err != nil || len(got) != 2 || got[1].SignInHint == "" {
		t.Fatalf("body %s (%v)", r.body, err)
	}
	if do(t, ts, "GET", "/kinds", "", nil).status != http.StatusUnauthorized {
		t.Error("/kinds must need the token")
	}
}

func TestCreateIsGatedBeforeTheBackend(t *testing.T) {
	calls := 0
	ts := newTestServer(t, Options{Kinds: fakeKinds{}, Backend: createCounter{calls: &calls}})
	for _, c := range []struct {
		kind, code string
		status     int
	}{
		{"codex", "not_signed_in", http.StatusConflict},
		{"pi", "not_installed", http.StatusConflict},
	} {
		r := do(t, ts, "POST", "/agents", `{"workspaceId":"w1","kind":"`+c.kind+`"}`, auth)
		if r.status != c.status || errorCode(t, r.body) != c.code {
			t.Errorf("%s: %d %s", c.kind, r.status, r.body)
		}
		var e api.ErrorBody
		json.Unmarshal([]byte(r.body), &e)
		if e.Error.Message == "" {
			t.Errorf("%s: no hint in the message", c.kind)
		}
	}
	if calls != 0 {
		t.Fatalf("backend Create ran %d times for kinds that can't start", calls)
	}
	if r := do(t, ts, "POST", "/agents", `{"workspaceId":"w1","kind":"claude"}`, auth); r.status != http.StatusCreated || calls != 1 {
		t.Fatalf("claude: %d %s, calls %d", r.status, r.body, calls)
	}
}
