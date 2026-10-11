package bridge

import (
	"strings"
	"testing"
)

func TestModeTag(t *testing.T) {
	cases := map[string]string{
		"plan":   "[Plan]",
		"build":  "[Build]",
		"PLAN":   "[PLAN]",
		"":       "",
		"  plan": "[Plan]",
	}
	for in, want := range cases {
		if got := modeTag(in); got != want {
			t.Errorf("modeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAssembleAnswerSingle(t *testing.T) {
	pieces := assembleAnswer("[Plan]\n", "", "hello", answerFooter, 1900)
	if len(pieces) != 1 || pieces[0] != "[Plan]\nhello\n[답변종료]" {
		t.Fatalf("got %#v", pieces)
	}
}

func TestAssembleAnswerSplits(t *testing.T) {
	body := strings.Repeat("word ", 400)
	pieces := assembleAnswer("[Build]\n", "🛠️ tool\n\n", body, answerFooter, 200)
	if len(pieces) < 2 {
		t.Fatalf("expected multiple pieces, got %d", len(pieces))
	}
	if !strings.HasPrefix(pieces[0], "[Build]\n") || !strings.HasPrefix(pieces[0], "[Build]\n🛠️ tool") {
		t.Fatalf("header/head not on first piece")
	}
	if !strings.HasSuffix(pieces[len(pieces)-1], answerFooter) {
		t.Fatalf("footer not on last piece")
	}
	joined := strings.Join(pieces, "")
	if strings.Count(joined, "[Build]") != 1 || strings.Count(joined, "[답변종료]") != 1 {
		t.Fatalf("header/footer not exactly once")
	}
	for i, p := range pieces {
		if len(p) > 200 {
			t.Fatalf("piece %d over limit: %d", i, len(p))
		}
	}
}

func TestAssembleAnswerEmpty(t *testing.T) {
	pieces := assembleAnswer("[Plan]\n", "", "", answerFooter, 1900)
	if len(pieces) != 1 || pieces[0] != "[Plan]\n(빈 응답)\n[답변종료]" {
		t.Fatalf("got %#v", pieces)
	}
}
