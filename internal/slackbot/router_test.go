package slackbot

import (
	"testing"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/state"
)

func testConfig() config.Config {
	c := config.Default()
	c.Harness = []string{"claude"}
	c.Box.WorkDir = "/work"
	c.Repos = map[string]config.Repo{
		"agents-cli": {URL: "git@github.com:Achno2k/agents-cli.git"},
		"aura":       {URL: "git@github.com:lt/aura.git"},
	}
	c.Slack.AllowedUserIDs = []string{"U1", "U2"}
	c.Slack.DefaultRepoByChannel = map[string]string{"CDEFAULT": "aura"}
	return c
}

func boundSession() *state.Session {
	return &state.Session{
		ID:           "a3f2",
		AgentName:    "agents-cli-a3f2",
		Kind:         "claude",
		Repo:         "agents-cli",
		SlackChannel: "CGEN",
		ThreadTS:     "100.1",
		Status:       state.StatusIdle,
	}
}

func TestDecide(t *testing.T) {
	cfg := testConfig()

	cases := []struct {
		name    string
		in      incoming
		bound   *state.Session
		pending bool
		want    decision
	}{
		{
			name: "mention names the repo",
			in: incoming{
				Channel: "CGEN", User: "U1", Mention: true, TS: "100.1",
				Text: "<@UBOT> fix the login bug in agents-cli",
			},
			want: decision{Action: actCreate, Channel: "CGEN", ThreadTS: "100.1",
				Repo: "agents-cli", Text: "fix the login bug in agents-cli"},
		},
		{
			name: "channel default supplies the repo",
			in: incoming{
				Channel: "CDEFAULT", User: "U1", Mention: true, TS: "200.1",
				Text: "<@UBOT> bump the deps",
			},
			want: decision{Action: actCreate, Channel: "CDEFAULT", ThreadTS: "200.1",
				Repo: "aura", Text: "bump the deps"},
		},
		{
			name: "repo comes from earlier in the thread",
			in: incoming{
				Channel: "CGEN", User: "U1", Mention: true, TS: "300.9", ThreadTS: "300.1",
				Text:       "<@UBOT> can you take this",
				ThreadText: "the aura build is red again\n",
			},
			want: decision{Action: actCreate, Channel: "CGEN", ThreadTS: "300.1",
				Repo: "aura", Text: "can you take this"},
		},
		{
			name: "the mention wins over the channel default",
			in: incoming{
				Channel: "CDEFAULT", User: "U1", Mention: true, TS: "210.1",
				Text: "<@UBOT> agents-cli please",
			},
			want: decision{Action: actCreate, Channel: "CDEFAULT", ThreadTS: "210.1",
				Repo: "agents-cli", Text: "agents-cli please"},
		},
		{
			name: "no repo anywhere asks the thread",
			in: incoming{
				Channel: "CGEN", User: "U1", Mention: true, TS: "400.1",
				Text: "<@UBOT> do the thing",
			},
			want: decision{Action: actAskRepo, Channel: "CGEN", ThreadTS: "400.1", Text: "do the thing",
				Reason: "no repo in the mention, the channel default or the thread"},
		},
		{
			name: "the answer to that question needs no mention",
			in: incoming{
				Channel: "CGEN", User: "U1", TS: "400.2", ThreadTS: "400.1",
				Text: "aura",
			},
			pending: true,
			want: decision{Action: actCreate, Channel: "CGEN", ThreadTS: "400.1",
				Repo: "aura", Text: "aura"},
		},
		{
			name: "reply in a bound thread continues it",
			in: incoming{
				Channel: "CGEN", User: "U1", TS: "100.2", ThreadTS: "100.1",
				Text: "also add a test",
			},
			bound: boundSession(),
			want:  decision{Action: actContinue, Channel: "CGEN", ThreadTS: "100.1", Text: "also add a test"},
		},
		{
			name: "new forks the bound thread",
			in: incoming{
				Channel: "CGEN", User: "U1", Mention: true, TS: "100.3", ThreadTS: "100.1",
				Text: "<@UBOT> new try the other approach",
			},
			bound: boundSession(),
			want: decision{Action: actFork, Channel: "CGEN", ThreadTS: "100.1",
				Repo: "agents-cli", Text: "try the other approach"},
		},
		{
			name: "new can name a different repo",
			in: incoming{
				Channel: "CGEN", User: "U1", Mention: true, TS: "100.4", ThreadTS: "100.1",
				Text: "<@UBOT> new aura instead",
			},
			bound: boundSession(),
			want: decision{Action: actFork, Channel: "CGEN", ThreadTS: "100.1",
				Repo: "aura", Text: "aura instead"},
		},
		{
			name: "attach binds a running agent",
			in: incoming{
				Channel: "CGEN", User: "U1", Mention: true, TS: "500.1",
				Text: "<@UBOT> attach aura-9c1d",
			},
			want: decision{Action: actAttach, Channel: "CGEN", ThreadTS: "500.1",
				Agent: "aura-9c1d", Text: "attach aura-9c1d"},
		},
		{
			name: "attach without a name does nothing",
			in: incoming{
				Channel: "CGEN", User: "U1", Mention: true, TS: "500.2",
				Text: "<@UBOT> attach",
			},
			want: decision{Action: actIgnore, Channel: "CGEN", ThreadTS: "500.2",
				Text: "attach", Reason: "attach needs an agent name"},
		},
		{
			name:  "diff is a command",
			in:    incoming{Channel: "CGEN", User: "U1", TS: "100.5", ThreadTS: "100.1", Text: "diff"},
			bound: boundSession(),
			want: decision{Action: actCommand, Channel: "CGEN", ThreadTS: "100.1",
				Command: "diff", Text: "diff"},
		},
		{
			name:  "show carries a path",
			in:    incoming{Channel: "CGEN", User: "U1", TS: "100.6", ThreadTS: "100.1", Text: "show internal/ui/ui.go"},
			bound: boundSession(),
			want: decision{Action: actCommand, Channel: "CGEN", ThreadTS: "100.1",
				Command: "show", Args: "internal/ui/ui.go", Text: "show internal/ui/ui.go"},
		},
		{
			name:  "done takes a flag",
			in:    incoming{Channel: "CGEN", User: "U1", TS: "100.7", ThreadTS: "100.1", Text: "done --clean"},
			bound: boundSession(),
			want: decision{Action: actCommand, Channel: "CGEN", ThreadTS: "100.1",
				Command: "done", Args: "--clean", Text: "done --clean"},
		},
		{
			name: "a stranger is ignored",
			in: incoming{
				Channel: "CGEN", User: "U9", Mention: true, TS: "600.1",
				Text: "<@UBOT> agents-cli do something",
			},
			want: decision{Action: actIgnore, Channel: "CGEN", ThreadTS: "600.1",
				Reason: "user not on the allowlist"},
		},
		{
			name: "another bot is ignored",
			in: incoming{
				Channel: "CGEN", User: "U1", BotID: "B123", Mention: true, TS: "700.1",
				Text: "<@UBOT> agents-cli go",
			},
			want: decision{Action: actIgnore, Channel: "CGEN", ThreadTS: "700.1",
				Reason: "not a plain human message"},
		},
		{
			name: "an edit is ignored",
			in: incoming{
				Channel: "CGEN", User: "U1", SubType: "message_changed", TS: "100.8", ThreadTS: "100.1",
				Text: "also add a test",
			},
			bound: boundSession(),
			want: decision{Action: actIgnore, Channel: "CGEN", ThreadTS: "100.1",
				Reason: "not a plain human message"},
		},
		{
			name: "chatter in an unbound thread is ignored",
			in: incoming{
				Channel: "CGEN", User: "U1", TS: "800.2", ThreadTS: "800.1",
				Text: "anyone looked at this yet",
			},
			want: decision{Action: actIgnore, Channel: "CGEN", ThreadTS: "800.1",
				Text: "anyone looked at this yet", Reason: "no mention in an unbound thread"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decide(tc.in, tc.bound, tc.pending, cfg)
			if got != tc.want {
				t.Fatalf("decide()\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestRepoFromTextMatchesWholeWordsOnly(t *testing.T) {
	cfg := testConfig()
	if got := repoFromText("the aurora borealis", cfg); got != "" {
		t.Fatalf("aurora should not match aura, got %q", got)
	}
	if got := repoFromText("look at aura, please", cfg); got != "aura" {
		t.Fatalf("got %q, want aura", got)
	}
	if got := repoFromText("in agents-cli/internal", cfg); got != "agents-cli" {
		t.Fatalf("got %q, want agents-cli", got)
	}
}

func TestStripMention(t *testing.T) {
	if got := stripMention("<@U123ABC> hello <@U9> there"); got != "hello there" {
		t.Fatalf("got %q", got)
	}
}

func TestThreadKeyFallsBackToTheMessageTS(t *testing.T) {
	if got := (incoming{TS: "1.1"}).ThreadKey(); got != "1.1" {
		t.Fatalf("got %q", got)
	}
	if got := (incoming{TS: "1.2", ThreadTS: "1.1"}).ThreadKey(); got != "1.1" {
		t.Fatalf("got %q", got)
	}
}
