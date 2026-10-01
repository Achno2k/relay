package slackbot

import (
	"regexp"
	"strings"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/state"
)

// action is what the router decided to do with one incoming message.
type action int

const (
	// actIgnore drops the message without a reply. Used for bot chatter,
	// edits, non-allowlisted users and ordinary talk in unbound threads.
	actIgnore      action = iota
	actCreate             // mention in an unbound thread: new session
	actFork               // "@bot new": fresh session, rebind the thread
	actAttach             // "@bot attach <agent>": bind to a running agent
	actContinue           // reply in a bound thread: follow-up prompt
	actCommand            // diff | show | full | pr | done
	actAskRepo            // no repo could be resolved; ask in the thread
	actRefuseAtCap        // MaxLive reached
)

func (a action) String() string {
	switch a {
	case actCreate:
		return "create"
	case actFork:
		return "fork"
	case actAttach:
		return "attach"
	case actContinue:
		return "continue"
	case actCommand:
		return "command"
	case actAskRepo:
		return "ask_repo"
	case actRefuseAtCap:
		return "refuse_at_cap"
	default:
		return "ignore"
	}
}

// incoming is a Slack event flattened to what the router needs. It covers both
// app_mention and message events.
type incoming struct {
	Channel  string
	User     string
	Text     string
	TS       string
	ThreadTS string // "" for a top level message
	BotID    string
	SubType  string
	Mention  bool // true when this arrived as app_mention

	// ThreadText is the concatenated text of the thread so far. The router
	// fills it in only for unbound mentions, where it is the third place we
	// look for a repo name.
	ThreadText string
}

// ThreadKey is the thread a message belongs to. A top level message is the
// root of the thread its reply would create.
func (in incoming) ThreadKey() string {
	if in.ThreadTS != "" {
		return in.ThreadTS
	}
	return in.TS
}

// decision is the router's verdict. It is a plain value so the routing rules
// can be tested without Slack, herdr or a database.
type decision struct {
	Action   action
	Channel  string
	ThreadTS string
	Repo     string // resolved repo short name
	Agent    string // target agent name for attach
	Command  string // diff | show | full | pr | done
	Args     string // command argument: a path, or "--clean"
	Text     string // user text with the bot mention stripped
	Reason   string // why, for logs and for ignore cases
}

var (
	mentionRE = regexp.MustCompile(`<@[A-Z0-9]+>`)
	runOfWS   = regexp.MustCompile(`[ \t]{2,}`)
)

// stripMention removes "<@U123>" mentions and closes the gaps they leave.
// Newlines survive, so a multi line instruction still reads as one.
func stripMention(s string) string {
	s = mentionRE.ReplaceAllString(s, "")
	return strings.TrimSpace(runOfWS.ReplaceAllString(s, " "))
}

// commands the bot answers inside a bound thread.
var threadCommands = map[string]bool{
	"diff": true, "show": true, "full": true, "pr": true, "done": true,
}

// decide maps one incoming message to an action. bound is the session already
// attached to this thread, or nil. pending is true when the bot has asked this
// thread for a repo name and is waiting for the answer.
//
// It is pure: no IO, no clock, no randomness.
func decide(in incoming, bound *state.Session, pending bool, cfg config.Config) decision {
	d := decision{Channel: in.Channel, ThreadTS: in.ThreadKey()}

	// Never react to ourselves or to other bots, and never to edits, joins,
	// deletions or any other subtyped message.
	if in.BotID != "" || in.SubType != "" {
		d.Reason = "not a plain human message"
		return d
	}
	if in.User == "" {
		d.Reason = "no author"
		return d
	}
	if !allowed(in.User, cfg) {
		d.Reason = "user not on the allowlist"
		return d
	}

	text := stripMention(in.Text)
	d.Text = text
	word, rest := firstWord(text)

	if bound != nil {
		switch {
		case threadCommands[word]:
			d.Action, d.Command, d.Args = actCommand, word, rest
		case word == "new":
			d.Action, d.Repo = actFork, bound.Repo
			if r := repoFromText(rest, cfg); r != "" {
				d.Repo = r
			}
			d.Text = rest
		case word == "attach":
			d.Action, d.Agent = actAttach, firstToken(rest)
			if d.Agent == "" {
				d.Action, d.Reason = actIgnore, "attach needs an agent name"
			}
		case in.Mention || in.ThreadTS != "":
			d.Action = actContinue
		default:
			d.Reason = "message outside the bound thread"
		}
		return d
	}

	// Unbound thread. Only a mention starts anything, except when we already
	// asked this thread which repo to use.
	if !in.Mention && !pending {
		d.Reason = "no mention in an unbound thread"
		return d
	}

	if word == "attach" {
		if agent := firstToken(rest); agent != "" {
			d.Action, d.Agent = actAttach, agent
			return d
		}
		d.Reason = "attach needs an agent name"
		return d
	}

	instruction := text
	if word == "new" {
		instruction = rest
	}
	repo := resolveRepo(text, in.Channel, in.ThreadText, cfg)
	if repo == "" {
		d.Action, d.Reason = actAskRepo, "no repo in the mention, the channel default or the thread"
		return d
	}
	d.Action, d.Repo, d.Text = actCreate, repo, strings.TrimSpace(instruction)
	return d
}

