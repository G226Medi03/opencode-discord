package discord

import (
	"context"
	"time"
)

// ChatApp is the chat platform the bridge talks to. DiscordClient implements it.
type ChatApp interface {
	Run(ctx context.Context) error
	Close() error
	Get_BotId() string

	Set_OnMessage(fn func(Message))
	Set_OnCommand(fn func(Input))
	RegisterCommands(specs []CommandSpec) error

	Channel(id string) (Channel, error)
	StartThread(channelID, messageID, name string) (Channel, error)

	Send_Msg(channelID, content string) (MessageRef, error)
	Reply_msg(channelID string, replyTo string, content string) (MessageRef, error)
	Edit(ref MessageRef, content string) error

	Send_Buttons(channelID, content string, buttons []Button) (MessageRef, error)
	ClearButtons(ref MessageRef, content string) error
	AwaitChoice(ref MessageRef, timeout time.Duration, allowed func(userID string) bool) (Choice, error)
}

var _ ChatApp = (*DiscordClient)(nil)
