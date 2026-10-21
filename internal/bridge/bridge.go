// Package bridge connects a chat.App to an agent.Pool and implements the
// OpenCode connector: bindings (channel/thread -> cwd/session), turn execution,
// streaming, permissions, and the command set.
package bridge

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/G226Medi03/chat-app-adapter/chat"
	"github.com/G226Medi03/chat-app-adapter/chatcmd"
	"oc-discord/internal/agent"
	"oc-discord/internal/bindings"
	"oc-discord/internal/config"
	"oc-discord/internal/logx"
	"oc-discord/internal/projects"
	"oc-discord/internal/store"
)

const thinking = "⏳ 작업 중…"

// Bridge is the OpenCode <-> Discord connector.
type Bridge struct {
	config     *config.Config
	logger     *logx.Logger
	app        chat.App
	agent_pool agent.Pool
	store      *store.Store
	bind       *bindings.Manager

	registry *chatcmd.Registry

	mu        sync.Mutex
	queues    map[string]*chain
	sessions  map[string]*sessionState
	autoAllow map[string]bool
}

type sessionState struct {
	agent     agent.Agent
	cwd       string
	sessionID string
	mode      string
	busy      bool
}

// New builds a Bridge.
func New(cfg *config.Config, log *logx.Logger, t chat.App, pool agent.Pool) *Bridge {
	st := store.Load(cfg.StatePath)
	b := &Bridge{
		config:     cfg,
		logger:     log,
		app:        t,
		agent_pool: pool,
		store:      st,
		bind:       bindings.New(cfg, st, log),
		queues:     map[string]*chain{},
		sessions:   map[string]*sessionState{},
		autoAllow:  map[string]bool{},
	}
	b.registry = b.buildRegistry()
	return b
}

// Store exposes the binding store.
func (b *Bridge) Store() *store.Store { return b.store }

// Deploy registers the slash commands on the transport.
func (b *Bridge) Deploy() error {
	return b.app.RegisterCommands(b.registry.Specs())
}

// Run wires handlers, optionally deploys, and runs the transport until ctx ends.
func (b *Bridge) Run(ctx context.Context) error {
	b.app.OnMessage(b.handleMessage)
	b.app.OnCommand(b.handleCommand)
	if b.config.DeployCommands {
		if err := b.Deploy(); err != nil {
			b.logger.Warnf("[deploy] 실패: %v", err)
		}
	}
	err := b.app.Run(ctx)
	_ = b.app.Close()
	_ = b.store.SaveNow()
	b.agent_pool.Shutdown()
	return err
}

// ---- inbound -------------------------------------------------------------

func (b *Bridge) handleMessage(m chat.Message) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Errorf("[msg] panic: %v", r)
		}
	}()

	ref := b.channelRef(m.ChannelID)
	if !b.bind.IsAllowed(ref) {
		return
	}
	if !b.isOwner(m.AuthorID) {
		if ref.IsDM {
			b.sendChannel(m.ChannelID, "권한이 없습니다(owner 전용).")
		}
		return
	}
	if b.bind.NeedsMention(ref) && !m.MentionedBot {
		return
	}
	text := b.extractText(m.Content)
	if text == "" && len(m.Attachments) == 0 {
		return
	}

	key := bindings.Key(ref)

	// Slash-equivalent text triggers ("/cmd dir" or "!cmd dir").
	if text != "" {
		if name, rest, ok := b.registry.Parse(text); ok {
			b.dispatch(&chatcmd.Invocation{
				Name:      name,
				Args:      b.registry.TextArgs(name, rest),
				ChannelID: m.ChannelID,
				UserID:    m.AuthorID,
				Reply:     func(c string) { b.sendChannel(m.ChannelID, c) },
			})
			return
		}
	}

	bindingPtr := b.bind.Ensure(ref)
	if bindingPtr == nil {
		b.sendChannel(m.ChannelID, b.unresolvedNotice(ref))
		return
	}
	b.enqueue(key, func() { b.runTurn(m, ref, key, *bindingPtr, text) })
}

func (b *Bridge) handleCommand(in chat.Input) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Errorf("[cmd] panic: %v", r)
		}
	}()
	if !b.isOwner(in.UserID) {
		if in.Reply != nil {
			in.Reply("권한이 없습니다(owner 전용).")
		}
		return
	}
	b.dispatch(&chatcmd.Invocation{
		Name:      in.Name,
		Args:      in.Args,
		ChannelID: in.ChannelID,
		UserID:    in.UserID,
		IsSlash:   true,
		Reply:     in.Reply,
	})
}

