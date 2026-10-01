// Package server is the HTTP and WebSocket front of the bridge: routing, bearer auth, the JSON
// error shape, body limits and the event hub. What the routes do lives behind Backend
// (internal/service implements it).
package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"relay/internal/api"
	"relay/internal/files"
	"relay/internal/herdr"
	"relay/internal/transcript"
	"relay/internal/uploads"
)

// MaxBodyBytes caps every JSON request body (Hummingbird's default maxUploadSize, 2 MB).
const MaxBodyBytes = 2 << 20

// CreateRequest is the body of POST /agents. Optional strings are nil when absent.
type CreateRequest struct {
	WorkspaceID string
	Kind        string
	Name        *string
	Prompt      *string
	Model       *string
	Effort      *string
	CwdFromPane *string
}

// Backend is everything the routes need. Errors are *api.Error or map through api.FromError.
type Backend interface {
	Workspaces(ctx context.Context) ([]api.Workspace, error)
	Agents(ctx context.Context) ([]api.Agent, error)
	Agent(ctx context.Context, id string) (api.Agent, error)
	Create(ctx context.Context, r CreateRequest) (api.Agent, error)
	Messages(ctx context.Context, id string, before *string, limit int) (api.MessagePage, error)
	Prompt(ctx context.Context, id, text string, attachments []string) error
	SendKeys(ctx context.Context, id string, keys []string) error
	Text(ctx context.Context, id, text string, submit bool) error
	Approval(ctx context.Context, id string) (*api.Approval, error)
	Upload(ctx context.Context, id string, data []byte, filename string) (api.Attachment, error)
	// FindUpload returns the stored file for an attachment id, across all agents.
	FindUpload(attachmentID string) (path string, ok bool)
	// File reads a cwd-relative file of the agent's project (GET /agents/:id/file).
	File(ctx context.Context, id, path string) (files.Result, error)
	Catalog() api.Controls
	KindControls(kind string) (api.AgentControls, error)
	Controls(ctx context.Context, id string) (api.AgentControls, error)
	Control(ctx context.Context, id string, r api.ControlRequest) (api.Agent, error)
}

// Usage is the usage cache behind GET /usage and POST /usage/refresh.
type Usage interface {
	Snapshot() []api.UsageProvider
	// RequestRefresh returns false when throttled.
	RequestRefresh() bool
}

type Options struct {
	Backend Backend
	Hub     *Hub
	Token   string
	// HerdrReachable reports the last herdr round trip for /health. nil = unavailable.
	HerdrReachable func() bool
	Usage          Usage // nil = no usage
	Machine        func() api.Machine
	StartedAt      time.Time
	// WebSocket keepalive; 0 = 30 s interval, 15 s pong timeout.
	PingInterval, PingTimeout time.Duration
}

type Server struct {
	opt Options
}

func New(opt Options) *Server {
	if opt.StartedAt.IsZero() {
		opt.StartedAt = time.Now()
	}
	if opt.PingInterval <= 0 {
		opt.PingInterval = defaultPingInterval
	}
	if opt.PingTimeout <= 0 {
		opt.PingTimeout = defaultPingTimeout
	}
	return &Server{opt: opt}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Server", "relay")
	path := r.URL.EscapedPath()
	if path != "/health" && !s.authorized(r, path) {
		writeError(w, api.Unauthorized)
		return
	}
	var segs []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			segs = append(segs, p)
		}
	}
	if err := s.route(w, r, segs); err != nil {
		writeError(w, err)
	}
}

// authorized: bearer token on everything but /health; /ws takes ?token= instead.
func (s *Server) authorized(r *http.Request, path string) bool {
	var presented string
	if path == "/ws" {
		q := r.URL.Query()
		if !q.Has("token") {
			return false
		}
		presented = q.Get("token")
	} else {
		h := r.Header.Get("Authorization")
		if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
			return false
		}
		presented = strings.Trim(h[7:], " \t")
	}
	return constantTimeEqual(presented, s.opt.Token)
}

