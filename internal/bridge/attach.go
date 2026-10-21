package bridge

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/G226Medi03/chat-app-adapter/chat"
)

// attachmentInfo is a downloaded inbound attachment.
type attachmentInfo struct {
	Name        string
	Size        int64
	ContentType string
	Path        string
}

// safeAttachmentName turns an untrusted filename into a single safe path element.
func safeAttachmentName(name string, index int) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		name = fmt.Sprintf("attachment-%d", index+1)
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	return name
}

// downloadAttachments stores inbound attachments under
// state/attachments/<messageID>/ and returns their local paths.
func (b *Bridge) downloadAttachments(m chat.Message) ([]attachmentInfo, error) {
	if len(m.Attachments) == 0 {
		return nil, nil
	}
	dir := filepath.Join(b.config.Root, "state", "attachments", m.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("첨부 저장 폴더 생성 실패: %w", err)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	out := make([]attachmentInfo, 0, len(m.Attachments))
	used := map[string]bool{}
	for i, a := range m.Attachments {
		name := safeAttachmentName(a.Name, i)
		for n := 1; used[name]; n++ {
			name = safeAttachmentName(a.Name, i) + fmt.Sprintf("-%d", n)
		}
		used[name] = true
		target := filepath.Join(dir, name)

		resp, err := client.Get(a.URL)
		if err != nil {
			return nil, fmt.Errorf("첨부 다운로드 실패(%s): %w", name, err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("첨부 다운로드 실패(%s): HTTP %d", name, resp.StatusCode)
		}
		f, err := os.Create(target)
		if err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("첨부 저장 실패(%s): %w", name, err)
		}
		n, copyErr := io.Copy(f, resp.Body)
		resp.Body.Close()
		if cerr := f.Close(); copyErr == nil {
			copyErr = cerr
		}
		if copyErr != nil {
			return nil, fmt.Errorf("첨부 저장 실패(%s): %w", name, copyErr)
		}
		out = append(out, attachmentInfo{Name: name, Size: n, ContentType: a.ContentType, Path: target})
		b.logger.Infof("[attach] downloaded name=%s size=%d type=%s path=%s", name, n, a.ContentType, target)
	}
	return out, nil
}

// formatAttachments renders the prompt block describing downloaded files.
func formatAttachments(atts []attachmentInfo) string {
	if len(atts) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<attachments>\n")
	for _, a := range atts {
		kind := a.ContentType
		if kind == "" {
			kind = "unknown"
		}
		fmt.Fprintf(&sb, "- %s (%s, %s)\n  path: %s\n", a.Name, humanSize(a.Size), kind, a.Path)
	}
	sb.WriteString("</attachments>")
	return sb.String()
}

// humanSize formats a byte count for a chat message.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
