package slackbot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/state"
)

func testBot(t *testing.T, sessions ...state.Session) (*Bot, *fakeSlack, *fakeHerdr, *fakeStore) {
	t.Helper()
	sc, h, st := newFakeSlack(), newFakeHerdr(), newFakeStore(sessions...)
	b := New(sc, h, st, testConfig(), log.New(io.Discard, "", 0))
	b.botUserID = "UBOT"
	return b, sc, h, st
}

func liveSession(id, thread string) state.Session {
	return state.Session{
		ID:           id,
		AgentName:    "agents-cli-" + id,
		Kind:         "claude",
		Repo:         "agents-cli",
		SlackChannel: "CGEN",
		ThreadTS:     thread,
		Status:       state.StatusIdle,
		LastActive:   time.Now(),
	}
}

func TestHandleMessageRefusesAtTheLiveCap(t *testing.T) {
	var live []state.Session
	for i := 0; i < 5; i++ {
		live = append(live, liveSession(string(rune('a'+i))+"111", "90"+string(rune('0'+i))+".1"))
	}
	b, sc, h, _ := testBot(t, live...)

	b.HandleMessage(context.Background(), incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "999.1",
		Text: "<@UBOT> agents-cli start something new",
	})

	if len(h.started) != 0 {
		t.Fatalf("no agent should start at the cap, started %v", h.started)
	}
	texts := sc.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "cap") {
		t.Fatalf("expected one refusal in the thread, got %v", texts)
	}
}

func TestHandleMessageAsksForARepoThenUsesTheAnswer(t *testing.T) {
	b, sc, _, _ := testBot(t)
	ctx := context.Background()

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})

	texts := sc.texts()
	if len(texts) != 1 || !strings.Contains(texts[0], "Which repo?") {
		t.Fatalf("expected a repo question, got %v", texts)
	}
	if !strings.Contains(texts[0], "agents-cli") || !strings.Contains(texts[0], "aura") {
		t.Fatalf("the question should list the repos it knows: %q", texts[0])
	}
	if !b.isPending("CGEN", "400.1") {
		t.Fatal("the thread should be waiting for a repo name")
	}
}

func TestHandleMessageIgnoresItself(t *testing.T) {
	b, sc, _, _ := testBot(t)
	b.HandleMessage(context.Background(), incoming{
		Channel: "CGEN", User: "UBOT", Mention: true, TS: "1.1",
		Text: "<@UBOT> agents-cli go",
	})
	if got := sc.texts(); len(got) != 0 {
		t.Fatalf("the bot must not answer itself, got %v", got)
	}
}

func TestHandleMessageAttachBindsARunningSession(t *testing.T) {
	sess := liveSession("a3f2", "100.1")
	b, sc, _, st := testBot(t, sess)

	b.HandleMessage(context.Background(), incoming{
		Channel: "CNEW", User: "U1", Mention: true, TS: "500.1",
		Text: "<@UBOT> attach agents-cli-a3f2",
	})

	got, err := st.Get(context.Background(), "a3f2")
	if err != nil {
		t.Fatal(err)
	}
	if got.SlackChannel != "CNEW" || got.ThreadTS != "500.1" {
		t.Fatalf("session was not rebound: %+v", got)
	}
	if texts := sc.texts(); len(texts) != 1 || !strings.Contains(texts[0], "agents-cli-a3f2") {
		t.Fatalf("expected a confirmation, got %v", texts)
	}
}

func TestHandleMessageAttachRejectsAnUnknownAgent(t *testing.T) {
	b, sc, _, _ := testBot(t)
	b.HandleMessage(context.Background(), incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "500.1",
		Text: "<@UBOT> attach nope-0000",
	})
	if texts := sc.texts(); len(texts) != 1 || !strings.Contains(texts[0], "don't have a session") {
		t.Fatalf("got %v", texts)
	}
}

