package api

// ControlChoice is one choice in GET /controls.
type ControlChoice struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Controls is GET /controls (Claude's catalog). Swift: ControlsCatalog.
type Controls struct {
	Models  []ControlChoice `json:"models"`
	Modes   []ControlChoice `json:"modes"`
	Efforts []ControlChoice `json:"efforts"`
}

func (c Controls) MarshalJSON() ([]byte, error) {
	type alias Controls
	c.Models, c.Modes, c.Efforts = nonNil(c.Models), nonNil(c.Modes), nonNil(c.Efforts)
	return Marshal(alias(c))
}

type ControlSupport struct {
	Model   bool `json:"model"`
	Effort  bool `json:"effort"`
	Mode    bool `json:"mode"`
	Compact bool `json:"compact"`
	Clear   bool `json:"clear"`
}

// AgentControls is GET /agents/:id/controls and GET /controls?kind=.
type AgentControls struct {
	Models   []ControlChoice `json:"models"`
	Efforts  []ControlChoice `json:"efforts"`
	Modes    []ControlChoice `json:"modes"`
	Supports ControlSupport  `json:"supports"`
	// The agent's saved defaults (always set by GET /controls?kind=).
	DefaultModel  *string `json:"defaultModel,omitzero"`
	DefaultEffort *string `json:"defaultEffort,omitzero"`
	// pi and codex: efforts per model id. nil is omitted; an empty non-nil map is `{}`.
	EffortsByModel map[string][]ControlChoice `json:"effortsByModel,omitzero"`
}

func (c AgentControls) MarshalJSON() ([]byte, error) {
	type alias AgentControls
	c.Models, c.Modes, c.Efforts = nonNil(c.Models), nonNil(c.Modes), nonNil(c.Efforts)
	if c.EffortsByModel != nil {
		m := make(map[string][]ControlChoice, len(c.EffortsByModel))
		for k, v := range c.EffortsByModel {
			m[k] = nonNil(v)
		}
		c.EffortsByModel = m
	}
	return Marshal(alias(c))
}

// ControlRequest is the body of POST /agents/:id/control: exactly one field set.
type ControlRequestKind string

const (
	ControlModel          ControlRequestKind = "model"
	ControlPermissionMode ControlRequestKind = "permissionMode"
	ControlEffort         ControlRequestKind = "effort"
	ControlCompact        ControlRequestKind = "compact"
	ControlClear          ControlRequestKind = "clear"
)

// ControlRequest: Value is the model/mode/effort id; empty for compact and clear.
type ControlRequest struct {
	Kind  ControlRequestKind
	Value string
}
