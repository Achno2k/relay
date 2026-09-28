package herdr

import "relay/internal/api"

// herdr wire types (snake_case on the wire). Swift's optional strings are plain strings here:
// "" means absent. Name stays a pointer because it crosses the wire as `null`.

type AgentSession struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"` // "id" | "path"
	Value  string `json:"value"`
}

type Agent struct {
	PaneID                string          `json:"pane_id"`
	WorkspaceID           string          `json:"workspace_id"`
	TabID                 string          `json:"tab_id"`
	Name                  *string         `json:"name"`
	Agent                 string          `json:"agent"`
	DisplayAgent          string          `json:"display_agent"`
	AgentStatus           api.AgentStatus `json:"agent_status"`
	Title                 string          `json:"title"`
	TerminalTitleStripped string          `json:"terminal_title_stripped"`
	Cwd                   string          `json:"cwd"`
	ForegroundCwd         string          `json:"foreground_cwd"`
	AgentSession          *AgentSession   `json:"agent_session"`
	StateChangeSeq        uint64          `json:"state_change_seq"`
	Revision              uint64          `json:"revision"`
}

// CwdOrForeground is Swift's `a.cwd ?? a.foregroundCwd`.
func (a Agent) CwdOrForeground() string {
	if a.Cwd != "" {
		return a.Cwd
	}
	return a.ForegroundCwd
}

type Workspace struct {
	WorkspaceID string `json:"workspace_id"`
	Label       string `json:"label"`
	Number      *int   `json:"number"`
}

type Pane struct {
	PaneID        string `json:"pane_id"`
	WorkspaceID   string `json:"workspace_id"`
	TabID         string `json:"tab_id"`
	Cwd           string `json:"cwd"`
	ForegroundCwd string `json:"foreground_cwd"`
	Agent         string `json:"agent"`
}

func (p Pane) CwdOrForeground() string {
	if p.Cwd != "" {
		return p.Cwd
	}
	return p.ForegroundCwd
}

type Read struct {
	Text      string `json:"text"`
	Revision  uint64 `json:"revision"`
	Truncated bool   `json:"truncated"`
}

type Tab struct {
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

type ReadSource string

const (
	SourceVisible         ReadSource = "visible"
	SourceRecent          ReadSource = "recent"
	SourceDetection       ReadSource = "detection"
	SourceRecentUnwrapped ReadSource = "recent_unwrapped"
)
