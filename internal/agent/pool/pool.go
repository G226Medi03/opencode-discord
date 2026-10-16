// Package pool keeps one agent process per key (usually a project cwd) and
// reaps idle processes. It is agent-implementation agnostic.
package pool

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"oc-discord/internal/agent"
	"oc-discord/internal/logx"
)

// Options configures a Pool.
type Options struct {
	Factory         agent.Factory
	IdleShutdownMin int
	MaxAgents       int
	Logger          *logx.Logger
}

// Entry is a pooled agent.
type Entry struct {
	Key       string
	Agent     agent.Agent
	Refs      int
	Sessions  map[string]struct{}
	LastUsed  time.Time
	StartedAt time.Time
}

// Pool manages agents keyed by absolute cwd.
type Pool struct {
	factory   agent.Factory
	idleMin   int
	maxAgents int
	log       *logx.Logger

	mu      sync.Mutex
	startMu sync.Mutex
	entries map[string]*Entry
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

var _ agent.Pool = (*Pool)(nil)

// New creates a pool and starts its reaper.
func New(opts Options) *Pool {
	logger := opts.Logger
	if logger == nil {
		logger = logx.New("error", "")
	}
	p := &Pool{
		factory:   opts.Factory,
		idleMin:   opts.IdleShutdownMin,
		maxAgents: opts.MaxAgents,
		log:       logger,
		entries:   map[string]*Entry{},
		stopCh:    make(chan struct{}),
	}
	p.wg.Add(1)
	go p.reapLoop()
	return p
}

// Size returns the number of live agents.
func (p *Pool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// Info returns a snapshot of pooled agents.
func (p *Pool) Info() []Entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Entry, 0, len(p.entries))
	for _, e := range p.entries {
		out = append(out, *e)
	}
	return out
}

// Acquire returns a running agent for cwd, starting one if necessary.
func (p *Pool) Acquire(ctx context.Context, cwd string) (agent.Agent, string, error) {
	key := absKey(cwd)

	p.mu.Lock()
	if e := p.entries[key]; e != nil {
		if e.Agent.Running() {
			e.Refs++
			e.LastUsed = time.Now()
			ag := e.Agent
			p.mu.Unlock()
			return ag, key, nil
		}
		delete(p.entries, key)
	}
	p.mu.Unlock()

	p.startMu.Lock()
	defer p.startMu.Unlock()

	p.mu.Lock()
	if e := p.entries[key]; e != nil && e.Agent.Running() {
		e.Refs++
		e.LastUsed = time.Now()
		ag := e.Agent
		p.mu.Unlock()
		return ag, key, nil
	}
	p.mu.Unlock()

	p.evictIfNeeded()

	var ag agent.Agent
	ag, err := p.factory(key, func(err error) {
		p.log.Warnf("[pool] 프로세스 종료 감지 cwd=%s err=%v", key, err)
		p.remove(key, ag)
	})
	if err != nil {
		return nil, key, err
	}
	if err := ag.Start(ctx); err != nil {
		return nil, key, err
	}
	now := time.Now()
	e := &Entry{Key: key, Agent: ag, Refs: 1, Sessions: map[string]struct{}{}, LastUsed: now, StartedAt: now}
	p.mu.Lock()
	p.entries[key] = e
	p.mu.Unlock()
	p.log.Infof("[pool] 시작 pid=%d cwd=%s", ag.PID(), key)
	return ag, key, nil
}

func (p *Pool) remove(key string, ag agent.Agent) {
	p.mu.Lock()
	if e := p.entries[key]; e != nil && e.Agent == ag {
		delete(p.entries, key)
	}
	p.mu.Unlock()
}

// Release decrements the in-use count for key.
func (p *Pool) Release(key string) {
	p.mu.Lock()
	if e := p.entries[key]; e != nil {
		if e.Refs > 0 {
			e.Refs--
		}
		e.LastUsed = time.Now()
	}
	p.mu.Unlock()
}

// Touch refreshes the idle timer for key.
func (p *Pool) Touch(key string) {
	p.mu.Lock()
	if e := p.entries[key]; e != nil {
		e.LastUsed = time.Now()
	}
	p.mu.Unlock()
}

// Track records a session id against a key.
func (p *Pool) Track(key, sessionID string) {
	p.mu.Lock()
	if e := p.entries[key]; e != nil && e.Sessions != nil {
		e.Sessions[sessionID] = struct{}{}
	}
	p.mu.Unlock()
}

// AgentFor returns the running agent for cwd, if any.
func (p *Pool) AgentFor(cwd string) agent.Agent {
	key := absKey(cwd)
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.entries[key]; e != nil && e.Agent.Running() {
		return e.Agent
	}
	return nil
}

// Kill stops and removes the agent for cwd.
func (p *Pool) Kill(cwd string) bool {
	key := absKey(cwd)
	p.mu.Lock()
	e := p.entries[key]
	if e != nil {
		delete(p.entries, key)
	}
	p.mu.Unlock()
	if e == nil {
		return false
	}
	p.stopEntry("kill", e)
	return true
}

// stopEntry stops an entry's agent and records why it was stopped.
func (p *Pool) stopEntry(reason string, e *Entry) {
	p.log.Infof("[pool] 종료 reason=%s pid=%d cwd=%s 가동=%s", reason, e.Agent.PID(), e.Key, time.Since(e.StartedAt).Round(time.Second))
	e.Agent.Stop()
}

func (p *Pool) evictIfNeeded() {
	if p.maxAgents <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.entries) < p.maxAgents {
		return
	}
	var oldest *Entry
	for _, e := range p.entries {
		if e.Refs > 0 {
			continue
		}
		if oldest == nil || e.LastUsed.Before(oldest.LastUsed) {
			oldest = e
		}
	}
	if oldest == nil {
		p.log.Warnf("[pool] maxAgents=%d 도달, 유휴 프로세스 없음 — 초과 허용", p.maxAgents)
		return
	}
	p.log.Infof("[pool] maxAgents=%d → 최고 오래된 유휴 프로세스 종료 cwd=%s", p.maxAgents, oldest.Key)
	delete(p.entries, oldest.Key)
	go p.stopEntry("evict", oldest)
}

func (p *Pool) reapLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.reap()
		}
	}
}

func (p *Pool) reap() {
	if p.idleMin <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(p.idleMin) * time.Minute)
	p.mu.Lock()
	var victims []*Entry
	for key, e := range p.entries {
		if e.Refs == 0 && e.Agent.Running() && e.LastUsed.Before(cutoff) {
			victims = append(victims, e)
			delete(p.entries, key)
		}
	}
	p.mu.Unlock()
	for _, e := range victims {
		p.stopEntry("idle", e)
	}
}

// Shutdown stops every agent.
func (p *Pool) Shutdown() {
	close(p.stopCh)
	p.mu.Lock()
	agents := make([]agent.Agent, 0, len(p.entries))
	for _, e := range p.entries {
		agents = append(agents, e.Agent)
	}
	p.entries = map[string]*Entry{}
	p.mu.Unlock()
	p.log.Infof("[pool] 전체 종료 reason=shutdown 개수=%d", len(agents))
	var wg sync.WaitGroup
	for _, ag := range agents {
		wg.Add(1)
		go func(a agent.Agent) { defer wg.Done(); a.Stop() }(ag)
	}
	wg.Wait()
}

func absKey(cwd string) string {
	if key, err := filepath.Abs(cwd); err == nil {
		return key
	}
	return cwd
}
