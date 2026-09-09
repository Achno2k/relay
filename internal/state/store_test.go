package state

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func open(t *testing.T) Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func sample(id string) Session {
	return Session{
		ID:           id,
		AgentName:    "web-" + id,
		Kind:         "claude",
		Repo:         "web",
		Branch:       "agents/" + id,
		WorktreePath: "/home/ubuntu/work/web/" + id,
		SlackChannel: "C123",
		ThreadTS:     "1710000000." + id,
		Status:       StatusWorking,
	}
}

func TestOpenCreatesTheDatabaseAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.db")

	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := st.Create(context.Background(), sample("a3f2")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	st.Close()

	// Reopening replays no migrations and keeps the data.
	st2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer st2.Close()
	if _, err := st2.Get(context.Background(), "a3f2"); err != nil {
		t.Fatalf("Get after reopen: %v", err)
	}
}

func TestCreateAndGetRoundTrip(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	in := sample("a3f2")

	if err := st.Create(ctx, in); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := st.Get(ctx, "a3f2")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentName != in.AgentName || got.Repo != in.Repo || got.Branch != in.Branch {
		t.Errorf("got %+v", got)
	}
	if got.WorktreePath != in.WorktreePath || got.Status != StatusWorking {
		t.Errorf("got %+v", got)
	}
	if got.CreatedAt.IsZero() || got.LastActive.IsZero() {
		t.Error("Create should stamp CreatedAt and LastActive")
	}
	if got.AttachedAt != nil {
		t.Errorf("AttachedAt = %v, want nil", got.AttachedAt)
	}
}

func TestCreateDefaultsStatusToStarting(t *testing.T) {
	st := open(t)
	s := sample("b1c2")
	s.Status = ""

	if err := st.Create(context.Background(), s); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, _ := st.Get(context.Background(), "b1c2")
	if got.Status != StatusStarting {
		t.Errorf("Status = %q, want starting", got.Status)
	}
}

func TestCreateRejectsEmptyID(t *testing.T) {
	st := open(t)
	s := sample("x")
	s.ID = ""
	if err := st.Create(context.Background(), s); err == nil {
		t.Fatal("want an error for an empty id")
	}
}

func TestLookupsMissAsErrNotFound(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	if _, err := st.Get(ctx, "zzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: %v, want ErrNotFound", err)
	}
	if _, err := st.ByAgent(ctx, "web-zzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByAgent: %v, want ErrNotFound", err)
	}
	if _, err := st.ByThread(ctx, "C999", "1.0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("ByThread: %v, want ErrNotFound", err)
	}
}

