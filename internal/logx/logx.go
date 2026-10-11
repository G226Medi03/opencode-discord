// Package logx is a tiny leveled logger writing to console + file, with secret redaction.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type level int

const (
	lvlDebug level = iota
	lvlInfo
	lvlWarn
	lvlError
)

var levelNames = map[level]string{lvlDebug: "debug", lvlInfo: "info", lvlWarn: "warn", lvlError: "error"}

// Logger writes redacted log lines to stdout/stderr and an optional file.
type Logger struct {
	mu   sync.Mutex
	min  level
	file *os.File
}

func parseLevel(s string) level {
	switch s {
	case "debug", "trace", "all":
		return lvlDebug
	case "warn", "warning":
		return lvlWarn
	case "error", "fatal":
		return lvlError
	default:
		return lvlInfo
	}
}

// New creates a logger. logDir may be empty to disable file logging.
func New(levelStr, logDir string) *Logger {
	l := &Logger{min: parseLevel(levelStr)}
	if logDir != "" {
		if err := os.MkdirAll(logDir, 0o755); err == nil {
			f, err := os.OpenFile(filepath.Join(logDir, "oc-discord.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				l.file = f
			}
		}
	}
	return l
}

var (
	reToken = regexp.MustCompile(`[A-Za-z0-9_\-]{24,}\.[A-Za-z0-9_\-]{6,}\.[A-Za-z0-9_\-]{20,}`)
	reKey   = regexp.MustCompile(`sk-[A-Za-z0-9_\-]{12,}`)
)

func redact(s string) string {
	s = reToken.ReplaceAllString(s, "<redacted-token>")
	s = reKey.ReplaceAllString(s, "<redacted-key>")
	return s
}

func (l *Logger) write(lv level, msg string) {
	if lv < l.min {
		return
	}
	line := fmt.Sprintf("[%s] [%s] %s", time.Now().Format("2006-01-02 15:04:05.000"), levelNames[lv], redact(msg))
	l.mu.Lock()
	defer l.mu.Unlock()
	if lv >= lvlError {
		fmt.Fprintln(os.Stderr, line)
	} else {
		fmt.Fprintln(os.Stdout, line)
	}
	if l.file != nil {
		_, _ = l.file.WriteString(line + "\n")
	}
}

func (l *Logger) Debugf(f string, a ...any) { l.write(lvlDebug, fmt.Sprintf(f, a...)) }
func (l *Logger) Infof(f string, a ...any)  { l.write(lvlInfo, fmt.Sprintf(f, a...)) }
func (l *Logger) Warnf(f string, a ...any)  { l.write(lvlWarn, fmt.Sprintf(f, a...)) }
func (l *Logger) Errorf(f string, a ...any) { l.write(lvlError, fmt.Sprintf(f, a...)) }

// Close releases the log file handle.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}
