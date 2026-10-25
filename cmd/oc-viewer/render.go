package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// partView is one rendered piece of an assistant message.
type partView struct {
	Kind   string // text, reasoning, tool
	Text   string
	Name   string
	Status string
	Input  string
	Output string
}

// messageView is a message prepared for the template.
type messageView struct {
	Type    string
	At      string
	Agent   string
	Model   string
	Text    string
	Parts   []partView
	Outcome string
}

type projectView struct {
	ID         string
	Worktree   string
	Name       string
	LastActive string
	Sessions   int
}

type sessionView struct {
	ID        string
	ProjectID string
	Title     string
	Agent     string
	Model     string
	Updated   string
	Tokens    string
	Cost      string
}

type pageData struct {
	Title    string
	DBPath   string
	Projects []projectView
	Project  projectView
	Sessions []sessionView
	Session  sessionView
	Messages []messageView
}

func fmtTime(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04")
}

func modelLabel(raw string) string {
	var m struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
	}
	if json.Unmarshal([]byte(raw), &m) == nil && (m.ID != "" || m.ProviderID != "") {
		if m.ProviderID != "" && m.ID != "" {
			return m.ProviderID + "/" + m.ID
		}
		return m.ProviderID + m.ID
	}
	return strings.TrimSpace(raw)
}

func toProjectView(p project) projectView {
	return projectView{
		ID:         p.ID,
		Worktree:   p.Worktree,
		Name:       p.Name,
		LastActive: fmtTime(p.TimeActive),
		Sessions:   p.Sessions,
	}
}

func toSessionView(s session) sessionView {
	return sessionView{
		ID:        s.ID,
		ProjectID: s.ProjectID,
		Title:     s.Title,
		Agent:     s.Agent,
		Model:     modelLabel(s.Model),
		Updated:   fmtTime(s.Updated),
		Tokens:    fmt.Sprintf("%d / %d", s.TokensIn, s.TokensOut),
		Cost:      fmt.Sprintf("$%.4f", s.Cost),
	}
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n… (truncated)"
}

func rawText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		b, err := json.MarshalIndent(v, "", "  ")
		if err == nil {
			return string(b)
		}
	}
	return string(raw)
}

// parseMessage turns a session_message row into a renderable view.
func parseMessage(m message) messageView {
	v := messageView{Type: m.Type, At: fmtTime(m.Created)}

	var obj struct {
		Text  string `json:"text"`
		Agent string `json:"agent"`
		Model struct {
			ID         string `json:"id"`
			ProviderID string `json:"providerID"`
		} `json:"model"`
		Time struct {
			Created int64 `json:"created"`
		} `json:"time"`
		Outcome string `json:"outcome"`
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Name  string `json:"name"`
			State struct {
				Status string          `json:"status"`
				Input  json.RawMessage `json:"input"`
				Output json.RawMessage `json:"output"`
			} `json:"state"`
		} `json:"content"`
	}
	if err := json.Unmarshal(m.Data, &obj); err != nil {
		v.Text = truncateText(string(m.Data), 4000)
		return v
	}
	if obj.Time.Created > 0 {
		v.At = fmtTime(obj.Time.Created)
	}
	v.Agent = obj.Agent
	if obj.Model.ID != "" || obj.Model.ProviderID != "" {
		v.Model = modelLabel(`{"id":"` + obj.Model.ID + `","providerID":"` + obj.Model.ProviderID + `"}`)
	}
	v.Outcome = obj.Outcome

	switch m.Type {
	case "user":
		v.Text = obj.Text
		if v.Text == "" {
			for _, p := range obj.Content {
				if p.Type == "text" {
					v.Text += p.Text
				}
			}
		}
	case "assistant":
		for _, p := range obj.Content {
			switch p.Type {
			case "text":
				if strings.TrimSpace(p.Text) != "" {
					v.Parts = append(v.Parts, partView{Kind: "text", Text: p.Text})
				}
			case "reasoning":
				if strings.TrimSpace(p.Text) != "" {
					v.Parts = append(v.Parts, partView{Kind: "reasoning", Text: truncateText(p.Text, 8000)})
				}
			case "tool":
				v.Parts = append(v.Parts, partView{
					Kind:   "tool",
					Name:   p.Name,
					Status: p.State.Status,
					Input:  truncateText(rawText(p.State.Input), 4000),
					Output: truncateText(rawText(p.State.Output), 4000),
				})
			}
		}
	default:
		if v.Text == "" {
			v.Text = truncateText(string(m.Data), 2000)
		}
	}
	return v
}