func TestByThreadAndByAgent(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if err := st.Create(ctx, sample("a3f2")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	byThread, err := st.ByThread(ctx, "C123", "1710000000.a3f2")
	if err != nil {
		t.Fatalf("ByThread: %v", err)
	}
	if byThread.ID != "a3f2" {
		t.Errorf("ByThread returned %q", byThread.ID)
	}

	byAgent, err := st.ByAgent(ctx, "web-a3f2")
	if err != nil {
		t.Fatalf("ByAgent: %v", err)
	}
	if byAgent.ID != "a3f2" {
		t.Errorf("ByAgent returned %q", byAgent.ID)
	}
}

// One thread is one session: PLAN.md's central routing rule.
func TestThreadIsUnique(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if err := st.Create(ctx, sample("a3f2")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	clash := sample("b7d1")
	clash.SlackChannel = "C123"
	clash.ThreadTS = "1710000000.a3f2"
	err := st.Create(ctx, clash)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("second session on the same thread: %v, want ErrExists", err)
	}
}

func TestAgentNameIsUnique(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if err := st.Create(ctx, sample("a3f2")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	clash := sample("b7d1")
	clash.AgentName = "web-a3f2"
	clash.SlackChannel = "C999"
	clash.ThreadTS = "1710000001.0"
	if err := st.Create(ctx, clash); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate agent name: %v, want ErrExists", err)
	}
}

// Terminal sessions have no slack thread, and many of them must coexist.
func TestManySessionsWithoutAThread(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	for _, id := range []string{"a1b2", "c3d4", "e5f6"} {
		s := sample(id)
		s.SlackChannel, s.ThreadTS = "", ""
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	all, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d sessions, want 3", len(all))
	}
}

func TestUpdate(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if err := st.Create(ctx, sample("a3f2")); err != nil {
		t.Fatalf("Create: %v", err)
	}

	s, _ := st.Get(ctx, "a3f2")
	now := time.Now().Truncate(time.Second)
	s.Status = StatusParked
	s.AttachedBy = "aman@amans-mbp"
	s.AttachedAt = &now
	s.LastSentTS = "1710000009.7"
	if err := st.Update(ctx, s); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := st.Get(ctx, "a3f2")
	if got.Status != StatusParked || got.AttachedBy != "aman@amans-mbp" {
		t.Errorf("got %+v", got)
	}
	if got.LastSentTS != "1710000009.7" {
		t.Errorf("LastSentTS = %q", got.LastSentTS)
	}
	if got.AttachedAt == nil || !got.AttachedAt.Equal(now) {
		t.Errorf("AttachedAt = %v, want %v", got.AttachedAt, now)
	}
}

// Clearing the attach fields is how `agents sessions kill` releases a session.
func TestUpdateCanClearAttachedAt(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if err := st.Create(ctx, sample("a3f2")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	s, _ := st.Get(ctx, "a3f2")
	now := time.Now()
	s.AttachedBy, s.AttachedAt = "aman@mbp", &now
	if err := st.Update(ctx, s); err != nil {
		t.Fatalf("Update: %v", err)
	}

	s, _ = st.Get(ctx, "a3f2")
	s.AttachedBy, s.AttachedAt = "", nil
	if err := st.Update(ctx, s); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := st.Get(ctx, "a3f2")
	if got.AttachedBy != "" || got.AttachedAt != nil {
		t.Errorf("attach fields not cleared: %+v", got)
	}
}

func TestUpdateAndDeleteMissIsErrNotFound(t *testing.T) {
	st := open(t)
	ctx := context.Background()

	if err := st.Update(ctx, sample("zzzz")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update: %v, want ErrNotFound", err)
	}
	if err := st.Delete(ctx, "zzzz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete: %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if err := st.Create(ctx, sample("a3f2")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := st.Delete(ctx, "a3f2"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := st.Get(ctx, "a3f2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}
}

func TestListIsNewestFirst(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)

	for i, id := range []string{"aaaa", "bbbb", "cccc"} {
		s := sample(id)
		s.CreatedAt = base.Add(time.Duration(i) * time.Minute)
		s.LastActive = s.CreatedAt
		if err := st.Create(ctx, s); err != nil {
			t.Fatalf("Create %s: %v", id, err)
		}
	}
	all, err := st.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"cccc", "bbbb", "aaaa"}
	for i, w := range want {
		if all[i].ID != w {
			t.Errorf("List[%d] = %q, want %q", i, all[i].ID, w)
		}
	}
}

func TestListOnEmptyStore(t *testing.T) {
	all, err := open(t).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("got %d sessions, want 0", len(all))
	}
}

func TestNewIDIsFourHexChars(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{4}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewID()
		if !re.MatchString(id) {
			t.Fatalf("NewID() = %q, want 4 hex chars", id)
		}
		seen[id] = true
	}
	if len(seen) < 50 {
		t.Errorf("only %d distinct ids in 200 draws, ids look predictable", len(seen))
	}
}

func TestSessionLive(t *testing.T) {
	cases := map[Status]bool{
		StatusStarting: true,
		StatusWorking:  true,
		StatusIdle:     true,
		StatusBlocked:  true,
		StatusParked:   false,
		StatusDone:     false,
	}
	for status, want := range cases {
		if got := (Session{Status: status}).Live(); got != want {
			t.Errorf("Session{%s}.Live() = %v, want %v", status, got, want)
		}
	}
}

func TestDefaultPathRespectsAgentsHome(t *testing.T) {
	t.Setenv("AGENTS_HOME", "/tmp/agents-home")
	if got := DefaultPath(); got != "/tmp/agents-home/state.db" {
		t.Errorf("DefaultPath() = %q", got)
	}
}
