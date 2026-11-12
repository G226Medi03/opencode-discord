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

func TestClearQueueOnlyCurrent(t *testing.T) {
	a1 := &modeAgent{}
	a2 := &modeAgent{}
	b := &Bridge{
		queues: map[string]*chain{},
		sessions: map[string]*sessionState{
			"k1": {agent: a1, sessionID: "s1", mode: "build"},
			"k2": {agent: a2, sessionID: "s2", mode: "build"},
		},
	}

	c1 := &chain{}
	c2 := &chain{}
	start1 := make(chan struct{})
	release1 := make(chan struct{})
	start2 := make(chan struct{})
	release2 := make(chan struct{})

	c1.push(func() { close(start1); <-release1 })
	c2.push(func() { close(start2); <-release2 })
	<-start1
	<-start2
	c1.push(func() {})
	c1.push(func() {})
	c2.push(func() {})
	b.queues["k1"] = c1
	b.queues["k2"] = c2

	if n := b.clearQueue("k1"); n != 2 {
		t.Fatalf("k1 cleared=%d", n)
	}
	if c1.depth() != 0 {
		t.Fatalf("k1 queue should be empty, depth=%d", c1.depth())
	}
	if c2.depth() != 1 {
		t.Fatalf("k2 queue should be untouched, depth=%d", c2.depth())
	}

	if !b.cancelSession("k1") {
		t.Fatal("k1 should be cancelled")
	}
	if len(a1.cancels) != 1 || a1.cancels[0] != "s1" {
		t.Fatalf("k1 cancels=%v", a1.cancels)
	}
	if len(a2.cancels) != 0 {
		t.Fatalf("k2 should not be cancelled: %v", a2.cancels)
	}
	if b.cancelSession("missing") {
		t.Fatal("missing key should not be cancelled")
	}

	close(release1)
	close(release2)
	deadline := time.Now().Add(2 * time.Second)
	for (c1.busy() || c2.busy()) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}
