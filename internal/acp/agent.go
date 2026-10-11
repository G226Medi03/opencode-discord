package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"oc-discord/internal/agent"
	"oc-discord/internal/logx"
)

// Options configures an Agent process.
type Options struct {
	Command  string
	Args     []string
	Cwd      string
	Env      []string
	Logger   *logx.Logger
	OnExit   func(err error)
	OnStderr func(line string)
	RawTap   func(dir string, line []byte)
}

// turn is the per-session sink/permission pair active while a prompt runs.
type turn struct {
	sink agent.Sink
	perm agent.PermissionFunc
}

// Agent is a single `opencode acp` process (one ACP connection, many sessions).
// It implements agent.Agent.
type Agent struct {
	opts Options
	log  *logx.Logger

	mu        sync.Mutex
	conn      *Conn
	cmd       *exec.Cmd
	startedAt time.Time
	lastUsed  time.Time
	exited    bool
	exitErr   error
	exitOnce  sync.Once
	turns     map[string]*turn
	meta      map[string]agent.SessionInfo
}

var _ agent.Agent = (*Agent)(nil)

// NewAgent creates (but does not start) an agent process wrapper.
func NewAgent(opts Options) *Agent {
	return &Agent{
		opts:      opts,
		log:       opts.Logger,
		startedAt: time.Now(),
		lastUsed:  time.Now(),
		turns:     map[string]*turn{},
		meta:      map[string]agent.SessionInfo{},
	}
}

// Start spawns the process and performs the ACP initialize handshake.
func (a *Agent) Start(ctx context.Context) error {
	cmd := exec.Command(a.opts.Command, a.opts.Args...)
	cmd.Dir = a.opts.Cwd
	if a.opts.Env != nil {
		cmd.Env = a.opts.Env
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	a.mu.Lock()
	a.cmd = cmd
	a.mu.Unlock()

	a.conn = NewConn(pipeRW{w: stdin, r: stdout}, a, a.log)
	a.conn.Tap = a.opts.RawTap
	a.conn.OnClose(func(err error) { a.markExited(err) })
	a.conn.Start()

	go a.readStderr(stderr)
	go func() {
		werr := cmd.Wait()
		a.markExited(werr)
	}()

	initParams := map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs": map[string]any{"readTextFile": false, "writeTextFile": false},
		},
		"clientInfo": map[string]any{"name": "oc-discord", "version": "0.1.0"},
	}
	if _, err := a.conn.Call(ctx, "initialize", initParams); err != nil {
		a.Stop()
		return fmt.Errorf("initialize: %w", err)
	}
	a.log.Infof("[acp] 준비 완료 pid=%d cwd=%s", a.PID(), a.opts.Cwd)
	return nil
}

func (a *Agent) readStderr(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 && a.opts.OnStderr != nil {
			a.opts.OnStderr(string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

// PID returns the child process id (0 if not started).
func (a *Agent) PID() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd == nil || a.cmd.Process == nil {
		return 0
	}
	return a.cmd.Process.Pid
}

// Running reports whether the process is alive.
func (a *Agent) Running() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.exited
}

// Cwd returns the process working directory.
func (a *Agent) Cwd() string { return a.opts.Cwd }

// Touch refreshes the last-used timestamp.
func (a *Agent) Touch() {
	a.mu.Lock()
	a.lastUsed = time.Now()
	a.mu.Unlock()
}

// LastUsed returns the last-used time.
func (a *Agent) LastUsed() time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastUsed
}

func (a *Agent) markExited(err error) {
	a.exitOnce.Do(func() {
		a.mu.Lock()
		a.exited = true
		a.exitErr = err
		cb := a.opts.OnExit
		a.mu.Unlock()
		a.log.Warnf("[acp] 종료 cwd=%s err=%v", a.opts.Cwd, err)
		if a.conn != nil {
			a.conn.Close()
		}
		if cb != nil {
			cb(err)
		}
	})
}

