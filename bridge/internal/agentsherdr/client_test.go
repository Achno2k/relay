package herdr

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// fixture is one captured `herdr` invocation: what the CLI printed on each
// stream and whether it exited non-zero.
type fixture struct {
	stdout string
	stderr string
	err    error
}

// stub records the argv it was called with and replays fixtures in order.
type stub struct {
	calls   [][]string
	replies []fixture
	n       int
}

func (s *stub) runner(_ context.Context, args ...string) ([]byte, []byte, error) {
	s.calls = append(s.calls, args)
	if s.n >= len(s.replies) {
		return nil, nil, errors.New("stub: unexpected call " + strings.Join(args, " "))
	}
	r := s.replies[s.n]
	s.n++
	return []byte(r.stdout), []byte(r.stderr), r.err
}

func load(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

func newStub(t *testing.T, replies ...fixture) (*client, *stub) {
	t.Helper()
	s := &stub{replies: replies}
	return &client{run: s.runner}, s
}

func TestListAgentsParsesRealFixture(t *testing.T) {
	c, _ := newStub(t, fixture{stdout: load(t, "agent_list.json")})

	agents, err := c.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 18 {
		t.Fatalf("got %d agents, want 18", len(agents))
	}

	// Named agents carry a name; unnamed panes come back with an empty one.
	var named *Agent
	for i := range agents {
		if agents[i].Name == "sig" {
			named = &agents[i]
		}
	}
	if named == nil {
		t.Fatal("agent \"sig\" not found in fixture")
	}
	if named.Kind != "claude" {
		t.Errorf("Kind = %q, want claude", named.Kind)
	}
	if named.PaneID != "wJ:p9" {
		t.Errorf("PaneID = %q, want wJ:p9", named.PaneID)
	}
	if named.State != StateIdle {
		t.Errorf("State = %q, want idle", named.State)
	}
	if named.CWD != "/Users/you/projects/wt/tools-signature" {
		t.Errorf("CWD = %q", named.CWD)
	}
}

func TestListAgentsReadsWorkingState(t *testing.T) {
	c, _ := newStub(t, fixture{stdout: load(t, "agent_list.json")})
	agents, err := c.ListAgents(context.Background())
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	working := 0
	for _, a := range agents {
		if a.State == StateWorking {
			working++
		}
	}
	if working != 6 {
		t.Errorf("working agents = %d, want 6", working)
	}
}

func TestGetAgent(t *testing.T) {
	c, s := newStub(t, fixture{stdout: load(t, "agent_get.json")})

	a, err := c.GetAgent(context.Background(), "coretest")
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if a.Name != "coretest" || a.PaneID != "wR:pA" || a.State != StateIdle {
		t.Errorf("got %+v", a)
	}
	want := []string{"agent", "get", "coretest"}
	if strings.Join(s.calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %v, want %v", s.calls[0], want)
	}
}

func TestServerErrorOnStderr(t *testing.T) {
	c, _ := newStub(t, fixture{
		stderr: load(t, "error_agent_not_found.json"),
		err:    errors.New("exit status 1"),
	})

	_, err := c.GetAgent(context.Background(), "nope")
	if err == nil {
		t.Fatal("want an error")
	}
	var he *Error
	if !errors.As(err, &he) {
		t.Fatalf("want *herdr.Error, got %T: %v", err, err)
	}
	if he.Code != "agent_not_found" {
		t.Errorf("Code = %q", he.Code)
	}
	if !IsNotFound(err) {
		t.Error("IsNotFound = false, want true")
	}
}

// agent start reports agent_not_ready on stdout, unlike every other command.
func TestServerErrorOnStdout(t *testing.T) {
	c, _ := newStub(t, fixture{
		stdout: `{"error":{"code":"agent_not_ready","message":"blocked during startup"},"id":"cli:agent:start"}`,
	})

	err := c.StartAgent(context.Background(), "a", "claude", "wR:p1")
	var he *Error
	if !errors.As(err, &he) || he.Code != "agent_not_ready" {
		t.Fatalf("want agent_not_ready, got %v", err)
	}
}

func TestUsageErrorIsNotSwallowed(t *testing.T) {
	c, _ := newStub(t, fixture{
		stderr: "usage: herdr agent get <target>\n",
		err:    errors.New("exit status 2"),
	})

	_, err := c.GetAgent(context.Background(), "")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "usage: herdr agent get") {
		t.Errorf("error lost the usage text: %v", err)
	}
}

