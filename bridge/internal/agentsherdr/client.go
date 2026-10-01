// Package herdr wraps the herdr CLI on the box. v1 shells out to `herdr ...`
// and parses JSON. Owner: session "core". Run `herdr agent`, `herdr pane`,
// and `herdr api schema` for the real shapes.
//
// Wire format, confirmed against herdr protocol 20:
//
//	success: {"id":"cli:agent:get","result":{"type":"agent_info","agent":{...}}}
//	error:   {"id":"cli:agent:get","error":{"code":"agent_not_found","message":"..."}}
//
// Errors arrive as JSON on stderr with exit status 1; usage errors are plain
// text with exit status 2. `herdr agent read` is the one exception to the JSON
// rule: it prints the pane text straight to stdout.
package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type AgentState string

const (
	StateIdle    AgentState = "idle"
	StateWorking AgentState = "working"
	StateBlocked AgentState = "blocked"
	StateDone    AgentState = "done"
	StateUnknown AgentState = "unknown"
)

type Agent struct {
	Name   string
	Kind   string // "claude" | "codex"
	PaneID string
	State  AgentState
	CWD    string
}

type Client interface {
	ListAgents(ctx context.Context) ([]Agent, error)
	GetAgent(ctx context.Context, name string) (Agent, error)
	// NewPane creates a shell pane in the bot's workspace/tab with cwd and
	// returns its pane id.
	NewPane(ctx context.Context, cwd string) (paneID string, err error)
	StartAgent(ctx context.Context, name, kind, paneID string, args ...string) error
	Prompt(ctx context.Context, name, text string) error
	// Wait blocks until the agent reaches one of the given states or ctx ends.
	Wait(ctx context.Context, name string, until ...AgentState) (AgentState, error)
	// Read returns recent unwrapped output, last n lines.
	Read(ctx context.Context, name string, lines int) (string, error)
	SendKeys(ctx context.Context, name string, keys ...string) error
	KillAgent(ctx context.Context, name string) error
	// Notify shows a herdr notification in the agent's pane.
	Notify(ctx context.Context, name, text string) error
}

// TabLabel is the dedicated tab NewPane creates and reuses.
const TabLabel = "agents"

// Error is a herdr server error, decoded from the JSON envelope.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return "herdr: " + e.Code + ": " + e.Message }

// IsNotFound reports whether err is a herdr "not found" error for an agent,
// pane, tab or workspace target.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && strings.HasSuffix(e.Code, "_not_found")
}

// ErrNoWorkspace means herdr is running but has no workspace to place a pane in.
var ErrNoWorkspace = errors.New("herdr: no workspace available")

// runner executes `herdr <args...>` and returns both streams. It is the seam
// tests replace with captured fixtures.
type runner func(ctx context.Context, args ...string) (stdout, stderr []byte, err error)

type client struct {
	run runner
}

// New returns a Client backed by the herdr binary on PATH.
func New() Client { return &client{run: execRunner} }

func execRunner(ctx context.Context, args ...string) ([]byte, []byte, error) {
	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	var out, errOut bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	return out.Bytes(), errOut.Bytes(), err
}

type envelope struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *Error          `json:"error"`
}

// call runs a herdr subcommand and returns the raw `result` object.
func (c *client) call(ctx context.Context, args ...string) (json.RawMessage, error) {
	out, errOut, runErr := c.run(ctx, args...)

	// The server reports failures as an error envelope. It normally lands on
	// stderr, but `agent start` returns agent_not_ready on stdout, so check both.
	for _, b := range [][]byte{out, errOut} {
		var env envelope
		if json.Unmarshal(bytes.TrimSpace(b), &env) == nil {
			if env.Error != nil {
				return nil, env.Error
			}
			if env.Result != nil {
				return env.Result, nil
			}
		}
	}
	if runErr != nil {
		return nil, fmt.Errorf("herdr %s: %w: %s", strings.Join(args, " "), runErr, snippet(errOut))
	}
	return nil, fmt.Errorf("herdr %s: unparsable response: %s", strings.Join(args, " "), snippet(out))
}

