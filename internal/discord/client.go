// Package discord is a Discord client (discordgo) with no OpenCode/agent logic:
// it only moves messages, edits, buttons, and slash commands in and out.
package discord

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"

	"oc-discord/internal/chunk"
	"oc-discord/internal/logx"
)

// DiscordClientOption configures the Discord transport.
type DiscordClientOption struct {
	Token   string
	AppID   string
	GuildID string
	Logger  *logx.Logger
}

// DiscordClient is a Discord client.
type DiscordClient struct {
	opts       DiscordClientOption
	log        *logx.Logger
	discord_go *discordgo.Session

	mu      sync.Mutex
	selfID  string
	onMsg   func(Message)
	onCmd   func(Input)
	waiters map[string]*waiter
}

type waiter struct {
	ch      chan Choice
	allowed func(string) bool
}

// New creates a Discord transport (does not connect).
func New(opts DiscordClientOption) *DiscordClient {
	return &DiscordClient{opts: opts, log: opts.Logger, waiters: map[string]*waiter{}}
}

// Self returns the bot user id ("" before connect).
func (self *DiscordClient) Get_BotId() string {
	self.mu.Lock()
	defer self.mu.Unlock()

	if self.selfID != "" {
		return self.selfID
	}
	if self.discord_go != nil && self.discord_go.State != nil && self.discord_go.State.User != nil {
		return self.discord_go.State.User.ID
	}
	return ""
}

// Channel returns metadata for a channel.
func (self *DiscordClient) Channel(id string) (Channel, error) {
	out := Channel{ID: id}
	if self.discord_go == nil {
		return out, fmt.Errorf("discord not connected")
	}
	var ch *discordgo.Channel
	if v, err := self.discord_go.State.Channel(id); err == nil && v != nil {
		ch = v
	} else if v, err := self.discord_go.Channel(id); err == nil {
		ch = v
	}
	if ch == nil {
		return out, fmt.Errorf("channel not found: %s", id)
	}
	out.Name = ch.Name
	out.ParentID = ch.ParentID
	out.GuildID = ch.GuildID

	switch ch.Type {
	case discordgo.ChannelTypeDM, discordgo.ChannelTypeGroupDM:
		out.IsDM = true
	case discordgo.ChannelTypeGuildPublicThread, discordgo.ChannelTypeGuildPrivateThread, discordgo.ChannelTypeGuildNewsThread:
		out.IsThread = true
	}
	if out.IsThread && out.ParentID != "" {
		if p, err := self.discord_go.Channel(out.ParentID); err == nil && p != nil {
			out.ParentName = p.Name
		}
	}
	return out, nil
}

func truncate(s string) string {
	return chunk.Head(s, 2000)
}

// Send_Msg posts a message.
func (self *DiscordClient) Send_Msg(channelID, content string) (MessageRef, error) {
	m, err := self.discord_go.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{Content: truncate(content), AllowedMentions: noMentions()})
	if err != nil {
		return MessageRef{}, err
	}
	return MessageRef{ChannelID: m.ChannelID, MessageID: m.ID}, nil
}

// Reply_msg posts a message as a reply to replyTo.
func (self *DiscordClient) Reply_msg(channelID string, replyTo string, content string) (MessageRef, error) {
	m, err := self.discord_go.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:         truncate(content),
		Reference:       &discordgo.MessageReference{MessageID: replyTo, ChannelID: channelID},
		AllowedMentions: noMentions(),
	})
	if err != nil {
		return MessageRef{}, err
	}
	return MessageRef{ChannelID: m.ChannelID, MessageID: m.ID}, nil
}

// Edit replaces the content of a message.
func (self *DiscordClient) Edit(ref MessageRef, content string) error {
	body := truncate(content)
	_, err := self.discord_go.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID: ref.MessageID, Channel: ref.ChannelID, Content: &body, AllowedMentions: noMentions(),
	})
	return err
}

