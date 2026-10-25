package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	gitTimeout   = 5 * time.Second
	maxViewBytes = 200 * 1024
)

type commitInfo struct {
	Hash    string
	Subject string
	Date    string
}

type fileChange struct {
	Path      string
	Status    string // A, M, D
	Additions int
	Deletions int
	Binary    bool
}

// ---- git process helpers -------------------------------------------------

type capWriter struct {
	w         io.Writer
	n         int
	max       int
	truncated bool
}

func (c *capWriter) Write(p []byte) (int, error) {
	original := len(p)
	if c.n >= c.max {
		c.truncated = true
		return original, nil
	}
	remain := c.max - c.n
	if len(p) > remain {
		p = p[:remain]
		c.truncated = true
	}
	n, err := c.w.Write(p)
	c.n += n
	if err != nil {
		return n, err
	}
	return original, nil
}

func runGit(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("git timeout")
		}
		return nil, fmt.Errorf("git: %s", strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func runGitCapped(dir string, args ...string) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var errb bytes.Buffer
	var out bytes.Buffer
	cw := &capWriter{w: &out, max: maxViewBytes}
	cmd.Stdout = cw
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", false, fmt.Errorf("git timeout")
		}
		return "", false, fmt.Errorf("git: %s", strings.TrimSpace(errb.String()))
	}
	return out.String(), cw.truncated, nil
}

// ---- snapshot repos ------------------------------------------------------

var snapCache = struct {
	sync.Mutex
	m map[string]snapEntry
}{m: map[string]snapEntry{}}

type snapEntry struct {
	path string
	err  error
	at   time.Time
}

func snapshotBase() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "snapshot")
}

func findSnapshotRepo(projectID string) (string, error) {
	snapCache.Lock()
	if e, ok := snapCache.m[projectID]; ok && time.Since(e.at) < 30*time.Second {
		snapCache.Unlock()
		return e.path, e.err
	}
	snapCache.Unlock()

	p, err := findSnapshotRepoIn(filepath.Join(snapshotBase(), projectID))
	snapCache.Lock()
	snapCache.m[projectID] = snapEntry{path: p, err: err, at: time.Now()}
	snapCache.Unlock()
	return p, err
}

func findSnapshotRepoIn(base string) (string, error) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(base, e.Name())
		if _, err := os.Stat(filepath.Join(p, "HEAD")); err != nil {
			continue
		}
		if st, err := os.Stat(filepath.Join(p, "objects")); err == nil && st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("snapshot repo not found under %s", base)
}

// ---- git queries ---------------------------------------------------------

var headCache = struct {
	sync.Mutex
	m map[string]headEntry
}{m: map[string]headEntry{}}

type headEntry struct {
	info commitInfo
	at   time.Time
}

func headCommit(worktree string) commitInfo {
	headCache.Lock()
	if e, ok := headCache.m[worktree]; ok && time.Since(e.at) < 30*time.Second {
		headCache.Unlock()
		return e.info
	}
	headCache.Unlock()

	var info commitInfo
	out, err := runGit(worktree, "log", "-1", "--format=%h%x09%ad%x09%s", "--date=short")
	if err == nil {
		parts := strings.SplitN(strings.TrimSpace(string(out)), "\t", 3)
		if len(parts) == 3 {
			info = commitInfo{Hash: parts[0], Date: parts[1], Subject: parts[2]}
		}
	}
	headCache.Lock()
	headCache.m[worktree] = headEntry{info: info, at: time.Now()}
	headCache.Unlock()
	return info
}

// changedFiles lists files changed between two snapshot trees.
func changedFiles(shadow, start, end string) ([]fileChange, error) {
	nameOut, err := runGit("", "--git-dir="+shadow, "diff", "--name-status", start, end)
	if err != nil {
		return nil, err
	}
	status := map[string]string{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(string(nameOut)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		st := parts[0][:1]
		status[parts[1]] = st
		order = append(order, parts[1])
	}
	numOut, err := runGit("", "--git-dir="+shadow, "diff", "--numstat", start, end)
	if err != nil {
		return nil, err
	}
	stats := map[string]fileChange{}
	for _, line := range strings.Split(strings.TrimSpace(string(numOut)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		add, del := 0, 0
		binary := false
		if parts[0] == "-" || parts[1] == "-" {
			binary = true
		} else {
			add, _ = strconv.Atoi(parts[0])
			del, _ = strconv.Atoi(parts[1])
		}
		stats[parts[2]] = fileChange{Additions: add, Deletions: del, Binary: binary}
	}
	out := make([]fileChange, 0, len(order))
	for _, path := range order {
		fc := stats[path]
		fc.Path = path
		fc.Status = status[path]
		out = append(out, fc)
	}
	return out, nil
}

// fileDiff returns the unified diff for one file between two trees.
func fileDiff(shadow, start, end, path string) (string, bool, error) {
	return runGitCapped("", "--git-dir="+shadow, "--no-pager", "diff", "--no-color", "-U3", start, end, "--", path)
}

// fileAt returns the content of path at a tree.
func fileAt(shadow, tree, path string) (string, bool, bool, error) {
	out, truncated, err := runGitCapped("", "--git-dir="+shadow, "--no-pager", "show", tree+":"+path)
	if err != nil {
		return "", false, false, err
	}
	binary := strings.ContainsRune(out, 0)
	return out, truncated, binary, nil
}

// findChange looks up a path in a change list.
func findChange(changes []fileChange, path string) (fileChange, bool) {
	for _, c := range changes {
		if c.Path == path {
			return c, true
		}
	}
	return fileChange{}, false
}