func TestContinueSendsOnlyTheNewMessages(t *testing.T) {
	sess := liveSession("a3f2", "100.1")
	sess.LastSentTS = "1700000001.000100"
	sess.WorktreePath = "/work/agents-cli/a3f2"
	b, sc, h, st := testBot(t, sess)

	sc.thread = []Message{
		{TS: "1700000000.000100", UserID: "U1", Text: "the login page 500s"},
		{TS: "1700000001.000100", UserID: "U1", Text: "only since this morning"},
		{TS: "1700000002.000100", UserID: "U1", Text: "also add a test"},
	}
	sc.names = map[string]string{"U1": "aman"}

	b.HandleMessage(context.Background(), incoming{
		Channel: "CGEN", User: "U1", TS: "1700000002.000100", ThreadTS: "100.1",
		Text: "also add a test",
	})

	if len(h.prompts) != 1 {
		t.Fatalf("expected one prompt, got %d", len(h.prompts))
	}
	p := h.prompts[0]
	if strings.Contains(p, "the login page 500s") || strings.Contains(p, "only since this morning") {
		t.Fatalf("a follow up must not resend old messages:\n%s", p)
	}
	if !strings.Contains(p, "aman: also add a test") {
		t.Fatalf("the new message is missing:\n%s", p)
	}
	if !strings.Contains(p, "/work/agents-cli/a3f2/"+ReplyRelPath) {
		t.Fatalf("the reply contract is missing:\n%s", p)
	}

	got, _ := st.Get(context.Background(), "a3f2")
	if got.LastSentTS != "1700000002.000100" {
		t.Fatalf("LastSentTS is %q, want the newest message", got.LastSentTS)
	}
	if len(h.notices) != 1 || !strings.Contains(h.notices[0], "agents-cli-a3f2") {
		t.Fatalf("the herdr notice should name the agent, got %v", h.notices)
	}
}

func TestApprovalKeysDifferByHarness(t *testing.T) {
	if got := approvalKeys("claude", actionAllow); len(got) != 1 || got[0] != "enter" {
		t.Fatalf("claude allow: got %v", got)
	}
	if got := approvalKeys("codex", actionAllowAll); len(got) != 2 || got[1] != "enter" {
		t.Fatalf("codex allow all: got %v", got)
	}
	// Codex puts "always allow" second and "decline" third; claude is the
	// other way round. The two tables must not be the same.
	claude := strings.Join(approvalKeys("claude", actionDeny), ",")
	codex := strings.Join(approvalKeys("codex", actionDeny), ",")
	if claude == codex {
		t.Fatalf("deny should differ between harnesses, both are %q", claude)
	}
	if got := approvalKeys("claude", "nonsense"); got != nil {
		t.Fatalf("an unknown action should map to nothing, got %v", got)
	}
}

func TestHandleInteractionSendsKeysAndClosesTheMessage(t *testing.T) {
	sess := liveSession("a3f2", "100.1")
	b, sc, h, _ := testBot(t, sess)
	sc.names = map[string]string{"U1": "aman"}

	cb := slack.InteractionCallback{
		User:    slack.User{ID: "U1"},
		Message: slack.Message{Msg: slack.Msg{Timestamp: "ts-buttons"}},
		ActionCallback: slack.ActionCallbacks{
			BlockActions: []*slack.BlockAction{{ActionID: actionAllowAll, Value: "a3f2"}},
		},
	}
	b.HandleInteraction(context.Background(), cb)

	if len(h.keys) != 1 {
		t.Fatalf("expected one key sequence, got %v", h.keys)
	}
	if want := approvalKeys("claude", actionAllowAll); strings.Join(h.keys[0], ",") != strings.Join(want, ",") {
		t.Fatalf("sent %v, want %v", h.keys[0], want)
	}
	if got := sc.updates["ts-buttons"]; !strings.Contains(got, "aman") || !strings.Contains(got, "won't ask again") {
		t.Fatalf("the approval message should be replaced, got %q", got)
	}
}