// constantTimeEqual walks every byte of equal-length inputs; only the length leaks.
func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

var errNotFound = api.NewError(http.StatusNotFound, "not_found", "Not Found")

func (s *Server) route(w http.ResponseWriter, r *http.Request, segs []string) error {
	ctx := r.Context()
	b := s.opt.Backend
	get, post := r.Method == http.MethodGet, r.Method == http.MethodPost
	switch {
	case len(segs) == 1 && segs[0] == "health" && get:
		reachable := s.opt.HerdrReachable != nil && s.opt.HerdrReachable()
		herdrState := "unavailable"
		if reachable {
			herdrState = "connected"
		}
		uptime := max(0, int(time.Since(s.opt.StartedAt).Seconds()))
		return writeJSON(w, http.StatusOK, api.NewHealth(herdrState, uptime))

	case len(segs) == 1 && segs[0] == "ws" && get:
		s.serveWS(w, r)
		return nil

	case len(segs) == 1 && segs[0] == "workspaces" && get:
		return respond(w, http.StatusOK)(b.Workspaces(ctx))

	case len(segs) == 1 && segs[0] == "agents" && get:
		return respond(w, http.StatusOK)(b.Agents(ctx))

	case len(segs) == 1 && segs[0] == "agents" && post:
		return s.create(w, r)

	case len(segs) == 1 && segs[0] == "machine" && get:
		return writeJSON(w, http.StatusOK, s.opt.Machine())

	case len(segs) == 1 && segs[0] == "usage" && get:
		var providers []api.UsageProvider
		if s.opt.Usage != nil {
			providers = s.opt.Usage.Snapshot()
		}
		return writeJSON(w, http.StatusOK, api.UsageSnapshot{Providers: providers})

	case len(segs) == 2 && segs[0] == "usage" && segs[1] == "refresh" && post:
		if s.opt.Usage != nil && !s.opt.Usage.RequestRefresh() {
			return api.NewError(http.StatusTooManyRequests, "rate_limited", "usage was just refreshed; try again in a few seconds")
		}
		return writeJSON(w, http.StatusAccepted, api.Empty{})

	case len(segs) == 1 && segs[0] == "controls" && get:
		q := r.URL.Query()
		if q.Has("kind") {
			return respond(w, http.StatusOK)(b.KindControls(q.Get("kind")))
		}
		return writeJSON(w, http.StatusOK, b.Catalog())

	case len(segs) >= 2 && segs[0] == "agents":
		return s.agentRoute(w, r, segs[1], segs[2:])
	}
	return errNotFound
}