func TestWaitParsesStateAndPassesUntil(t *testing.T) {
	c, s := newStub(t, fixture{stdout: load(t, "agent_wait.json")})

	got, err := c.Wait(context.Background(), "coretest", StateIdle, StateDone)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got != StateIdle {
		t.Errorf("state = %q, want idle", got)
	}
	argv := strings.Join(s.calls[0], " ")
	if argv != "agent wait coretest --until idle --until done" {
		t.Errorf("argv = %q", argv)
	}
}

func TestWaitSendsTimeoutFromContextDeadline(t *testing.T) {
	c, s := newStub(t, fixture{stdout: load(t, "agent_wait.json")})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := c.Wait(ctx, "coretest", StateDone); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	argv := strings.Join(s.calls[0], " ")
	if !strings.Contains(argv, "--timeout ") {
		t.Fatalf("no --timeout in %q", argv)
	}
}

func TestPromptDoesNotWait(t *testing.T) {
	c, s := newStub(t, fixture{stdout: load(t, "agent_prompted.json")})

	if err := c.Prompt(context.Background(), "coretest", "hello there"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	argv := strings.Join(s.calls[0], " ")
	if argv != "agent prompt coretest hello there" {
		t.Errorf("argv = %q", argv)
	}
}

// agent read prints plain text, not a JSON envelope.
func TestReadUsesRecentUnwrapped(t *testing.T) {
	c, s := newStub(t, fixture{stdout: "line one\nline two\n"})

	got, err := c.Read(context.Background(), "coretest", 40)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "line one\nline two\n" {
		t.Errorf("text = %q", got)
	}
	argv := strings.Join(s.calls[0], " ")
	if argv != "agent read coretest --source recent-unwrapped --lines 40" {
		t.Errorf("argv = %q", argv)
	}
}

// Full-screen agents leave recent-unwrapped empty, so we fall back to the
// rendered viewport.
func TestReadFallsBackToVisible(t *testing.T) {
	c, s := newStub(t,
		fixture{stdout: "\n  \n"},
		fixture{stdout: "the visible screen\n"},
	)

	got, err := c.Read(context.Background(), "coretest", 10)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != "the visible screen\n" {
		t.Errorf("text = %q", got)
	}
	if len(s.calls) != 2 {
		t.Fatalf("made %d calls, want 2", len(s.calls))
	}
	if !strings.Contains(strings.Join(s.calls[1], " "), "--source visible") {
		t.Errorf("fallback argv = %v", s.calls[1])
	}
}

func TestStartAgentPassesNativeArgsAfterDoubleDash(t *testing.T) {
	c, s := newStub(t, fixture{stdout: `{"id":"x","result":{"type":"agent_started"}}`})

	err := c.StartAgent(context.Background(), "web-a3f2", "claude", "wR:pA", "--model", "opus")
	if err != nil {
		t.Fatalf("StartAgent: %v", err)
	}
	argv := strings.Join(s.calls[0], " ")
	want := "agent start web-a3f2 --kind claude --pane wR:pA -- --model opus"
	if argv != want {
		t.Errorf("argv = %q, want %q", argv, want)
	}
}

func TestSendKeys(t *testing.T) {
	c, s := newStub(t, fixture{stdout: load(t, "ok.json")})

	if err := c.SendKeys(context.Background(), "coretest", "esc", "ctrl+c"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	if got := strings.Join(s.calls[0], " "); got != "agent send-keys coretest esc ctrl+c" {
		t.Errorf("argv = %q", got)
	}
}

func TestSendKeysWithNoKeysIsANoop(t *testing.T) {
	c, s := newStub(t)
	if err := c.SendKeys(context.Background(), "coretest"); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
	if len(s.calls) != 0 {
		t.Errorf("made %d calls, want 0", len(s.calls))
	}
}

// herdr has no agent-level kill, so KillAgent closes the pane hosting it.
func TestKillAgentClosesThePane(t *testing.T) {
	c, s := newStub(t,
		fixture{stdout: load(t, "agent_get.json")},
		fixture{stdout: load(t, "ok.json")},
	)

	if err := c.KillAgent(context.Background(), "coretest"); err != nil {
		t.Fatalf("KillAgent: %v", err)
	}
	if got := strings.Join(s.calls[1], " "); got != "pane close wR:pA" {
		t.Errorf("argv = %q, want pane close wR:pA", got)
	}
}

func TestKillAgentIsIdempotent(t *testing.T) {
	c, _ := newStub(t, fixture{
		stderr: load(t, "error_agent_not_found.json"),
		err:    errors.New("exit status 1"),
	})

	if err := c.KillAgent(context.Background(), "gone"); err != nil {
		t.Errorf("killing a missing agent should succeed, got %v", err)
	}
}

func TestNotify(t *testing.T) {
	c, s := newStub(t, fixture{stdout: load(t, "notification_show.json")})

	if err := c.Notify(context.Background(), "web-a3f2", "review ready"); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	want := "notification show web-a3f2 --body review ready --sound none"
	if got := strings.Join(s.calls[0], " "); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestNewPaneReusesTheAgentsTab(t *testing.T) {
	t.Setenv("HERDR_WORKSPACE_ID", "wR")
	c, s := newStub(t,
		// tab list: no "agents" tab yet
		fixture{stdout: load(t, "tab_list.json")},
		// tab create
		fixture{stdout: load(t, "tab_created.json")},
		// pane layout for the split direction
		fixture{stdout: load(t, "pane_layout.json")},
		// pane split
		fixture{stdout: load(t, "pane_split.json")},
	)

	paneID, err := c.NewPane(context.Background(), "/home/ubuntu/work/web/a3f2")
	if err != nil {
		t.Fatalf("NewPane: %v", err)
	}
	if paneID != "wR:pA" {
		t.Errorf("paneID = %q, want wR:pA", paneID)
	}

	create := strings.Join(s.calls[1], " ")
	if !strings.Contains(create, "--label agents") {
		t.Errorf("tab was not labelled agents: %q", create)
	}
	if !strings.Contains(create, "--no-focus") {
		t.Errorf("tab create stole focus: %q", create)
	}
	split := strings.Join(s.calls[3], " ")
	if !strings.Contains(split, "--cwd /home/ubuntu/work/web/a3f2") {
		t.Errorf("split lost the cwd: %q", split)
	}
	if !strings.Contains(split, "--no-focus") {
		t.Errorf("split stole focus: %q", split)
	}
}

func TestNewPaneSplitsAnExistingAgentsTab(t *testing.T) {
	t.Setenv("HERDR_WORKSPACE_ID", "wR")
	c, s := newStub(t,
		fixture{stdout: `{"id":"x","result":{"type":"tab_list","tabs":[{"tab_id":"wR:t7","workspace_id":"wR","number":1,"label":"agents","focused":false,"pane_count":2,"agent_status":"idle"}]}}`},
		fixture{stdout: `{"id":"x","result":{"type":"pane_list","panes":[{"pane_id":"wR:p1","tab_id":"wR:t7","workspace_id":"wR","terminal_id":"t","focused":false,"agent_status":"idle","revision":0}]}}`},
		fixture{stdout: load(t, "pane_layout.json")},
		fixture{stdout: load(t, "pane_layout.json")},
		fixture{stdout: load(t, "pane_split.json")},
	)

	if _, err := c.NewPane(context.Background(), "/tmp/x"); err != nil {
		t.Fatalf("NewPane: %v", err)
	}
	// No tab was created; the existing one was reused.
	for _, call := range s.calls {
		if len(call) >= 2 && call[0] == "tab" && call[1] == "create" {
			t.Fatalf("created a tab despite an existing one: %v", call)
		}
	}
}

func TestToStateFallsBackToUnknown(t *testing.T) {
	cases := map[string]AgentState{
		"idle":      StateIdle,
		"working":   StateWorking,
		"blocked":   StateBlocked,
		"done":      StateDone,
		"unknown":   StateUnknown,
		"":          StateUnknown,
		"brand-new": StateUnknown,
	}
	for in, want := range cases {
		if got := toState(in); got != want {
			t.Errorf("toState(%q) = %q, want %q", in, got, want)
		}
	}
}
