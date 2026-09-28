package api

import (
	"encoding/json"
	"fmt"
)

type EventType string

const (
	EventHello           EventType = "hello"
	EventAgentUpdated    EventType = "agent.updated"
	EventAgentCreated    EventType = "agent.created"
	EventAgentClosed     EventType = "agent.closed"
	EventMessageUpserted EventType = "message.upserted"
	EventReplyLive       EventType = "reply.live"
	EventUsageUpdated    EventType = "usage.updated"
)

// ServerEvent is one WS frame. Build it with the constructors; MarshalJSON writes only the
// keys of its Type.
type ServerEvent struct {
	Type     EventType
	Agent    *Agent
	AgentID  string
	Message  *Message
	Text     *string
	Tool     *LiveTool
	Seq      int
	Provider *UsageProvider
}

func Hello() ServerEvent                { return ServerEvent{Type: EventHello} }
func AgentUpdated(a Agent) ServerEvent  { return ServerEvent{Type: EventAgentUpdated, Agent: &a} }
func AgentCreated(a Agent) ServerEvent  { return ServerEvent{Type: EventAgentCreated, Agent: &a} }
func AgentClosed(id string) ServerEvent { return ServerEvent{Type: EventAgentClosed, AgentID: id} }
func UsageUpdated(p UsageProvider) ServerEvent {
	return ServerEvent{Type: EventUsageUpdated, Provider: &p}
}
func MessageUpserted(agentID string, m Message) ServerEvent {
	return ServerEvent{Type: EventMessageUpserted, AgentID: agentID, Message: &m}
}

// ReplyLive is the live preview of the current turn. nil text/tool encode as `null`.
func ReplyLive(agentID string, text *string, tool *LiveTool, seq int) ServerEvent {
	return ServerEvent{Type: EventReplyLive, AgentID: agentID, Text: text, Tool: tool, Seq: seq}
}

func (e ServerEvent) MarshalJSON() ([]byte, error) {
	switch e.Type {
	case EventHello:
		return Marshal(struct {
			Type EventType `json:"type"`
		}{e.Type})
	case EventAgentUpdated, EventAgentCreated:
		return Marshal(struct {
			Type  EventType `json:"type"`
			Agent *Agent    `json:"agent"`
		}{e.Type, e.Agent})
	case EventAgentClosed:
		return Marshal(struct {
			Type    EventType `json:"type"`
			AgentID string    `json:"agentId"`
		}{e.Type, e.AgentID})
	case EventMessageUpserted:
		return Marshal(struct {
			Type    EventType `json:"type"`
			AgentID string    `json:"agentId"`
			Message *Message  `json:"message"`
		}{e.Type, e.AgentID, e.Message})
	case EventReplyLive:
		return Marshal(struct {
			Type    EventType `json:"type"`
			AgentID string    `json:"agentId"`
			Text    *string   `json:"text"`
			Tool    *LiveTool `json:"tool"`
			Seq     int       `json:"seq"`
		}{e.Type, e.AgentID, e.Text, e.Tool, e.Seq})
	case EventUsageUpdated:
		return Marshal(struct {
			Type     EventType      `json:"type"`
			Provider *UsageProvider `json:"provider"`
		}{e.Type, e.Provider})
	}
	return nil, fmt.Errorf("unknown event type %q", e.Type)
}

// UnmarshalJSON reads a frame back (clients, parity tool, tests).
func (e *ServerEvent) UnmarshalJSON(b []byte) error {
	var raw struct {
		Type     EventType      `json:"type"`
		Agent    *Agent         `json:"agent"`
		AgentID  string         `json:"agentId"`
		Message  *Message       `json:"message"`
		Text     *string        `json:"text"`
		Tool     *LiveTool      `json:"tool"`
		Seq      int            `json:"seq"`
		Provider *UsageProvider `json:"provider"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	switch raw.Type {
	case EventHello, EventAgentUpdated, EventAgentCreated, EventAgentClosed, EventMessageUpserted, EventReplyLive, EventUsageUpdated:
	default:
		return fmt.Errorf("unknown event type %q", raw.Type)
	}
	*e = ServerEvent(raw)
	return nil
}