func (s *Server) agentRoute(w http.ResponseWriter, r *http.Request, rawID string, rest []string) error {
	ctx := r.Context()
	b := s.opt.Backend
	get, post := r.Method == http.MethodGet, r.Method == http.MethodPost
	sub := ""
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch {
	case len(rest) == 0 && get:
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		return respond(w, http.StatusOK)(b.Agent(ctx, id))

	case len(rest) == 1 && sub == "messages" && get:
		q := r.URL.Query()
		limit := 50
		if n, err := strconv.Atoi(q.Get("limit")); err == nil {
			limit = n
		}
		limit = min(max(limit, 1), 500)
		var before *string
		if q.Has("before") {
			before = api.Str(q.Get("before"))
		}
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		return respond(w, http.StatusOK)(b.Messages(ctx, id, before, limit))

	case len(rest) == 1 && sub == "prompt" && post:
		var body struct {
			Text        *string
			Attachments *[]string
		}
		if err := decodeBody(w, r, fields{"text": &body.Text, "attachments": &body.Attachments}); err != nil {
			return err
		}
		text, atts := deref(body.Text), []string{}
		if body.Attachments != nil {
			atts = *body.Attachments
		}
		if text == "" && len(atts) == 0 {
			return api.BadRequest("text or attachments is required")
		}
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		if err := b.Prompt(ctx, id, text, atts); err != nil {
			return err
		}
		return writeJSON(w, http.StatusAccepted, api.Empty{})

	case len(rest) == 1 && sub == "keys" && post:
		var keys *[]string
		if err := decodeBody(w, r, fields{"keys": &keys}, "keys"); err != nil {
			return err
		}
		if len(*keys) == 0 {
			return api.BadRequest("keys is required")
		}
		if bad, ok := herdr.FirstInvalidKey(*keys); ok {
			return api.BadRequest("invalid key \"" + bad + "\"")
		}
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		if err := b.SendKeys(ctx, id, *keys); err != nil {
			return err
		}
		return writeJSON(w, http.StatusAccepted, api.Empty{})

	case len(rest) == 1 && sub == "text" && post:
		var body struct {
			Text   *string
			Submit *bool
		}
		if err := decodeBody(w, r, fields{"text": &body.Text, "submit": &body.Submit}, "text"); err != nil {
			return err
		}
		if *body.Text == "" {
			return api.BadRequest("text is required")
		}
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		submit := body.Submit == nil || *body.Submit
		if err := b.Text(ctx, id, *body.Text, submit); err != nil {
			return err
		}
		return writeJSON(w, http.StatusAccepted, api.Empty{})

	case len(rest) == 1 && sub == "approval" && get:
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		a, err := b.Approval(ctx, id)
		if err != nil {
			return err
		}
		if a == nil {
			w.WriteHeader(http.StatusNoContent)
			return nil
		}
		return writeJSON(w, http.StatusOK, a)

	case len(rest) == 1 && sub == "controls" && get:
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		return respond(w, http.StatusOK)(b.Controls(ctx, id))

	case len(rest) == 1 && sub == "control" && post:
		var body struct{ Model, PermissionMode, Effort, Command *string }
		if err := decodeBody(w, r, fields{"model": &body.Model, "permissionMode": &body.PermissionMode,
			"effort": &body.Effort, "command": &body.Command}); err != nil {
			return err
		}
		// Swift evaluates the agent id before the body's request.
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		req, err := controlRequest(body.Model, body.PermissionMode, body.Effort, body.Command)
		if err != nil {
			return err
		}
		return respond(w, http.StatusAccepted)(b.Control(ctx, id, req))

	case len(rest) == 1 && sub == "attachments" && post:
		return s.upload(w, r, rawID)

	case len(rest) == 1 && sub == "file" && get:
		id, err := agentID(rawID)
		if err != nil {
			return err
		}
		res, err := b.File(ctx, id, r.URL.Query().Get("path"))
		if err != nil {
			return err
		}
		if res.Text != nil {
			return writeJSON(w, http.StatusOK, res.Text)
		}
		h := w.Header()
		h.Set("Content-Type", res.ContentType)
		h.Set("Content-Length", strconv.Itoa(len(res.Image)))
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(res.Image)
		return nil

	case len(rest) == 2 && sub == "attachments" && get:
		if _, err := agentID(rawID); err != nil {
			return err
		}
		return serveAttachment(w, b, unescape(rest[1]))
	}
	return errNotFound
}

