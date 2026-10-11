package bindings

import (
	"path/filepath"
	"testing"

	"oc-discord/internal/config"
	"oc-discord/internal/logx"
	"oc-discord/internal/store"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	cfg := &config.Config{
		AllowedChannelIDs:     []string{"c1"},
		AllowedCategoryIDs:    []string{"cat1"},
		RespondWithoutMention: true,
	}
	st := store.Load(filepath.Join(t.TempDir(), "b.json"))
	return New(cfg, st, logx.New("error", ""))
}

func TestIsAllowed(t *testing.T) {
	m := newTestManager(t)

	cases := []struct {
		name string
		ref  ChannelRef
		want bool
	}{
		{"dm", ChannelRef{ID: "d1", IsDM: true}, true},
		{"explicit id", ChannelRef{ID: "c1"}, true},
		{"channel under allowed category", ChannelRef{ID: "c2", ParentID: "cat1"}, true},
		{"thread of allowed channel", ChannelRef{ID: "t1", IsThread: true, ParentID: "c1"}, true},
		{"thread via parent category", ChannelRef{ID: "t2", IsThread: true, ParentID: "c2", ParentCategoryID: "cat1"}, true},
		{"other channel", ChannelRef{ID: "c3", ParentID: "cat9"}, false},
		{"thread of disallowed", ChannelRef{ID: "t3", IsThread: true, ParentID: "c3"}, false},
	}
	for _, c := range cases {
		if got := m.IsAllowed(c.ref); got != c.want {
			t.Errorf("%s: IsAllowed=%v, want %v", c.name, got, c.want)
		}
	}
}
