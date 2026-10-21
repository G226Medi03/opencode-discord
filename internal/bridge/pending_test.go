package bridge

import "testing"

func TestPendingConfigLastWins(t *testing.T) {
	b := &Bridge{pending: map[string]pendingConfig{}}
	b.setPendingMode("k", "plan")
	b.setPendingModel("k", "model-a")
	b.setPendingMode("k", "build")

	p := b.pending["k"]
	if p.mode != "build" || p.model != "model-a" {
		t.Fatalf("got %#v", p)
	}

	b.clearPendingMode("k")
	p = b.pending["k"]
	if p.mode != "" || p.model != "model-a" {
		t.Fatalf("after clear mode: %#v", p)
	}

	b.clearPendingModel("k")
	if _, ok := b.pending["k"]; ok {
		t.Fatalf("entry should be removed when empty")
	}
}
