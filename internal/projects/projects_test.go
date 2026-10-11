package projects

import (
	"os"
	"path/filepath"
	"testing"
)

func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveExact(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "ccst", "Other")
	got := Resolve("ccst", root, nil)
	if got == nil || got.Rule != "exact" || filepath.Base(got.Folder) != "ccst" {
		t.Fatalf("exact: %+v", got)
	}
}

func TestResolveNormalized(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "CustomCCST")
	got := Resolve("custom_ccst", root, nil)
	if got == nil || got.Rule != "normalized" || filepath.Base(got.Folder) != "CustomCCST" {
		t.Fatalf("normalized: %+v", got)
	}
}

func TestResolveSuffixUnique(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "CustomCCST", "Unrelated")
	got := Resolve("ccst", root, nil)
	if got == nil || got.Rule != "suffix" || filepath.Base(got.Folder) != "CustomCCST" {
		t.Fatalf("suffix: %+v", got)
	}
}

func TestResolveAmbiguousSuffix(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "FooCCST", "BarCCST")
	if got := Resolve("ccst", root, nil); got != nil {
		t.Fatalf("ambiguous suffix should not resolve: %+v", got)
	}
}

func TestResolveOverride(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "ccst")
	got := Resolve("ccst", root, map[string]string{"ccst": `D:\elsewhere`})
	if got == nil || got.Rule != "override" || got.Folder != `D:\elsewhere` {
		t.Fatalf("override: %+v", got)
	}
}

func TestResolveNoMatch(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, root, "alpha")
	if got := Resolve("zzz_nomatch", root, nil); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}
