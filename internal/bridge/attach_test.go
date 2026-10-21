package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeAttachmentName(t *testing.T) {
	cases := map[string]string{
		"report.pdf":           "report.pdf",
		`..\..\etc\passwd`:     "passwd",
		"/tmp/a b.txt":         "a b.txt",
		"a:b*c?d\"e<f>g|h.txt": "a_b_c_d_e_f_g_h.txt",
		"":                     "attachment-1",
		"..":                   "attachment-1",
	}
	for in, want := range cases {
		if got := safeAttachmentName(in, 0); got != want {
			t.Errorf("safeAttachmentName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatAttachments(t *testing.T) {
	atts := []attachmentInfo{
		{Name: "a.pdf", Size: 2048, ContentType: "application/pdf", Path: `C:\x\a.pdf`},
		{Name: "b.bin", Size: 10, Path: `C:\x\b.bin`},
	}
	got := formatAttachments(atts)
	for _, want := range []string{"<attachments>", "a.pdf (2.0 KB, application/pdf)", `path: C:\x\a.pdf`, "b.bin (10 B, unknown)", "</attachments>"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{10: "10 B", 1024: "1.0 KB", 1536: "1.5 KB", 2 * 1024 * 1024: "2.0 MB"}
	for n, want := range cases {
		if got := humanSize(n); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestResolveInsideCwd(t *testing.T) {
	dir := t.TempDir()
	inside := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveInsideCwd(dir, "a.txt")
	if err != nil {
		t.Fatalf("inside path rejected: %v", err)
	}
	if got != inside {
		t.Fatalf("got %q, want %q", got, inside)
	}
	if _, err := resolveInsideCwd(dir, filepath.Join("..", "a.txt")); err == nil {
		t.Fatal("expected outside path to be rejected")
	}
	if _, err := resolveInsideCwd(dir, ""); err == nil {
		t.Fatal("expected empty path to be rejected")
	}
}