// Send_Buttons posts a message with an action row of buttons.
func (self *DiscordClient) Send_Buttons(channelID, content string, buttons []Button) (MessageRef, error) {
	row := discordgo.ActionsRow{Components: make([]discordgo.MessageComponent, 0, len(buttons))}
	for _, b := range buttons {
		row.Components = append(row.Components, discordgo.Button{
			Label: b.Label, Style: dgStyle(b.Style), CustomID: b.ID, Disabled: b.Disabled,
		})
	}
	m, err := self.discord_go.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: truncate(content), Components: []discordgo.MessageComponent{row}, AllowedMentions: noMentions(),
	})
	if err != nil {
		return MessageRef{}, err
	}
	return MessageRef{ChannelID: m.ChannelID, MessageID: m.ID}, nil
}

// ClearButtons removes components (and sets content).
func (self *DiscordClient) ClearButtons(ref MessageRef, content string) error {
	body := truncate(content)
	empty := []discordgo.MessageComponent{}
	_, err := self.discord_go.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID: ref.MessageID, Channel: ref.ChannelID, Content: &body, Components: &empty,
	})
	return err
}

// StartThread creates a thread on messageID and returns it.
func (self *DiscordClient) StartThread(channelID, messageID, name string) (Channel, error) {
	if len([]rune(name)) > 90 {
		name = string([]rune(name)[:90])
	}
	th, err := self.discord_go.MessageThreadStart(channelID, messageID, name, 1440)
	if err != nil {
		return Channel{}, err
	}
	return Channel{
		ID:       th.ID,
		Name:     th.Name,
		ParentID: th.ParentID,
		GuildID:  th.GuildID,
		IsThread: true,
	}, nil
}

// AwaitChoice waits for a button click on ref, filtered by allowed.
func (self *DiscordClient) AwaitChoice(ref MessageRef, timeout time.Duration, allowed func(userID string) bool) (Choice, error) {
	ch := make(chan Choice, 1)
	self.mu.Lock()
	self.waiters[ref.MessageID] = &waiter{ch: ch, allowed: allowed}
	self.mu.Unlock()
	defer func() {
		self.mu.Lock()
		delete(self.waiters, ref.MessageID)
		self.mu.Unlock()
	}()

	select {
	case choice := <-ch:
		return choice, nil
	case <-time.After(timeout):
		return Choice{}, fmt.Errorf("permission timeout")
	}
}

// Set_OnMessage registers the inbound message handler.
func (self *DiscordClient) Set_OnMessage(fn func(Message)) {
	self.mu.Lock()
	self.onMsg = fn
	self.mu.Unlock()
}

// Set_OnCommand registers the inbound slash-command handler.
func (self *DiscordClient) Set_OnCommand(fn func(Input)) {
	self.mu.Lock()
	self.onCmd = fn
	self.mu.Unlock()
}

func (self *DiscordClient) Get_OnMessage() func(Message) {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.onMsg
}
func (self *DiscordClient) Get_OnCommand() func(Input) {
	self.mu.Lock()
	defer self.mu.Unlock()
	return self.onCmd
}

// RegisterCommands overwrites the Discord application commands.
func (self *DiscordClient) RegisterCommands(specs []CommandSpec) error {
	if self.opts.AppID == "" || self.opts.Token == "" {
		return fmt.Errorf("app id / token required")
	}
	if self.discord_go == nil {
		self.discord_go = self.newSession()
	}
	cmds := make([]*discordgo.ApplicationCommand, 0, len(specs))
	for _, s := range specs {
		ac := &discordgo.ApplicationCommand{Name: s.Name, Description: s.Description}
		for _, o := range s.Options {
			ac.Options = append(ac.Options, &discordgo.ApplicationCommandOption{
				Type: discordgo.ApplicationCommandOptionString, Name: o.Name, Description: o.Description, Required: o.Required,
			})
		}
		cmds = append(cmds, ac)
	}
	created, err := self.discord_go.ApplicationCommandBulkOverwrite(self.opts.AppID, self.opts.GuildID, cmds)
	if err != nil {
		return err
	}
	self.log.Infof("[deploy] 슬래시 커맨드 %d개 등록", len(created))
	return nil
}

func (self *DiscordClient) newSession() *discordgo.Session {
	dg, err := discordgo.New("Bot " + self.opts.Token)
	if err != nil {
		self.log.Errorf("[discord] 세션 생성 실패: %v", err)
		return nil
	}
	dg.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent |
		discordgo.IntentsDirectMessages
	return dg
}

