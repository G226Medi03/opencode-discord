package store

import (
	"path/filepath"
	"testing"
)

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "bindings.json")
	s := Load(path)
	s.Update("ch:1", func(b *Binding) {
		b.Cwd = `D:\Coding\ccst`
		b.Rule = "exact"
		b.SessionID = "ses_x"
	})
	b, ok := s.Get("ch:1")
	if !ok || b.Cwd != `D:\Coding\ccst` || b.SessionID != "ses_x" {
		t.Fatalf("get: %+v %v", b, ok)
	}
	if err := s.SaveNow(); err != nil {
		t.Fatal(err)
	}

	reloaded := Load(path)
	rb, ok := reloaded.Get("ch:1")
	if !ok || rb.Cwd != `D:\Coding\ccst` || rb.SessionID != "ses_x" {
		t.Fatalf("reload: %+v %v", rb, ok)
	}
}

func TestClearSession(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "b.json"))
	s.Update("ch:1", func(b *Binding) {
		b.Cwd = `D:\x`
		b.SessionID = "ses_y"
	})
	s.ClearSession("ch:1")
	b, _ := s.Get("ch:1")
	if b.SessionID != "" {
		t.Fatalf("session not cleared: %+v", b)
	}
	if b.Cwd != `D:\x` {
		t.Fatalf("cwd should be preserved: %+v", b)
	}
}

func TestMissingFile(t *testing.T) {
	s := Load(filepath.Join(t.TempDir(), "nope.json"))
	if _, ok := s.Get("x"); ok {
		t.Fatal("expected empty store")
	}
}
