package slackbot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/Achno2k/agents-cli/internal/config"
	"github.com/Achno2k/agents-cli/internal/herdr"
	"github.com/Achno2k/agents-cli/internal/state"
	"github.com/Achno2k/agents-cli/internal/worktree"
)

// Bot is the Socket Mode bot. Everything it touches is an interface, so the
// router, prompt builder and watcher can be driven by fakes in tests.
type Bot struct {
	Slack  SlackClient
	Herdr  herdr.Client
	Store  state.Store
	Config config.Config
	Log    *log.Logger

	// Socket is the live Socket Mode connection. Tests leave it nil and feed
	// events straight to HandleEvent.
	Socket *socketmode.Client

	// Reload re-reads the config file. It runs before every event, so a repo
	// added by `agents init` after the bot started, an allowlist change or a
	// new channel default all take effect without a restart. Nil disables the
	// reload and pins whatever Config holds, which is what tests want.
	Reload func() (config.Config, error)

	cfgMu sync.RWMutex

	botUserID string

	mu       sync.Mutex
	watchers map[string]*watcherHandle
	pending  map[string]bool // "channel/threadTS" waiting for a repo name
}

type watcherHandle struct {
	w      *watcher
	cancel context.CancelFunc
}

// ErrNoTokens is returned when the bot's two Slack tokens are not in the
// environment. systemd puts them there from ~/.agents/slack.env.
var ErrNoTokens = errors.New("slackbot: SLACK_BOT_TOKEN and SLACK_APP_TOKEN must be set")

// New builds a bot from already constructed dependencies.
func New(sc SlackClient, h herdr.Client, st state.Store, cfg config.Config, logger *log.Logger) *Bot {
	if logger == nil {
		logger = log.New(os.Stderr, "slackbot ", log.LstdFlags)
	}
	return &Bot{
		Slack:    sc,
		Herdr:    h,
		Store:    st,
		Config:   cfg,
		Log:      logger,
		watchers: map[string]*watcherHandle{},
		pending:  map[string]bool{},
	}
}

// NewFromEnv builds a bot wired to the real Slack, reading SLACK_BOT_TOKEN and
// SLACK_APP_TOKEN.
func NewFromEnv(h herdr.Client, st state.Store, cfg config.Config, logger *log.Logger) (*Bot, error) {
	botToken, appToken := os.Getenv("SLACK_BOT_TOKEN"), os.Getenv("SLACK_APP_TOKEN")
	if botToken == "" || appToken == "" {
		return nil, ErrNoTokens
	}
	if !strings.HasPrefix(appToken, "xapp-") {
		return nil, errors.New("slackbot: SLACK_APP_TOKEN must be an app level token starting with xapp-")
	}
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	b := New(NewSlackClient(api), h, st, cfg, logger)
	b.Socket = socketmode.New(api)
	b.Reload = config.Load
	return b, nil
}

// config returns the current configuration. Everything that reads config goes
// through here so a reload is picked up mid-flight.
func (b *Bot) config() config.Config {
	b.cfgMu.RLock()
	defer b.cfgMu.RUnlock()
	return b.Config
}

// reloadConfig re-reads the config file. A read failure is logged once and the
// previous configuration is kept: a half written file should not take the bot
// down or silently empty the allowlist.
func (b *Bot) reloadConfig() {
	if b.Reload == nil {
		return
	}
	cfg, err := b.Reload()
	if err != nil {
		b.logf("reload config: %v", err)
		return
	}
	b.cfgMu.Lock()
	b.Config = cfg
	b.cfgMu.Unlock()
}

func (b *Bot) logf(format string, args ...any) {
	if b.Log != nil {
		b.Log.Printf(format, args...)
	}
}

// Run connects to Slack and serves until ctx ends.
func (b *Bot) Run(ctx context.Context) error {
	if b.Socket == nil {
		return errors.New("slackbot: no Socket Mode connection")
	}
	id, err := b.Slack.BotUserID(ctx)
	if err != nil {
		return fmt.Errorf("slackbot: auth.test: %w", err)
	}
	b.botUserID = id

	b.resumeWatchers(ctx)
	go b.sweep(ctx)

	errc := make(chan error, 1)
	go func() { errc <- b.Socket.RunContext(ctx) }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errc:
			return err
		case evt := <-b.Socket.Events:
			b.handleSocketEvent(ctx, evt)
		}
	}
}