// callText runs a subcommand whose stdout is plain text, not JSON.
func (c *client) callText(ctx context.Context, args ...string) (string, error) {
	out, errOut, runErr := c.run(ctx, args...)
	var env envelope
	if json.Unmarshal(bytes.TrimSpace(errOut), &env) == nil && env.Error != nil {
		return "", env.Error
	}
	if runErr != nil {
		return "", fmt.Errorf("herdr %s: %w: %s", strings.Join(args, " "), runErr, snippet(errOut))
	}
	return string(out), nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// --- wire types, mirroring herdr's success_response $defs ---

type agentInfo struct {
	Agent       string `json:"agent"` // the kind: claude, codex, pi, …
	AgentStatus string `json:"agent_status"`
	CWD         string `json:"cwd"`
	Name        string `json:"name"`
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

func (a agentInfo) toAgent() Agent {
	return Agent{
		Name:   a.Name,
		Kind:   a.Agent,
		PaneID: a.PaneID,
		State:  toState(a.AgentStatus),
		CWD:    a.CWD,
	}
}

func toState(s string) AgentState {
	switch AgentState(s) {
	case StateIdle, StateWorking, StateBlocked, StateDone:
		return AgentState(s)
	default:
		return StateUnknown
	}
}

type paneInfo struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

type tabInfo struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	PaneCount   int    `json:"pane_count"`
}

type workspaceInfo struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Focused     bool   `json:"focused"`
	Number      int    `json:"number"`
}

type rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type layoutPane struct {
	PaneID string `json:"pane_id"`
	Rect   rect   `json:"rect"`
}

// --- Client implementation ---

func (c *client) ListAgents(ctx context.Context) ([]Agent, error) {
	res, err := c.call(ctx, "agent", "list")
	if err != nil {
		return nil, err
	}
	var body struct {
		Agents []agentInfo `json:"agents"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return nil, fmt.Errorf("herdr agent list: %w", err)
	}
	agents := make([]Agent, 0, len(body.Agents))
	for _, a := range body.Agents {
		agents = append(agents, a.toAgent())
	}
	return agents, nil
}

func (c *client) GetAgent(ctx context.Context, name string) (Agent, error) {
	a, err := c.getAgent(ctx, name)
	if err != nil {
		return Agent{}, err
	}
	return a.toAgent(), nil
}

// getAgent keeps the raw info so callers can reach pane_id and tab_id.
func (c *client) getAgent(ctx context.Context, name string) (agentInfo, error) {
	res, err := c.call(ctx, "agent", "get", name)
	if err != nil {
		return agentInfo{}, err
	}
	var body struct {
		Agent agentInfo `json:"agent"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return agentInfo{}, fmt.Errorf("herdr agent get %s: %w", name, err)
	}
	return body.Agent, nil
}