const pageCSS = `
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin: 0; background: #0f1115; color: #e6e6e6; font: 14px/1.6 -apple-system, "Segoe UI", sans-serif; }
header { padding: 14px 22px; border-bottom: 1px solid #262a33; background: #12151b; position: sticky; top: 0; }
header a { color: #8ab4ff; text-decoration: none; font-weight: 600; }
main { max-width: 1080px; margin: 0 auto; padding: 22px; }
h1 { font-size: 20px; margin: 8px 0 16px; word-break: break-all; }
h2 { font-size: 15px; margin: 0 0 8px; }
a { color: #8ab4ff; }
table { width: 100%; border-collapse: collapse; }
th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid #232730; vertical-align: top; }
th { color: #9aa4b2; font-weight: 600; font-size: 12px; }
tr:hover td { background: #151922; }
.muted { color: #8b94a3; }
.mono { font-family: Consolas, "Cascadia Mono", monospace; font-size: 12px; }
.meta { color: #9aa4b2; margin-bottom: 18px; display: flex; gap: 14px; flex-wrap: wrap; }
.msg { border: 1px solid #232730; border-radius: 10px; padding: 12px 14px; margin: 12px 0; background: #12151b; }
.msg.user { border-color: #2b4a7a; background: #101826; }
.role { font-size: 12px; color: #9aa4b2; margin-bottom: 6px; }
.text { white-space: pre-wrap; word-break: break-word; }
pre { white-space: pre-wrap; word-break: break-word; background: #0b0d11; border: 1px solid #232730; border-radius: 8px; padding: 10px; overflow: auto; max-height: 420px; }
details { margin: 8px 0; }
summary { cursor: pointer; color: #b9c3d2; }
details.reason > summary { color: #b7a6e8; }
details.tool > summary { color: #7fd1a3; }
.status { color: #8b94a3; font-size: 12px; }
.badge { display: inline-block; padding: 1px 8px; border-radius: 999px; background: #1d2430; color: #9aa4b2; font-size: 12px; }
.back { margin-bottom: 12px; }
`

const pageStart = `<!doctype html>
<html lang="ko"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · oc-viewer</title>
<style>` + pageCSS + `</style></head><body>
<header><a href="/">oc-viewer</a> <span class="muted">OpenCode 세션 기록</span></header>
<main>`

const pageEnd = `</main></body></html>`

var projectsTmpl = mustTmpl(`
<h1>프로젝트</h1>
<p class="muted mono">DB: {{.DBPath}}</p>
<table>
<thead><tr><th>worktree</th><th>이름</th><th>세션</th><th>마지막 활성</th></tr></thead>
<tbody>
{{range .Projects}}
<tr>
  <td><a href="/project/{{.ID}}">{{.Worktree}}</a></td>
  <td>{{.Name}}</td>
  <td>{{.Sessions}}</td>
  <td class="muted">{{.LastActive}}</td>
</tr>
{{else}}
<tr><td colspan="4" class="muted">프로젝트가 없습니다.</td></tr>
{{end}}
</tbody></table>
`)

var sessionsTmpl = mustTmpl(`
<div class="back"><a href="/">← 프로젝트</a></div>
<h1>{{.Project.Worktree}}</h1>
<div class="meta"><span>세션 {{len .Sessions}}개</span><span>마지막 활성 {{.Project.LastActive}}</span></div>
<table>
<thead><tr><th>제목</th><th>agent</th><th>model</th><th>tokens in/out</th><th>cost</th><th>갱신</th></tr></thead>
<tbody>
{{range .Sessions}}
<tr>
  <td><a href="/session/{{.ID}}">{{if .Title}}{{.Title}}{{else}}(제목 없음){{end}}</a><div class="muted mono">{{.ID}}</div></td>
  <td>{{.Agent}}</td>
  <td class="mono">{{.Model}}</td>
  <td class="mono">{{.Tokens}}</td>
  <td class="mono">{{.Cost}}</td>
  <td class="muted">{{.Updated}}</td>
</tr>
{{else}}
<tr><td colspan="6" class="muted">세션이 없습니다.</td></tr>
{{end}}
</tbody></table>
`)

var sessionTmpl = mustTmpl(`
<div class="back"><a href="/project/{{.Session.ProjectID}}">← 세션 목록</a> · <a href="/">프로젝트</a></div>
<h1>{{if .Session.Title}}{{.Session.Title}}{{else}}(제목 없음){{end}}</h1>
<div class="meta">
  <span class="mono">{{.Session.ID}}</span>
  <span>agent: {{.Session.Agent}}</span>
  <span>model: {{.Session.Model}}</span>
  <span>tokens: {{.Session.Tokens}}</span>
  <span>cost: {{.Session.Cost}}</span>
  <span>갱신: {{.Session.Updated}}</span>
</div>
{{range .Messages}}
<div class="msg {{.Type}}">
  <div class="role">{{.Type}}{{if .At}} · {{.At}}{{end}}{{if .Agent}} · {{.Agent}}{{end}}{{if .Model}} · {{.Model}}{{end}}</div>
  {{if .Text}}<div class="text">{{.Text}}</div>{{end}}
  {{if .Outcome}}<div class="badge">idle: {{.Outcome}}</div>{{end}}
  {{range .Parts}}
    {{if eq .Kind "text"}}<div class="text">{{.Text}}</div>{{end}}
    {{if eq .Kind "reasoning"}}
      <details class="reason"><summary>reasoning</summary><pre>{{.Text}}</pre></details>
    {{end}}
    {{if eq .Kind "tool"}}
      <details class="tool">
        <summary>🛠️ {{if .Name}}{{.Name}}{{else}}tool{{end}}{{if .Status}} <span class="status">({{.Status}})</span>{{end}}</summary>
        {{if .Input}}<pre>{{.Input}}</pre>{{end}}
        {{if .Output}}<pre>{{.Output}}</pre>{{end}}
      </details>
    {{end}}
  {{end}}
</div>
{{else}}
<p class="muted">메시지가 없습니다.</p>
{{end}}
`)

func mustTmpl(body string) *template.Template {
	return template.Must(template.New("page").Parse(pageStart + body + pageEnd))
}