func (b *Bridge) dispatch(in *chatcmd.Invocation) {
	if !b.isOwner(in.UserID) {
		in.Reply("권한이 없습니다(owner 전용).")
		return
	}
	c := b.registry.Get(in.Name)
	if c == nil {
		in.Reply("알 수 없는 커맨드입니다: " + in.Name)
		return
	}
	c.Handler(in)
}

// ---- turn execution ------------------------------------------------------

func (b *Bridge) runTurn(m chat.Message, ref bindings.ChannelRef, key string, binding store.Binding, text string) {
	ctx := context.Background()
	cwd := binding.Cwd
	if !projects.IsDir(cwd) {
		b.sendChannel(ref.ID, fmt.Sprintf("cwd 폴더가 없습니다: `%s`", cwd))
		return
	}

	atts, err := b.downloadAttachments(m)
	if err != nil {
		b.logger.Warnf("[attach] 처리 실패 message=%s: %v", m.ID, err)
		b.sendChannel(ref.ID, "첨부 파일 처리 실패: "+err.Error())
		return
	}
	promptText := text
	if strings.TrimSpace(promptText) == "" && len(atts) > 0 {
		promptText = "첨부한 파일을 확인해 주세요."
	}

	ag, agKey, err := b.agent_pool.Acquire(ctx, cwd)
	if err != nil {
		b.logger.Errorf("[turn] 에이전트 기동 실패: %v", err)
		b.sendChannel(ref.ID, fmt.Sprintf("opencode acp 기동 실패: `%v`", err))
		return
	}
	sess, err := b.ensureSession(ctx, ref, key, ag, agKey, binding)
	if err != nil {
		b.agent_pool.Release(agKey)
		b.logger.Errorf("[turn] 세션 생성 실패: %v", err)
		b.sendChannel(ref.ID, fmt.Sprintf("세션 생성 실패: `%v`", err))
		return
	}

	// Optional: answer inside a new thread that shares the parent session.
	if b.config.Bot.Threads && !ref.IsDM && !ref.IsThread {
		if th, terr := b.app.StartThread(ref.ID, m.ID, threadName(m)); terr == nil {
			tref := bindings.ChannelRef{
				ID: th.ID, Name: th.Name, ParentID: ref.ID, ParentName: ref.Name,
				GuildID: th.GuildID, IsThread: true,
			}
			tkey := bindings.Key(tref)
			b.bind.SetCwd(tref, binding.Cwd, "thread-of-parent")
			b.mu.Lock()
			b.sessions[tkey] = sess // share the parent's session
			b.mu.Unlock()
			b.logger.Infof("[thread] %s 생성 (부모 세션 %s 공유)", th.ID, sess.sessionID)
			ref = tref
			key = tkey
		} else {
			b.logger.Warnf("[thread] 생성 실패 → 채널에 응답: %v", terr)
		}
	}

	b.setBusy(key, true)
	defer b.setBusy(key, false)

	st := newStream(b, ref.ID, sess.mode)
	if ref.IsThread {
		if ph, err := b.app.Send(ref.ID, thinking); err == nil {
			st.attach(ph)
		} else {
			b.logger.Warnf("[turn] placeholder 전송 실패: %v", err)
		}
	} else if ph, err := b.app.Reply(ref.ID, m.ID, thinking); err == nil {
		st.attach(ph)
	} else {
		b.logger.Warnf("[turn] placeholder 전송 실패: %v", err)
	}

	sink := &turnSink{b: b, stream: st, key: key}
	perm := func(req agent.PermissionRequest) agent.Outcome {
		return b.handlePermission(ref, sess.sessionID, req)
	}

	stop, err := ag.Prompt(ctx, sess.sessionID, b.buildPrompt(m, ref, promptText, sess.mode, atts), sink, perm)
	b.agent_pool.Release(agKey)
	if err != nil && !ag.Running() && st.length() == 0 && !sink.toolSeen {
		b.logger.Warnf("[turn] 프로세스 종료로 실패 — 세션 %s 로 재기동 후 재시도 key=%s: %v", sess.sessionID, key, err)
		var ag2 agent.Agent
		ag2, agKey, err = b.agent_pool.Acquire(ctx, cwd)
		if err == nil {
			ag = ag2
			sess, err = b.ensureSession(ctx, ref, key, ag, agKey, binding)
			if err != nil {
				b.agent_pool.Release(agKey)
			}
		}
		if err == nil {
			stop, err = ag.Prompt(ctx, sess.sessionID, b.buildPrompt(m, ref, promptText, sess.mode, atts), sink, perm)
			b.agent_pool.Release(agKey)
		}
	}
	if err != nil {
		dead := !ag.Running()
		st.fail(fmt.Sprintf("%v", err) + deadSuffix(dead))
		if dead {
			b.forgetSession(key)
		}
		return
	}
	st.finish()
	b.logger.Infof("[turn] key=%s session=%s stop=%s chars=%d", key, sess.sessionID, stop, st.length())
}

