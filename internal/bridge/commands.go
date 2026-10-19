package bridge

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/G226Medi03/chat-app-adapter/chatcmd"
	"oc-discord/internal/agent"
	"oc-discord/internal/bindings"
	"oc-discord/internal/projects"
	"oc-discord/internal/store"
)

func strOpt(name, desc string, required bool) chatcmd.Option {
	return chatcmd.Option{Name: name, Description: desc, Required: required}
}

func (b *Bridge) buildRegistry() *chatcmd.Registry {
	r := chatcmd.New()
	r.Add(&chatcmd.Command{Name: "help", Description: "OpenCode 커넥터 사용법", Usage: "/help · !help", Handler: b.cmdHelp})
	r.Add(&chatcmd.Command{Name: "status", Description: "이 채널의 cwd/세션/모델/에이전트 표시", Usage: "/status · !status", Handler: b.cmdStatus})
	r.Add(&chatcmd.Command{Name: "cwd", Description: "작업 폴더(cwd) 확인/변경 — 변경 시 세션 초기화", Usage: "/cwd <path|list> · !cwd <path>",
		Options: []chatcmd.Option{strOpt("path", "절대경로 | 폴더명 | list", false)}, ArgName: "path", Handler: b.cmdCwd})
	r.Add(&chatcmd.Command{Name: "new", Description: "세션 초기화(다음 메시지에서 새 ACP 세션)", Usage: "/new · !new", Handler: b.cmdNew})
	r.Add(&chatcmd.Command{Name: "stop", Description: "진행 중인 턴 취소", Usage: "/stop · !stop", Handler: b.cmdStop})
	r.Add(&chatcmd.Command{Name: "agent", Description: "에이전트(mode) 확인/변경", Usage: "/agent [value] · !agent [value]",
		Options: []chatcmd.Option{strOpt("value", "build | plan 등", false)}, ArgName: "value", Handler: b.cmdAgent})
	r.Add(&chatcmd.Command{Name: "mode", Description: "에이전트(mode) 별칭", Usage: "/mode [value] · !mode [value]",
		Options: []chatcmd.Option{strOpt("value", "build | plan 등", false)}, ArgName: "value", Handler: b.cmdAgent})
	r.Add(&chatcmd.Command{Name: "model", Description: "모델 확인/변경", Usage: "/model [id] · !model [id]",
		Options: []chatcmd.Option{strOpt("value", "provider/model (생략 시 목록)", false)}, ArgName: "value", Handler: b.cmdModel})
	r.Add(&chatcmd.Command{Name: "sessions", Description: "이 프로젝트의 세션 목록", Usage: "/sessions · !sessions", Handler: b.cmdSessions})
	r.Add(&chatcmd.Command{Name: "compact", Description: "세션 컨텍스트 압축(/compact 실행)", Usage: "/compact · !compact", Handler: b.cmdCompact})
	r.Add(&chatcmd.Command{Name: "cmd", Description: "이 채널의 cwd 에서 셸 명령 실행(owner 전용)", Usage: "/cmd <명령> · !cmd <명령>",
		Options: []chatcmd.Option{strOpt("command", "실행할 cmd 명령", true)}, ArgName: "command", Handler: b.cmdShell})
	return r
}

func (b *Bridge) resolve(channelID string) (bindings.ChannelRef, string, store.Binding) {
	ref := b.channelRef(channelID)
	key := bindings.Key(ref)
	var binding store.Binding
	if p := b.bind.Ensure(ref); p != nil {
		binding = *p
	}
	return ref, key, binding
}

func (b *Bridge) cmdHelp(in *chatcmd.Invocation) { in.Reply(b.helpText()) }

func (b *Bridge) cmdStatus(in *chatcmd.Invocation) {
	ref, key, binding := b.resolve(in.ChannelID)
	in.Reply(b.statusText(ref, key, binding))
}

func (b *Bridge) cmdNew(in *chatcmd.Invocation) {
	ref := b.channelRef(in.ChannelID)
	b.bind.ResetSession(ref)
	b.forgetSession(bindings.Key(ref))
	in.Reply("세션을 초기화했습니다 — 다음 메시지가 새 ACP 세션으로 실행됩니다.")
}