// Stop terminates the process tree and the connection.
func (a *Agent) Stop() {
	if a.conn != nil {
		a.conn.Close()
	}
	a.mu.Lock()
	cmd := a.cmd
	a.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/pid", strconv.Itoa(pid), "/t", "/f").Run()
	} else {
		_ = cmd.Process.Kill()
	}
}

// Meta returns tracked metadata for a session.
func (a *Agent) Meta(sessionID string) (agent.SessionInfo, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m, ok := a.meta[sessionID]
	return m, ok
}

// ---- ACP methods (agent.Agent) -------------------------------------------

// NewSession creates a session bound to cwd.
func (a *Agent) NewSession(ctx context.Context, cwd string) (agent.SessionInfo, error) {
	raw, err := a.conn.Call(ctx, "session/new", map[string]any{"cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return agent.SessionInfo{}, err
	}
	var res newSessionResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return agent.SessionInfo{}, err
	}
	m := agent.SessionInfo{ID: res.SessionID, Cwd: cwd, Config: res.ConfigOptions}
	a.mu.Lock()
	a.meta[res.SessionID] = m
	a.mu.Unlock()
	return m, nil
}

// ResumeSession reattaches to an existing session without replaying history.
func (a *Agent) ResumeSession(ctx context.Context, sessionID, cwd string) (agent.SessionInfo, error) {
	raw, err := a.conn.Call(ctx, "session/resume", map[string]any{"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return agent.SessionInfo{}, err
	}
	var res newSessionResult
	_ = json.Unmarshal(raw, &res)
	m := agent.SessionInfo{ID: sessionID, Cwd: cwd, Config: res.ConfigOptions}
	a.mu.Lock()
	a.meta[sessionID] = m
	a.mu.Unlock()
	return m, nil
}

// LoadSession loads a session and replays its history.
func (a *Agent) LoadSession(ctx context.Context, sessionID, cwd string) (agent.SessionInfo, error) {
	raw, err := a.conn.Call(ctx, "session/load", map[string]any{"sessionId": sessionID, "cwd": cwd, "mcpServers": []any{}})
	if err != nil {
		return agent.SessionInfo{}, err
	}
	var res newSessionResult
	_ = json.Unmarshal(raw, &res)
	m := agent.SessionInfo{ID: sessionID, Cwd: cwd, Config: res.ConfigOptions}
	a.mu.Lock()
	a.meta[sessionID] = m
	a.mu.Unlock()
	return m, nil
}

// ListSessions returns the sessions known to the agent for cwd.
func (a *Agent) ListSessions(ctx context.Context, cwd string) ([]agent.ListEntry, error) {
	params := map[string]any{}
	if cwd != "" {
		params["cwd"] = cwd
	}
	raw, err := a.conn.Call(ctx, "session/list", params)
	if err != nil {
		return nil, err
	}
	var res sessionListResult
	_ = json.Unmarshal(raw, &res)
	return res.Sessions, nil
}

// Prompt sends a text prompt, streams updates to sink, and waits for completion.
func (a *Agent) Prompt(ctx context.Context, sessionID, text string, sink agent.Sink, perm agent.PermissionFunc) (string, error) {
	a.Touch()
	a.mu.Lock()
	a.turns[sessionID] = &turn{sink: sink, perm: perm}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.turns, sessionID)
		a.mu.Unlock()
	}()

	raw, err := a.conn.Call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"prompt":    []ContentBlock{{Type: "text", Text: text}},
	})
	if err != nil {
		return "", err
	}
	var pr promptResult
	_ = json.Unmarshal(raw, &pr)
	return pr.StopReason, nil
}

// Cancel requests cancellation of the running turn (notification).
func (a *Agent) Cancel(sessionID string) {
	a.conn.Notify("session/cancel", map[string]any{"sessionId": sessionID})
}

// SetConfig changes a session config option (e.g. model, effort).
func (a *Agent) SetConfig(ctx context.Context, sessionID, configID, value string) ([]agent.ConfigOption, error) {
	raw, err := a.conn.Call(ctx, "session/set_config_option", map[string]any{
		"sessionId": sessionID, "configId": configID, "value": value,
	})
	if err != nil {
		return nil, err
	}
	var res struct {
		ConfigOptions []agent.ConfigOption `json:"configOptions"`
	}
	_ = json.Unmarshal(raw, &res)
	if len(res.ConfigOptions) > 0 {
		a.mu.Lock()
		if m, ok := a.meta[sessionID]; ok {
			m.Config = res.ConfigOptions
			a.meta[sessionID] = m
		}
		a.mu.Unlock()
	}
	return res.ConfigOptions, nil
}

