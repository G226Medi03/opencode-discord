// Package launcher resolves how to start the `opencode` CLI, including the
// Windows npm shim (opencode.cmd -> @opencode/cli/bin/opencode.exe).
package launcher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Spec describes how to invoke opencode. Callers append the subcommand and args.
type Spec struct {
	Command string
	Args    []string // prefix args (e.g. {"node", launcher.js} or {"cmd", "/c", "opencode"})
	Via     string
}

func exists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func pathDirs() []string {
	var dirs []string
	for _, d := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// Resolve returns the best way to run opencode. explicit may be empty.
func Resolve(explicit string) Spec {
	// 1. explicit path
	if explicit != "" {
		return forFile(explicit, "explicit")
	}
	// 2. OPENCODE_BIN
	if v := os.Getenv("OPENCODE_BIN"); v != "" {
		return forFile(v, "OPENCODE_BIN")
	}

	if runtime.GOOS == "windows" {
		for _, dir := range pathDirs() {
			// native exe on PATH
			exe := filepath.Join(dir, "opencode.exe")
			if exists(exe) {
				return Spec{Command: exe, Via: "PATH " + exe}
			}
			// npm shim: sibling node_modules/@opencode/cli/bin/opencode.exe
			cmdShim := filepath.Join(dir, "opencode.cmd")
			if exists(cmdShim) {
				cand := filepath.Join(dir, "node_modules", "@opencode", "cli", "bin", "opencode.exe")
				if exists(cand) {
					return Spec{Command: cand, Via: "npm shim " + cand}
				}
				return Spec{Command: "cmd.exe", Args: []string{"/c", "opencode"}, Via: "cmd shim"}
			}
		}
		return Spec{Command: "cmd.exe", Args: []string{"/c", "opencode"}, Via: "cmd (PATH)"}
	}

	return Spec{Command: "opencode", Via: "PATH"}
}

func forFile(p string, via string) Spec {
	ext := strings.ToLower(filepath.Ext(p))
	if runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat") {
		return Spec{Command: "cmd.exe", Args: []string{"/c", p}, Via: via}
	}
	return Spec{Command: p, Via: via}
}
