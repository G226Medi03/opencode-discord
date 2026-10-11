package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"oc-discord/internal/logx"
)

// Handler receives inbound ACP messages.
type Handler interface {
	// OnNotification handles a notification (no id), e.g. session/update.
	OnNotification(method string, params json.RawMessage)
	// OnRequest handles an agent -> client request; the returned value is sent
	// back as the JSON-RPC result. An error is returned to the agent as an
	// internal error.
	OnRequest(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error)
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type rpcResult struct {
	result json.RawMessage
	err    error
}

type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Conn is a newline-delimited JSON-RPC 2.0 connection.
type Conn struct {
	rw      io.ReadWriteCloser
	handler Handler
	log     *logx.Logger

	writeMu  sync.Mutex
	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan rpcResult
	closed   bool
	closeErr error
	onClose  func(error)
	wg       sync.WaitGroup

	ctx    context.Context
	cancel context.CancelFunc

	// Tap, if set, receives every raw protocol line (dir is "in" or "out").
	Tap func(dir string, line []byte)
}

// NewConn wraps rw. Call Start to begin reading.
func NewConn(rw io.ReadWriteCloser, h Handler, log *logx.Logger) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		rw:      rw,
		handler: h,
		log:     log,
		pending: map[int64]chan rpcResult{},
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start launches the read loop.
func (c *Conn) Start() {
	c.wg.Add(1)
	go c.readLoop()
}

func (c *Conn) readLoop() {
	defer c.wg.Done()
	r := bufio.NewReaderSize(c.rw, 1<<16)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			c.handleLine(line)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.fail(err)
			} else {
				c.fail(io.EOF)
			}
			return
		}
	}
}

func (c *Conn) handleLine(line []byte) {
	line = trimSpace(line)
	if len(line) == 0 {
		return
	}
	if c.Tap != nil {
		c.Tap("in", line)
	}
	var m rpcMessage
	if err := json.Unmarshal(line, &m); err != nil {
		c.log.Debugf("[acp:non-json] %s", string(truncBytes(line, 200)))
		return
	}
	switch {
	case m.Method == "" && len(m.ID) > 0:
		// response to one of our calls
		var id int64
		if json.Unmarshal(m.ID, &id) != nil {
			return
		}
		c.mu.Lock()
		ch, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.mu.Unlock()
		if !ok {
			return
		}
		if m.Error != nil {
			ch <- rpcResult{err: fmt.Errorf("acp error %d: %s", m.Error.Code, m.Error.Message)}
		} else {
			ch <- rpcResult{result: m.Result}
		}
	case m.Method != "" && len(m.ID) == 0:
		// notification
		c.handler.OnNotification(m.Method, m.Params)
	default:
		// agent -> client request
		id := append(json.RawMessage(nil), m.ID...)
		method := m.Method
		params := m.Params
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			res, err := c.handler.OnRequest(c.ctx, method, params)
			if err != nil {
				c.replyError(id, -32603, err.Error())
				return
			}
			c.replyResult(id, res)
		}()
	}
}

// Call sends a request and waits for the response or ctx cancellation.
func (c *Conn) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	p, err := marshalParams(params)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if c.closed {
		err := c.closeErr
		c.mu.Unlock()
		if err == nil {
			err = errors.New("acp connection closed")
		}
		return nil, err
	}
	id := c.nextID
	c.nextID++
	ch := make(chan rpcResult, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if len(p) > 0 {
		msg["params"] = p
	}
	if err := c.writeMessage(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	case r := <-ch:
		return r.result, r.err
	}
}

// Notify sends a notification (no response expected).
func (c *Conn) Notify(method string, params any) {
	p, err := marshalParams(params)
	if err != nil {
		return
	}
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if len(p) > 0 {
		msg["params"] = p
	}
	_ = c.writeMessage(msg)
}

func (c *Conn) replyResult(id json.RawMessage, result json.RawMessage) {
	if len(result) == 0 {
		result = json.RawMessage("null")
	}
	_ = c.writeMessage(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (c *Conn) replyError(id json.RawMessage, code int, message string) {
	_ = c.writeMessage(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

func (c *Conn) writeMessage(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	body = append(body, '\n')
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return errors.New("acp connection closed")
	}
	c.log.Debugf("[acp>>] %s", string(truncBytes(body, 500)))
	if c.Tap != nil {
		c.Tap("out", trimSpace(body))
	}
	_, err = c.rw.Write(body)
	return err
}

func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.closeErr = err
	for id, ch := range c.pending {
		ch <- rpcResult{err: err}
		delete(c.pending, id)
	}
	cb := c.onClose
	c.mu.Unlock()
	c.cancel()
	_ = c.rw.Close()
	if cb != nil {
		cb(err)
	}
}

// Close shuts the connection down.
func (c *Conn) Close() {
	c.fail(errors.New("closed by client"))
}

// Closed reports whether the connection has terminated.
func (c *Conn) Closed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// OnClose registers a callback invoked once when the connection dies.
func (c *Conn) OnClose(fn func(error)) {
	c.mu.Lock()
	c.onClose = fn
	c.mu.Unlock()
}

// Wait blocks until the read loop finishes.
func (c *Conn) Wait() { c.wg.Wait() }

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		return raw, nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func trimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}

func truncBytes(b []byte, n int) []byte {
	if len(b) <= n {
		return b
	}
	return b[:n]
}