func (b *Bot) handleSocketEvent(ctx context.Context, evt socketmode.Event) {
	switch evt.Type {
	case socketmode.EventTypeConnected:
		b.logf("connected to slack as %s", b.botUserID)
	case socketmode.EventTypeEventsAPI:
		api, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return
		}
		if evt.Request != nil {
			b.Socket.Ack(*evt.Request)
		}
		if in, ok := toIncoming(api); ok {
			b.HandleMessage(ctx, in)
		}
	case socketmode.EventTypeInteractive:
		cb, ok := evt.Data.(slack.InteractionCallback)
		if !ok {
			return
		}
		if evt.Request != nil {
			b.Socket.Ack(*evt.Request)
		}
		b.HandleInteraction(ctx, cb)
	}
}

// toIncoming flattens the two event shapes the bot subscribes to.
func toIncoming(api slackevents.EventsAPIEvent) (incoming, bool) {
	switch e := api.InnerEvent.Data.(type) {
	case *slackevents.AppMentionEvent:
		return incoming{
			Channel:  e.Channel,
			User:     e.User,
			Text:     e.Text,
			TS:       e.TimeStamp,
			ThreadTS: e.ThreadTimeStamp,
			BotID:    e.BotID,
			Mention:  true,
		}, true
	case *slackevents.MessageEvent:
		return incoming{
			Channel:  e.Channel,
			User:     e.User,
			Text:     e.Text,
			TS:       e.TimeStamp,
			ThreadTS: e.ThreadTimeStamp,
			BotID:    e.BotID,
			SubType:  e.SubType,
		}, true
	}
	return incoming{}, false
}

// HandleMessage routes one message. It is exported so tests can drive the bot
// without a Socket Mode connection.
func (b *Bot) HandleMessage(ctx context.Context, in incoming) {
	if in.User != "" && in.User == b.botUserID {
		return
	}
	// Pick up a repo, allowlist entry or channel default added since the last
	// event. The file is small and events are rare, so re-reading it beats
	// making the user restart the bot after `agents init`.
	b.reloadConfig()
	cfg := b.config()

	threadTS := in.ThreadKey()
	bound := b.sessionForThread(ctx, in.Channel, threadTS)

	// A mention in a thread we do not know yet is the one case where the repo
	// might be named further up the thread.
	if bound == nil && in.Mention && in.ThreadTS != "" {
		in.ThreadText = b.threadText(ctx, in.Channel, threadTS)
	}

	d := decide(in, bound, b.isPending(in.Channel, threadTS), cfg)
	if d.Action != actIgnore {
		b.logf("session=%s action=%s repo=%s", sessionID(bound), d.Action, d.Repo)
	}

	switch d.Action {
	case actIgnore:
		return
	case actAskRepo:
		b.askRepo(ctx, d, cfg)
	case actCreate:
		b.clearPending(d.Channel, d.ThreadTS)
		b.createSession(ctx, d, nil)
	case actFork:
		b.createSession(ctx, d, bound)
	case actAttach:
		b.attachSession(ctx, d, bound)
	case actContinue:
		if bound != nil {
			b.continueSession(ctx, d, *bound)
		}
	case actCommand:
		if bound != nil {
			b.runCommand(ctx, d, *bound)
		}
	}
}

func sessionID(s *state.Session) string {
	if s == nil {
		return "-"
	}
	return s.ID
}

func (b *Bot) sessionForThread(ctx context.Context, channel, threadTS string) *state.Session {
	s, err := b.Store.ByThread(ctx, channel, threadTS)
	if err != nil {
		return nil
	}
	if !s.Live() {
		return nil
	}
	return &s
}

