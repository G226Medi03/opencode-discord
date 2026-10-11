package bridge

import (
	"time"

	"oc-discord/internal/agent"
	"oc-discord/internal/bindings"
	"oc-discord/internal/discord"
	"oc-discord/internal/perms"
)

func (b *Bridge) handlePermission(ref bindings.ChannelRef, sessionID string, req agent.PermissionRequest) agent.Outcome {
	allow := func(id string) agent.Outcome { return agent.Outcome{Selected: true, OptionID: id} }
	pol := perms.Policy{Mode: b.config.Bot.Permissions, Allow: b.config.Bot.AllowPatterns, Deny: b.config.Bot.DenyPatterns}
	wanted := perms.Evaluate(pol, req.Title, req.Kind, req.RawInput)
	b.logger.Infof("[perm] %s :: %s", wanted, req.Title)

	if wanted == perms.DecideAsk && b.config.Bot.RememberAllowAlways {
		b.mu.Lock()
		allowed := b.autoAllow[sessionID]
		b.mu.Unlock()
		if allowed {
			wanted = perms.DecideAllow
		}
	}

	opts := make([]perms.Option, 0, len(req.Options))
	for _, o := range req.Options {
		opts = append(opts, perms.Option{OptionID: o.OptionID, Name: o.Name, Kind: o.Kind})
	}

	switch wanted {
	case perms.DecideAllow:
		if id, ok := perms.PickOptionID(opts, perms.DecideAllow); ok {
			return allow(id)
		}
		return agent.Outcome{}
	case perms.DecideDeny:
		if id, ok := perms.PickOptionID(opts, perms.DecideDeny); ok {
			return allow(id)
		}
		return agent.Outcome{}
	}

	info := perms.SummarizeToolCall(req.Title, req.Kind, req.RawInput)
	_, canAlways := perms.PickOptionID(opts, perms.DecideAllowAlways)
	answer := b.askViaButtons(ref.ID, info, canAlways)
	if answer == "allow" || answer == "always" {
		if answer == "always" && b.config.Bot.RememberAllowAlways {
			b.mu.Lock()
			b.autoAllow[sessionID] = true
			b.mu.Unlock()
		}
		w := perms.DecideAllow
		if answer == "always" {
			w = perms.DecideAllowAlways
		}
		if id, ok := perms.PickOptionID(opts, w); ok {
			return allow(id)
		}
	}
	if id, ok := perms.PickOptionID(opts, perms.DecideDeny); ok {
		return allow(id)
	}
	return agent.Outcome{}
}

// askViaButtons posts an approval question and waits for the owner's choice.
func (b *Bridge) askViaButtons(channelID string, info perms.Summary, canAlways bool) string {
	head := "🔐 툴 승인 요청: **" + info.Title + "**"
	body := head
	if info.Detail != "" {
		body = head + "\n```\n" + info.Detail + "\n```"
	}
	if len(body) > 1990 {
		body = body[:1990]
	}
	btns := []discord.Button{
		{ID: "allow", Label: "허용", Style: discord.StyleSuccess},
		{ID: "always", Label: "항상 허용", Style: discord.StylePrimary, Disabled: !canAlways},
		{ID: "deny", Label: "거부", Style: discord.StyleDanger},
	}
	ref, err := b.discord_client.Send_Buttons(channelID, body, btns)
	if err != nil {
		b.logger.Warnf("[perm] 승인 질문 전송 실패: %v", err)
		return "deny"
	}
	timeout := time.Duration(maxInt(10, b.config.Bot.PermissionTimeoutSec)) * time.Second
	choice, err := b.discord_client.AwaitChoice(ref, timeout, b.isOwner)
	if err != nil {
		_ = b.discord_client.ClearButtons(ref, head+" → 시간 초과(거부) ⏱️")
		return "deny"
	}
	label := map[string]string{"allow": "허용 ✅", "always": "항상 허용 ☑️", "deny": "거부 ❌"}[choice.ButtonID]
	_ = b.discord_client.ClearButtons(ref, head+" → "+label)
	return choice.ButtonID
}
