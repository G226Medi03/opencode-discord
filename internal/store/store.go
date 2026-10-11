// Package store persists channel/thread bindings to a JSON file atomically with
// a short debounce.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Binding is the persisted per-channel/thread state.
type Binding struct {
	Cwd              string `json:"cwd,omitempty"`
	Rule             string `json:"rule,omitempty"`
	ChannelName      string `json:"channelName,omitempty"`
	SessionID        string `json:"sessionId,omitempty"`
	SessionCreatedAt string `json:"sessionCreatedAt,omitempty"`
	Model            string `json:"model,omitempty"`
	Agent            string `json:"agent,omitempty"`
	UpdatedAt        string `json:"updatedAt,omitempty"`
}

type fileFormat struct {
	Version  int                `json:"version"`
	Bindings map[string]Binding `json:"bindings"`
}

// Store is a debounced, atomically-persisted binding map.
type Store struct {
	mu    sync.Mutex
	path  string
	data  map[string]Binding
	timer *time.Timer
	dirty bool
}

// Load reads bindings from path (missing/corrupt files yield an empty store).
func Load(path string) *Store {
	s := &Store{path: path, data: map[string]Binding{}}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	b = []byte(trimBOM(string(b)))
	var f fileFormat
	if json.Unmarshal(b, &f) != nil {
		_ = os.WriteFile(path+".corrupt.bak", b, 0o644)
		return s
	}
	if f.Bindings != nil {
		s.data = f.Bindings
	}
	return s
}

func trimBOM(s string) string {
	if len(s) >= 3 && s[0] == 0xEF && s[1] == 0xBB && s[2] == 0xBF {
		return s[3:]
	}
	return s
}

func nowISO() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// Get returns the binding for id.
func (s *Store) Get(id string) (Binding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.data[id]
	return b, ok
}

// All returns a copy of every binding.
func (s *Store) All() map[string]Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Binding, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

// Update mutates the binding for id (creating it if needed) and schedules a save.
func (s *Store) Update(id string, fn func(*Binding)) Binding {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.data[id]
	fn(&b)
	b.UpdatedAt = nowISO()
	s.data[id] = b
	s.scheduleLocked()
	return b
}

// ClearSession drops the stored ACP session id so the next turn starts fresh.
func (s *Store) ClearSession(id string) {
	s.Update(id, func(b *Binding) {
		b.SessionID = ""
		b.SessionCreatedAt = ""
	})
}

func (s *Store) scheduleLocked() {
	s.dirty = true
	if s.timer != nil {
		return
	}
	s.timer = time.AfterFunc(300*time.Millisecond, func() {
		_ = s.SaveNow()
	})
}

// SaveNow flushes pending changes to disk.
func (s *Store) SaveNow() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !s.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	body, err := json.MarshalIndent(fileFormat{Version: 1, Bindings: s.data}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}