// SetMode switches the session's primary agent (mode).
func (a *Agent) SetMode(ctx context.Context, sessionID, modeID string) error {
	_, err := a.conn.Call(ctx, "session/set_mode", map[string]any{"sessionId": sessionID, "modeId": modeID})
	if err != nil {
		return err
	}
	a.mu.Lock()
	if m, ok := a.meta[sessionID]; ok {
		for i := range m.Config {
			if m.Config[i].ID == "mode" {
				m.Config[i].CurrentValue = json.RawMessage(strconv.Quote(modeID))
			}
		}
		a.meta[sessionID] = m
	}
	a.mu.Unlock()
	return nil
}

// CloseSession detaches a session from the process.
func (a *Agent) CloseSession(ctx context.Context, sessionID string) error {
	_, err := a.conn.Call(ctx, "session/close", map[string]any{"sessionId": sessionID})
	a.mu.Lock()
	delete(a.meta, sessionID)
	a.mu.Unlock()
	return err
}

// DeleteSession removes a session.
func (a *Agent) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := a.conn.Call(ctx, "session/delete", map[string]any{"sessionId": sessionID})
	return err
}

// ---- Handler -------------------------------------------------------------

// OnNotification implements Handler.
func (a *Agent) OnNotification(method string, params json.RawMessage) {
	switch method {
	case "session/update":
		var p struct {
			SessionID string        `json:"sessionId"`
			Update    SessionUpdate `json:"update"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		a.mu.Lock()
		t := a.turns[p.SessionID]
		a.mu.Unlock()
		if t == nil || t.sink == nil {
			return
		}
		switch p.Update.SessionUpdate {
		case "agent_message_chunk":
			if p.Update.Content != nil {
				t.sink.Text(p.Update.Content.Text)
			}
		case "tool_call", "tool_call_update":
			t.sink.Tool(p.Update.Title, p.Update.Status)
		case "current_mode_update":
			if p.Update.CurrentModeID != "" {
				t.sink.Mode(p.Update.CurrentModeID)
			}
		case "config_option_update":
			for _, co := range p.Update.ConfigOptions {
				if co.ID == "mode" {
					if v := co.CurrentString(); v != "" {
						t.sink.Mode(v)
					}
				}
			}
		default:
			a.log.Debugf("[acp] update %s", p.Update.SessionUpdate)
		}
	default:
		a.log.Debugf("[acp:notify] %s", method)
	}
}

// OnRequest implements Handler.
func (a *Agent) OnRequest(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "session/request_permission":
		var p wirePermissionParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		a.mu.Lock()
		t := a.turns[p.SessionID]
		a.mu.Unlock()
		var out agent.Outcome
		if t != nil && t.perm != nil {
			req := agent.PermissionRequest{
				SessionID: p.SessionID,
				Title:     p.ToolCall.Title,
				Kind:      p.ToolCall.Kind,
				RawInput:  p.ToolCall.RawInput,
			}
			for _, o := range p.Options {
				req.Options = append(req.Options, agent.PermissionOption{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
			}
			out = t.perm(req)
		}
		return json.Marshal(newWireOutcome(out))
	case "fs/read_text_file", "fs/write_text_file":
		return nil, fmt.Errorf("not supported: %s", method)
	default:
		return nil, fmt.Errorf("method not implemented: %s", method)
	}
}

// pipeRW adapts separate stdin/stdout pipes to a single ReadWriteCloser.
type pipeRW struct {
	w io.WriteCloser
	r io.ReadCloser
}

func (p pipeRW) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipeRW) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipeRW) Close() error {
	e := p.w.Close()
	if e2 := p.r.Close(); e == nil {
		e = e2
	}
	return e
}
