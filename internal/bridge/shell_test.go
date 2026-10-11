package bridge

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunShellEcho(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows cmd test")
	}
	out, err := runShell(context.Background(), t.TempDir(), "echo hello-cmd", 10*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v (out=%q)", err, out)
	}
	if !strings.Contains(out, "hello-cmd") {
		t.Fatalf("output missing echo: %q", out)
	}
}

func TestRunShellNonZero(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows cmd test")
	}
	if _, err := runShell(context.Background(), t.TempDir(), "exit /b 3", 10*time.Second); err == nil {
		t.Fatal("expected non-zero exit error")
	}
}

func TestRunShellTimeout(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows cmd test")
	}
	start := time.Now()
	out, err := runShell(context.Background(), t.TempDir(), "ping -n 10 127.0.0.1 >nul", 1*time.Second)
	if err == nil {
		t.Fatalf("expected timeout error (out=%q)", out)
	}
	if time.Since(start) > 15*time.Second {
		t.Fatal("timeout was not enforced")
	}
	if !strings.Contains(out, "시간 초과") {
		t.Fatalf("expected timeout note in output: %q", out)
	}
}

func TestCapWriterTruncates(t *testing.T) {
	w := &capWriter{limit: 10}
	n, _ := w.Write([]byte("0123456789ABCDEF"))
	if n != 16 {
		t.Fatalf("Write should report full length, got %d", n)
	}
	if w.String() != "0123456789" || !w.truncated.Load() {
		t.Fatalf("cap/flag failed: %q %v", w.String(), w.truncated.Load())
	}
}
