package bridge

import (
	"strings"
	"sync"
	"time"

	"github.com/G226Medi03/chat-app-adapter/chat"
	"github.com/G226Medi03/chat-app-adapter/chunk"
)

// answerFooter is appended to the end of every conversational answer.
const answerFooter = "\n[답변종료]"

// discordMaxChars is Discord's per-message limit, counted in characters.
const discordMaxChars = 2000

// stream renders streamed assistant text into a single edited chat message.
type stream struct {
	b         *Bridge
	channelID string
	messageID string
	tag       string

	mu          sync.Mutex
	text        strings.Builder
	toolLines   []string
	lastEdit    time.Time
	lastContent string
	timer       *time.Timer
	closed      bool
}

func newStream(b *Bridge, channelID, mode string) *stream {
	return &stream{b: b, channelID: channelID, tag: modeTag(mode)}
}

func modeTag(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return ""
	}
	r := []rune(mode)
	return "[" + strings.ToUpper(string(r[0])) + string(r[1:]) + "]"
}

func (s *stream) ref() chat.MessageRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	return chat.MessageRef{ChannelID: s.channelID, MessageID: s.messageID}
}

func (s *stream) attach(ref chat.MessageRef) {
	s.mu.Lock()
	s.messageID = ref.MessageID
	s.channelID = ref.ChannelID
	s.mu.Unlock()
}

func (s *stream) length() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.text.Len()
}

func (s *stream) pushText(t string) {
	if t == "" {
		return
	}
	s.mu.Lock()
	s.text.WriteString(t)
	s.scheduleLocked()
	s.mu.Unlock()
}

func (s *stream) pushTool(line string) {
	s.mu.Lock()
	s.toolLines = append(s.toolLines, line)
	s.scheduleLocked()
	s.mu.Unlock()
}

func (s *stream) headerLocked() string {
	if s.tag == "" {
		return ""
	}
	return s.tag + "\n"
}

func (s *stream) headLocked() string {
	if !s.b.config.Bot.ShowTools || len(s.toolLines) == 0 {
		return ""
	}
	lines := s.toolLines
	if len(lines) > 6 {
		lines = lines[len(lines)-6:]
	}
	h := strings.Join(lines, "\n") + "\n\n"
	if chunk.Len(h) > 600 {
		h = "… " + chunk.Tail(h, 596)
	}
	return h
}

func (s *stream) contentLocked() string {
	header := s.headerLocked()
	head := s.headLocked()
	room := maxInt(400, s.b.config.Bot.MaxChunk-chunk.Len(header)-chunk.Len(head)-chunk.Len(answerFooter))
	if strings.TrimSpace(s.text.String()) == "" {
		return header + head + thinking
	}
	return header + head + chunk.LivePreview(s.text.String(), room)
}

func (s *stream) scheduleLocked() {
	if !s.b.config.Bot.Stream || s.timer != nil || s.messageID == "" || s.closed {
		return
	}
	wait := time.Duration(s.b.config.Bot.StreamEditMS)*time.Millisecond - time.Since(s.lastEdit)
	if wait < 0 {
		wait = 0
	}
	s.timer = time.AfterFunc(wait, func() {
		s.mu.Lock()
		s.timer = nil
		if s.closed {
			s.mu.Unlock()
			return
		}
		content := s.contentLocked()
		last := s.lastContent
		s.mu.Unlock()
		if content != last {
			s.edit(content)
		}
	})
}

func (s *stream) edit(content string) {
	if n := chunk.Len(content); n > discordMaxChars {
		s.b.logger.Warnf("[stream] 편집 내용이 %d자로 제한(%d자)을 넘어 잘립니다 channel=%s", n, discordMaxChars, s.channelID)
		content = chunk.Head(content, discordMaxChars)
	}
	s.mu.Lock()
	if s.messageID == "" {
		s.mu.Unlock()
		return
	}
	s.lastContent = content
	s.lastEdit = time.Now()
	ref := chat.MessageRef{ChannelID: s.channelID, MessageID: s.messageID}
	s.mu.Unlock()

	if err := s.b.app.Edit(ref, content); err != nil {
		s.b.logger.Warnf("[stream] edit 실패 channel=%s msg=%s len=%d: %v", ref.ChannelID, ref.MessageID, len(content), err)
	}
}

func (s *stream) finish() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.closed = true
	header := s.headerLocked()
	headLines := len(s.toolLines)
	body := s.text.String()
	hasMsg := s.messageID != ""
	s.mu.Unlock()

	// Tool status lines are live-progress only; the finished answer must be clean.
	pieces := assembleAnswer(header, "", body, answerFooter, s.b.config.Bot.MaxChunk)
	maxPiece := 0
	for _, p := range pieces {
		maxPiece = maxInt(maxPiece, chunk.Len(p))
	}
	s.b.logger.Infof("[stream] finish channel=%s pieces=%d maxPieceChars=%d hasMsg=%v hasHead=%v bodyChars=%d", s.channelID, len(pieces), maxPiece, hasMsg, headLines > 0, chunk.Len(body))

	if hasMsg {
		s.edit(pieces[0])
		for _, p := range pieces[1:] {
			s.b.sendChannel(s.channelID, p)
		}
		return
	}
	for _, p := range pieces {
		s.b.sendChannel(s.channelID, p)
	}
}

// fail renders an error. Error messages deliberately carry no mode header/footer.
func (s *stream) fail(msg string) {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.closed = true
	head := s.headLocked()
	hasMsg := s.messageID != ""
	s.mu.Unlock()

	text := head + "❌ " + msg
	if hasMsg {
		s.edit(text)
	} else {
		s.b.sendChannel(s.channelID, text)
	}
}

// assembleAnswer splits body into sized pieces, putting header+head on the
// first piece and footer on the last. Pure so it can be unit-tested.
func assembleAnswer(header, head, body, footer string, limit int) []string {
	room := limit - chunk.Len(header) - chunk.Len(head) - chunk.Len(footer)
	if room < 50 {
		room = 50
	}
	chunks := splitAnswer(body, room)
	if len(chunks) == 0 {
		chunks = []string{"(빈 응답)"}
	}
	pieces := make([]string, len(chunks))
	copy(pieces, chunks)
	pieces[0] = header + head + pieces[0]
	pieces[len(pieces)-1] = pieces[len(pieces)-1] + footer
	return pieces
}