func (b *Bridge) ensureSession(ctx context.Context, ref bindings.ChannelRef, key string, ag agent.Agent, agentKey string, binding store.Binding) (*sessionState, error) {
	b.mu.Lock()
	if st := b.sessions[key]; st != nil && st.agent == ag && st.cwd == binding.Cwd && st.sessionID != "" {
		b.mu.Unlock()
		return st, nil
	}
	b.mu.Unlock()

	sessionID := ""
	var configOptions []agent.ConfigOption
	if binding.SessionID != "" {
		if meta, err := ag.ResumeSession(ctx, binding.SessionID, binding.Cwd); err == nil {
			sessionID = meta.ID
			configOptions = meta.Config
			b.logger.Infof("[session] resume sessionId=%s cwd=%s key=%s", sessionID, binding.Cwd, key)
		} else {
			b.logger.Warnf("[session] resume 실패 sessionId=%s cwd=%s pid=%d running=%v: %v — 새 세션 생성", binding.SessionID, binding.Cwd, ag.PID(), ag.Running(), err)
		}
	}
	if sessionID == "" {
		meta, err := ag.NewSession(ctx, binding.Cwd)
		if err != nil {
			return nil, err
		}
		sessionID = meta.ID
		configOptions = meta.Config
		b.logger.Infof("[session] 신규 sessionId=%s cwd=%s key=%s", sessionID, binding.Cwd, key)
	}

	if model := b.modelFor(binding); model != "" {
		if _, err := ag.SetConfig(ctx, sessionID, "model", model); err != nil {
			b.logger.Warnf("[session] 모델 적용 실패 model=%s: %v", model, err)
		}
	}
	if binding.Agent != "" {
		if err := ag.SetMode(ctx, sessionID, binding.Agent); err != nil {
			b.logger.Warnf("[session] 에이전트 적용 실패 mode=%s sessionId=%s pid=%d running=%v: %v", binding.Agent, sessionID, ag.PID(), ag.Running(), err)
		}
	}

	b.bind.SetSession(ref, sessionID, time.Now().UTC().Format(time.RFC3339))
	b.agent_pool.Track(agentKey, sessionID)

	st := &sessionState{agent: ag, cwd: binding.Cwd, sessionID: sessionID, mode: modeFromOptions(configOptions)}
	if binding.Agent != "" {
		st.mode = binding.Agent
	}
	b.mu.Lock()
	b.sessions[key] = st
	b.mu.Unlock()
	return st, nil
}

// ensureLiveSession returns a session backed by a running process, restarting
// the agent and resuming the saved session when the old process has exited.
// The caller must Release the returned pool key.
func (b *Bridge) ensureLiveSession(ctx context.Context, ref bindings.ChannelRef, key string, binding store.Binding) (*sessionState, string, error) {
	ag, agKey, err := b.agent_pool.Acquire(ctx, binding.Cwd)
	if err != nil {
		return nil, "", err
	}
	st, err := b.ensureSession(ctx, ref, key, ag, agKey, binding)
	if err != nil {
		b.agent_pool.Release(agKey)
		return nil, "", err
	}
	return st, agKey, nil
}

// ---- helpers -------------------------------------------------------------

// describeAgentErr turns an ACP error into a message the Discord user can act on.
func describeAgentErr(err error) string {
	if strings.Contains(err.Error(), "closed by client") {
		return "OpenCode 프로세스가 종료되어 연결이 끊겼습니다. 다음 메시지를 보내면 자동으로 다시 시작합니다."
	}
	return err.Error()
}

// forcedModel returns the globally forced model (bot-config.json "model" /
// OCD_MODEL), or "" when no force is configured.
func (b *Bridge) forcedModel() string {
	return strings.TrimSpace(b.config.Bot.Model)
}

// modelFor resolves the model to apply for a binding. A configured forced model
// always wins over the per-binding value.
func (b *Bridge) modelFor(binding store.Binding) string {
	if m := b.forcedModel(); m != "" {
		return m
	}
	return binding.Model
}

func modeFromOptions(opts []agent.ConfigOption) string {
	for _, co := range opts {
		if co.ID == "mode" {
			return co.CurrentString()
		}
	}
	return ""
}

func (b *Bridge) setSessionMode(key, mode string) {
	if mode == "" {
		return
	}
	b.mu.Lock()
	if st := b.sessions[key]; st != nil {
		st.mode = mode
	}
	b.mu.Unlock()
}

func (b *Bridge) setBusy(key string, busy bool) {
	b.mu.Lock()
	if st := b.sessions[key]; st != nil {
		st.busy = busy
	}
	b.mu.Unlock()
}

