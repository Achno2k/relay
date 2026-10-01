// Package herdrtest is an in-process herdr for tests: a unix socket answering one JSON line
// per request from a canned handler (Swift: FakeHerdr).
package herdrtest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Handler answers one request. Return Error to send `{"error": ...}`; anything else is `result`.
type Handler func(method string, params map[string]any) any

// Error makes the fake answer with herdr's error envelope.
type Error struct {
	Code    string
	Message string
}

// Hangup makes the fake close the connection without answering, like herdr dying mid-request.
type Hangup struct{}

// Call is one request the fake received. Params is the params object as JSON with sorted keys.
type Call struct {
	Method string
	Params string
}

// Server is the fake herdr. Point a herdr.SocketClient (or EventStream) at SocketPath.
type Server struct {
	Dir        string
	SocketPath string

	handler Handler
	ln      net.Listener
	done    chan struct{}

	mu      sync.Mutex
	conns   map[net.Conn]bool
	stopped bool
	calls   []Call
}

// New starts a fake herdr. The socket lives in a short /tmp dir (sun_path is 104 bytes on
// macOS, too short for t.TempDir()). It stops and cleans up when the test ends.
func New(t testing.TB, handler Handler) *Server {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "relay-")
	if err != nil {
		t.Fatal(err)
	}
	s, err := start(dir, filepath.Join(dir, "s"), handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.Stop()
		os.RemoveAll(dir)
	})
	return s
}

// Reusing simulates herdr restarting on the same socket path: start it after stopping prev.
func Reusing(t testing.TB, prev *Server, handler Handler) *Server {
	t.Helper()
	os.Remove(prev.SocketPath)
	s, err := start(prev.Dir, prev.SocketPath, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s
}

func start(dir, path string, handler Handler) (*Server, error) {
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	s := &Server{Dir: dir, SocketPath: path, handler: handler, ln: ln, done: make(chan struct{}), conns: map[net.Conn]bool{}}
	go s.acceptLoop()
	return s, nil
}

// Stop closes the listener and drops live connections (e.g. an events.subscribe), like a
// herdr crash or restart would. Returns once nothing new can connect. Safe to call twice.
func (s *Server) Stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	s.ln.Close()
	<-s.done
	os.Remove(s.SocketPath)
}

// Calls returns every request so far, in order.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// Methods returns the method of every request so far.
func (s *Server) Methods() []string {
	var out []string
	for _, c := range s.Calls() {
		out = append(out, c.Method)
	}
	return out
}

// Params returns the params JSON of the last call to method, or "" if none.
func (s *Server) Params(method string) string {
	calls := s.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method == method {
			return calls[i].Params
		}
	}
	return ""
}

func (s *Server) acceptLoop() {
	defer close(s.done)
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			c.Close()
			return
		}
		s.conns[c] = true
		s.mu.Unlock()
		go s.serve(c)
	}
}

func (s *Server) serve(c net.Conn) {
	defer func() {
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
		c.Close()
	}()
	r := bufio.NewReaderSize(c, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req struct {
			ID     any            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(line, &req) != nil || req.Method == "" {
			continue
		}
		if req.Params == nil {
			req.Params = map[string]any{}
		}
		s.mu.Lock()
		s.calls = append(s.calls, Call{req.Method, marshal(req.Params)})
		s.mu.Unlock()
		out := s.handler(req.Method, req.Params)
		if _, ok := out.(Hangup); ok {
			return
		}
		if req.ID == nil {
			req.ID = ""
		}
		resp := map[string]any{"id": req.ID}
		if e, ok := out.(Error); ok {
			resp["error"] = map[string]any{"code": e.Code, "message": e.Message}
		} else {
			resp["result"] = out
		}
		if _, err := c.Write([]byte(marshal(resp) + "\n")); err != nil {
			return
		}
		if req.Method == "events.subscribe" {
			// Keep the subscription open until the client leaves.
			_, _ = r.WriteTo(discard{})
			return
		}
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// marshal is sorted-key JSON without HTML or slash escaping (Swift's .sortedKeys,
// .withoutEscapingSlashes).
func marshal(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

// Push writes a raw line (an event) to every open connection, e.g. a live events.subscribe.
func (s *Server) Push(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		_, _ = c.Write([]byte(line + "\n"))
	}
}
