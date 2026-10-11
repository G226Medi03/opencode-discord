// Package chatcmd is a small, platform-agnostic command registry. Commands are
// recognized from both slash input and text triggers ("/name ..." or "!name ...").
package chatcmd

import (
	"strings"

	"oc-discord/internal/discord"
)

// Option is a slash-command option.
type Option struct {
	Name        string
	Description string
	Required    bool
}

// Command is a registered command.
type Command struct {
	Name        string
	Description string
	Usage       string
	Options     []Option
	ArgName     string // option that receives the text remainder ("" = none)
	Handler     func(*Invocation)
}

// Invocation is a resolved command call.
type Invocation struct {
	Name      string
	Args      map[string]string
	ChannelID string
	UserID    string
	IsSlash   bool
	Reply     func(content string)
}

// Arg returns an option value ("" if absent).
func (in *Invocation) Arg(name string) string {
	if in.Args == nil {
		return ""
	}
	return in.Args[name]
}

// Registry holds commands by name.
type Registry struct {
	order  []*Command
	byName map[string]*Command
}

// New creates an empty registry.
func New() *Registry {
	return &Registry{byName: map[string]*Command{}}
}

// Add registers a command.
func (r *Registry) Add(c *Command) {
	r.order = append(r.order, c)
	r.byName[c.Name] = c
}

// Get looks up a command by name.
func (r *Registry) Get(name string) *Command {
	return r.byName[strings.ToLower(strings.TrimSpace(name))]
}

// All returns the commands in registration order.
func (r *Registry) All() []*Command { return r.order }

// Specs renders the registry as slash command specs.
func (r *Registry) Specs() []discord.CommandSpec {
	out := make([]discord.CommandSpec, 0, len(r.order))
	for _, c := range r.order {
		spec := discord.CommandSpec{Name: c.Name, Description: c.Description}
		for _, o := range c.Options {
			spec.Options = append(spec.Options, discord.CommandOption{Name: o.Name, Description: o.Description, Required: o.Required})
		}
		out = append(out, spec)
	}
	return out
}

// Parse recognizes "/name ..." or "!name ..." text triggers. It returns ok only
// for registered command names.
func (r *Registry) Parse(text string) (name, rest string, ok bool) {
	t := strings.TrimSpace(text)
	if t == "" || (t[0] != '!' && t[0] != '/') {
		return "", "", false
	}
	body := t[1:]
	name = body
	if i := strings.IndexAny(body, " \t\n"); i >= 0 {
		name = body[:i]
		rest = strings.TrimSpace(body[i+1:])
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || r.Get(name) == nil {
		return "", "", false
	}
	return name, rest, true
}

// TextArgs maps a parsed text remainder onto the command's argument option.
func (r *Registry) TextArgs(name, rest string) map[string]string {
	args := map[string]string{}
	c := r.Get(name)
	if c != nil && c.ArgName != "" && rest != "" {
		args[c.ArgName] = rest
	}
	return args
}
