// Package chunk splits long text for Discord's 2000-char limit while keeping
// code fences balanced.
package chunk

import (
	"strings"
)

var fenceRE = func(line string) (bool, string) {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "```") && !strings.HasPrefix(t, "~~~") {
		return false, ""
	}
	marker := t[:3]
	rest := strings.TrimSpace(t[3:])
	// language token only (no spaces)
	for _, r := range rest {
		if !(r == '+' || r == '#' || r == '.' || r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false, ""
		}
	}
	_ = marker
	return true, rest
}

// Split breaks text into pieces of at most limit chars, closing/reopening code
// fences across boundaries. Empty input yields nil.
func Split(text string, limit int) []string {
	if limit <= 0 {
		limit = 1900
	}
	src := strings.ReplaceAll(text, "\r\n", "\n")
	if len(src) <= limit {
		if strings.TrimSpace(src) == "" {
			return nil
		}
		return []string{src}
	}

	var out []string
	fenceActive := false
	fenceLang := ""
	buf := ""

	fenceClose := func() string {
		if fenceActive {
			return "```"
		}
		return ""
	}
	fenceOpen := func() string {
		if fenceActive {
			return "```" + fenceLang
		}
		return ""
	}
	flush := func() {
		if strings.TrimSpace(buf) != "" {
			out = append(out, strings.TrimRight(buf+fenceClose(), " \t\n"))
		}
		buf = fenceOpen()
	}

	for _, line := range strings.Split(src, "\n") {
		isFence, lang := fenceRE(line)

		if len(line)+1+len(fenceClose()) > limit {
			rest := line
			for len(rest) > 0 {
				room := limit - len(buf) - len(fenceClose())
				if len(rest) <= room {
					buf += rest
					break
				}
				if room <= 0 {
					flush()
					continue
				}
				cut := strings.LastIndex(rest[:room], " ")
				if cut <= 0 {
					cut = room
				}
				buf += rest[:cut]
				out = append(out, strings.TrimRight(buf+fenceClose(), " \t\n"))
				buf = fenceOpen()
				rest = strings.TrimLeft(rest[cut:], " ")
			}
			buf += "\n"
		} else {
			if len(buf)+len(line)+1+len(fenceClose()) > limit {
				flush()
			}
			buf += line + "\n"
		}
		if isFence {
			if fenceActive {
				fenceActive = false
				fenceLang = ""
			} else {
				fenceActive = true
				fenceLang = lang
			}
		}
	}
	if strings.TrimSpace(buf) != "" {
		out = append(out, strings.TrimRight(buf+fenceClose(), " \t\n"))
	}
	res := out[:0]
	for _, s := range out {
		if len(s) > 0 {
			res = append(res, s)
		}
	}
	return res
}

// LivePreview shows the tail of text, truncated to limit, with a cursor marker.
func LivePreview(text string, limit int) string {
	if limit <= 0 {
		limit = 1900
	}
	if len(text) <= limit-2 {
		return text + " ▌"
	}
	return "… " + text[len(text)-(limit-4):] + " ▌"
}

var statusIcon = map[string]string{
	"pending": "🛠️", "in_progress": "⏳", "running": "⏳",
	"completed": "✅", "failed": "❌", "cancelled": "🚫",
}

// ToolLine renders a single tool status line.
func ToolLine(title, status string) string {
	icon := statusIcon[status]
	if icon == "" {
		icon = "🛠️"
	}
	title = strings.Join(strings.Fields(title), " ")
	if len(title) > 160 {
		title = title[:160]
	}
	suffix := ""
	if status != "" && status != "pending" && status != "in_progress" {
		suffix = "  (" + status + ")"
	}
	return icon + " " + title + suffix
}
