package herdrtest

import "strings"

// AgentJSON is a synthetic herdr agent (Swift: World.agent). session may be nil. kind "" is claude.
func AgentJSON(pane string, name *string, status, cwd string, session map[string]any, title string) map[string]any {
	ws := strings.SplitN(pane, ":", 2)[0]
	if title == "" {
		title = "Some task"
	}
	a := map[string]any{
		"pane_id": pane, "workspace_id": ws, "tab_id": ws + ":t1",
		"terminal_id": "term_" + pane, "agent": "claude", "agent_status": status, "focused": false, "revision": 1,
		"terminal_title_stripped": title, "cwd": cwd, "foreground_cwd": cwd, "state_change_seq": 3,
	}
	if name != nil {
		a["name"] = *name
	}
	if session != nil {
		a["agent_session"] = session
	}
	return a
}

// Workspaces is the synthetic workspace list the route tests use (Swift: World.workspaces).
func Workspaces() []map[string]any {
	return []map[string]any{
		{"workspace_id": "w1", "label": "shop-api", "number": 1, "focused": false, "pane_count": 2, "tab_count": 1, "active_tab_id": "w1:t1", "agent_status": "idle"},
		{"workspace_id": "w2", "label": "website", "number": 2, "focused": false, "pane_count": 1, "tab_count": 1, "active_tab_id": "w2:t1", "agent_status": "idle"},
	}
}
