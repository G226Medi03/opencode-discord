package bridge

import (
	"testing"
	"time"
)

func TestAllQueuesIdle(t *testing.T) {
	b := &Bridge{queues: map[string]*chain{}}
	if !b.allQueuesIdle() {
		t.Fatal("empty pool should be idle")
	}

	c := &chain{}
	b.queues["k"] = c
	started := make(chan struct{})
	release := make(chan struct{})
	c.push(func() {
		close(started)
		<-release
	})
	<-started

	if b.allQueuesIdle() {
		t.Fatal("running chain should not be idle")
	}
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	for !b.allQueuesIdle() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !b.allQueuesIdle() {
		t.Fatal("chain should become idle")
	}
}

func TestTurnGateBlocksNewTurns(t *testing.T) {
	b := &Bridge{}
	b.turnGate.RLock()

	locked := make(chan struct{})
	go func() {
		b.turnGate.Lock()
		close(locked)
		b.turnGate.Unlock()
	}()

	select {
	case <-locked:
		t.Fatal("writer should wait for the running turn")
	case <-time.After(100 * time.Millisecond):
	}

	b.turnGate.RUnlock()

	select {
	case <-locked:
	case <-time.After(2 * time.Second):
		t.Fatal("writer should proceed after the turn ends")
	}
}
