// Package projects resolves a Discord channel name to a project folder (cwd).
// Resolution order:
//  1. override (channel id or lowercase channel name) in channel-cwd.json
//  2. exact        lowercase(channel) == lowercase(folder)
//  3. normalized   separators removed (custom_ccst -> CustomCCST)
//  4. suffix       channel is the unique suffix of a folder (ccst -> CustomCCST)
//  5. norm-suffix  separator-insensitive variant of 4
package projects

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Match is the outcome of a successful resolution.
type Match struct {
	Folder string
	Rule   string
}

type folderEntry struct {
	name  string
	lower string
	norm  string
	full  string
}

func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func listFolders(root string) []folderEntry {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	out := make([]folderEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		lower := strings.ToLower(e.Name())
		out = append(out, folderEntry{
			name:  e.Name(),
			lower: lower,
			norm:  norm(e.Name()),
			full:  filepath.Join(root, e.Name()),
		})
	}
	return out
}

// Resolve maps a channel name to a project folder under root.
func Resolve(channelName, root string, overrides map[string]string) *Match {
	name := strings.TrimSpace(channelName)
	if name == "" {
		return nil
	}
	lower := strings.ToLower(name)
	n := norm(name)
	folders := listFolders(root)

	// 1. override
	if overrides != nil {
		for k, v := range overrides {
			if k == lower || k == name {
				return &Match{Folder: v, Rule: "override"}
			}
		}
	}

	// 2. exact
	var exact []folderEntry
	for _, f := range folders {
		if f.lower == lower {
			exact = append(exact, f)
		}
	}
	if len(exact) == 1 {
		return &Match{Folder: exact[0].full, Rule: "exact"}
	}

	// 3. normalized
	if n != "" {
		var normalized []folderEntry
		for _, f := range folders {
			if f.norm != "" && f.norm == n {
				normalized = append(normalized, f)
			}
		}
		if len(normalized) == 1 {
			return &Match{Folder: normalized[0].full, Rule: "normalized"}
		}
	}

	// 4. suffix
	var suffix []folderEntry
	for _, f := range folders {
		if f.lower == lower {
			continue
		}
		if strings.HasSuffix(f.lower, "_"+lower) || strings.HasSuffix(f.lower, "-"+lower) ||
			strings.HasSuffix(f.lower, " "+lower) || strings.HasSuffix(f.lower, lower) {
			suffix = append(suffix, f)
		}
	}
	if len(suffix) == 1 {
		return &Match{Folder: suffix[0].full, Rule: "suffix"}
	}

	// 5. norm-suffix
	if n != "" {
		var normSuffix []folderEntry
		for _, f := range folders {
			if f.norm != n && len(f.norm) > len(n) && strings.HasSuffix(f.norm, n) {
				normSuffix = append(normSuffix, f)
			}
		}
		if len(normSuffix) == 1 {
			return &Match{Folder: normSuffix[0].full, Rule: "norm-suffix"}
		}
	}

	return nil
}

// IsDir reports whether p exists and is a directory.
func IsDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Describe lists folder names under root (sorted), capped at limit.
func Describe(root string, limit int) []string {
	folders := listFolders(root)
	names := make([]string, 0, len(folders))
	for _, f := range folders {
		names = append(names, f.name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	if limit <= 0 || len(names) <= limit {
		return names
	}
	return append(names[:limit], "… "+itoa(len(names)-limit)+" more")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