// resolveRepo looks for a repo in the order the plan fixes: named in the
// mention, then the channel default, then a repo name somewhere in the thread.
func resolveRepo(text, channel, threadText string, cfg config.Config) string {
	if r := repoFromText(text, cfg); r != "" {
		return r
	}
	// "aura" should still find a repo configured as "aura-repo".
	if r := repoByTrimmedName(text, cfg); r != "" {
		return r
	}
	if r := cfg.Slack.DefaultRepoByChannel[channel]; r != "" {
		return r
	}
	return repoFromText(threadText, cfg)
}

// repoFromText returns the first configured repo name that appears in s as a
// whole word. Matching is case insensitive.
func repoFromText(s string, cfg config.Config) string {
	if s == "" || len(cfg.Repos) == 0 {
		return ""
	}
	best, bestAt := "", -1
	for name := range cfg.Repos {
		for _, alias := range repoAliases(name) {
			at := wordIndex(s, alias)
			if at < 0 {
				continue
			}
			if bestAt < 0 || at < bestAt || (at == bestAt && len(name) > len(best)) {
				best, bestAt = name, at
			}
		}
	}
	return best
}

// repoAliases is what counts as naming a repo: the configured name, and the
// same name with a trailing "repo" word taken off. People write "the aura repo"
// and they configure "aura-repo", and both should land on the same session.
// Matching itself is case insensitive, which wordIndex handles.
func repoAliases(name string) []string {
	aliases := []string{name}
	if trimmed := trimRepoWord(name); trimmed != "" && trimmed != name {
		aliases = append(aliases, trimmed)
	}
	return aliases
}

// trimRepoWord drops a trailing "repo" that is a word of its own, so
// "aura repo", "aura-repo" and "aura_repo" all become "aura". A name that is
// only the word "repo" is left alone: there would be nothing left of it.
func trimRepoWord(s string) string {
	t := strings.TrimRight(s, " \t")
	if len(t) < len("repo") || !strings.EqualFold(t[len(t)-4:], "repo") {
		return t
	}
	head := t[:len(t)-4]
	if head == "" {
		return t
	}
	switch head[len(head)-1] {
	case ' ', '-', '_':
		return strings.TrimRight(head[:len(head)-1], " \t-_")
	}
	return t
}

// repoByTrimmedName matches a bare answer such as "aura" against configured
// names once a trailing "repo" word is taken off each side.
func repoByTrimmedName(s string, cfg config.Config) string {
	want := trimRepoWord(strings.TrimSpace(s))
	if want == "" {
		return ""
	}
	for name := range cfg.Repos {
		if strings.EqualFold(trimRepoWord(name), want) {
			return name
		}
	}
	return ""
}

// wordIndex finds needle in s on a word boundary, case insensitively.
func wordIndex(s, needle string) int {
	if needle == "" {
		return -1
	}
	ls, ln := strings.ToLower(s), strings.ToLower(needle)
	for from := 0; ; {
		i := strings.Index(ls[from:], ln)
		if i < 0 {
			return -1
		}
		i += from
		if isBoundary(ls, i-1) && isBoundary(ls, i+len(ln)) {
			return i
		}
		from = i + 1
		if from >= len(ls) {
			return -1
		}
	}
}

func isBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	return !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_')
}

// allowed reports whether the Slack user may drive the bot. An empty allowlist
// means the bot is closed, not open: `agents init` writes the owner into it.
func allowed(userID string, cfg config.Config) bool {
	for _, id := range cfg.Slack.AllowedUserIDs {
		if id == userID {
			return true
		}
	}
	return false
}

func firstWord(s string) (word, rest string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t\n")
	if i < 0 {
		return strings.ToLower(s), ""
	}
	return strings.ToLower(s[:i]), strings.TrimSpace(s[i:])
}

func firstToken(s string) string {
	w, _ := firstWord(s)
	return w
}
