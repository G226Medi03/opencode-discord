package chunk

import (
	"strings"
	"testing"
)

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
