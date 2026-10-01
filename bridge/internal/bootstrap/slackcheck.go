package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// slackAPI is the Slack Web API root. Tests point it somewhere else.
var slackAPI = "https://slack.com/api"

// slackHTTP is the client used for token checks. Short timeout: this runs in
// front of a spinner.
var slackHTTP = &http.Client{Timeout: 15 * time.Second}

// Slack token hints, printed when a check fails, because the two tokens live on
// different pages of the app config and are easy to swap.
const (
	BotTokenHint = "The bot token is the xoxb- one from OAuth & Permissions, after Install to Workspace."
	AppTokenHint = "The app-level token is the xapp- one from Basic Information → App-Level Tokens, with scope connections:write."
)

// SlackTokenError is a token the Slack API rejected. Err is Slack's own error
// string ("invalid_auth", "not_allowed_token_type", …) and Hint says where the
// right token lives.
type SlackTokenError struct {
	Kind string // "bot" | "app"
	Err  string
	Hint string
}

func (e *SlackTokenError) Error() string {
	return fmt.Sprintf("Slack rejected the %s token: %s", e.Kind, e.Err)
}

// CheckBotToken calls auth.test to prove the xoxb- token works. It returns the
// workspace and bot user Slack reports, so the caller can show what it just
// authenticated as.
func CheckBotToken(ctx context.Context, token string) (team, user string, err error) {
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Team  string `json:"team"`
		User  string `json:"user"`
	}
	if err := slackPost(ctx, "auth.test", token, nil, &out); err != nil {
		return "", "", err
	}
	if !out.OK {
		return "", "", &SlackTokenError{Kind: "bot", Err: slackErr(out.Error), Hint: BotTokenHint}
	}
	return out.Team, out.User, nil
}

// CheckAppToken calls apps.connections.open to prove the xapp- token can open a
// socket-mode connection. The websocket URL it hands back is discarded: opening
// it here would take the slot the bot needs.
func CheckAppToken(ctx context.Context, token string) error {
	var out struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		URL   string `json:"url"`
	}
	if err := slackPost(ctx, "apps.connections.open", token, nil, &out); err != nil {
		return err
	}
	if !out.OK {
		return &SlackTokenError{Kind: "app", Err: slackErr(out.Error), Hint: AppTokenHint}
	}
	return nil
}

// CheckSlackTokens validates both tokens before anything is written to the box.
func CheckSlackTokens(ctx context.Context, botToken, appToken string) (team, user string, err error) {
	team, user, err = CheckBotToken(ctx, botToken)
	if err != nil {
		return "", "", err
	}
	if err := CheckAppToken(ctx, appToken); err != nil {
		return "", "", err
	}
	return team, user, nil
}

// slackPost posts an empty form to a Slack Web API method with the token as a
// bearer credential and decodes the JSON reply.
func slackPost(ctx context.Context, method, token string, form url.Values, out any) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return fmt.Errorf("%s: empty token", method)
	}
	body := strings.NewReader(form.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackAPI+"/"+method, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")

	resp, err := slackHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s: slack returned %s", method, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: unreadable reply: %w", method, err)
	}
	return nil
}

// slackErr keeps Slack's own wording, falling back when it sends none.
func slackErr(s string) string {
	if s = strings.TrimSpace(s); s != "" {
		return s
	}
	return "unknown error"
}

// ParseUserIDs splits a comma separated list of Slack member IDs, dropping
// blanks and any stray @ or surrounding punctuation.
func ParseUserIDs(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		id := strings.TrimSpace(part)
		id = strings.Trim(id, "<>@ ")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
