// Package agent defines a coding-agent abstraction that a chat bridge can drive
// without knowing which agent is behind it. The ACP implementation lives in
// internal/agent/acp; OpenCode is wired through internal/agent/opencode.
package agent

import (
	"context"
	"encoding/json"
)

// ConfigOptionValue is one selectable value of a ConfigOption.
type ConfigOptionValue struct {
	Value       string `json:"value"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ConfigOption is a session config option (e.g. model / effort / mode).
type ConfigOption struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	Category     string              `json:"category,omitempty"`
	Type         string              `json:"type,omitempty"`
	CurrentValue json.RawMessage     `json:"currentValue,omitempty"`
	Options      []ConfigOptionValue `json:"options,omitempty"`
}

// CurrentString renders CurrentValue as a string.
func (c ConfigOption) CurrentString() string {
	if len(c.CurrentValue) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(c.CurrentValue, &s) == nil {
		return s
	}
	return string(c.CurrentValue)
}

// SessionInfo is the tracked state of a live session.
type SessionInfo struct {
	ID     string
	Cwd    string
	Config []ConfigOption
}

// Current returns the current value of a config option ("" if absent).
func (s SessionInfo) Current(id string) string {
	for _, c := range s.Config {
		if c.ID == id {
			return c.CurrentString()
		}
	}
	return ""
}

// ListEntry is one session from session/list.
type ListEntry struct {
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd,omitempty"`
	Title     string `json:"title,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// Sink receives streaming updates while a prompt runs.
type Sink interface {
	Text(string)
	Tool(title, status string)
	Mode(modeID string)
}

// PermissionOption is one option in a permission request.
type PermissionOption struct {
	OptionID string
	Name     string
	Kind     string
}

// PermissionRequest asks the host to approve a tool action.
type PermissionRequest struct {
	SessionID string
	Title     string
	Kind      string
	RawInput  json.RawMessage
	Options   []PermissionOption
}

// Outcome is the decision for a PermissionRequest.
type Outcome struct {
	Selected bool
	OptionID string
}

// PermissionFunc decides a permission request.
type PermissionFunc func(PermissionRequest) Outcome

// Agent is a single coding-agent process (e.g. `opencode acp`).
type Agent interface {
	Cwd() string
	PID() int
	Running() bool
	Start(ctx context.Context) error
	NewSession(ctx context.Context, cwd string) (SessionInfo, error)
	ResumeSession(ctx context.Context, sessionID, cwd string) (SessionInfo, error)
	Prompt(ctx context.Context, sessionID, text string, sink Sink, perm PermissionFunc) (stopReason string, err error)
	Cancel(sessionID string)
	SetConfig(ctx context.Context, sessionID, key, value string) ([]ConfigOption, error)
	SetMode(ctx context.Context, sessionID, mode string) error
	ListSessions(ctx context.Context, cwd string) ([]ListEntry, error)
	Meta(sessionID string) (SessionInfo, bool)
	Stop()
}

// Factory creates (but does not start) an agent for a key. onExit is invoked
// once when the agent process terminates.
type Factory func(key string, onExit func(err error)) (Agent, error)

// Pool manages agents keyed by a string (usually an absolute cwd).
type Pool interface {
	Acquire(ctx context.Context, key string) (Agent, string, error)
	Release(key string)
	Track(key, sessionID string)
	Size() int
	Shutdown()
}