func controlRequest(model, mode, effort, command *string) (api.ControlRequest, error) {
	n := 0
	for _, p := range []*string{model, mode, effort, command} {
		if p != nil {
			n++
		}
	}
	if n != 1 {
		return api.ControlRequest{}, api.BadRequest("send exactly one of model, permissionMode, effort, command")
	}
	switch {
	case model != nil:
		return api.ControlRequest{Kind: api.ControlModel, Value: *model}, nil
	case mode != nil:
		return api.ControlRequest{Kind: api.ControlPermissionMode, Value: *mode}, nil
	case effort != nil:
		return api.ControlRequest{Kind: api.ControlEffort, Value: *effort}, nil
	}
	switch *command {
	case "compact":
		return api.ControlRequest{Kind: api.ControlCompact}, nil
	case "clear":
		return api.ControlRequest{Kind: api.ControlClear}, nil
	}
	return api.ControlRequest{}, api.BadRequest("command must be compact or clear")
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		WorkspaceID, Kind                        *string
		Name, Prompt, Model, Effort, CwdFromPane *string
	}
	err := decodeBody(w, r, fields{
		"workspaceId": &body.WorkspaceID, "kind": &body.Kind, "name": &body.Name, "prompt": &body.Prompt,
		"model": &body.Model, "effort": &body.Effort, "cwdFromPane": &body.CwdFromPane,
	}, "workspaceId", "kind")
	if err != nil {
		return err
	}
	if *body.Kind == "" {
		return api.BadRequest("kind is required")
	}
	if body.Name != nil && !ValidName(*body.Name) {
		return api.BadRequest("name must match [a-z][a-z0-9_-]{0,31}")
	}
	return respond(w, http.StatusCreated)(s.opt.Backend.Create(r.Context(), CreateRequest{
		WorkspaceID: *body.WorkspaceID, Kind: *body.Kind, Name: body.Name, Prompt: body.Prompt,
		Model: body.Model, Effort: body.Effort, CwdFromPane: body.CwdFromPane,
	}))
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request, rawID string) error {
	filename := "file"
	if vs := r.Header.Values("X-Filename"); len(vs) > 0 {
		filename = unescape(vs[0])
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, uploads.MaxBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return api.NewError(http.StatusRequestEntityTooLarge, "too_large", "files are limited to 20 MB")
		}
		return api.BadRequest("couldn't read the upload")
	}
	if len(data) == 0 {
		return api.BadRequest("empty file")
	}
	id, err := agentID(rawID)
	if err != nil {
		return err
	}
	return respond(w, http.StatusCreated)(s.opt.Backend.Upload(r.Context(), id, data, filename))
}

func serveAttachment(w http.ResponseWriter, b Backend, attID string) error {
	notFound := api.NotFound("no attachment (uploads expire after 7 days)")
	path, ok := b.FindUpload(attID)
	if !ok {
		return notFound
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return notFound
	}
	h := w.Header()
	h.Set("Content-Type", uploads.MimeType(path))
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "private, max-age=604800")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
	return nil
}

// agentID decodes a path id (`w13%3Ap1`) and rejects obviously adversarial input (a control
// character, or a huge id meant to stress path scrubbing or lookups) before it reaches herdr.
func agentID(raw string) (string, error) {
	id := unescape(raw)
	if len(id) > 128 || strings.ContainsFunc(id, func(r rune) bool { return r < 0x20 }) {
		return "", api.BadRequest("invalid agent id")
	}
	return id, nil
}

// unescape percent-decodes once, keeping the raw text when it isn't valid (Swift's
// `removingPercentEncoding ?? raw`). `+` stays a plus.
func unescape(raw string) string {
	if s, err := url.PathUnescape(raw); err == nil {
		return s
	}
	return raw
}

// ValidName is herdr's agent name rule, [a-z][a-z0-9_-]{0,31}.
func ValidName(name string) bool {
	if len(name) == 0 || len(name) > 32 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// respond writes a (value, error) result: the value with status, or the error.
func respond(w http.ResponseWriter, status int) func(any, error) error {
	return func(v any, err error) error {
		if err != nil {
			return err
		}
		return writeJSON(w, status, v)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) error {
	data, err := api.Marshal(v)
	if err != nil {
		return err
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	_, _ = w.Write(data)
	return nil
}

// writeError renders err as the JSON error shape. Messages the bridge didn't write itself (herdr's,
// internal errors, an agent's screen quoted in control_failed) can carry absolute paths, so those
// are scrubbed: full paths never cross the wire.
func writeError(w http.ResponseWriter, err error) {
	e := api.FromError(err)
	var own *api.Error
	if !errors.As(err, &own) || e.Code == "control_failed" {
		e = api.NewError(e.Status, e.Code, pathScrubber.Scrub(e.Message))
	}
	_ = writeJSON(w, e.Status, e.Body())
}

// pathScrubber has no cwd, so every absolute path keeps only its last component.
var pathScrubber = transcript.NewScrubber("")
