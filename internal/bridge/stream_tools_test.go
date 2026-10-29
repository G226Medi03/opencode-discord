package bridge

import (
	"errors"
	"strings"
	"testing"

	"github.com/G226Medi03/chat-app-adapter/chat"
	"oc-discord/internal/config"
	"oc-discord/internal/logx"
)

// fakeApp implements only what the stream uses; other chat.App methods are
// promoted from the embedded nil interface and would panic if called.
type fakeApp struct {
	chat.App
	edits     []string
	sends     []string
	deletes   []chat.MessageRef
	deleteErr error
}

func (f *fakeApp) Edit(ref chat.MessageRef, content string) error {
	f.edits = append(f.edits, content)
	return nil
}

func (f *fakeApp) Delete(ref chat.MessageRef) error {
	f.deletes = append(f.deletes, ref)
	return f.deleteErr
}

func (f *fakeApp) Send(channelID, content string) (chat.MessageRef, error) {
	f.sends = append(f.sends, content)
	return chat.MessageRef{ChannelID: channelID}, nil
}

func newTestStream(app *fakeApp) *stream {
	b := &Bridge{
		config: &config.Config{Bot: config.BotConfig{MaxChunk: 1900, ShowTools: true}},
		logger: logx.New("error", ""),
		app:    app,
	}
	s := newStream(b, "c1", "build")
	s.attach(chat.MessageRef{ChannelID: "c1", MessageID: "m1"})
	return s
}

func TestFinishDropsToolLines(t *testing.T) {
	app := &fakeApp{}
	s := newTestStream(app)
	s.pushTool("🛠️ shell")
	s.pushTool("⏳ go test ./...")
	s.pushText("최종 답변입니다.")
	s.finish()

	if len(app.deletes) != 1 {
		t.Fatalf("placeholder should be deleted, got %d", len(app.deletes))
	}
	if len(app.edits) != 0 {
		t.Fatalf("final answer must not edit the placeholder: %#v", app.edits)
	}
	if len(app.sends) == 0 {
		t.Fatal("no final message was sent")
	}
	got := app.sends[0]
	if strings.Contains(got, "🛠️") || strings.Contains(got, "⏳") {
		t.Fatalf("tool status leaked into final answer:\n%s", got)
	}
	if !strings.HasPrefix(got, "[Build]\n") || !strings.Contains(got, "최종 답변입니다.") {
		t.Fatalf("unexpected final answer:\n%s", got)
	}
}

func TestFinishFallsBackToEdit(t *testing.T) {
	app := &fakeApp{deleteErr: errors.New("cannot delete")}
	s := newTestStream(app)
	s.pushText("최종 답변입니다.")
	s.finish()

	if len(app.deletes) != 1 {
		t.Fatalf("delete should be attempted once, got %d", len(app.deletes))
	}
	if len(app.edits) != 1 || !strings.Contains(app.edits[0], "최종 답변입니다.") {
		t.Fatalf("expected edit fallback, got %#v", app.edits)
	}
	if len(app.sends) != 0 {
		t.Fatalf("unexpected send after failed delete: %#v", app.sends)
	}
}

func TestPreviewKeepsToolLines(t *testing.T) {
	app := &fakeApp{}
	s := newTestStream(app)
	s.pushTool("🛠️ shell")
	s.pushText("작업 중")

	s.mu.Lock()
	preview := s.contentLocked()
	s.mu.Unlock()

	if !strings.Contains(preview, "🛠️ shell") {
		t.Fatalf("tool line missing from live preview:\n%s", preview)
	}
}