func (b *Bridge) cmdStop(in *chatcmd.Invocation) {
	ref := b.channelRef(in.ChannelID)
	st := b.sessionFor(bindings.Key(ref))
	if st == nil || st.agent == nil || st.sessionID == "" || !st.agent.Running() {
		in.Reply("진행 중인 세션이 없습니다.")
		return
	}
	st.agent.Cancel(st.sessionID)
	in.Reply("취소(session/cancel) 요청을 보냈습니다.")
}

func (b *Bridge) cmdCwd(in *chatcmd.Invocation) {
	ref, key, binding := b.resolve(in.ChannelID)
	raw := strings.TrimSpace(in.Arg("path"))
	current := "(미결정)"
	if binding.Cwd != "" {
		current = "`" + binding.Cwd + "`"
		if binding.Rule != "" {
			current += " (rule: " + binding.Rule + ")"
		}
	}
	if raw == "" || strings.ToLower(raw) == "list" {
		limit := 25
		if strings.ToLower(raw) == "list" {
			limit = 60
		}
		folders := projects.Describe(b.config.CwdRoot, limit)
		in.Reply(strings.Join([]string{
			"현재 cwd: " + current,
			"",
			"`" + b.config.CwdRoot + "` 후보 폴더:",
			bulletJoin(folders),
			"",
			"변경: `/cwd <경로>` 또는 `!cwd <경로>` — 변경 시 세션이 초기화됩니다.",
		}, "\n"))
		return
	}
	target := b.resolveTargetCwd(raw)
	if !projects.IsDir(target) {
		in.Reply("폴더가 없습니다: `" + target + "`")
		return
	}
	b.bind.SetCwd(ref, target, "manual")
	b.bind.ResetSession(ref)
	b.forgetSession(key)
	in.Reply("cwd 변경: `" + target + "`\n세션을 초기화했습니다 — 다음 메시지가 새 ACP 세션으로 실행됩니다.")
}

