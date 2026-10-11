package discord

import "time"

// MessageRef identifies a sent message.
type MessageRef struct {
	ChannelID string
	MessageID string
}

// Channel describes a conversation surface.
type Channel struct {
	ID         string
	Name       string
	ParentID   string
	ParentName string
	GuildID    string
	IsDM       bool
	IsThread   bool
}

// Message is an inbound user message.
type Message struct {
	ID           string
	ChannelID    string
	AuthorID     string
	AuthorName   string
	Content      string
	MentionedBot bool
	GuildID      string
	IsDM         bool
	Timestamp    time.Time
}

// Style is a button style.
type Style int

const (
	StylePrimary Style = iota
	StyleSecondary
	StyleSuccess
	StyleDanger
)

// Button is one button in an action row.
type Button struct {
	ID       string
	Label    string
	Style    Style
	Disabled bool
}

// Choice is the result of a button click.
type Choice struct {
	ButtonID string
	UserID   string
}

// CommandOption describes a slash-command option.
type CommandOption struct {
	Name        string
	Description string
	Required    bool
}

// CommandSpec describes a slash command for registration.
type CommandSpec struct {
	Name        string
	Description string
	Options     []CommandOption
}

// Input is a normalized command invocation.
type Input struct {
	Name      string
	Args      map[string]string
	UserID    string
	ChannelID string
	IsSlash   bool
	Reply     func(content string)
}

// Arg returns an option value ("" if absent).
func (in *Input) Arg(name string) string {
	if in.Args == nil {
		return ""
	}
	return in.Args[name]
}