func (b *Bot) threadText(ctx context.Context, channel, threadTS string) string {
	msgs, err := b.Slack.Thread(ctx, channel, threadTS)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, m := range msgs {
		if m.FromBot() {
			continue
		}
		sb.WriteString(m.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

func (b *Bot) pendingKey(channel, threadTS string) string { return channel + "/" + threadTS }

func (b *Bot) isPending(channel, threadTS string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pending[b.pendingKey(channel, threadTS)]
}

func (b *Bot) setPending(channel, threadTS string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending[b.pendingKey(channel, threadTS)] = true
}

func (b *Bot) clearPending(channel, threadTS string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, b.pendingKey(channel, threadTS))
}

// askRepo asks the thread which repo to work in and remembers that the next
// message is the answer.
//
// If the thread was already waiting, this reply was meant to be that answer and
// did not name a repo we know. Say so, once per attempt: staying silent left
// the user typing into a thread that only logged ask_repo and never spoke.
func (b *Bot) askRepo(ctx context.Context, d decision, cfg config.Config) {
	if b.isPending(d.Channel, d.ThreadTS) {
		word := firstField(d.Text)
		if word == "" {
			return
		}
		b.say(ctx, d.Channel, d.ThreadTS, unknownRepoMessage(word, repoNamesOf(cfg)))
		return
	}
	b.setPending(d.Channel, d.ThreadTS)
	msg := "Which repo? I don't have a default for this channel."
	if known := repoNamesOf(cfg); len(known) > 0 {
		msg += " I know: " + strings.Join(known, ", ") + "."
	}
	b.say(ctx, d.Channel, d.ThreadTS, msg)
}

// unknownRepoMessage names what the user typed and what is actually on the box,
// so the next thing they try is a name that works.
func unknownRepoMessage(word string, known []string) string {
	msg := "I don't know " + word + "."
	if len(known) > 0 {
		msg += " Repos on this box: " + strings.Join(known, ", ") + "."
	} else {
		msg += " There are no repos on this box yet."
	}
	return msg + " Run `agents init` from that repo to add it."
}

func (b *Bot) repoNames() []string { return repoNamesOf(b.config()) }

func repoNamesOf(cfg config.Config) []string {
	repos := cfg.Repos
	out := make([]string, 0, len(repos))
	for name := range repos {
		out = append(out, name)
	}
	sortStrings(out)
	return out
}

func (b *Bot) say(ctx context.Context, channel, threadTS, text string) {
	if _, err := b.Slack.PostMessage(ctx, channel, threadTS, text); err != nil {
		b.logf("post to %s/%s: %v", channel, threadTS, err)
	}
}

// harnessKind is the harness the bot starts. The plan lists harnesses in
// preference order, so the first one wins.
func (b *Bot) harnessKind() string {
	if h := b.config().Harness; len(h) > 0 && h[0] != "" {
		return h[0]
	}
	return "claude"
}

func (b *Bot) workDir() string {
	if w := b.config().Box.WorkDir; w != "" {
		return w
	}
	return config.Default().Box.WorkDir
}

// repoDir is the primary checkout worktrees branch from:
// <work_dir>/<repo>/main.
func (b *Bot) repoDir(repo string) string {
	return worktree.Path(b.workDir(), repo, "main")
}

// liveCount is how many sessions still hold a running agent.
func (b *Bot) liveCount(ctx context.Context) int {
	all, err := b.Store.List(ctx)
	if err != nil {
		return 0
	}
	n := 0
	for _, s := range all {
		if s.Live() {
			n++
		}
	}
	return n
}

func (b *Bot) maxLive() int {
	if n := b.config().Sessions.MaxLive; n > 0 {
		return n
	}
	return config.Default().Sessions.MaxLive
}

// createSession builds the worktree, starts the agent and sends the first
// prompt. old is non-nil for a fork, whose thread is rebound to the new
// session and whose worktree is left alone.
func (b *Bot) createSession(ctx context.Context, d decision, old *state.Session) {
	if n := b.liveCount(ctx); n >= b.maxLive() {
		b.say(ctx, d.Channel, d.ThreadTS, fmt.Sprintf(
			"%d sessions are already live, which is the cap. Finish one with `done` first.", n))
		return
	}
	repoCfg, ok := b.config().Repos[d.Repo]
	if !ok {
		b.say(ctx, d.Channel, d.ThreadTS, "I don't know a repo called `"+d.Repo+"`.")
		return
	}
	_ = repoCfg

	// A fork releases the thread first so the unique index on (channel,
	// thread) does not reject the new row, and stops the old agent so two
	// sessions are not left running against one thread. Its worktree stays.
	if old != nil {
		b.stopWatcher(old.ID)
		if err := b.Herdr.KillAgent(ctx, old.AgentName); err != nil {
			b.logf("kill forked-from %s: %v", old.AgentName, err)
		}
		b.unbindThread(ctx, *old)
	}

	id := state.NewID()
	sess := state.Session{
		ID:           id,
		AgentName:    d.Repo + "-" + id,
		Kind:         b.harnessKind(),
		Repo:         d.Repo,
		Branch:       worktree.Branch(id),
		SlackChannel: d.Channel,
		ThreadTS:     d.ThreadTS,
		Status:       state.StatusStarting,
		CreatedAt:    time.Now(),
		LastActive:   time.Now(),
	}

	path, err := worktree.Create(ctx, b.repoDir(d.Repo), b.workDir(), d.Repo, id)
	if err != nil {
		b.say(ctx, d.Channel, d.ThreadTS, "Could not make a worktree: "+err.Error())
		return
	}
	sess.WorktreePath = path

	// The reply file lives inside the worktree, so keep it out of every diff.
	if err := excludeAgentsDir(ctx, path); err != nil {
		b.logf("exclude .agents in %s: %v", path, err)
	}

	// Claude Code asks whether to trust a new folder before doing anything,
	// and every session is a new folder. Accept it up front.
	if sess.Kind == "claude" {
		if err := trustWorktree(path); err != nil {
			b.logf("trust worktree %s: %v", path, err)
		}
	}

	paneID, err := b.Herdr.NewPane(ctx, path)
	if err != nil {
		b.say(ctx, d.Channel, d.ThreadTS, "herdr could not open a pane: "+err.Error())
		return
	}
	extra := b.config().HarnessArgs[sess.Kind]
	if err := b.Herdr.StartAgent(ctx, sess.AgentName, sess.Kind, paneID, extra...); err != nil {
		b.say(ctx, d.Channel, d.ThreadTS, "herdr could not start "+sess.Kind+": "+err.Error())
		return
	}
	if err := b.Store.Create(ctx, sess); err != nil {
		// Without a row nothing can drive this agent, so do not leave it running.
		if killErr := b.Herdr.KillAgent(ctx, sess.AgentName); killErr != nil {
			b.logf("kill orphaned %s: %v", sess.AgentName, killErr)
		}
		b.say(ctx, d.Channel, d.ThreadTS, "Could not record the session: "+err.Error())
		return
	}

	b.say(ctx, d.Channel, d.ThreadTS, fmt.Sprintf("Started `%s` on `%s` in `%s`.",
		sess.AgentName, sess.Branch, sess.WorktreePath))

	w := b.startWatcher(ctx, sess)
	b.sendPrompt(ctx, sess, d.Text, w)
}

// unbindThread detaches an old session from its thread so a fork can take it.
// Clearing thread_ts drops the row out of the partial unique index, which is
// the same shape a session started from the terminal has.
func (b *Bot) unbindThread(ctx context.Context, s state.Session) {
	b.mu.Lock()
	defer b.mu.Unlock()
	cur, err := b.Store.Get(ctx, s.ID)
	if err != nil {
		return
	}
	cur.ThreadTS = ""
	if err := b.Store.Update(ctx, cur); err != nil {
		b.logf("unbind thread for %s: %v", s.ID, err)
	}
}

// attachSession binds this thread to an agent that is already running.
func (b *Bot) attachSession(ctx context.Context, d decision, bound *state.Session) {
	if bound != nil {
		b.say(ctx, d.Channel, d.ThreadTS, "This thread is already `"+bound.AgentName+"`. Say `new` for a fresh session.")
		return
	}
	sess, err := b.Store.ByAgent(ctx, d.Agent)
	if err != nil {
		b.say(ctx, d.Channel, d.ThreadTS, "I don't have a session called `"+d.Agent+"`.")
		return
	}
	if !sess.Live() {
		b.say(ctx, d.Channel, d.ThreadTS, "`"+d.Agent+"` is "+string(sess.Status)+", not running.")
		return
	}
	b.mu.Lock()
	sess.SlackChannel, sess.ThreadTS, sess.LastActive = d.Channel, d.ThreadTS, time.Now()
	err = b.Store.Update(ctx, sess)
	b.mu.Unlock()
	if err != nil {
		b.say(ctx, d.Channel, d.ThreadTS, "Could not bind that session: "+err.Error())
		return
	}
	b.clearPending(d.Channel, d.ThreadTS)
	b.say(ctx, d.Channel, d.ThreadTS, "This thread now drives `"+sess.AgentName+"`.")
	b.startWatcher(ctx, sess)
}

// continueSession forwards the new part of the thread to a running agent.
func (b *Bot) continueSession(ctx context.Context, d decision, sess state.Session) {
	w := b.watcherFor(sess.ID)
	if w == nil {
		w = b.startWatcher(ctx, sess)
	}
	b.sendPrompt(ctx, sess, d.Text, w)
}

// sendPrompt builds the prompt from the thread and hands it to the agent.
func (b *Bot) sendPrompt(ctx context.Context, sess state.Session, instruction string, w *watcher) {
	msgs, err := b.Slack.Thread(ctx, sess.SlackChannel, sess.ThreadTS)
	if err != nil {
		b.logf("read thread %s: %v", sess.ThreadTS, err)
	}
	names := b.resolveNames(ctx, msgs)
	text := BuildPrompt(PromptInput{
		Messages:     msgs,
		Names:        names,
		Instruction:  instruction,
		Since:        sess.LastSentTS,
		WorktreePath: sess.WorktreePath,
	})

	if w != nil {
		w.noteTurnStart()
	}
	if err := b.Herdr.Prompt(ctx, sess.AgentName, text); err != nil {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "Could not reach the agent: "+err.Error())
		return
	}
	// herdr notifications are client wide rather than per pane, so the agent
	// name has to be in the text for it to mean anything.
	_ = b.Herdr.Notify(ctx, sess.AgentName, sess.AgentName+": new Slack message")

	if latest := LatestTS(msgs); latest != "" {
		b.mu.Lock()
		if cur, err := b.Store.Get(ctx, sess.ID); err == nil {
			cur.LastSentTS = latest
			cur.LastActive = time.Now()
			cur.Status = state.StatusWorking
			if err := b.Store.Update(ctx, cur); err != nil {
				b.logf("update last_sent for %s: %v", sess.ID, err)
			}
		}
		b.mu.Unlock()
	}
}

// resolveNames looks up every author in the thread once.
func (b *Bot) resolveNames(ctx context.Context, msgs []Message) map[string]string {
	names := map[string]string{}
	for _, m := range msgs {
		if m.UserID == "" || names[m.UserID] != "" {
			continue
		}
		n, err := b.Slack.UserName(ctx, m.UserID)
		if err != nil || n == "" {
			n = m.UserID
		}
		names[m.UserID] = n
	}
	return names
}

// HandleInteraction answers an approval button click.
func (b *Bot) HandleInteraction(ctx context.Context, cb slack.InteractionCallback) {
	if len(cb.ActionCallback.BlockActions) == 0 {
		return
	}
	b.reloadConfig()
	act := cb.ActionCallback.BlockActions[0]
	if !isApprovalAction(act.ActionID) {
		return
	}
	if !allowed(cb.User.ID, b.config()) {
		return
	}
	sess, err := b.Store.Get(ctx, act.Value)
	if err != nil {
		b.logf("approval for unknown session %s", act.Value)
		return
	}
	keys := approvalKeys(sess.Kind, act.ActionID)
	if len(keys) == 0 {
		return
	}
	if err := b.Herdr.SendKeys(ctx, sess.AgentName, keys...); err != nil {
		b.say(ctx, sess.SlackChannel, sess.ThreadTS, "Could not answer the prompt: "+err.Error())
		return
	}
	// Replace the buttons so the decision cannot be double clicked.
	name, _ := b.Slack.UserName(ctx, cb.User.ID)
	if name == "" {
		name = cb.User.ID
	}
	text := approvalLabel(act.ActionID) + " by " + name + "."
	if cb.Message.Timestamp != "" {
		if err := b.Slack.UpdateMessage(ctx, sess.SlackChannel, cb.Message.Timestamp, text); err != nil {
			b.logf("update approval message: %v", err)
		}
		return
	}
	b.say(ctx, sess.SlackChannel, sess.ThreadTS, text)
}

// startWatcher launches the goroutine that mirrors a session onto its thread.
func (b *Bot) startWatcher(ctx context.Context, sess state.Session) *watcher {
	b.mu.Lock()
	defer b.mu.Unlock()
	if h, ok := b.watchers[sess.ID]; ok {
		h.w.session = sess
		return h.w
	}
	wctx, cancel := context.WithCancel(ctx)
	w := newWatcher(b, sess)
	b.watchers[sess.ID] = &watcherHandle{w: w, cancel: cancel}
	go w.run(wctx)
	return w
}

func (b *Bot) watcherFor(id string) *watcher {
	b.mu.Lock()
	defer b.mu.Unlock()
	if h, ok := b.watchers[id]; ok {
		return h.w
	}
	return nil
}

func (b *Bot) stopWatcher(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if h, ok := b.watchers[id]; ok {
		h.cancel()
		delete(b.watchers, id)
	}
}

// resumeWatchers picks up sessions that outlived the last bot process.
func (b *Bot) resumeWatchers(ctx context.Context) {
	all, err := b.Store.List(ctx)
	if err != nil {
		b.logf("list sessions: %v", err)
		return
	}
	for _, s := range all {
		if s.Live() && s.ThreadTS != "" {
			b.startWatcher(ctx, s)
		}
	}
}

// sortStrings is a tiny insertion sort so config maps render in a stable order
// without pulling in sort for one call site.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