func (b *Bridge) cmdAgent(in *chatcmd.Invocation) {
	ref, key, binding := b.resolve(in.ChannelID)
	value := strings.TrimSpace(in.Arg("value"))
	rt := b.runtime(key)
	if value == "" {
		var avail []string
		for _, co := range rt.ConfigOptions {
			if co.ID == "mode" {
				for _, ov := range co.Options {
					mark := ""
					if ov.Value == rt.Agent {
						mark = " (현재)"
					}
					avail = append(avail, ov.Value+mark)
				}
			}
		}
		in.Reply("에이전트: " + orDash(rt.Agent) + "\n사용 가능: " + orDash(strings.Join(avail, ", ")))
		return
	}
	b.bind.Store().Update(key, func(x *store.Binding) { x.Agent = value })
	binding.Agent = value
	msg := "에이전트 = `" + value + "`"
	if binding.Cwd == "" || !projects.IsDir(binding.Cwd) {
		in.Reply(msg + " (cwd 미결정 — 다음 세션 생성 시 적용)")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	st, agKey, err := b.ensureLiveSession(ctx, ref, key, binding)
	if err != nil {
		b.logger.Warnf("[cmd] 에이전트 적용용 세션 확보 실패 mode=%s cwd=%s: %v", value, binding.Cwd, err)
		in.Reply(msg + " (설정 저장됨 — 다음 세션 생성 시 적용)")
		return
	}
	defer b.agent_pool.Release(agKey)
	if err := st.agent.SetMode(ctx, st.sessionID, value); err != nil {
		b.logger.Warnf("[cmd] 에이전트 적용 실패 mode=%s sessionId=%s pid=%d running=%v: %v", value, st.sessionID, st.agent.PID(), st.agent.Running(), err)
		in.Reply(msg + " (설정 저장됨 — 다음 세션 생성 시 적용: " + describeAgentErr(err) + ")")
		return
	}
	b.setSessionMode(key, value)
	in.Reply(msg)
}

func (b *Bridge) cmdModel(in *chatcmd.Invocation) {
	ref, key, binding := b.resolve(in.ChannelID)
	value := strings.TrimSpace(in.Arg("value"))
	rt := b.runtime(key)
	forced := b.forcedModel()
	if value == "" {
		model := rt.Model
		if forced != "" {
			model = forced
		}
		body := "모델: `" + orDash(model) + "`"
		if forced != "" {
			body += " (bot-config.json / OCD_MODEL 로 고정됨 · 변경 불가)"
		} else {
			var avail []string
			for _, co := range rt.ConfigOptions {
				if co.ID == "model" {
					for _, ov := range co.Options {
						avail = append(avail, "`"+ov.Value+"`")
					}
				}
			}
			if len(avail) > 0 {
				if len(avail) > 60 {
					avail = avail[:60]
				}
				body += "\n\n사용 가능(일부):\n" + strings.Join(avail, " ")
			}
		}
		in.Reply(body)
		return
	}
	if forced != "" {
		in.Reply("모델이 `" + forced + "`(으)로 고정되어 있어 변경할 수 없습니다. (bot-config.json / OCD_MODEL)")
		return
	}
	b.bind.Store().Update(key, func(x *store.Binding) { x.Model = value })
	binding.Model = value
	msg := "모델 = `" + value + "`"
	if binding.Cwd == "" || !projects.IsDir(binding.Cwd) {
		in.Reply(msg + " (cwd 미결정 — 다음 세션 생성 시 적용)")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	st, agKey, err := b.ensureLiveSession(ctx, ref, key, binding)
	if err != nil {
		b.logger.Warnf("[cmd] 모델 적용용 세션 확보 실패 model=%s cwd=%s: %v", value, binding.Cwd, err)
		in.Reply(msg + " (설정 저장됨 — 다음 세션 생성 시 적용)")
		return
	}
	defer b.agent_pool.Release(agKey)
	if _, err := st.agent.SetConfig(ctx, st.sessionID, "model", value); err != nil {
		b.logger.Warnf("[cmd] 모델 적용 실패 model=%s sessionId=%s pid=%d running=%v: %v", value, st.sessionID, st.agent.PID(), st.agent.Running(), err)
		in.Reply(msg + " (설정 저장됨 — 다음 세션 생성 시 적용: " + describeAgentErr(err) + ")")
		return
	}
	in.Reply(msg)
}

func (b *Bridge) cmdSessions(in *chatcmd.Invocation) {
	_, _, binding := b.resolve(in.ChannelID)
	if binding.Cwd == "" {
		in.Reply("cwd 가 결정되지 않았습니다. `/cwd <경로>` 로 지정하세요.")
		return
	}
	if !projects.IsDir(binding.Cwd) {
		in.Reply("cwd 폴더가 없습니다: `" + binding.Cwd + "`")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	ag, agKey, err := b.agent_pool.Acquire(ctx, binding.Cwd)
	if err != nil {
		in.Reply("세션 목록 조회 실패: " + err.Error())
		return
	}
	defer b.agent_pool.Release(agKey)
	entries, err := ag.ListSessions(ctx, binding.Cwd)
	if err != nil {
		in.Reply("session/list 실패: " + err.Error())
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].UpdatedAt > entries[j].UpdatedAt })
	if len(entries) == 0 {
		in.Reply("세션 없음 — 아직 대화가 없습니다.")
		return
	}
	limit := len(entries)
	if limit > 15 {
		limit = 15
	}
	lines := make([]string, 0, limit)
	for _, e := range entries[:limit] {
		title := strings.Join(strings.Fields(e.Title), " ")
		if len(title) > 40 {
			title = title[:40]
		}
		lines = append(lines, fmt.Sprintf("• `%s` %s %s", e.SessionID, title, shortTime(e.UpdatedAt)))
	}
	in.Reply(fmt.Sprintf("세션 %d개(최신 %d):\n%s", len(entries), limit, strings.Join(lines, "\n")))
}

func (b *Bridge) cmdCompact(in *chatcmd.Invocation) {
	in.Reply("`/compact` 를 실행합니다 — 결과는 이 채널에 게시됩니다.")
	ref := b.channelRef(in.ChannelID)
	key := bindings.Key(ref)
	b.enqueue(key, func() {
		bp := b.bind.Ensure(ref)
		if bp == nil {
			b.sendChannel(ref.ID, "cwd 가 결정되지 않았습니다.")
			return
		}
		b.runSynthetic(ref, *bp, "/compact")
	})
}

func (b *Bridge) cmdShell(in *chatcmd.Invocation) {
	ref := b.channelRef(in.ChannelID)
	command := strings.TrimSpace(in.Arg("command"))
	if command == "" {
		in.Reply("사용법: `/cmd <명령>` (예: `/cmd dir`, `!cmd dir`)")
		return
	}
	bindingPtr := b.bind.Ensure(ref)
	if bindingPtr == nil || bindingPtr.Cwd == "" {
		in.Reply("이 채널의 cwd 가 결정되지 않았습니다. `/cwd <경로>` 로 지정하세요.")
		return
	}
	cwd := bindingPtr.Cwd
	if !projects.IsDir(cwd) {
		in.Reply("cwd 폴더가 없습니다: `" + cwd + "`")
		return
	}
	timeout := time.Duration(maxInt(5, b.config.Bot.CmdTimeoutSec)) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout+10*time.Second)
	defer cancel()

	out, err := runShell(ctx, cwd, command, timeout)
	if err != nil {
		b.logger.Infof("[cmd] %q -> err=%v (cwd=%s)", command, err, cwd)
	} else {
		b.logger.Infof("[cmd] %q ok (cwd=%s)", command, cwd)
	}
	body := out
	if strings.TrimSpace(body) == "" {
		body = "(출력 없음)"
	}
	pieces := assembleAnswer("[Cmd]\n", "", body, "\n[종료]", b.config.Bot.MaxChunk)
	in.Reply(pieces[0])
	for _, p := range pieces[1:] {
		b.sendChannel(ref.ID, p)
	}
}

