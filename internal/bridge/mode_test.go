package bridge

import (
	"context"
	"testing"

	"oc-discord/internal/agent"
	"oc-discord/internal/logx"
	"oc-discord/internal/store"
)

type modeAgent struct {
	agent.Agent
	mode string
	sets []string
}

func (a *modeAgent) SetMode(ctx context.Context, sessionID, mode string) error {
	a.mode = mode
	a.sets = append(a.sets, mode)
	return nil
}

func (a *modeAgent) Running() bool { return true }

func TestModeAtReceipt(t *testing.T) {
	b := &Bridge{sessions: map[string]*sessionState{}}
	if got := b.modeAtReceipt("k", store.Binding{Agent: "plan"}); got != "plan" {
		t.Fatalf("binding mode: %q", got)
	}
	b.sessions["k"] = &sessionState{mode: "build"}
	if got := b.modeAtReceipt("k", store.Binding{}); got != "build" {
		t.Fatalf("session mode: %q", got)
	}
	if got := b.modeAtReceipt("missing", store.Binding{}); got != "" {
		t.Fatalf("missing mode: %q", got)
	}
}

func TestApplyTurnMode(t *testing.T) {
	fa := &modeAgent{}
	b := &Bridge{
		logger:   logx.New("error", ""),
		sessions: map[string]*sessionState{},
	}
	st := &sessionState{agent: fa, sessionID: "s1", mode: "build"}
	b.sessions["k"] = st

	b.applyTurnMode(context.Background(), "k", st, "plan")
	if len(fa.sets) != 1 || fa.sets[0] != "plan" {
		t.Fatalf("sets: %#v", fa.sets)
	}
	if st.mode != "plan" {
		t.Fatalf("session mode: %q", st.mode)
	}

	// Same mode should not call SetMode again.
	b.applyTurnMode(context.Background(), "k", st, "plan")
	if len(fa.sets) != 1 {
		t.Fatalf("unexpected extra SetMode: %#v", fa.sets)
	}
}

// Ensure the fake agent satisfies the interface at compile time.
var _ agent.Agent = (*modeAgent)(nil)
