package slackbot

import (
	"context"
	"sync"

	"github.com/Achno2k/agents-cli/internal/herdr"
	"github.com/Achno2k/agents-cli/internal/state"
)

// --- Slack ---

type post struct {
	Channel  string
	ThreadTS string
	Text     string
	Buttons  []Button
}

type upload struct {
	Filename string
	Title    string
	Content  string
}

type fakeSlack struct {
	mu        sync.Mutex
	posts     []post
	uploads   []upload
	updates   map[string]string
	added     []string
	removed   []string
	thread    []Message
	names     map[string]string
	self      string
	threadErr error
}

func newFakeSlack() *fakeSlack {
	return &fakeSlack{
		updates: map[string]string{},
		names:   map[string]string{},
		self:    "UBOT",
	}
}

func (f *fakeSlack) PostMessage(_ context.Context, channel, threadTS, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, post{Channel: channel, ThreadTS: threadTS, Text: text})
	return "ts-post", nil
}

func (f *fakeSlack) PostButtons(_ context.Context, channel, threadTS, text string, buttons []Button) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, post{Channel: channel, ThreadTS: threadTS, Text: text, Buttons: buttons})
	return "ts-buttons", nil
}

func (f *fakeSlack) UpdateMessage(_ context.Context, _, ts, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates[ts] = text
	return nil
}

func (f *fakeSlack) UploadText(_ context.Context, _, _, filename, title, content string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, upload{Filename: filename, Title: title, Content: content})
	return nil
}

func (f *fakeSlack) AddReaction(_ context.Context, _, _, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.added = append(f.added, name)
	return nil
}

func (f *fakeSlack) RemoveReaction(_ context.Context, _, _, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, name)
	return nil
}

func (f *fakeSlack) Thread(context.Context, string, string) ([]Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.thread, f.threadErr
}

func (f *fakeSlack) UserName(_ context.Context, id string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.names[id]; ok {
		return n, nil
	}
	return id, nil
}

func (f *fakeSlack) BotUserID(context.Context) (string, error) { return f.self, nil }

// texts returns every message posted, in order.
func (f *fakeSlack) texts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.posts))
	for _, p := range f.posts {
		out = append(out, p.Text)
	}
	return out
}

// --- herdr ---

type fakeHerdr struct {
	mu       sync.Mutex
	prompts  []string
	keys     [][]string
	notices  []string
	started  []string
	killed   []string
	panes    int
	readText string
	waitCh   chan herdr.AgentState
	waitErr  error

	startErr error
	paneErr  error
	promptEr error
}

func newFakeHerdr() *fakeHerdr {
	return &fakeHerdr{waitCh: make(chan herdr.AgentState, 8)}
}

func (f *fakeHerdr) ListAgents(context.Context) ([]herdr.Agent, error) { return nil, nil }

func (f *fakeHerdr) GetAgent(_ context.Context, name string) (herdr.Agent, error) {
	return herdr.Agent{Name: name}, nil
}

func (f *fakeHerdr) NewPane(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.paneErr != nil {
		return "", f.paneErr
	}
	f.panes++
	return "w1:p9", nil
}

func (f *fakeHerdr) StartAgent(_ context.Context, name, _, _ string, _ ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, name)
	return nil
}

func (f *fakeHerdr) Prompt(_ context.Context, _, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.promptEr != nil {
		return f.promptEr
	}
	f.prompts = append(f.prompts, text)
	return nil
}

func (f *fakeHerdr) Wait(ctx context.Context, _ string, _ ...herdr.AgentState) (herdr.AgentState, error) {
	if f.waitErr != nil {
		return herdr.StateUnknown, f.waitErr
	}
	select {
	case <-ctx.Done():
		return herdr.StateUnknown, ctx.Err()
	case s := <-f.waitCh:
		return s, nil
	}
}

func (f *fakeHerdr) Read(context.Context, string, int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readText, nil
}

func (f *fakeHerdr) SendKeys(_ context.Context, _ string, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys = append(f.keys, keys)
	return nil
}

func (f *fakeHerdr) KillAgent(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.killed = append(f.killed, name)
	return nil
}

func (f *fakeHerdr) Notify(_ context.Context, _, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notices = append(f.notices, text)
	return nil
}

// --- state ---

type fakeStore struct {
	mu   sync.Mutex
	rows map[string]state.Session
}

func newFakeStore(sessions ...state.Session) *fakeStore {
	s := &fakeStore{rows: map[string]state.Session{}}
	for _, sess := range sessions {
		s.rows[sess.ID] = sess
	}
	return s
}

func (f *fakeStore) Create(_ context.Context, s state.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[s.ID]; ok {
		return state.ErrExists
	}
	for _, r := range f.rows {
		if r.SlackChannel == s.SlackChannel && r.ThreadTS == s.ThreadTS {
			return state.ErrExists
		}
	}
	f.rows[s.ID] = s
	return nil
}

func (f *fakeStore) Get(_ context.Context, id string) (state.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.rows[id]; ok {
		return s, nil
	}
	return state.Session{}, state.ErrNotFound
}

func (f *fakeStore) ByThread(_ context.Context, channel, ts string) (state.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.rows {
		if s.SlackChannel == channel && s.ThreadTS == ts {
			return s, nil
		}
	}
	return state.Session{}, state.ErrNotFound
}

func (f *fakeStore) ByAgent(_ context.Context, name string) (state.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.rows {
		if s.AgentName == name {
			return s, nil
		}
	}
	return state.Session{}, state.ErrNotFound
}

func (f *fakeStore) List(context.Context) ([]state.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]state.Session, 0, len(f.rows))
	for _, s := range f.rows {
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeStore) Update(_ context.Context, s state.Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[s.ID]; !ok {
		return state.ErrNotFound
	}
	f.rows[s.ID] = s
	return nil
}

func (f *fakeStore) Delete(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
	return nil
}

func (f *fakeStore) Close() error { return nil }
