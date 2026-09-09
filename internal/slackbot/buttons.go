package slackbot

import "strings"

// approvalBlockID names the Block Kit action row so interaction callbacks can
// be told apart from any other buttons the bot grows later.
const approvalBlockID = "agents_approval"

// Approval action ids. The value carries the session id so a click needs no
// lookup table on our side.
const (
	actionAllow    = "approve_allow"
	actionDeny     = "approve_deny"
	actionAllowAll = "approve_allow_all"
)

// approvalButtons is the row posted under a blocked agent's prompt.
func approvalButtons(sessionID string) []Button {
	return []Button{
		{Label: "Allow", ActionID: actionAllow, Value: sessionID, Style: "primary"},
		{Label: "Deny", ActionID: actionDeny, Value: sessionID, Style: "danger"},
		{Label: "Allow all", ActionID: actionAllowAll, Value: sessionID},
	}
}

// approvalKeys maps a button to the key sequence that answers the harness's
// approval prompt in its pane.
//
// Both harnesses render approval as a select list, so the answer is a cursor
// move plus Enter rather than a single character:
//
//	claude 2.x  "Do you want to proceed?"     1 Yes · 2 No · 3 Yes, don't ask again
//	codex 0.14x "Allow Codex to run …"        1 Allow · 2 Always allow · 3 Decline
//
// The orderings come from the strings in the shipped binaries and are the one
// part of the bot that guesses at another program's UI. Everything else routes
// through here, so a wrong guess is a one line fix in this table.
func approvalKeys(kind string, actionID string) []string {
	claude := map[string][]string{
		actionAllow:    {"enter"},
		actionDeny:     {"down", "enter"},
		actionAllowAll: {"down", "down", "enter"},
	}
	codex := map[string][]string{
		actionAllow:    {"enter"},
		actionAllowAll: {"down", "enter"},
		actionDeny:     {"down", "down", "enter"},
	}
	switch strings.ToLower(kind) {
	case "codex":
		return codex[actionID]
	default:
		return claude[actionID]
	}
}

// approvalLabel is how a click is reported back into the thread.
func approvalLabel(actionID string) string {
	switch actionID {
	case actionAllow:
		return "Allowed"
	case actionDeny:
		return "Denied"
	case actionAllowAll:
		return "Allowed, and won't ask again this session"
	default:
		return "Answered"
	}
}

// isApprovalAction reports whether an action id belongs to this row.
func isApprovalAction(actionID string) bool {
	switch actionID {
	case actionAllow, actionDeny, actionAllowAll:
		return true
	}
	return false
}
