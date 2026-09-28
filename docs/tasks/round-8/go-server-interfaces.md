# What go-server consumes

Agreed: go-live (plus `live.NewMonitorWith(h, hub, tracker, interval)`), go-drivers (Apply returns the pane id).

go-server owns `internal/server` (routes, middleware, WS, `EventHub`) and `internal/service` (AgentService, AgentMonitor, app wiring in `service.Run`). It imports the packages below. Names are proposals: reply with changes, keep them small.

Conventions
- Optional wire strings stay `*string` in `internal/api`. Internal "no value" is `""` (cwd, etc.).
- Anything that emits WS events takes a small interface it declares itself:
  ```go
  type Hub interface { Broadcast(api.ServerEvent); Count() int } // Count = connected /ws clients
  ```
  `*server.Hub` satisfies it. Use go-core's event type name from `internal/api`.
- Long-running parts expose `Run(ctx context.Context)` and return when `ctx` is done.
- Errors meant for the client are `*api.Error` (go-core). Anything else from herdr goes through go-core's herdr→api mapping in the error middleware.

## go-transcripts: `internal/transcript`
```go
type Format string // "claude" | "pi" | "codex"
type Ref struct { Path string; Format Format; Cwd string }

func NewLocator(claudeProjects string, codex *CodexRollouts) *Locator // "" = defaults (~/.claude/projects, ~/.codex/sessions)
func (l *Locator) Locate(a herdr.Agent, notBefore time.Time) *Ref     // zero notBefore = no bound; nil = none
func Parse(data []byte, f Format, cwd string, up *uploads.Store) []api.Message

func NewTailer(ref Ref, up *uploads.Store, onMessage func(api.Message)) *Tailer
func (t *Tailer) Start()
func (t *Tailer) Stop()
func (t *Tailer) Path() string
func (t *Tailer) Dead() bool

func NewScrubber(cwd string) Scrubber   // PathScrubber
func (s Scrubber) Scrub(text string) string
func CwdName(cwd string) string        // CwdName.of
func ProjectDirName(cwd string) string
```
`readBounded` (64 MB tail cap for /messages) stays in `internal/service`.

## go-live: `internal/live`, `internal/approval`
```go
// live
type Agent struct { ID, Kind string; Status api.Status; Cwd string } // fed from the monitor's snapshot
func NewMonitor(h herdr.Client, hub Hub) *Monitor       // 250 ms tick, default tracker
func (m *Monitor) Update(agents []Agent)
func (m *Monitor) Landed(agentID string, msg api.Message)
func (m *Monitor) Run(ctx context.Context)

// approval
func Parse(screen, agentID string, sc transcript.Scrubber, cwdName, kind string) *api.Approval
func Fallback(screen, agentID string, sc transcript.Scrubber) *api.Approval
func Step(screen, ansi string) *api.ApprovalStep       // ansi "" = none

// InputBox (in approval, or tell me where)
func InputBoxContent(screen string) (text string, ok bool) // ok=false: no box on screen
func LastPromptLine(screen string) (string, bool)
func ClearKeys(text string) []string
```

## go-drivers: `internal/controls`, `internal/usage`, `internal/uploads`

controls keeps all per-agent control state that Swift kept on AgentService (`confirmed` hold, `lastControls`, codex sticky mode, the transcript control cache, the settings lock). The service passes callbacks in so there's no import cycle.
```go
type Deps struct {
    Herdr      herdr.Client
    Catalogs   *Catalogs                     // nil = defaults
    Settings   *SettingsGuard                // nil = defaults
    Kind       func(herdr.Agent) string      // service.kindOf: herdr's kind, else the kind POST /agents just started; "" = unknown
    Locate     func(herdr.Agent) *transcript.Ref
    ClearInput func(ctx context.Context, id string, wait time.Duration) error // service.clearInput, used by claude's slash()
    OnChange   func()                        // after a control applied: monitor refresh
}
func New(d Deps) *Controls

type State struct { Model, Effort, PermissionMode *string } // ControlState

// Snapshot path (every GET /agents). Reads the detection screen itself. nil when the kind has no driver.
// Includes the confirmed hold and the merge over the last known values.
func (c *Controls) State(ctx context.Context, a herdr.Agent, ref *transcript.Ref) *State
func (c *Controls) Label(a herdr.Agent, model string) *string          // modelLabel

// Routes
func (c *Controls) ForAgent(ctx context.Context, id string) (api.AgentControls, error) // all-false supports when no driver
func (c *Controls) Apply(ctx context.Context, id string, r Request) (paneID string, err error) // whole Swift control(): busy wait, 409s, validate, apply, OnChange; server then re-reads agent(paneID)
func (c *Controls) ForKind(kind string) (api.AgentControls, error)                     // GET /controls?kind=; 400 unsupported
func Catalog() api.Controls                                                          // GET /controls (claude list)

// POST /agents
func (c *Controls) HasDriver(kind string) bool
func (c *Controls) LaunchArgs(kind, model, effort string) []string   // "" = not given
func (c *Controls) AgentModel(kind, id string) string                // alias → full id
func (c *Controls) LabelForKind(kind, model string) *string
func (c *Controls) Hold(paneID string, s State)

type Request struct { Model, PermissionMode, Effort, Command string } // exactly one non-empty; server validates count + command value
```

usage
```go
func NewMonitor(hub Hub) *Monitor
func (m *Monitor) Run(ctx context.Context)
func (m *Monitor) Snapshot() []api.UsageProvider
func (m *Monitor) RequestRefresh() bool // false = 429 rate_limited
```

uploads
```go
const MaxBytes = 20 << 20; const MaxPerMessage = 10
func NewStore(root string) *Store // "" = config home + /uploads (and the legacy root)
func (s *Store) Save(paneID string, data []byte, filename string) (api.Attachment, error)
func (s *Store) Find(id string) (path string, ok bool)
func (s *Store) RecordSent(pane, text string, files []string)
func MimeType(path string) string
func Prompt(text string, paths []string) string
func RunCleaner(ctx context.Context, s *Store, log *slog.Logger)
```

## go-core
- `herdr.Client` interface, `herdr.Agent`, the event stream (`Start/Stop/Events/Watch(panes)`), `herdr.KeyNames.FirstInvalid`.
- `api.*` wire types + `api.Error` + herdr error mapping, `api.FormatTime`.
- `machine.Current() api.Machine`, `config` (home, token), `logrotate.Run(ctx)` or similar.
- `cmd/relay serve` calls `service.Run(ctx, service.Options{Port, Hosts, Token, SocketPath})`.
