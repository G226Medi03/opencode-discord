// Package bindings maps Discord channels/threads to a (cwd, session) binding.
package bindings

import (
	"oc-discord/internal/config"
	"oc-discord/internal/logx"
	"oc-discord/internal/projects"
	"oc-discord/internal/store"
)

// ChannelRef is a transport-neutral description of a Discord channel.
type ChannelRef struct {
	ID               string
	Name             string
	ParentID         string
	ParentName       string
	ParentCategoryID string
	GuildID          string
	IsDM             bool
	IsThread         bool
}

// Key returns the binding key for a channel.
func Key(ref ChannelRef) string {
	if ref.IsDM {
		return "dm:" + ref.ID
	}
	return "ch:" + ref.ID
}

func nameOf(ref ChannelRef) string {
	if ref.Name != "" {
		return ref.Name
	}
	return ref.ID
}

// Label is a human-readable description of a channel.
func Label(ref ChannelRef) string {
	kind := "channel"
	switch {
	case ref.IsDM:
		kind = "DM"
	case ref.IsThread:
		kind = "thread"
	}
	parent := ""
	if !ref.IsDM && ref.IsThread && ref.ParentName != "" {
		parent = " in #" + ref.ParentName
	}
	return kind + " #" + nameOf(ref) + " (" + ref.ID + ")" + parent
}

// Manager resolves and persists bindings.
type Manager struct {
	cfg        *config.Config
	store      *store.Store
	log        *logx.Logger
	allowed    map[string]bool
	categories map[string]bool
}

// New builds a Manager.
func New(cfg *config.Config, st *store.Store, log *logx.Logger) *Manager {
	m := &Manager{
		cfg:        cfg,
		store:      st,
		log:        log,
		allowed:    map[string]bool{},
		categories: map[string]bool{},
	}
	for _, id := range cfg.AllowedChannelIDs {
		m.allowed[id] = true
	}
	for _, id := range cfg.AllowedCategoryIDs {
		m.categories[id] = true
	}
	return m
}

// IsAllowed reports whether the bot should respond in a channel.
func (m *Manager) IsAllowed(ref ChannelRef) bool {
	if ref.IsDM {
		return true
	}
	if m.allowed[ref.ID] {
		return true
	}
	// Channel directly under an allowed category.
	if ref.ParentID != "" && m.categories[ref.ParentID] {
		return true
	}
	// Thread whose parent channel is allowed (by id or by category).
	if ref.IsThread && ref.ParentID != "" {
		if m.allowed[ref.ParentID] {
			return true
		}
		if ref.ParentCategoryID != "" && m.categories[ref.ParentCategoryID] {
			return true
		}
	}
	return false
}

// NeedsMention reports whether a guild message must mention the bot.
func (m *Manager) NeedsMention(ref ChannelRef) bool {
	if ref.IsDM {
		return false
	}
	if m.cfg.Bot.RequireMention != nil {
		return *m.cfg.Bot.RequireMention
	}
	return !m.cfg.RespondWithoutMention
}

// Get returns the stored binding.
func (m *Manager) Get(ref ChannelRef) (store.Binding, bool) {
	return m.store.Get(Key(ref))
}

// Ensure returns the binding, resolving a cwd if needed (nil when unresolved).
func (m *Manager) Ensure(ref ChannelRef) *store.Binding {
	key := Key(ref)
	if b, ok := m.store.Get(key); ok && b.Cwd != "" && projects.IsDir(b.Cwd) {
		return &b
	}

	if ref.IsThread && ref.ParentID != "" {
		pk := "ch:" + ref.ParentID
		if pb, ok := m.store.Get(pk); ok && pb.Cwd != "" && projects.IsDir(pb.Cwd) {
			b := m.store.Update(key, func(x *store.Binding) {
				x.Cwd = pb.Cwd
				x.Rule = "parent"
				x.ChannelName = ref.Name
			})
			m.log.Infof("[bind] %s -> %s (부모 %s 상속)", key, pb.Cwd, pk)
			return &b
		}
	}

	name := ref.Name
	if ref.IsThread && name == "" {
		name = ref.ParentName
	}
	hit := projects.Resolve(name, m.cfg.CwdRoot, m.cfg.CwdOverrides)
	if hit == nil {
		m.log.Warnf("[bind] %s (%s) 규칙 불일치 — cwd 미결정", key, name)
		return nil
	}
	b := m.store.Update(key, func(x *store.Binding) {
		x.Cwd = hit.Folder
		x.Rule = hit.Rule
		x.ChannelName = name
	})
	m.log.Infof("[bind] %s (%s) -> %s [%s]", key, name, hit.Folder, hit.Rule)
	return &b
}

// SetCwd stores a manual cwd.
func (m *Manager) SetCwd(ref ChannelRef, cwd, rule string) store.Binding {
	return m.store.Update(Key(ref), func(x *store.Binding) {
		x.Cwd = cwd
		x.Rule = rule
		x.ChannelName = nameOf(ref)
	})
}

// SetSession records the session id for a channel.
func (m *Manager) SetSession(ref ChannelRef, sessionID, createdAt string) store.Binding {
	return m.store.Update(Key(ref), func(x *store.Binding) {
		x.SessionID = sessionID
		x.SessionCreatedAt = createdAt
	})
}

// ResetSession clears the session id so the next turn creates/resumes fresh.
func (m *Manager) ResetSession(ref ChannelRef) {
	m.store.ClearSession(Key(ref))
}

// Store exposes the underlying store for commands that need direct access.
func (m *Manager) Store() *store.Store { return m.store }
