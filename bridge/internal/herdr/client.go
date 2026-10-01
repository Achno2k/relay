package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

// Client is everything the bridge asks herdr. *SocketClient implements it; tests use
// herdrtest.Server behind a real SocketClient.
type Client interface {
	Agents(ctx context.Context) ([]Agent, error)
	Agent(ctx context.Context, target string) (Agent, error)
	Workspaces(ctx context.Context) ([]Workspace, error)
	// Panes lists panes; workspaceID "" means all workspaces.
	Panes(ctx context.Context, workspaceID string) ([]Pane, error)
	// Read reads a pane's screen. lines <= 0 means herdr's default.
	Read(ctx context.Context, target string, source ReadSource, lines int, ansi bool) (Read, error)
	Prompt(ctx context.Context, target, text string) error
	SendKeys(ctx context.Context, target string, keys []string) error
	// SendText types text literally into the pane, without Enter.
	SendText(ctx context.Context, paneID, text string) error
	// CreateTab creates a tab in workspaceID and returns its root pane. "" cwd/label = not sent.
	CreateTab(ctx context.Context, workspaceID, cwd, label string) (Pane, error)
	// CloseTab closes a tab and everything in it.
	CloseTab(ctx context.Context, tabID string) error
	// StartAgent starts an agent; timeoutMs <= 0 means 30000.
	StartAgent(ctx context.Context, name, kind, paneID string, args []string, timeoutMs int) (Agent, error)
}

// DefaultSocketPath is $HERDR_SOCKET_PATH, else ~/.config/herdr/herdr.sock.
func DefaultSocketPath() string {
	if p := os.Getenv("HERDR_SOCKET_PATH"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config/herdr/herdr.sock")
}

// How long a call waits for herdr (connect, write and answer together). herdr answers reads
// and lists in milliseconds, so a hung herdr surfaces as 504 within a few seconds instead of
// piling up requests (QA R8-7). Actions get longer; agent.start waits its own timeout + 10 s.
const (
	ReadTimeout   = 3 * time.Second
	ActionTimeout = 10 * time.Second
)

// SocketClient uses one short-lived connection per request so a slow call (agent.start)
// never blocks others.
type SocketClient struct {
	SocketPath string
	nextID     atomic.Int64
}

var _ Client = (*SocketClient)(nil)

func NewClient(socketPath string) *SocketClient {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &SocketClient{SocketPath: socketPath}
}

func (c *SocketClient) Agents(ctx context.Context) ([]Agent, error) {
	var r struct {
		Agents []Agent `json:"agents"`
	}
	err := c.Call(ctx, "agent.list", map[string]any{}, &r, ReadTimeout)
	return r.Agents, err
}

func (c *SocketClient) Agent(ctx context.Context, target string) (Agent, error) {
	var r struct {
		Agent *Agent `json:"agent"`
	}
	if err := c.Call(ctx, "agent.get", map[string]any{"target": target}, &r, ReadTimeout); err != nil {
		return Agent{}, err
	}
	if r.Agent == nil {
		return Agent{}, Decoding("agent.get: missing agent")
	}
	return *r.Agent, nil
}

func (c *SocketClient) Workspaces(ctx context.Context) ([]Workspace, error) {
	var r struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	err := c.Call(ctx, "workspace.list", map[string]any{}, &r, ReadTimeout)
	return r.Workspaces, err
}

func (c *SocketClient) Panes(ctx context.Context, workspaceID string) ([]Pane, error) {
	var r struct {
		Panes []Pane `json:"panes"`
	}
	params := map[string]any{}
	if workspaceID != "" {
		params["workspace_id"] = workspaceID
	}
	err := c.Call(ctx, "pane.list", params, &r, ReadTimeout)
	return r.Panes, err
}

func (c *SocketClient) Read(ctx context.Context, target string, source ReadSource, lines int, ansi bool) (Read, error) {
	var r struct {
		Read *Read `json:"read"`
	}
	format := "text"
	if ansi {
		format = "ansi"
	}
	params := map[string]any{"target": target, "source": string(source), "strip_ansi": !ansi, "format": format}
	if lines > 0 {
		params["lines"] = lines
	}
	if err := c.Call(ctx, "agent.read", params, &r, ReadTimeout); err != nil {
		return Read{}, err
	}
	if r.Read == nil {
		return Read{}, Decoding("agent.read: missing read")
	}
	return *r.Read, nil
}

func (c *SocketClient) Prompt(ctx context.Context, target, text string) error {
	return c.Call(ctx, "agent.prompt", map[string]any{"target": target, "text": text}, nil, ActionTimeout)
}

func (c *SocketClient) SendKeys(ctx context.Context, target string, keys []string) error {
	if keys == nil {
		keys = []string{}
	}
	return c.Call(ctx, "agent.send_keys", map[string]any{"target": target, "keys": keys}, nil, ActionTimeout)
}

func (c *SocketClient) SendText(ctx context.Context, paneID, text string) error {
	return c.Call(ctx, "pane.send_text", map[string]any{"pane_id": paneID, "text": text}, nil, ActionTimeout)
}

func (c *SocketClient) CreateTab(ctx context.Context, workspaceID, cwd, label string) (Pane, error) {
	var r struct {
		Tab      *Tab  `json:"tab"`
		RootPane *Pane `json:"root_pane"`
	}
	params := map[string]any{"workspace_id": workspaceID, "focus": false}
	if cwd != "" {
		params["cwd"] = cwd
	}
	if label != "" {
		params["label"] = label
	}
	if err := c.Call(ctx, "tab.create", params, &r, ActionTimeout); err != nil {
		return Pane{}, err
	}
	if r.Tab == nil || r.RootPane == nil {
		return Pane{}, Decoding("tab.create: missing tab or root_pane")
	}
	return *r.RootPane, nil
}

func (c *SocketClient) CloseTab(ctx context.Context, tabID string) error {
	return c.Call(ctx, "tab.close", map[string]any{"tab_id": tabID}, nil, ActionTimeout)
}

func (c *SocketClient) StartAgent(ctx context.Context, name, kind, paneID string, args []string, timeoutMs int) (Agent, error) {
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}
	var r struct {
		Agent *Agent `json:"agent"`
	}
	params := map[string]any{"name": name, "kind": kind, "pane_id": paneID, "timeout_ms": timeoutMs}
	if len(args) > 0 {
		params["args"] = args
	}
	timeout := time.Duration(timeoutMs)*time.Millisecond + 10*time.Second
	if err := c.Call(ctx, "agent.start", params, &r, timeout); err != nil {
		return Agent{}, err
	}
	if r.Agent == nil {
		return Agent{}, Decoding("agent.start: missing agent")
	}
	return *r.Agent, nil
}

