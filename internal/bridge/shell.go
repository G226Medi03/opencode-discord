package bridge

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// maxCmdOutput caps captured command output to keep memory and messages sane.
const maxCmdOutput = 100 * 1024

// runShell executes command in cwd and returns the combined stdout+stderr.
func runShell(ctx context.Context, cwd, command string, timeout time.Duration) (string, error) {
	if runtime.GOOS == "windows" {
		// Force UTF-8 code page so non-ASCII output round-trips.
		return runShellCmd(ctx, cwd, timeout, "cmd.exe", []string{"/c", "chcp 65001>nul & " + command})
	}
	return runShellCmd(ctx, cwd, timeout, "sh", []string{"-c", command})
}

func runShellCmd(ctx context.Context, cwd string, timeout time.Duration, name string, args []string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	w := &capWriter{limit: maxCmdOutput}
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Dir = cwd
	cmd.Stdout = w
	cmd.Stderr = w

	err := cmd.Run()
	out := w.String()

	if w.truncated.Load() {
		out += "\n… (출력 생략)"
	}
	if cctx.Err() == context.DeadlineExceeded {
		out += fmt.Sprintf("\n(시간 초과: %s)", timeout)
	}
	return out, err
}

// capWriter is a concurrency-safe writer that stops buffering past limit.
type capWriter struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int
	truncated atomic.Bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	remaining := w.limit - w.buf.Len()
	if remaining > 0 {
		if len(p) <= remaining {
			w.buf.Write(p)
		} else {
			w.buf.Write(p[:remaining])
			w.truncated.Store(true)
		}
	} else {
		w.truncated.Store(true)
	}
	return len(p), nil
}

func (w *capWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