func (b *Bridge) runSynthetic(ref bindings.ChannelRef, binding store.Binding, text string) {
	ctx := context.Background()
	if !projects.IsDir(binding.Cwd) {
		b.sendChannel(ref.ID, "cwd 폴더가 없습니다: `"+binding.Cwd+"`")
		return
	}
	ag, agKey, err := b.agent_pool.Acquire(ctx, binding.Cwd)
	if err != nil {
		b.sendChannel(ref.ID, "opencode acp 기동 실패: `"+err.Error()+"`")
		return
	}
	defer b.agent_pool.Release(agKey)
	key := bindings.Key(ref)
	sess, err := b.ensureSession(ctx, ref, key, ag, agKey, binding)
	if err != nil {
		b.sendChannel(ref.ID, "세션 생성 실패: `"+err.Error()+"`")
		return
	}
	st := newStream(b, ref.ID, "")
	if ph, err := b.app.Send(ref.ID, thinking); err == nil {
		st.attach(ph)
	}
	sink := &turnSink{b: b, stream: st, key: key}
	perm := func(req agent.PermissionRequest) agent.Outcome {
		return b.handlePermission(ref, sess.sessionID, req)
	}
	if _, err := ag.Prompt(ctx, sess.sessionID, text, sink, perm); err != nil {
		st.fail(err.Error())
		return
	}
	st.finish()
}

// ---- help / status -------------------------------------------------------

func (b *Bridge) helpText() string {
	bo := b.config.Bot
	lines := []string{
		"**OpenCode × Discord 커넥터** — 채널/스레드 = 프로젝트(cwd) = ACP 세션",
		"",
		"• 일반 메시지를 보내면 그 채널의 cwd 로 OpenCode 세션을 만들어 실행합니다.",
		"• 명령은 `슬래시(/명령)` 또는 `텍스트(!명령)` **둘 다** 동일하게 동작합니다(답은 채널 공개).",
		"",
	}
	for _, c := range b.registry.All() {
		lines = append(lines, fmt.Sprintf("• `%s` — %s", c.Usage, c.Description))
	}
	lines = append(lines,
		"",
		fmt.Sprintf("cwd 자동결정 루트: `%s`", b.config.CwdRoot),
		"우선순위: `channel-cwd.json override > 폴더명 일치 > 구분자무시 일치 > 유일한 접미사`",
		"",
		fmt.Sprintf("툴 승인 정책: `%s` (auto|ask|deny) — bot-config.json / OCD_PERMISSIONS", bo.Permissions),
		fmt.Sprintf("스트리밍: %v · 유휴 종료: %d분 · cmd 타임아웃: %ds", bo.Stream, bo.IdleShutdownMin, bo.CmdTimeoutSec),
	)
	return strings.Join(lines, "\n")
}