func TestHandleInteractionIgnoresStrangers(t *testing.T) {
	sess := liveSession("a3f2", "100.1")
	b, _, h, _ := testBot(t, sess)

	b.HandleInteraction(context.Background(), slack.InteractionCallback{
		User: slack.User{ID: "U9"},
		ActionCallback: slack.ActionCallbacks{
			BlockActions: []*slack.BlockAction{{ActionID: actionAllow, Value: "a3f2"}},
		},
	})
	if len(h.keys) != 0 {
		t.Fatalf("a stranger must not answer a prompt, got %v", h.keys)
	}
}

func TestSweepParksIdleSessionsOnly(t *testing.T) {
	now := time.Now()

	idle := liveSession("aaaa", "1.1")
	idle.LastActive = now.Add(-30 * time.Hour)

	busy := liveSession("bbbb", "2.1")
	busy.Status = state.StatusWorking
	busy.LastActive = now.Add(-30 * time.Hour)

	fresh := liveSession("cccc", "3.1")
	fresh.LastActive = now.Add(-1 * time.Hour)

	gone := liveSession("dddd", "4.1")
	gone.Status = state.StatusDone
	gone.LastActive = now.Add(-99 * time.Hour)

	b, sc, h, st := testBot(t, idle, busy, fresh, gone)

	if n := b.sweepOnce(context.Background(), now); n != 1 {
		t.Fatalf("parked %d sessions, want 1", n)
	}
	if len(h.killed) != 1 || h.killed[0] != "agents-cli-aaaa" {
		t.Fatalf("killed %v, want just the idle one", h.killed)
	}
	got, _ := st.Get(context.Background(), "aaaa")
	if got.Status != state.StatusParked {
		t.Fatalf("status is %q, want parked", got.Status)
	}
	if texts := sc.texts(); len(texts) != 1 || !strings.Contains(texts[0], "Parked") {
		t.Fatalf("expected one parked notice, got %v", texts)
	}
	if busyRow, _ := st.Get(context.Background(), "bbbb"); busyRow.Status != state.StatusWorking {
		t.Fatal("a working session must never be parked")
	}
}