// Call sends one request and decodes `result` into out (nil = ignore it). Errors are *Error.
func (c *SocketClient) Call(ctx context.Context, method string, params map[string]any, out any, timeout time.Duration) error {
	id := c.nextID.Add(1)
	body, err := json.Marshal(map[string]any{"id": fmt.Sprintf("relay-%d", id), "method": method, "params": params})
	if err != nil {
		return Decoding(fmt.Sprintf("%s: %v", method, err))
	}
	line, err := roundTrip(ctx, c.SocketPath, body, timeout)
	if err != nil {
		return err
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		return Decoding(fmt.Sprintf("%s: %v", method, err))
	}
	if env.Error != nil {
		return Remote(env.Error.Code, env.Error.Message)
	}
	if len(env.Result) == 0 || string(env.Result) == "null" {
		return Decoding(method + ": empty response")
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return Decoding(fmt.Sprintf("%s: %v", method, err))
	}
	return nil
}

// Dial connects to herdr's socket, mapping failure to ErrUnavailable.
func Dial(ctx context.Context, socketPath string) (net.Conn, error) {
	if err := CheckSocketPath(socketPath); err != nil {
		return nil, err
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, Unavailable("cannot connect to herdr socket: " + errText(err))
	}
	return conn, nil
}

func roundTrip(ctx context.Context, socketPath string, body []byte, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	dctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	conn, err := Dial(dctx, socketPath)
	if err != nil {
		if ctx.Err() == nil && dctx.Err() != nil {
			return nil, Timeout()
		}
		return nil, err
	}
	defer conn.Close()
	// Cancelling ctx unblocks the read by closing the connection.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(append(body, '\n')); err != nil {
		return nil, ioErr("write", ctx, err)
	}
	line, err := readLine(bufio.NewReaderSize(conn, 64*1024))
	if err != nil {
		return nil, ioErr("read", ctx, err)
	}
	return line, nil
}

// readLine returns the next complete line without its newline, however long.
func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return line[:len(line)-1], nil
}

func ioErr(op string, ctx context.Context, err error) *Error {
	var ne net.Error
	switch {
	case ctx.Err() != nil:
		return IOError(op + ": " + ctx.Err().Error())
	case errors.As(err, &ne) && ne.Timeout():
		return Timeout()
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		// herdr went away before answering: same as not being there (QA R8-6).
		return Unavailable("herdr closed the connection")
	}
	return IOError(op + ": " + errText(err))
}

func errText(err error) string {
	var op *net.OpError
	if errors.As(err, &op) && op.Err != nil {
		return op.Err.Error()
	}
	return err.Error()
}

// CheckSocketPath rejects paths that don't fit a unix socket address (sun_path is 104 bytes
// on macOS, 108 on Linux, including the NUL), so `relay serve` can refuse to start (QA R8-20).
func CheckSocketPath(p string) error {
	if len(p) >= maxSocketPath {
		return Unavailable(fmt.Sprintf("herdr socket path is %d bytes, longer than the %d this OS allows: %s", len(p), maxSocketPath-1, p))
	}
	return nil
}