func (b *Bridge) forgetSession(key string) {
	b.mu.Lock()
	delete(b.sessions, key)
	b.mu.Unlock()
}

func (b *Bridge) sessionFor(key string) *sessionState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessions[key]
}

func (b *Bridge) isOwner(userID string) bool {
	return b.config.OwnerUserID != "" && userID == b.config.OwnerUserID
}

func (b *Bridge) extractText(content string) string {
	text := content
	if id := b.app.BotID(); id != "" {
		re := regexp.MustCompile(`<@!?` + regexp.QuoteMeta(id) + `>`)
		text = re.ReplaceAllString(text, " ")
	}
	for _, role := range b.config.MentionRoleIDs {
		re := regexp.MustCompile(`<@&` + regexp.QuoteMeta(role) + `>`)
		text = re.ReplaceAllString(text, " ")
	}
	return strings.Join(strings.Fields(text), " ")
}

func (b *Bridge) channelRef(channelID string) bindings.ChannelRef {
	ref := bindings.ChannelRef{ID: channelID}
	ch, err := b.app.Channel(channelID)
	if err != nil {
		return ref
	}
	ref.Name = ch.Name
	ref.ParentID = ch.ParentID
	ref.ParentName = ch.ParentName
	ref.GuildID = ch.GuildID
	ref.IsDM = ch.IsDM
	ref.IsThread = ch.IsThread
	if ref.IsThread && ref.ParentID != "" {
		if p, err := b.app.Channel(ref.ParentID); err == nil {
			ref.ParentCategoryID = p.ParentID
			if ref.ParentName == "" {
				ref.ParentName = p.Name
			}
		}
	}
	return ref
}

func (b *Bridge) unresolvedNotice(ref bindings.ChannelRef) string {
	return strings.Join([]string{
		"이 채널의 cwd 를 자동 결정하지 못했습니다: " + bindings.Label(ref),
		"",
		"`" + b.config.CwdRoot + "` 아래에 채널명과 일치하는 폴더가 없습니다.",
		"해결: `/cwd` 로 후보를 보거나 `/cwd <경로>` 로 지정하세요.",
	}, "\n")
}

func (b *Bridge) buildPrompt(m chat.Message, ref bindings.ChannelRef, text, mode string, atts []attachmentInfo) string {
	if mode == "" {
		mode = "build"
	}
	lines := []string{
		"<discord_message_context>",
		"authorLabel: " + m.AuthorName,
		"channel: #" + chanName(ref),
		fmt.Sprintf("isOwner: %v", b.isOwner(m.AuthorID)),
		"</discord_message_context>",
	}
	if block := formatAttachments(atts); block != "" {
		lines = append(lines, "", block)
	}
	lines = append(lines,
		"",
		`<user_input mode="`+mode+`">`,
		text,
		"</user_input>",
	)
	return strings.Join(lines, "\n")
}

func chanName(ref bindings.ChannelRef) string {
	if ref.Name != "" {
		return ref.Name
	}
	return ref.ID
}

func (b *Bridge) sendChannel(channelID, content string) {
	if _, err := b.app.Send(channelID, content); err != nil {
		b.logger.Warnf("[send] 실패 channel=%s len=%d: %v", channelID, len(content), err)
	}
}

func deadSuffix(dead bool) string {
	if dead {
		return "\n(에이전트 프로세스가 종료되었습니다 — 다음 메시지에서 재기동합니다)"
	}
	return ""
}

func threadName(m chat.Message) string {
	n := strings.TrimSpace(m.AuthorName)
	if n == "" {
		n = "task"
	}
	return "opencode " + n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ---- per-key serial queue ------------------------------------------------

type chain struct {
	mu      sync.Mutex
	running bool
	items   []func()
}

func (c *chain) push(fn func()) {
	c.mu.Lock()
	c.items = append(c.items, fn)
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()
	go c.run()
}

func (c *chain) run() {
	for {
		c.mu.Lock()
		if len(c.items) == 0 {
			c.running = false
			c.mu.Unlock()
			return
		}
		fn := c.items[0]
		c.items = c.items[1:]
		c.mu.Unlock()
		fn()
	}
}

func (c *chain) depth() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

func (b *Bridge) enqueue(key string, fn func()) {
	b.mu.Lock()
	c := b.queues[key]
	if c == nil {
		c = &chain{}
		b.queues[key] = c
	}
	b.mu.Unlock()
	c.push(fn)
}

func (b *Bridge) queueDepth(key string) int {
	b.mu.Lock()
	c := b.queues[key]
	b.mu.Unlock()
	if c == nil {
		return 0
	}
	return c.depth()
}