func TestSafeJoinRefusesToEscapeTheWorktree(t *testing.T) {
	if _, err := safeJoin("/work/aura/9c1d", "../../etc/passwd"); err == nil {
		t.Fatal("expected an error for a path outside the worktree")
	}
	if _, err := safeJoin("/work/aura/9c1d", "/etc/passwd"); err == nil {
		t.Fatal("expected an error for an absolute path")
	}
	got, err := safeJoin("/work/aura/9c1d", "internal/ui/ui.go")
	if err != nil || got != "/work/aura/9c1d/internal/ui/ui.go" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestHarnessKindIsTheFirstConfigured(t *testing.T) {
	b, _, _, _ := testBot(t)
	if got := b.harnessKind(); got != "claude" {
		t.Fatalf("got %q", got)
	}
	b.Config.Harness = []string{"codex", "claude"}
	if got := b.harnessKind(); got != "codex" {
		t.Fatalf("got %q", got)
	}
	b.Config.Harness = nil
	if got := b.harnessKind(); got != "claude" {
		t.Fatalf("an empty list should fall back to claude, got %q", got)
	}
}

func TestRepoDirIsTheMainCheckout(t *testing.T) {
	b, _, _, _ := testBot(t)
	if got := b.repoDir("aura"); got != "/work/aura/main" {
		t.Fatalf("got %q, want /work/aura/main", got)
	}
}

func TestUnbindThreadClearsTheBindingForAFork(t *testing.T) {
	sess := liveSession("a3f2", "100.1")
	b, _, _, st := testBot(t, sess)
	ctx := context.Background()

	b.unbindThread(ctx, sess)

	got, err := st.Get(ctx, "a3f2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ThreadTS != "" {
		t.Fatalf("ThreadTS is %q, want empty so the thread is free", got.ThreadTS)
	}
	// The freed thread must now accept the forked session.
	fresh := liveSession("b7c1", "100.1")
	if err := st.Create(ctx, fresh); err != nil {
		t.Fatalf("the fork could not take the thread: %v", err)
	}
}

func TestCreateSessionKillsTheAgentWhenTheRowCannotBeWritten(t *testing.T) {
	// A session already owns this thread, so Create will be refused.
	taken := liveSession("a3f2", "100.1")
	b, sc, h, _ := testBot(t, taken)
	b.Config.Box.WorkDir = t.TempDir()

	// Point the router at the taken thread with no bound session in the way by
	// asking createSession directly.
	b.createSession(context.Background(), decision{
		Action: actCreate, Channel: "CGEN", ThreadTS: "100.1", Repo: "agents-cli", Text: "go",
	}, nil)

	// worktree.Create fails first (there is no checkout to branch from), so the
	// bot should have said so and started nothing.
	if len(h.started) != 0 {
		t.Fatalf("no agent should be running, started %v", h.started)
	}
	if texts := sc.texts(); len(texts) != 1 || !strings.Contains(texts[0], "worktree") {
		t.Fatalf("expected one worktree failure, got %v", texts)
	}
}

func TestAskRepoAsksOnceThenNamesWhatItDoesNotKnow(t *testing.T) {
	b, sc, _, _ := testBot(t)
	ctx := context.Background()

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})
	// The answer names a repo this box has never heard of.
	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", TS: "400.2", ThreadTS: "400.1",
		Text: "conduit",
	})

	texts := sc.texts()
	if len(texts) != 2 {
		t.Fatalf("expected the question then an answer, got %v", texts)
	}
	if !strings.Contains(texts[0], "Which repo?") {
		t.Fatalf("first reply should be the question: %q", texts[0])
	}
	want := "I don't know conduit. Repos on this box: agents-cli, aura. Run `agents init` from that repo to add it."
	if texts[1] != want {
		t.Fatalf("second reply:\n got %q\nwant %q", texts[1], want)
	}
	if !b.isPending("CGEN", "400.1") {
		t.Fatal("the thread should still be waiting for a repo")
	}
}

// The live bug: the bot stayed silent on every retry, so the thread only ever
// logged ask_repo. Each attempt gets exactly one reply.
func TestEveryUnknownRepoAttemptGetsOneReply(t *testing.T) {
	b, sc, _, _ := testBot(t)
	ctx := context.Background()

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})
	for i, word := range []string{"conduit", "event-orbit", "app-clip-web"} {
		b.HandleMessage(ctx, incoming{
			Channel: "CGEN", User: "U1", TS: fmt.Sprintf("400.%d", i+2), ThreadTS: "400.1",
			Text: word,
		})
	}

	texts := sc.texts()
	if len(texts) != 4 {
		t.Fatalf("expected 1 question + 3 answers, got %d: %v", len(texts), texts)
	}
	for i, word := range []string{"conduit", "event-orbit", "app-clip-web"} {
		if !strings.Contains(texts[i+1], "I don't know "+word+".") {
			t.Fatalf("reply %d should name %q, got %q", i+1, word, texts[i+1])
		}
	}
}

func TestUnknownRepoReplyEchoesTheFirstWordNotTheMention(t *testing.T) {
	b, sc, _, _ := testBot(t)
	ctx := context.Background()

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})
	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.2", ThreadTS: "400.1",
		Text: "<@UBOT> Conduit please, the API one",
	})

	texts := sc.texts()
	if len(texts) != 2 {
		t.Fatalf("got %v", texts)
	}
	// The user's own casing, and never the "<@UBOT>" mention.
	if !strings.Contains(texts[1], "I don't know Conduit.") {
		t.Fatalf("got %q", texts[1])
	}
	if strings.Contains(texts[1], "UBOT") {
		t.Fatalf("the mention leaked into the reply: %q", texts[1])
	}
}

