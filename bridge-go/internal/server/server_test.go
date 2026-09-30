package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"relay/internal/api"
	"relay/internal/files"
	"relay/internal/herdr"
)

const testToken = "test-token"

// stubBackend answers every agent call like a herdr that knows nothing.
type stubBackend struct{}

var errUnused = herdr.Remote("unused", "unused")

func (stubBackend) Workspaces(context.Context) ([]api.Workspace, error) { return nil, errUnused }
func (stubBackend) Agents(context.Context) ([]api.Agent, error)         { return nil, errUnused }
func (stubBackend) Agent(context.Context, string) (api.Agent, error)    { return api.Agent{}, errUnused }
func (stubBackend) Create(context.Context, CreateRequest) (api.Agent, error) {
	return api.Agent{}, errUnused
}
func (stubBackend) Messages(context.Context, string, *string, int) (api.MessagePage, error) {
	return api.MessagePage{}, errUnused
}
func (stubBackend) Prompt(context.Context, string, string, []string) error  { return errUnused }
func (stubBackend) SendKeys(context.Context, string, []string) error        { return errUnused }
func (stubBackend) Text(context.Context, string, string, bool) error        { return errUnused }
func (stubBackend) Approval(context.Context, string) (*api.Approval, error) { return nil, errUnused }
func (stubBackend) Upload(context.Context, string, []byte, string) (api.Attachment, error) {
	return api.Attachment{}, errUnused
}
func (stubBackend) FindUpload(string) (string, bool) { return "", false }
func (stubBackend) File(context.Context, string, string) (files.Result, error) {
	return files.Result{}, errUnused
}
func (stubBackend) Catalog() api.Controls { return api.Controls{} }
func (stubBackend) KindControls(string) (api.AgentControls, error) {
	return api.AgentControls{}, errUnused
}
func (stubBackend) Controls(context.Context, string) (api.AgentControls, error) {
	return api.AgentControls{}, errUnused
}
func (stubBackend) Control(context.Context, string, api.ControlRequest) (api.Agent, error) {
	return api.Agent{}, errUnused
}

func newTestServer(t *testing.T, opt Options) *httptest.Server {
	t.Helper()
	if opt.Backend == nil {
		opt.Backend = stubBackend{}
	}
	if opt.Hub == nil {
		opt.Hub = NewHub()
	}
	if opt.Token == "" {
		opt.Token = testToken
	}
	if opt.Machine == nil {
		opt.Machine = func() api.Machine { return api.Machine{ID: "m", Name: "n", Kind: "laptop", Model: "x", OS: "macOS 26"} }
	}
	ts := httptest.NewServer(New(opt))
	t.Cleanup(ts.Close)
	return ts
}

type response struct {
	status int
	header http.Header
	body   string
}

func do(t *testing.T, ts *httptest.Server, method, uri, body string, headers map[string]string) response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+uri, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return response{res.StatusCode, res.Header, string(b)}
}

var auth = map[string]string{"Authorization": "Bearer " + testToken}

func errorCode(t *testing.T, body string) string {
	t.Helper()
	var e api.ErrorBody
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		t.Fatalf("not an error body: %q", body)
	}
	return e.Error.Code
}
