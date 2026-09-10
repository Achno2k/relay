package slackbot

import (
	"strconv"
	"strings"
)

// actionsBlockID names the Block Kit action row. Callbacks are routed by
// action id, not by block id, so both rows share it.
const actionsBlockID = "agents_actions"

// decisionActionPrefix keeps the agent's own decision buttons clear of the
// harness approval row, which is answered with key presses rather than a
// prompt.
const decisionActionPrefix = "decide_"

// decisionSep separates the fields packed into a decision button's value.
// Slack round trips the value verbatim, and a unit separator will not appear
// in an option the agent wrote.
const decisionSep = "\x1f"

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

// decisionButtons is the row posted under a reply that ends in a decision
// block. Slack allows five buttons in a row; the contract allows four.
func decisionButtons(sessionID string, options []string) []Button {
	if len(options) > maxDecisionOptions {
		options = options[:maxDecisionOptions]
	}
	out := make([]Button, 0, len(options))
	for i, opt := range options {
		n := i + 1
		b := Button{
			Label:    decisionLabel(n, opt),
			ActionID: decisionActionPrefix + strconv.Itoa(n),
			Value:    encodeDecision(sessionID, n, opt),
		}
		// The first option is the agent's recommendation.
		if i == 0 {
			b.Style = "primary"
		}
		out = append(out, b)
	}
	return out
}

// decisionLabel keeps a button readable. Slack truncates hard at 75 characters
// and an agent's option can run longer than that.
func decisionLabel(n int, option string) string {
	const maxLabel = 74
	label := strconv.Itoa(n) + ". " + option
	if r := []rune(label); len(r) > maxLabel {
		label = string(r[:maxLabel-1]) + "…"
	}
	return label
}

// encodeDecision packs everything a click needs into the button value: which
// session, which option, and the option text to send back.
func encodeDecision(sessionID string, n int, option string) string {
	return sessionID + decisionSep + strconv.Itoa(n) + decisionSep + option
}

// decodeDecision unpacks a value written by encodeDecision.
func decodeDecision(value string) (sessionID string, n int, option string, ok bool) {
	parts := strings.SplitN(value, decisionSep, 3)
	if len(parts) != 3 {
		return "", 0, "", false
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 1 {
		return "", 0, "", false
	}
	if parts[0] == "" || strings.TrimSpace(parts[2]) == "" {
		return "", 0, "", false
	}
	return parts[0], n, parts[2], true
}

// isDecisionAction reports whether an action id belongs to the decision row.
func isDecisionAction(actionID string) bool {
	return strings.HasPrefix(actionID, decisionActionPrefix)
}

// isApprovalAction reports whether an action id belongs to this row.
func isApprovalAction(actionID string) bool {
	switch actionID {
	case actionAllow, actionDeny, actionAllowAll:
		return true
	}
	return false
}
