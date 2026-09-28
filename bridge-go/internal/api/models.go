package api

import (
	"encoding/json"
	"fmt"
)

type Workspace struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AgentCount int    `json:"agentCount"`
}

type AgentStatus string

const (
	StatusIdle    AgentStatus = "idle"
	StatusWorking AgentStatus = "working"
	StatusBlocked AgentStatus = "blocked"
	StatusDone    AgentStatus = "done"
	StatusUnknown AgentStatus = "unknown"
)

// TranscriptState: see api.md `Agent.transcriptState`.
type TranscriptState string

const (
	TranscriptReady       TranscriptState = "ready"
	TranscriptPending     TranscriptState = "pending"
	TranscriptUnsupported TranscriptState = "unsupported"
)

// ParsedKinds are the agent kinds whose transcripts the bridge can parse.
var ParsedKinds = map[string]bool{"claude": true, "pi": true, "codex": true}

func TranscriptStateOf(kind string, hasTranscript bool) TranscriptState {
	switch {
	case hasTranscript:
		return TranscriptReady
	case ParsedKinds[kind]:
		return TranscriptPending
	default:
		return TranscriptUnsupported
	}
}

// Agent always encodes every key; nil optionals are `null` so the app sees a stable shape.
type Agent struct {
	ID              string          `json:"id"`
	Name            *string         `json:"name"`
	Kind            string          `json:"kind"`
	Title           string          `json:"title"`
	WorkspaceID     string          `json:"workspaceId"`
	WorkspaceName   string          `json:"workspaceName"`
	CwdName         string          `json:"cwdName"`
	Status          AgentStatus     `json:"status"`
	HasTranscript   bool            `json:"hasTranscript"`
	UpdatedAt       string          `json:"updatedAt"`
	Model           *string         `json:"model"`
	ModelLabel      *string         `json:"modelLabel"`
	PermissionMode  *string         `json:"permissionMode"`
	Effort          *string         `json:"effort"`
	SessionID       *string         `json:"sessionId"`
	TranscriptState TranscriptState `json:"transcriptState"`
}

// UnmarshalJSON derives a missing transcriptState the way older bridges imply it.
func (a *Agent) UnmarshalJSON(b []byte) error {
	type alias Agent
	var v alias
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if v.TranscriptState == "" {
		v.TranscriptState = TranscriptStateOf(v.Kind, v.HasTranscript)
	}
	*a = Agent(v)
	return nil
}

// Str returns a pointer to s, for the optional string fields.
func Str(s string) *string { return &s }

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type BlockType string

const (
	BlockText       BlockType = "text"
	BlockThinking   BlockType = "thinking"
	BlockToolCall   BlockType = "toolCall"
	BlockToolResult BlockType = "toolResult"
	BlockAttachment BlockType = "attachment"
)

// Block is the tagged union from api.md. Only the fields of its Type are encoded;
// build it with the constructors below.
type Block struct {
	Type BlockType
	// text, thinking
	Text string
	// toolCall, attachment
	ID   string
	Name string
	// toolCall
	Summary string
	Input   string
	// toolResult
	ToolCallID string
	IsError    bool
	Preview    string
	// attachment
	Kind AttachmentKind
}

func TextBlock(text string) Block     { return Block{Type: BlockText, Text: text} }
func ThinkingBlock(text string) Block { return Block{Type: BlockThinking, Text: text} }
func ToolCallBlock(id, name, summary, input string) Block {
	return Block{Type: BlockToolCall, ID: id, Name: name, Summary: summary, Input: input}
}
func ToolResultBlock(toolCallID string, isError bool, preview string) Block {
	return Block{Type: BlockToolResult, ToolCallID: toolCallID, IsError: isError, Preview: preview}
}
func AttachmentBlock(id, name string, kind AttachmentKind) Block {
	return Block{Type: BlockAttachment, ID: id, Name: name, Kind: kind}
}

type textJSON struct {
	Type BlockType `json:"type"`
	Text string    `json:"text"`
}
type toolCallJSON struct {
	Type    BlockType `json:"type"`
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Summary string    `json:"summary"`
	Input   string    `json:"input"`
}
type toolResultJSON struct {
	Type       BlockType `json:"type"`
	ToolCallID string    `json:"toolCallId"`
	IsError    bool      `json:"isError"`
	Preview    string    `json:"preview"`
}
type attachmentJSON struct {
	Type BlockType      `json:"type"`
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Kind AttachmentKind `json:"kind"`
}

