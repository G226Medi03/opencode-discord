package bridge

import (
	"testing"
	"time"
)

func TestChainClearSkipsQueued(t *testing.T) {
	c := &chain{}
	started := make(chan struct{})
	release := make(chan struct{})
	ran := 0

	c.push(func() {
		close(started)
		<-release
	})
	<-started

	c.push(func() { ran++ })
	c.push(func() { ran++ })

	if n := c.clear(); n != 2 {
		t.Fatalf("expected 2 cleared items, got %d", n)
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for c.busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if ran != 0 {
		t.Fatalf("queued items should not run after clear, ran=%d", ran)
	}
}

func TestClearAllQueues(t *testing.T) {
	a := &modeAgent{}
	b := &Bridge{
		queues: map[string]*chain{},
		sessions: map[string]*sessionState{
			"k1": {agent: a, sessionID: "s1", mode: "build"},
			"k2": {agent: a, sessionID: "s1", mode: "build"}, // thread sharing the session
		},
	}
	c := &chain{}
	started := make(chan struct{})
	release := make(chan struct{})
	c.push(func() {
		close(started)
		<-release
	})
	<-started
	c.push(func() {})
	c.push(func() {})
	b.queues["k1"] = c

	queued, cancelled := b.clearAllQueues()
	if queued != 2 || cancelled != 1 {
		t.Fatalf("queued=%d cancelled=%d", queued, cancelled)
	}
	if len(a.cancels) != 1 || a.cancels[0] != "s1" {
		t.Fatalf("cancels=%v", a.cancels)
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for c.busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}