// NewPane creates or reuses the tab labelled "agents" in the default workspace
// and splits a pane there with the given cwd. It never steals focus.
func (c *client) NewPane(ctx context.Context, cwd string) (string, error) {
	ws, err := c.defaultWorkspace(ctx)
	if err != nil {
		return "", err
	}
	tabID, rootPane, err := c.ensureTab(ctx, ws, cwd)
	if err != nil {
		return "", err
	}

	target := rootPane
	if target == "" {
		if target, err = c.splitTarget(ctx, ws, tabID); err != nil {
			return "", err
		}
	}
	dir, err := c.splitDirection(ctx, target)
	if err != nil {
		return "", err
	}

	args := []string{"pane", "split", target, "--direction", dir, "--no-focus"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	res, err := c.call(ctx, args...)
	if err != nil {
		return "", err
	}
	var body struct {
		Pane paneInfo `json:"pane"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return "", fmt.Errorf("herdr pane split: %w", err)
	}
	if body.Pane.PaneID == "" {
		return "", fmt.Errorf("herdr pane split: no pane id in response")
	}
	return body.Pane.PaneID, nil
}

// defaultWorkspace prefers the caller's own workspace, then the focused one,
// then the lowest-numbered one. It creates a workspace only if there is none.
func (c *client) defaultWorkspace(ctx context.Context) (string, error) {
	if id := os.Getenv("HERDR_WORKSPACE_ID"); id != "" {
		return id, nil
	}
	res, err := c.call(ctx, "workspace", "list")
	if err != nil {
		return "", err
	}
	var body struct {
		Workspaces []workspaceInfo `json:"workspaces"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return "", fmt.Errorf("herdr workspace list: %w", err)
	}
	best := ""
	bestNum := 0
	for _, w := range body.Workspaces {
		if w.Focused {
			return w.WorkspaceID, nil
		}
		if best == "" || w.Number < bestNum {
			best, bestNum = w.WorkspaceID, w.Number
		}
	}
	if best != "" {
		return best, nil
	}
	res, err = c.call(ctx, "workspace", "create", "--label", TabLabel, "--no-focus")
	if err != nil {
		return "", err
	}
	var created struct {
		Workspace workspaceInfo `json:"workspace"`
	}
	if err := json.Unmarshal(res, &created); err != nil {
		return "", fmt.Errorf("herdr workspace create: %w", err)
	}
	if created.Workspace.WorkspaceID == "" {
		return "", ErrNoWorkspace
	}
	return created.Workspace.WorkspaceID, nil
}

// ensureTab returns the id of the "agents" tab in ws, creating it if needed.
// rootPane is non-empty only when the tab was just created, in which case its
// fresh root pane is the right thing to split.
func (c *client) ensureTab(ctx context.Context, ws, cwd string) (tabID, rootPane string, err error) {
	res, err := c.call(ctx, "tab", "list", "--workspace", ws)
	if err != nil {
		return "", "", err
	}
	var body struct {
		Tabs []tabInfo `json:"tabs"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return "", "", fmt.Errorf("herdr tab list: %w", err)
	}
	for _, t := range body.Tabs {
		if t.Label == TabLabel {
			return t.TabID, "", nil
		}
	}

	args := []string{"tab", "create", "--workspace", ws, "--label", TabLabel, "--no-focus"}
	if cwd != "" {
		args = append(args, "--cwd", cwd)
	}
	res, err = c.call(ctx, args...)
	if err != nil {
		return "", "", err
	}
	var created struct {
		Tab      tabInfo  `json:"tab"`
		RootPane paneInfo `json:"root_pane"`
	}
	if err := json.Unmarshal(res, &created); err != nil {
		return "", "", fmt.Errorf("herdr tab create: %w", err)
	}
	return created.Tab.TabID, created.RootPane.PaneID, nil
}

// splitTarget picks the largest pane in the tab so repeated splits stay even.
func (c *client) splitTarget(ctx context.Context, ws, tabID string) (string, error) {
	res, err := c.call(ctx, "pane", "list", "--workspace", ws)
	if err != nil {
		return "", err
	}
	var body struct {
		Panes []paneInfo `json:"panes"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return "", fmt.Errorf("herdr pane list: %w", err)
	}
	var inTab []string
	for _, p := range body.Panes {
		if p.TabID == tabID {
			inTab = append(inTab, p.PaneID)
		}
	}
	if len(inTab) == 0 {
		return "", fmt.Errorf("herdr: tab %s has no panes", tabID)
	}
	panes, err := c.layout(ctx, inTab[0])
	if err != nil {
		return inTab[0], nil //nolint:nilerr // geometry is a nicety, not a requirement
	}
	best, bestArea := inTab[0], -1
	for _, p := range panes {
		if area := p.Rect.Width * p.Rect.Height; area > bestArea {
			best, bestArea = p.PaneID, area
		}
	}
	return best, nil
}

// splitDirection keeps both halves usable: sideways while the pane is wide
// enough to survive being halved, downwards while it is tall enough. It falls
// back to "down" when geometry is unavailable.
func (c *client) splitDirection(ctx context.Context, paneID string) (string, error) {
	const (
		minCols = 40 // narrower than this and an agent TUI wraps badly
		minRows = 8
	)
	panes, err := c.layout(ctx, paneID)
	if err != nil {
		return "down", nil //nolint:nilerr // geometry is a nicety, not a requirement
	}
	for _, p := range panes {
		if p.PaneID != paneID {
			continue
		}
		switch {
		case p.Rect.Width/2 >= minCols:
			return "right", nil
		case p.Rect.Height/2 >= minRows:
			return "down", nil
		default:
			return "right", nil
		}
	}
	return "down", nil
}

func (c *client) layout(ctx context.Context, paneID string) ([]layoutPane, error) {
	res, err := c.call(ctx, "pane", "layout", "--pane", paneID)
	if err != nil {
		return nil, err
	}
	var body struct {
		Layout struct {
			Panes []layoutPane `json:"panes"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return nil, fmt.Errorf("herdr pane layout: %w", err)
	}
	return body.Layout.Panes, nil
}

// StartAgent launches kind in an existing idle shell pane under the given name.
// Native agent arguments are passed after "--".
func (c *client) StartAgent(ctx context.Context, name, kind, paneID string, args ...string) error {
	argv := []string{"agent", "start", name, "--kind", kind, "--pane", paneID}
	if ms, ok := timeoutMS(ctx); ok {
		argv = append(argv, "--timeout", ms)
	}
	if len(args) > 0 {
		argv = append(argv, "--")
		argv = append(argv, args...)
	}
	_, err := c.call(ctx, argv...)
	return err
}

// Prompt submits text to the agent and returns as soon as herdr accepts it.
// Callers that need the turn to finish follow up with Wait.
func (c *client) Prompt(ctx context.Context, name, text string) error {
	_, err := c.call(ctx, "agent", "prompt", name, text)
	return err
}

func (c *client) Wait(ctx context.Context, name string, until ...AgentState) (AgentState, error) {
	argv := []string{"agent", "wait", name}
	for _, s := range until {
		argv = append(argv, "--until", string(s))
	}
	if ms, ok := timeoutMS(ctx); ok {
		argv = append(argv, "--timeout", ms)
	}
	res, err := c.call(ctx, argv...)
	if err != nil {
		return StateUnknown, err
	}
	var body struct {
		Agent agentInfo `json:"agent"`
	}
	if err := json.Unmarshal(res, &body); err != nil {
		return StateUnknown, fmt.Errorf("herdr agent wait %s: %w", name, err)
	}
	return toState(body.Agent.AgentStatus), nil
}

// Read returns the agent's recent output with soft wraps joined.
//
// Full-screen agents such as Claude Code draw on the terminal's alternate
// screen, whose rows never reach herdr's scrollback, so recent-unwrapped comes
// back empty for them. In that case we fall back to the rendered viewport,
// which is the only place their output exists.
func (c *client) Read(ctx context.Context, name string, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	n := strconv.Itoa(lines)
	text, err := c.callText(ctx, "agent", "read", name, "--source", "recent-unwrapped", "--lines", n)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) != "" {
		return text, nil
	}
	return c.callText(ctx, "agent", "read", name, "--source", "visible", "--lines", n)
}

func (c *client) SendKeys(ctx context.Context, name string, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	argv := append([]string{"agent", "send-keys", name}, keys...)
	_, err := c.call(ctx, argv...)
	return err
}

// KillAgent stops the agent by closing the pane hosting it. herdr has no
// agent-level kill; the pane is the process, so closing it is the kill.
func (c *client) KillAgent(ctx context.Context, name string) error {
	a, err := c.getAgent(ctx, name)
	if err != nil {
		if IsNotFound(err) {
			return nil // already gone
		}
		return err
	}
	if a.PaneID == "" {
		return fmt.Errorf("herdr: agent %s has no pane", name)
	}
	_, err = c.call(ctx, "pane", "close", a.PaneID)
	if err != nil && IsNotFound(err) {
		return nil
	}
	return err
}

// Notify shows a herdr notification titled with the agent name. herdr's
// notification surface is per-client, not per-pane, so the name goes in the
// title to say which session it is about.
func (c *client) Notify(ctx context.Context, name, text string) error {
	_, err := c.call(ctx, "notification", "show", name, "--body", text, "--sound", "none")
	return err
}

// timeoutMS converts a context deadline into herdr's --timeout milliseconds.
func timeoutMS(ctx context.Context) (string, bool) {
	dl, ok := ctx.Deadline()
	if !ok {
		return "", false
	}
	ms := time.Until(dl).Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return strconv.FormatInt(ms, 10), true
}