func TestUnknownRepoReplyWhenTheBoxHasNoRepos(t *testing.T) {
	b, sc, _, _ := testBot(t)
	b.Config.Repos = nil
	ctx := context.Background()

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})
	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", TS: "400.2", ThreadTS: "400.1",
		Text: "aura",
	})

	texts := sc.texts()
	if len(texts) != 2 || !strings.Contains(texts[1], "no repos on this box yet") {
		t.Fatalf("got %v", texts)
	}
}

// An empty reply is not an answer, so it gets nothing rather than a confusing
// "I don't know ." line.
func TestBlankReplyWhilePendingSaysNothing(t *testing.T) {
	b, sc, _, _ := testBot(t)
	ctx := context.Background()

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})
	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.2", ThreadTS: "400.1",
		Text: "<@UBOT>",
	})

	if texts := sc.texts(); len(texts) != 1 {
		t.Fatalf("expected only the question, got %v", texts)
	}
}

// --- config reload ---

// The live bug: the bot read config once at startup, so a repo added by
// `agents init` afterwards stayed invisible until someone restarted it.
func TestRepoAddedAfterStartupIsPickedUpWithoutARestart(t *testing.T) {
	b, sc, _, _ := testBot(t)
	ctx := context.Background()

	// The file on disk, which `agents init` is about to rewrite. It is a whole
	// fresh value each time, sharing no maps with what the bot already holds,
	// so nothing here can pass without an actual reload.
	onDisk := testConfig()
	b.Config = testConfig()
	b.Reload = func() (config.Config, error) { return onDisk, nil }

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})
	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", TS: "400.2", ThreadTS: "400.1",
		Text: "conduit",
	})
	if texts := sc.texts(); !strings.Contains(texts[len(texts)-1], "I don't know conduit.") {
		t.Fatalf("conduit should be unknown at this point, got %q", texts[len(texts)-1])
	}

	// `agents init` runs and syncs the config to the box.
	onDisk = testConfig()
	onDisk.Repos["conduit"] = config.Repo{URL: "git@github.com:lt/conduit.git"}

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", TS: "400.3", ThreadTS: "400.1",
		Text: "conduit",
	})

	// No restart: the next reply is accepted and a session is attempted.
	last := sc.texts()[len(sc.texts())-1]
	if strings.Contains(last, "I don't know") {
		t.Fatalf("the new repo should be known after the reload, got %q", last)
	}
	if b.isPending("CGEN", "400.1") {
		t.Fatal("the thread should no longer be waiting for a repo")
	}
}

func TestReloadPicksUpAllowlistAndChannelDefaultChanges(t *testing.T) {
	b, sc, _, _ := testBot(t)
	ctx := context.Background()

	onDisk := testConfig()
	onDisk.Slack.AllowedUserIDs = []string{"U1"}
	b.Config = testConfig()
	b.Config.Slack.AllowedUserIDs = []string{"U1"}
	b.Reload = func() (config.Config, error) { return onDisk, nil }

	// U7 is not on the allowlist yet, so nothing happens.
	b.HandleMessage(ctx, incoming{
		Channel: "CNEW", User: "U7", Mention: true, TS: "500.1",
		Text: "<@UBOT> have a look",
	})
	if got := sc.texts(); len(got) != 0 {
		t.Fatalf("a stranger should be ignored, got %v", got)
	}

	// Someone adds U7 and gives the channel a default repo.
	onDisk = testConfig()
	onDisk.Slack.AllowedUserIDs = []string{"U1", "U7"}
	onDisk.Slack.DefaultRepoByChannel["CNEW"] = "aura"

	b.HandleMessage(ctx, incoming{
		Channel: "CNEW", User: "U7", Mention: true, TS: "500.2",
		Text: "<@UBOT> have a look",
	})
	// U7 is allowed now, and the channel default answered the repo question,
	// so this went straight to creating a session rather than asking.
	for _, m := range sc.texts() {
		if strings.Contains(m, "Which repo?") {
			t.Fatalf("the channel default should have supplied the repo, got %v", sc.texts())
		}
	}
	if len(sc.texts()) == 0 {
		t.Fatal("expected the bot to act on U7 after the reload")
	}
}

