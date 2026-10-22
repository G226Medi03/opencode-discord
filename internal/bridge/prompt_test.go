package bridge

import (
	"strings"
	"testing"

	"github.com/G226Medi03/chat-app-adapter/chat"
	"oc-discord/internal/bindings"
	"oc-discord/internal/config"
)

func TestBuildPromptExtraInstructions(t *testing.T) {
	b := &Bridge{config: &config.Config{Bot: config.BotConfig{ExtraInstructions: "use long timeouts"}}}
	got := b.buildPrompt(chat.Message{AuthorName: "g226"}, bindings.ChannelRef{Name: "raven"}, "hi", "build", nil)
	if !strings.Contains(got, "<runtime_instructions>\nuse long timeouts\n</runtime_instructions>") {
		t.Fatalf("missing runtime instructions in prompt:\n%s", got)
	}
}

func TestBuildPromptNoExtraInstructions(t *testing.T) {
	b := &Bridge{config: &config.Config{}}
	got := b.buildPrompt(chat.Message{AuthorName: "g226"}, bindings.ChannelRef{Name: "raven"}, "hi", "build", nil)
	if strings.Contains(got, "<runtime_instructions>") {
		t.Fatalf("unexpected runtime instructions:\n%s", got)
	}
}
