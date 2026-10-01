package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// slackStub serves canned Slack replies and records what each method was called
// with.
func slackStub(t *testing.T, replies map[string]string) (auth map[string]string, method map[string]string) {
	t.Helper()
	auth, method = map[string]string{}, map[string]string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		auth[name] = r.Header.Get("Authorization")
		method[name] = r.Method
		body, ok := replies[name]
		if !ok {
			t.Errorf("unexpected call to %s", name)
			body = `{"ok":false,"error":"unexpected_call"}`
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	old := slackAPI
	slackAPI = srv.URL
	t.Cleanup(func() { slackAPI = old })
	return auth, method
}

func TestCheckSlackTokensOK(t *testing.T) {
	auth, method := slackStub(t, map[string]string{
		"auth.test":             `{"ok":true,"team":"Acme","user":"agents"}`,
		"apps.connections.open": `{"ok":true,"url":"wss://example.invalid/link"}`,
	})

	team, user, err := CheckSlackTokens(context.Background(), "xoxb-good", "xapp-good")
	if err != nil {
		t.Fatalf("CheckSlackTokens: %v", err)
	}
	if team != "Acme" || user != "agents" {
		t.Errorf("got team %q user %q, want Acme/agents", team, user)
	}
	if auth["auth.test"] != "Bearer xoxb-good" {
		t.Errorf("auth.test sent %q", auth["auth.test"])
	}
	if auth["apps.connections.open"] != "Bearer xapp-good" {
		t.Errorf("apps.connections.open sent %q", auth["apps.connections.open"])
	}
	for name, m := range method {
		if m != http.MethodPost {
			t.Errorf("%s used %s, want POST", name, m)
		}
	}
}

func TestCheckSlackTokensBadBotToken(t *testing.T) {
	slackStub(t, map[string]string{
		"auth.test": `{"ok":false,"error":"invalid_auth"}`,
	})

	_, _, err := CheckSlackTokens(context.Background(), "xoxb-bad", "xapp-good")
	var tokErr *SlackTokenError
	if !errors.As(err, &tokErr) {
		t.Fatalf("got %v, want *SlackTokenError", err)
	}
	if tokErr.Kind != "bot" || tokErr.Err != "invalid_auth" {
		t.Errorf("got %+v, want bot/invalid_auth", tokErr)
	}
	if !strings.Contains(tokErr.Hint, "xoxb-") {
		t.Errorf("bot hint does not mention xoxb-: %q", tokErr.Hint)
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error text drops Slack's wording: %q", err.Error())
	}
}

// A swapped pair is the common mistake: the app token in the bot slot fails
// auth.test, and the bot token in the app slot fails apps.connections.open.
func TestCheckSlackTokensBadAppToken(t *testing.T) {
	slackStub(t, map[string]string{
		"auth.test":             `{"ok":true,"team":"Acme","user":"agents"}`,
		"apps.connections.open": `{"ok":false,"error":"not_allowed_token_type"}`,
	})

	_, _, err := CheckSlackTokens(context.Background(), "xoxb-good", "xoxb-wrong-slot")
	var tokErr *SlackTokenError
	if !errors.As(err, &tokErr) {
		t.Fatalf("got %v, want *SlackTokenError", err)
	}
	if tokErr.Kind != "app" || tokErr.Err != "not_allowed_token_type" {
		t.Errorf("got %+v, want app/not_allowed_token_type", tokErr)
	}
	if !strings.Contains(tokErr.Hint, "connections:write") {
		t.Errorf("app hint does not mention the scope: %q", tokErr.Hint)
	}
}

func TestCheckSlackTokensEmpty(t *testing.T) {
	slackStub(t, map[string]string{})
	if _, _, err := CheckSlackTokens(context.Background(), "  ", "xapp-good"); err == nil {
		t.Error("empty bot token was accepted")
	}
}

func TestCheckSlackTokensHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	old := slackAPI
	slackAPI = srv.URL
	defer func() { slackAPI = old }()

	if _, _, err := CheckSlackTokens(context.Background(), "xoxb-good", "xapp-good"); err == nil {
		t.Error("a 500 from Slack was treated as success")
	}
}

func TestParseUserIDs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"U012ABCDEF", []string{"U012ABCDEF"}},
		{"U012ABCDEF, U345GHIJKL", []string{"U012ABCDEF", "U345GHIJKL"}},
		{" U1 ,, U2 , ", []string{"U1", "U2"}},
		{"<@U1>,@U2", []string{"U1", "U2"}},
		{"U1,U1", []string{"U1"}},
		{"", nil},
		{"  ,  ", nil},
	}
	for _, c := range cases {
		if got := ParseUserIDs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseUserIDs(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}