type rtInfo struct {
	SessionID     string
	Cwd           string
	PID           int
	Running       bool
	Busy          bool
	Model         string
	Agent         string
	ConfigOptions []agent.ConfigOption
}

func (b *Bridge) runtime(key string) rtInfo {
	st := b.sessionFor(key)
	var info rtInfo
	if st == nil {
		return info
	}
	info.SessionID = st.sessionID
	info.Cwd = st.cwd
	info.Busy = st.busy
	if st.agent != nil {
		info.PID = st.agent.PID()
		info.Running = st.agent.Running()
		if !info.Running {
			return info
		}
		if meta, ok := st.agent.Meta(st.sessionID); ok {
			info.ConfigOptions = meta.Config
			for _, co := range meta.Config {
				switch co.ID {
				case "model":
					info.Model = co.CurrentString()
				case "mode":
					info.Agent = co.CurrentString()
				}
			}
		}
	}
	return info
}

func (b *Bridge) statusText(ref bindings.ChannelRef, key string, binding store.Binding) string {
	rt := b.runtime(key)
	cwdLine := "(미결정)"
	if binding.Cwd != "" {
		cwdLine = "`" + binding.Cwd + "`"
		if binding.Rule != "" {
			cwdLine += " (rule: " + binding.Rule + ")"
		}
	}
	proc := "(미기동)"
	if rt.PID != 0 && rt.Running {
		proc = fmt.Sprintf("pid %d (running)", rt.PID)
	} else if rt.PID != 0 {
		proc = fmt.Sprintf("pid %d (종료됨 — 다음 메시지에서 재기동)", rt.PID)
	}
	busy := "idle"
	if rt.Busy {
		busy = "busy"
	}
	bo := b.config.Bot
	modelLine := orDash(rt.Model)
	if forced := b.forcedModel(); forced != "" {
		modelLine = orDash(forced) + " (고정)"
	}
	return strings.Join([]string{
		"채널: " + bindings.Label(ref),
		"바인딩 키: `" + key + "`",
		"cwd: " + cwdLine,
		"세션: `" + orDash(rt.SessionID) + "`",
		"에이전트: " + orDash(rt.Agent) + " · 모델: " + modelLine,
		fmt.Sprintf("프로세스: %s · 턴: %s", proc, busy),
		fmt.Sprintf("정책: permissions=%s · stream=%v", bo.Permissions, bo.Stream),
		fmt.Sprintf("풀 프로세스: %d개 · 대기열: %d", b.agent_pool.Size(), b.queueDepth(key)),
	}, "\n")
}

func (b *Bridge) resolveTargetCwd(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if filepath.IsAbs(v) {
		return v
	}
	direct := filepath.Join(b.config.CwdRoot, v)
	if projects.IsDir(direct) {
		return direct
	}
	if hit := projects.Resolve(v, b.config.CwdRoot, b.config.CwdOverrides); hit != nil {
		return hit.Folder
	}
	return direct
}

func orDash(s string) string {
	if s == "" {
		return "(없음)"
	}
	return s
}

func bulletJoin(items []string) string {
	if len(items) == 0 {
		return "(없음)"
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, "• "+it)
	}
	return strings.Join(parts, "\n")
}

func shortTime(s string) string {
	if s == "" {
		return ""
	}
	s = strings.Replace(s, "T", " ", 1)
	if len(s) > 19 {
		s = s[:19]
	}
	return s
}
