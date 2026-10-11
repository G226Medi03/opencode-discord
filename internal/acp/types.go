package acp

import (
	"encoding/json"

	"oc-discord/internal/agent"
)

// ContentBlock is a prompt content block (text is what we send).
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// SessionUpdate is a session/update notification payload (ACP wire shape).
type SessionUpdate struct {
	SessionUpdate     string               `json:"sessionUpdate"`
	Content           *ContentBlock        `json:"content,omitempty"`
	ToolCallID        string               `json:"toolCallId,omitempty"`
	Title             string               `json:"title,omitempty"`
	Kind              string               `json:"kind,omitempty"`
	Status            string               `json:"status,omitempty"`
	RawInput          json.RawMessage      `json:"rawInput,omitempty"`
	RawOutput         json.RawMessage      `json:"rawOutput,omitempty"`
	CurrentModeID     string               `json:"currentModeId,omitempty"`
	ConfigOptions     []agent.ConfigOption `json:"configOptions,omitempty"`
	AvailableCommands json.RawMessage      `json:"availableCommands,omitempty"`
}

// ---- wire types (ACP-specific, not part of the agent abstraction) ---------

type newSessionResult struct {
	SessionID     string               `json:"sessionId"`
	ConfigOptions []agent.ConfigOption `json:"configOptions,omitempty"`
	Modes         json.RawMessage      `json:"modes,omitempty"`
}

type promptResult struct {
	StopReason string          `json:"stopReason"`
	Usage      json.RawMessage `json:"usage,omitempty"`
}

type sessionListResult struct {
	Sessions   []agent.ListEntry `json:"sessions"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

type wirePermissionParams struct {
	SessionID string `json:"sessionId"`
	ToolCall  struct {
		ToolCallID string          `json:"toolCallId"`
		Title      string          `json:"title"`
		Kind       string          `json:"kind"`
		Status     string          `json:"status"`
		RawInput   json.RawMessage `json:"rawInput"`
	} `json:"toolCall"`
	Options []struct {
		OptionID string `json:"optionId"`
		Name     string `json:"name"`
		Kind     string `json:"kind"`
	} `json:"options"`
}

type wirePermissionOutcome struct {
	Outcome struct {
		Outcome  string `json:"outcome"`
		OptionID string `json:"optionId,omitempty"`
	} `json:"outcome"`
}

func newWireOutcome(o agent.Outcome) wirePermissionOutcome {
	var w wirePermissionOutcome
	if o.Selected && o.OptionID != "" {
		w.Outcome.Outcome = "selected"
		w.Outcome.OptionID = o.OptionID
	} else {
		w.Outcome.Outcome = "cancelled"
	}
	return w
}
