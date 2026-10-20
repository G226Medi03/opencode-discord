package bridge

import (
	"strings"
	"testing"

	"github.com/G226Medi03/chat-app-adapter/chunk"
)

// TestSplitAnswerKeepsCodeBlockWhole checks the split point moves before a code
// block instead of cutting through it.
func TestSplitAnswerKeepsCodeBlockWhole(t *testing.T) {
	plain := strings.Repeat("설명 문장입니다. ", 100) // ~1000 chars
	code := "```go\n" + strings.Repeat("fmt.Println(\"hello\")\n", 20) + "```\n"
	text := plain + "\n" + code

	pieces := splitAnswer(text, 1200)
	if len(pieces) < 2 {
		t.Fatalf("expected multiple pieces, got %d", len(pieces))
	}
	block := strings.TrimSpace(code)
	found := 0
	for _, p := range pieces {
		if strings.Contains(p, block) {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("code block must stay in one piece (found in %d pieces): %#v", found, pieces)
	}
	for i, p := range pieces {
		if chunk.Len(p) > 1200 {
			t.Fatalf("piece %d over limit: %d", i, chunk.Len(p))
		}
	}
}

// TestSplitAnswerOversizedBlockFallsBack checks a block longer than the limit
// still splits with balanced fences (legacy behaviour).
func TestSplitAnswerOversizedBlockFallsBack(t *testing.T) {
	code := "```go\n" + strings.Repeat("x := 1\n", 100) + "```"
	pieces := splitAnswer(code, 200)
	if len(pieces) < 2 {
		t.Fatalf("expected multiple pieces, got %d", len(pieces))
	}
	for i, p := range pieces {
		if chunk.Len(p) > 200 {
			t.Fatalf("piece %d over limit: %d", i, chunk.Len(p))
		}
		if strings.Count(p, "```")%2 != 0 {
			t.Fatalf("piece %d has unbalanced fences:\n%s", i, p)
		}
	}
}

func TestSplitAnswerPlainText(t *testing.T) {
	body := strings.Repeat("word ", 400)
	pieces := splitAnswer(body, 200)
	if len(pieces) < 2 {
		t.Fatalf("expected multiple pieces, got %d", len(pieces))
	}
	for i, p := range pieces {
		if chunk.Len(p) > 200 {
			t.Fatalf("piece %d over limit: %d", i, chunk.Len(p))
		}
	}
}

func TestSplitAnswerShortPassthrough(t *testing.T) {
	got := splitAnswer("hello", 1900)
	if len(got) != 1 || got[0] != "hello" {
		t.Fatalf("got %#v", got)
	}
	if got := splitAnswer("   \n ", 100); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}
