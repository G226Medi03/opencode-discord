package bridge

import (
	"strings"

	"oc-discord/internal/chunk"
)

// turnSink adapts agent streaming updates to a chat message stream.
type turnSink struct {
	b      *Bridge
	stream *stream
	key    string
}

func (s *turnSink) Text(t string) { s.stream.pushText(t) }

func (s *turnSink) Tool(title, status string) {
	if !s.b.config.Bot.ShowTools {
		return
	}
	if strings.TrimSpace(title) == "" {
		title = "tool"
	}
	s.stream.pushTool(chunk.ToolLine(title, status))
}

func (s *turnSink) Mode(modeID string) { s.b.setSessionMode(s.key, modeID) }