func (b Block) MarshalJSON() ([]byte, error) {
	switch b.Type {
	case BlockText, BlockThinking:
		return Marshal(textJSON{b.Type, b.Text})
	case BlockToolCall:
		return Marshal(toolCallJSON{b.Type, b.ID, b.Name, b.Summary, b.Input})
	case BlockToolResult:
		return Marshal(toolResultJSON{b.Type, b.ToolCallID, b.IsError, b.Preview})
	case BlockAttachment:
		return Marshal(attachmentJSON{b.Type, b.ID, b.Name, b.Kind})
	}
	return nil, fmt.Errorf("unknown block type %q", b.Type)
}

func (b *Block) UnmarshalJSON(data []byte) error {
	var raw struct {
		Type       BlockType      `json:"type"`
		Text       *string        `json:"text"`
		ID         *string        `json:"id"`
		Name       *string        `json:"name"`
		Summary    *string        `json:"summary"`
		Input      *string        `json:"input"`
		ToolCallID *string        `json:"toolCallId"`
		IsError    *bool          `json:"isError"`
		Preview    *string        `json:"preview"`
		Kind       AttachmentKind `json:"kind"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	need := func(fields ...any) error {
		for _, f := range fields {
			switch p := f.(type) {
			case *string:
				if p == nil {
					return fmt.Errorf("block %q: missing field", raw.Type)
				}
			case *bool:
				if p == nil {
					return fmt.Errorf("block %q: missing field", raw.Type)
				}
			}
		}
		return nil
	}
	var err error
	switch raw.Type {
	case BlockText, BlockThinking:
		if err = need(raw.Text); err == nil {
			*b = Block{Type: raw.Type, Text: *raw.Text}
		}
	case BlockToolCall:
		if err = need(raw.ID, raw.Name, raw.Summary, raw.Input); err == nil {
			*b = ToolCallBlock(*raw.ID, *raw.Name, *raw.Summary, *raw.Input)
		}
	case BlockToolResult:
		if err = need(raw.ToolCallID, raw.IsError, raw.Preview); err == nil {
			*b = ToolResultBlock(*raw.ToolCallID, *raw.IsError, *raw.Preview)
		}
	case BlockAttachment:
		if err = need(raw.ID, raw.Name); err == nil && raw.Kind == "" {
			err = fmt.Errorf("block %q: missing field", raw.Type)
		}
		if err == nil {
			*b = AttachmentBlock(*raw.ID, *raw.Name, raw.Kind)
		}
	default:
		err = fmt.Errorf("unknown block type %q", raw.Type)
	}
	return err
}

type Message struct {
	ID        string  `json:"id"`
	Role      Role    `json:"role"`
	CreatedAt string  `json:"createdAt"`
	Blocks    []Block `json:"blocks"`
}

func (m Message) MarshalJSON() ([]byte, error) {
	type alias Message
	m.Blocks = nonNil(m.Blocks)
	return Marshal(alias(m))
}

type MessagePage struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"hasMore"`
}

func (p MessagePage) MarshalJSON() ([]byte, error) {
	type alias MessagePage
	p.Messages = nonNil(p.Messages)
	return Marshal(alias(p))
}

type ApprovalOption struct {
	Label string   `json:"label"`
	Keys  []string `json:"keys"`
	// True on Claude's "Type something." row: send Keys, then the answer via POST /agents/:id/text.
	FreeText *bool `json:"freeText,omitzero"`
}

func (o ApprovalOption) MarshalJSON() ([]byte, error) {
	type alias ApprovalOption
	o.Keys = nonNil(o.Keys)
	return Marshal(alias(o))
}

// ApprovalStep is progress through a multi-question prompt. Index is 1-based; Count includes Submit.
type ApprovalStep struct {
	Index int     `json:"index"`
	Count int     `json:"count"`
	Title *string `json:"title,omitzero"`
}

type Approval struct {
	AgentID  string           `json:"agentId"`
	Question string           `json:"question"`
	Options  []ApprovalOption `json:"options"`
	Step     *ApprovalStep    `json:"step,omitzero"`
}

func (a Approval) MarshalJSON() ([]byte, error) {
	type alias Approval
	a.Options = nonNil(a.Options)
	return Marshal(alias(a))
}

// LiveTool is the `tool` part of a `reply.live` frame; see api.md "Live reply".
type LiveTool struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	State   string `json:"state"`
}

// NewLiveTool matches Swift's default state.
func NewLiveTool(name, summary string) LiveTool {
	return LiveTool{Name: name, Summary: summary, State: "running"}
}
