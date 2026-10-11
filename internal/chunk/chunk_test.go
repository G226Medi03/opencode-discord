package chunk

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitKoreanNoLossNoBrokenRune(t *testing.T) {
	src := strings.Repeat("가나다 라마바사 ", 1000) // ~7000 chars, 3 bytes each
	got := Split(src, 1900)
	if len(got) < 4 {
		t.Fatalf("expected several chunks, got %d", len(got))
	}
	total := 0
	for i, c := range got {
		if !utf8.ValidString(c) {
			t.Fatalf("chunk %d is invalid UTF-8", i)
		}
		if Len(c) > 1900 {
			t.Fatalf("chunk %d over limit: %d chars", i, Len(c))
		}
		total += strings.Count(c, "가") + strings.Count(c, "나") + strings.Count(c, "다")
	}
	if total != 3000 {
		t.Fatalf("content lost: got %d of 3000 syllables", total)
	}
}

func TestSplitKoreanLongRunNoSpaces(t *testing.T) {
	got := Split(strings.Repeat("가", 5000), 1900)
	joined := ""
	for i, c := range got {
		if !utf8.ValidString(c) || Len(c) > 1900 {
			t.Fatalf("chunk %d invalid or over limit (%d chars)", i, Len(c))
		}
		joined += c
	}
	if Len(joined) != 5000 {
		t.Fatalf("expected 5000 chars, got %d", Len(joined))
	}
}

func TestHeadTailCharBoundary(t *testing.T) {
	if got := Head("가나다라", 2); got != "가나" {
		t.Fatalf("Head: %q", got)
	}
	if got := Tail("가나다라", 2); got != "다라" {
		t.Fatalf("Tail: %q", got)
	}
	if got := Head("abc", 10); got != "abc" {
		t.Fatalf("Head short: %q", got)
	}
}

func TestLivePreviewKorean(t *testing.T) {
	got := LivePreview(strings.Repeat("가", 3000), 1900)
	if !utf8.ValidString(got) || Len(got) > 1900 {
		t.Fatalf("invalid or over limit: %d chars", Len(got))
	}
}

func TestSplitShort(t *testing.T) {
	got := Split("hello world", 1900)
	if len(got) != 1 || got[0] != "hello world" {
		t.Fatalf("got %#v", got)
	}
}

func TestSplitEmpty(t *testing.T) {
	if got := Split("   \n  ", 100); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestSplitRespectsLimit(t *testing.T) {
	line := strings.Repeat("word ", 200) // long line
	got := Split(line, 100)
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	for _, c := range got {
		if len(c) > 100 {
			t.Fatalf("chunk over limit: %d", len(c))
		}
	}
}

func TestSplitKeepsFences(t *testing.T) {
	src := "before\n```go\n" + strings.Repeat("x := 1\n", 50) + "```\nafter"
	got := Split(src, 80)
	if len(got) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(got))
	}
	for i, c := range got {
		if strings.Count(c, "```")%2 != 0 {
			t.Fatalf("chunk %d has unbalanced fences:\n%s", i, c)
		}
	}
	// first chunk opens a fence and must close it
	if !strings.Contains(got[0], "```") {
		t.Fatalf("first chunk should contain a fence: %q", got[0])
	}
}

func TestToolLine(t *testing.T) {
	if got := ToolLine("read_file x", "completed"); !strings.HasPrefix(got, "✅") {
		t.Fatalf("completed icon: %q", got)
	}
	if got := ToolLine("run", "pending"); !strings.HasPrefix(got, "🛠️") {
		t.Fatalf("pending icon: %q", got)
	}
}

func TestLivePreview(t *testing.T) {
	got := LivePreview("abc", 1900)
	if !strings.HasSuffix(got, "▌") {
		t.Fatalf("missing cursor: %q", got)
	}
}
