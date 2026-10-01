package slackbot

import (
	"testing"

	"github.com/slack-go/slack/slackevents"

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

// A direct message has nobody to mention, so toIncoming marks it as one.
// Without that the router treats it as chatter in an unbound thread and drops
// it, which is how DMs to the bot went unanswered.
func TestDirectMessagesCountAsMentions(t *testing.T) {
	cfg := testConfig()

	cases := []struct {
		name string
		in   incoming
		want decision
	}{
		{
			name: "top level DM starts a session keyed by its own ts",
			in: incoming{
				Channel: "D123", User: "U1", Mention: true, TS: "700.1",
				Text: "fix the login bug in agents-cli",
			},
			want: decision{Action: actCreate, Channel: "D123", ThreadTS: "700.1",
				Repo: "agents-cli", Text: "fix the login bug in agents-cli"},
		},
		{
			name: "a DM with no repo still asks rather than ignoring",
			in: incoming{
				Channel: "D123", User: "U1", Mention: true, TS: "701.1",
				Text: "have a look",
			},
			want: decision{Action: actAskRepo, Channel: "D123", ThreadTS: "701.1",
				Text:   "have a look",
				Reason: "no repo in the mention, the channel default or the thread"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decide(tc.in, nil, false, cfg); got != tc.want {
				t.Fatalf("decide()\n got %+v\nwant %+v", got, tc.want)
			}
		})
	}

	// Once the thread is bound, a reply carries on without a mention, exactly
	// as it does in a channel.
	bound := boundSession()
	bound.SlackChannel, bound.ThreadTS = "D123", "700.1"
	reply := incoming{
		Channel: "D123", User: "U1", Mention: true, TS: "700.2", ThreadTS: "700.1",
		Text: "also add a test",
	}
	want := decision{Action: actContinue, Channel: "D123", ThreadTS: "700.1", Text: "also add a test"}
	if got := decide(reply, bound, false, cfg); got != want {
		t.Fatalf("reply in a bound DM thread:\n got %+v\nwant %+v", got, want)
	}

	// Commands work the same in a DM.
	cmd := incoming{
		Channel: "D123", User: "U1", Mention: true, TS: "700.3", ThreadTS: "700.1",
		Text: "diff",
	}
	wantCmd := decision{Action: actCommand, Channel: "D123", ThreadTS: "700.1",
		Command: "diff", Text: "diff"}
	if got := decide(cmd, bound, false, cfg); got != wantCmd {
		t.Fatalf("command in a DM:\n got %+v\nwant %+v", got, wantCmd)
	}
}

func TestIsDM(t *testing.T) {
	cases := []struct {
		channelType, channel string
		want                 bool
	}{
		{"im", "D123", true},
		{"im", "", true},       // channel_type is the authority
		{"", "D0ABC123", true}, // fallback for payloads without it
		{"channel", "CGEN", false},
		{"group", "GPRIV", false},
		{"", "CGEN", false},
		{"mpim", "G123", false}, // a group DM is not a one to one DM
	}
	for _, tc := range cases {
		if got := isDM(tc.channelType, tc.channel); got != tc.want {
			t.Fatalf("isDM(%q, %q) = %v, want %v", tc.channelType, tc.channel, got, tc.want)
		}
	}
}

// A message event in a normal channel must still need a real mention.
func TestChannelMessagesAreNotTreatedAsMentions(t *testing.T) {
	got := decide(incoming{
		Channel: "CGEN", User: "U1", TS: "800.2", ThreadTS: "800.1",
		Text: "anyone looked at this yet",
	}, nil, false, testConfig())

	if got.Action != actIgnore {
		t.Fatalf("channel chatter should still be ignored, got %+v", got)
	}
}

// The router tests build an incoming directly, so this covers the wiring that
// actually sets Mention on a real Slack payload.
func TestToIncomingMarksDirectMessages(t *testing.T) {
	dm := slackevents.EventsAPIEvent{InnerEvent: slackevents.EventsAPIInnerEvent{
		Data: &slackevents.MessageEvent{
			Channel: "D123", ChannelType: "im", User: "U1",
			Text: "fix the login bug", TimeStamp: "700.1",
		},
	}}
	got, ok := toIncoming(dm)
	if !ok || !got.Mention {
		t.Fatalf("a DM should arrive as a mention: %+v (ok=%v)", got, ok)
	}

	channel := slackevents.EventsAPIEvent{InnerEvent: slackevents.EventsAPIInnerEvent{
		Data: &slackevents.MessageEvent{
			Channel: "CGEN", ChannelType: "channel", User: "U1",
			Text: "just chatting", TimeStamp: "800.1",
		},
	}}
	got, ok = toIncoming(channel)
	if !ok || got.Mention {
		t.Fatalf("channel chatter is not a mention: %+v (ok=%v)", got, ok)
	}

	// An app_mention is a mention wherever it lands.
	mention := slackevents.EventsAPIEvent{InnerEvent: slackevents.EventsAPIInnerEvent{
		Data: &slackevents.AppMentionEvent{
			Channel: "CGEN", User: "U1", Text: "<@UBOT> go", TimeStamp: "800.1",
		},
	}}
	if got, ok := toIncoming(mention); !ok || !got.Mention {
		t.Fatalf("app_mention should stay a mention: %+v (ok=%v)", got, ok)
	}
}