// A config file caught mid-write must not empty the allowlist or take the bot
// down; the previous configuration stands.
func TestReloadFailureKeepsTheLastGoodConfig(t *testing.T) {
	b, sc, _, _ := testBot(t)
	b.Reload = func() (config.Config, error) { return config.Config{}, errors.New("unexpected EOF") }

	b.HandleMessage(context.Background(), incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})

	if got := sc.texts(); len(got) != 1 || !strings.Contains(got[0], "Which repo?") {
		t.Fatalf("the bot should still be using the last good config, got %v", got)
	}
}

func TestNilReloadPinsTheConfig(t *testing.T) {
	b, _, _, _ := testBot(t)
	if b.Reload != nil {
		t.Fatal("New should not wire a reload; only NewFromEnv does")
	}
	before := b.config()
	b.reloadConfig()
	if len(b.config().Repos) != len(before.Repos) {
		t.Fatal("reloadConfig with a nil Reload must change nothing")
	}
}

// --- repo matching ---

func TestRepoMatchingIgnoresCaseAndATrailingRepoWord(t *testing.T) {
	cfg := testConfig()

	for _, in := range []string{
		"aura", "AURA", "Aura", "the aura repo", "aura repo", "aura-repo", "aura_repo",
	} {
		if got := resolveRepo(in, "CGEN", "", cfg); got != "aura" {
			t.Fatalf("resolveRepo(%q) = %q, want aura", in, got)
		}
	}
}

// The mirror image: a repo configured with "repo" in its name is found when
// the user leaves it off.
func TestRepoConfiguredWithATrailingRepoWordMatchesTheBareName(t *testing.T) {
	cfg := testConfig()
	cfg.Repos = map[string]config.Repo{"conduit-repo": {}}

	for _, in := range []string{"conduit", "conduit-repo", "Conduit", "conduit repo"} {
		if got := resolveRepo(in, "CGEN", "", cfg); got != "conduit-repo" {
			t.Fatalf("resolveRepo(%q) = %q, want conduit-repo", in, got)
		}
	}
}

func TestTrimRepoWord(t *testing.T) {
	cases := map[string]string{
		"aura":       "aura",
		"aura-repo":  "aura",
		"aura_repo":  "aura",
		"aura repo":  "aura",
		"aura-REPO":  "aura",
		"repo":       "repo", // nothing would be left
		"repository": "repository",
		"agents-cli": "agents-cli",
		"reposition": "reposition",
	}
	for in, want := range cases {
		if got := trimRepoWord(in); got != want {
			t.Fatalf("trimRepoWord(%q) = %q, want %q", in, got, want)
		}
	}
}

// "aurora" must not answer for "aura" just because a trailing word was trimmed.
func TestRepoMatchingStillRefusesNearMisses(t *testing.T) {
	cfg := testConfig()
	for _, in := range []string{"aurora", "auras", "agents", "cli"} {
		if got := resolveRepo(in, "CGEN", "", cfg); got != "" {
			t.Fatalf("resolveRepo(%q) = %q, want no match", in, got)
		}
	}
}

func TestAnsweringWithATrailingRepoWordCreatesTheSession(t *testing.T) {
	b, sc, _, _ := testBot(t)
	ctx := context.Background()

	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
		Text: "<@UBOT> have a look",
	})
	b.HandleMessage(ctx, incoming{
		Channel: "CGEN", User: "U1", TS: "400.2", ThreadTS: "400.1",
		Text: "the Aura repo",
	})

	for _, m := range sc.texts() {
		if strings.Contains(m, "I don't know") {
			t.Fatalf("\"the Aura repo\" should resolve to aura, got %v", sc.texts())
		}
	}
	if b.isPending("CGEN", "400.1") {
		t.Fatal("the repo was answered, so the thread should not still be pending")
	}
}