// Run connects to Discord and blocks until ctx is cancelled.
func (self *DiscordClient) Run(ctx context.Context) error {
	if self.opts.Token == "" {
		return fmt.Errorf("DISCORD_BOT_TOKEN required")
	}
	if self.discord_go == nil {
		self.discord_go = self.newSession()
		if self.discord_go == nil {
			return fmt.Errorf("discord session init failed")
		}
	}
	self.discord_go.AddHandler(self.onReady)
	self.discord_go.AddHandler(self.onMessageCreate)
	self.discord_go.AddHandler(self.onInteraction)
	if err := self.discord_go.Open(); err != nil {
		return fmt.Errorf("gateway 연결 실패: %w", err)
	}
	<-ctx.Done()
	return nil
}

// Close disconnects from Discord.
func (self *DiscordClient) Close() error {
	if self.discord_go != nil {
		return self.discord_go.Close()
	}
	return nil
}

// ---- gateway handlers ----------------------------------------------------

func (self *DiscordClient) onReady(s *discordgo.Session, r *discordgo.Ready) {
	self.mu.Lock()
	self.selfID = r.User.ID
	self.mu.Unlock()
	self.log.Infof("[ready] %s (%s) · 길드 %d개", r.User.String(), r.User.ID, len(r.Guilds))
}

func (self *DiscordClient) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m == nil || m.Author == nil || m.Author.Bot || m.WebhookID != "" {
		return
	}
	msg := Message{
		ID:           m.ID,
		ChannelID:    m.ChannelID,
		AuthorID:     m.Author.ID,
		AuthorName:   m.Author.Username,
		Content:      m.Content,
		MentionedBot: self.mentioned(m),
		GuildID:      m.GuildID,
		Timestamp:    m.Timestamp,
	}
	if ch, err := self.Channel(m.ChannelID); err == nil {
		msg.IsDM = ch.IsDM
	}
	if fn := self.Get_OnMessage(); fn != nil {
		go fn(msg)
	}
}

func (self *DiscordClient) mentioned(m *discordgo.MessageCreate) bool {
	id := self.Get_BotId()
	if id == "" {
		return false
	}
	for _, u := range m.Mentions {
		if u != nil && u.ID == id {
			return true
		}
	}
	return false
}

func (self *DiscordClient) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		go self.handleSlash(i)
	case discordgo.InteractionMessageComponent:
		go self.handleComponent(i)
	}
}

func (self *DiscordClient) handleSlash(i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	args := map[string]string{}
	for _, o := range data.Options {
		if o == nil {
			continue
		}
		if v, ok := o.Value.(string); ok {
			args[o.Name] = v
		}
	}
	responded := false
	reply := func(content string) {
		content = chunk.Head(content, 1990)
		if !responded {
			responded = true
			_ = self.discord_go.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
			})
		}
		_, _ = self.discord_go.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{Content: content, AllowedMentions: noMentions()})
	}
	if fn := self.Get_OnCommand(); fn != nil {
		fn(Input{
			Name: data.Name, Args: args, UserID: interactionUserID(i),
			ChannelID: i.ChannelID, IsSlash: true, Reply: reply,
		})
	}
}

func (self *DiscordClient) handleComponent(i *discordgo.InteractionCreate) {
	uid := interactionUserID(i)
	msgID := ""
	if i.Message != nil {
		msgID = i.Message.ID
	}
	self.mu.Lock()
	w := self.waiters[msgID]
	self.mu.Unlock()
	if w == nil {
		return
	}
	if w.allowed != nil && !w.allowed(uid) {
		_ = self.discord_go.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{Content: "권한이 없습니다(owner 전용).", Flags: discordgo.MessageFlagsEphemeral},
		})
		return
	}
	// Acknowledge by removing components (bridge sets the final content).
	empty := []discordgo.MessageComponent{}
	_ = self.discord_go.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{Components: empty},
	})
	select {
	case w.ch <- Choice{ButtonID: i.MessageComponentData().CustomID, UserID: uid}:
	default:
	}
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

func dgStyle(s Style) discordgo.ButtonStyle {
	switch s {
	case StylePrimary:
		return discordgo.PrimaryButton
	case StyleSuccess:
		return discordgo.SuccessButton
	case StyleDanger:
		return discordgo.DangerButton
	default:
		return discordgo.SecondaryButton
	}
}

func noMentions() *discordgo.MessageAllowedMentions {
	return &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}}
}
