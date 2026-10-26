package main

import (
	"encoding/json"
	"fmt"
	"html"
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

// snapshotView describes the git tree snapshot attached to a message.
type snapshotView struct {
	Start      string
	End        string
	StartShort string
	EndShort   string
	Files      []string
}

// messageView is a message prepared for the template.
type messageView struct {
	ID       string
	Type     string
	At       string
	Created  int64
	Agent    string
	Model    string
	Text     string
	Parts    []partView
	Outcome  string
	Snapshot *snapshotView
	Preview  string
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

type projectForm struct {
	Q, From, To, Sort string
}

type sessionForm struct {
	Q, Agent, Model, From, To, Sort, MinCost, MinTokens string
	Agents                                              []string
	Models                                              []modelOption
}

type timelineForm struct {
	Q, From, To, Tool                       string
	User, Assistant, Idle, Reasoning, Tools bool
	ToolsList                               []string
}

type pageData struct {
	Title    string
	DBPath   string
	Projects []projectView
	Project  projectView
	Sessions []sessionView
	Session  sessionView
	Messages []messageView
	Head     commitInfo
	PF       projectForm
	SF       sessionForm
	TF       timelineForm

	Tail          int
	FilteredTotal int
	NextTail      int
	PrevTail      int
	OlderURL      string
	NewerURL      string
}

func fmtTime(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	return time.UnixMilli(ms).Local().Format("2006-01-02 15:04")
}

func shortHash(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
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
	v := messageView{ID: m.ID, Type: m.Type, At: fmtTime(m.Created), Created: m.Created}

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
		Outcome  string `json:"outcome"`
		Snapshot *struct {
			Start string   `json:"start"`
			End   string   `json:"end"`
			Files []string `json:"files"`
		} `json:"snapshot"`
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
		v.Preview = previewText(v.Text)
		return v
	}
	if obj.Time.Created > 0 {
		v.Created = obj.Time.Created
		v.At = fmtTime(obj.Time.Created)
	}
	v.Agent = obj.Agent
	if obj.Model.ID != "" || obj.Model.ProviderID != "" {
		v.Model = modelLabel(`{"id":"` + obj.Model.ID + `","providerID":"` + obj.Model.ProviderID + `"}`)
	}
	v.Outcome = obj.Outcome
	if obj.Snapshot != nil && obj.Snapshot.Start != "" {
		v.Snapshot = &snapshotView{
			Start:      obj.Snapshot.Start,
			End:        obj.Snapshot.End,
			StartShort: shortHash(obj.Snapshot.Start),
			EndShort:   shortHash(obj.Snapshot.End),
			Files:      obj.Snapshot.Files,
		}
	}

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
	v.Preview = previewMessage(v)
	return v
}

func previewText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 90 {
		return string(r[:90]) + "…"
	}
	return s
}

func previewMessage(v messageView) string {
	if v.Text != "" {
		return previewText(v.Text)
	}
	for _, p := range v.Parts {
		if p.Kind == "text" && p.Text != "" {
			return previewText(p.Text)
		}
	}
	for _, p := range v.Parts {
		if p.Kind == "tool" {
			return "🛠️ " + p.Name
		}
	}
	return ""
}

// snapshotFromData extracts the snapshot block from raw message JSON.
func snapshotFromData(data []byte) snapshotView {
	var obj struct {
		Snapshot *struct {
			Start string   `json:"start"`
			End   string   `json:"end"`
			Files []string `json:"files"`
		} `json:"snapshot"`
	}
	if json.Unmarshal(data, &obj) != nil || obj.Snapshot == nil {
		return snapshotView{}
	}
	return snapshotView{
		Start:      obj.Snapshot.Start,
		End:        obj.Snapshot.End,
		StartShort: shortHash(obj.Snapshot.Start),
		EndShort:   shortHash(obj.Snapshot.End),
		Files:      obj.Snapshot.Files,
	}
}

// messageFilter selects and trims messages for the timeline.
type messageFilter struct {
	Q             string
	Types         map[string]bool
	TypesSet      bool
	ShowReasoning bool
	ShowTools     bool
	Tool          string
	From, To      int64
}

func filterMessages(in []messageView, f messageFilter) []messageView {
	needle := strings.ToLower(strings.TrimSpace(f.Q))
	out := make([]messageView, 0, len(in))
	for _, m := range in {
		if f.TypesSet && !f.Types[m.Type] {
			continue
		}
		if f.From > 0 && m.Created < f.From {
			continue
		}
		if f.To > 0 && m.Created > f.To {
			continue
		}
		if f.Tool != "" && !hasTool(m.Parts, f.Tool) {
			continue
		}
		v := m
		var parts []partView
		for _, p := range m.Parts {
			switch p.Kind {
			case "reasoning":
				if !f.ShowReasoning {
					continue
				}
			case "tool":
				if !f.ShowTools && f.Tool == "" {
					continue
				}
				if f.Tool != "" && p.Name != f.Tool {
					continue
				}
			}
			parts = append(parts, p)
		}
		v.Parts = parts
		if needle != "" && !containsText(v, needle) {
			continue
		}
		if m.Type == "assistant" && strings.TrimSpace(v.Text) == "" && len(v.Parts) == 0 {
			continue
		}
		out = append(out, v)
	}
	return out
}

func hasTool(parts []partView, name string) bool {
	for _, p := range parts {
		if p.Kind == "tool" && p.Name == name {
			return true
		}
	}
	return false
}

func containsText(m messageView, needle string) bool {
	if strings.Contains(strings.ToLower(m.Text), needle) {
		return true
	}
	for _, p := range m.Parts {
		if strings.Contains(strings.ToLower(p.Text), needle) ||
			strings.Contains(strings.ToLower(p.Name), needle) ||
			strings.Contains(strings.ToLower(p.Input), needle) ||
			strings.Contains(strings.ToLower(p.Output), needle) {
			return true
		}
	}
	return false
}

// distinctTools lists tool names present in a parsed session.
func distinctTools(msgs []messageView) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range msgs {
		for _, p := range m.Parts {
			if p.Kind == "tool" && p.Name != "" && !seen[p.Name] {
				seen[p.Name] = true
				out = append(out, p.Name)
			}
		}
	}
	return out
}

// tailSlice returns the newest tail messages from an ascending list.
func tailSlice(in []messageView, tail int) []messageView {
	if tail <= 0 {
		tail = 1000
	}
	if len(in) <= tail {
		return in
	}
	return in[len(in)-tail:]
}

// colorizeDiff escapes a unified diff and marks added/removed/hunk lines.
func colorizeDiff(s string) template.HTML {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		cls := ""
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			cls = "meta"
		case strings.HasPrefix(line, "+"):
			cls = "add"
		case strings.HasPrefix(line, "-"):
			cls = "del"
		case strings.HasPrefix(line, "@@"):
			cls = "hunk"
		case strings.HasPrefix(line, "diff "), strings.HasPrefix(line, "index "):
			cls = "meta"
		}
		esc := html.EscapeString(line)
		if cls != "" {
			fmt.Fprintf(&b, "<span class=\"%s\">%s</span>\n", cls, esc)
		} else {
			b.WriteString(esc + "\n")
		}
	}
	return template.HTML(b.String())
}

const pageCSS = `
:root { color-scheme: dark; }
* { box-sizing: border-box; }
body { margin: 0; background: #0f1115; color: #e6e6e6; font: 14px/1.6 -apple-system, "Segoe UI", sans-serif; }
header { padding: 14px 22px; border-bottom: 1px solid #262a33; background: #12151b; position: sticky; top: 0; z-index: 5; display: flex; justify-content: space-between; align-items: center; gap: 12px; }
header a { color: #8ab4ff; text-decoration: none; font-weight: 600; }
main { max-width: 1100px; margin: 0 auto; padding: 22px; }
h1 { font-size: 20px; margin: 8px 0 16px; word-break: break-all; }
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
.msg.flash { outline: 2px solid #ffd166; }
.role { font-size: 12px; color: #9aa4b2; margin-bottom: 6px; }
.text { white-space: pre-wrap; word-break: break-word; }
pre { white-space: pre-wrap; word-break: break-word; background: #0b0d11; border: 1px solid #232730; border-radius: 8px; padding: 10px; overflow: auto; max-height: 480px; }
details { margin: 8px 0; }
summary { cursor: pointer; color: #b9c3d2; }
details.reason > summary { color: #b7a6e8; }
details.tool > summary { color: #7fd1a3; }
.status { color: #8b94a3; font-size: 12px; }
.badge { display: inline-block; padding: 1px 8px; border-radius: 999px; background: #1d2430; color: #9aa4b2; font-size: 12px; }
.back { margin-bottom: 12px; }
.filters { display: flex; gap: 10px; flex-wrap: wrap; align-items: flex-end; margin: 0 0 14px; padding: 12px; border: 1px solid #232730; border-radius: 10px; background: #12151b; }
.filters label { display: flex; flex-direction: column; gap: 4px; font-size: 12px; color: #9aa4b2; }
.filters input, .filters select { background: #0b0d11; color: #e6e6e6; border: 1px solid #2a303b; border-radius: 6px; padding: 6px 8px; min-width: 120px; }
.filters input[type=date] { min-width: 140px; }
.filters .chk { flex-direction: row; align-items: center; gap: 6px; padding-bottom: 6px; }
.filters .chk input { min-width: 0; }
.filters button { background: #2b4a7a; color: #fff; border: 0; border-radius: 6px; padding: 8px 14px; cursor: pointer; }
.filters button:hover { background: #355a92; }
.filters .reset { color: #8b94a3; text-decoration: none; padding-bottom: 8px; }
.count { color: #8b94a3; margin: 0 0 10px; }
.empty { color: #8b94a3; padding: 18px; text-align: center; border: 1px dashed #2a303b; border-radius: 10px; }
.bm { position: relative; }
#bm-toggle { background: #1a1f29; color: #e6e6e6; border: 1px solid #2a303b; border-radius: 8px; padding: 6px 10px; cursor: pointer; }
.bm-panel { position: absolute; right: 0; top: 40px; width: 390px; max-height: 440px; overflow: auto; background: #12151b; border: 1px solid #2a303b; border-radius: 10px; padding: 10px; z-index: 20; box-shadow: 0 12px 32px rgba(0,0,0,.55); }
.bm-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.bm-head button { background: #2a1f22; color: #ffb4b4; border: 1px solid #4a2b30; border-radius: 6px; padding: 3px 8px; cursor: pointer; }
#bm-list { list-style: none; margin: 0; padding: 0; }
#bm-list li { display: flex; gap: 8px; align-items: flex-start; padding: 8px 0; border-top: 1px solid #232730; }
#bm-list li:first-child { border-top: 0; }
#bm-list a { display: flex; flex-direction: column; gap: 2px; text-decoration: none; flex: 1; }
.bm-preview { color: #e6e6e6; }
.bm-meta { color: #8b94a3; font-size: 12px; }
.bm-del { background: transparent; border: 0; color: #8b94a3; cursor: pointer; }
.bm-del:hover { color: #ff8a8a; }
.bm-toggle { background: transparent; border: 0; color: #8b94a3; cursor: pointer; font-size: 15px; float: right; }
.bm-toggle.on { color: #ffd166; }
.bm-empty { color: #8b94a3; }
.banner { background: #2a1f22; border: 1px solid #4a2b30; color: #ffd0d0; padding: 10px 12px; border-radius: 8px; margin-bottom: 12px; }
.changes { margin-top: 10px; border-top: 1px dashed #2a303b; padding-top: 8px; }
.changes-head { color: #9aa4b2; font-size: 12px; margin-bottom: 6px; display: flex; gap: 10px; flex-wrap: wrap; }
details.file { margin: 4px 0; }
details.file summary { color: #c9d1dd; }
.file-body { margin-top: 6px; }
.diff-wrap { border: 1px solid #232730; border-radius: 8px; overflow: hidden; }
.diff-head { padding: 8px 10px; border-bottom: 1px solid #232730; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.diff-tabs { display: flex; gap: 6px; padding: 8px 10px; border-bottom: 1px solid #232730; }
.diff-tabs button { background: #1a1f29; color: #c9d1dd; border: 1px solid #2a303b; border-radius: 6px; padding: 4px 10px; cursor: pointer; }
.diff-tabs button.active { background: #2b4a7a; color: #fff; }
pre.diff { border: 0; border-radius: 0; margin: 0; max-height: 520px; }
pre.diff .add { color: #7fd1a3; }
pre.diff .del { color: #ff8a8a; }
pre.diff .hunk { color: #8ab4ff; }
pre.diff .meta { color: #9aa4b2; }
.st-M { color: #e8c07d; }
.st-A { color: #7fd1a3; }
.st-D { color: #ff8a8a; }
`

const pageStart = `<!doctype html>
<html lang="ko"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · oc-viewer</title>
<style>` + pageCSS + `</style></head><body>
<header>
  <div><a href="/">oc-viewer</a> <span class="muted">OpenCode 세션 기록</span></div>
  <div class="bm">
    <button id="bm-toggle" type="button">★ Bookmarks <span id="bm-count"></span></button>
    <div id="bm-panel" class="bm-panel" hidden>
      <div class="bm-head"><strong>Bookmarks</strong><button id="bm-clear" type="button">전체 삭제</button></div>
      <ul id="bm-list"></ul>
    </div>
  </div>
</header>
<main>
<div id="bm-missing" class="banner" hidden></div>`

const pageEnd = `</main>
<script>
(function () {
  var BM = 'ocv.bookmarks';
  function loadBm() { try { var a = JSON.parse(localStorage.getItem(BM) || '[]'); return Array.isArray(a) ? a : []; } catch (e) { return []; } }
  function keyOf(x) { return x.session + '|' + x.msg; }
  function saveBm(a) { localStorage.setItem(BM, JSON.stringify(a)); renderBm(); }
  function renderBm() {
    var list = document.getElementById('bm-list');
    var cnt = document.getElementById('bm-count');
    if (!list) return;
    var items = loadBm();
    if (cnt) cnt.textContent = items.length ? '(' + items.length + ')' : '';
    list.textContent = '';
    if (!items.length) {
      var empty = document.createElement('li');
      empty.className = 'bm-empty';
      empty.textContent = '저장된 북마크가 없습니다.';
      list.appendChild(empty);
    }
    items.forEach(function (b) {
      var li = document.createElement('li');
      var a = document.createElement('a');
      a.href = '/session/' + encodeURIComponent(b.session) + '#msg-' + encodeURIComponent(b.msg);
      var p = document.createElement('span'); p.className = 'bm-preview'; p.textContent = b.preview || '(내용 없음)';
      var meta = document.createElement('span'); meta.className = 'bm-meta';
      meta.textContent = (b.worktree || '') + ' · ' + (b.title || '');
      a.appendChild(p); a.appendChild(meta);
      var del = document.createElement('button');
      del.type = 'button'; del.className = 'bm-del'; del.textContent = '✕'; del.title = '삭제';
      del.addEventListener('click', function (e) {
        e.preventDefault(); e.stopPropagation();
        saveBm(loadBm().filter(function (x) { return keyOf(x) !== keyOf(b); }));
      });
      li.appendChild(a); li.appendChild(del); list.appendChild(li);
    });
    var items2 = items;
    document.querySelectorAll('.bm-toggle').forEach(function (btn) {
      var card = btn.closest('.msg');
      if (!card) return;
      var k = card.dataset.session + '|' + card.dataset.msg;
      var on = items2.some(function (x) { return keyOf(x) === k; });
      btn.textContent = on ? '★' : '☆';
      btn.classList.toggle('on', on);
    });
  }
  function toggleBm(btn) {
    var card = btn.closest('.msg');
    if (!card) return;
    var b = {
      session: card.dataset.session, msg: card.dataset.msg,
      preview: card.dataset.preview || '', title: card.dataset.title || '',
      worktree: card.dataset.worktree || '', savedAt: Date.now()
    };
    var items = loadBm();
    var k = keyOf(b);
    var i = items.findIndex(function (x) { return keyOf(x) === k; });
    if (i >= 0) items.splice(i, 1); else items.unshift(b);
    saveBm(items);
  }
  document.addEventListener('click', function (e) {
    var t = e.target.closest('.bm-toggle');
    if (t) { e.preventDefault(); toggleBm(t); return; }
    if (e.target.closest('#bm-clear')) {
      if (confirm('북마크를 모두 삭제할까요?')) saveBm([]);
      return;
    }
    if (e.target.closest('#bm-toggle')) {
      var panel = document.getElementById('bm-panel');
      if (panel) panel.hidden = !panel.hidden;
      return;
    }
    if (!e.target.closest('.bm')) {
      var p = document.getElementById('bm-panel');
      if (p) p.hidden = true;
    }
  });

  // Lazy file diff loading.
  document.addEventListener('toggle', function (e) {
    var d = e.target;
    if (!d.matches || !d.matches('details.file') || !d.open || d.dataset.loaded) return;
    d.dataset.loaded = '1';
    var body = d.querySelector('.file-body');
    var msg = d.dataset.msg, file = d.dataset.file;
    body.textContent = '불러오는 중…';
    fetch('/diff?msg=' + encodeURIComponent(msg) + '&file=' + encodeURIComponent(file) + '&view=diff')
      .then(function (r) { return r.ok ? r.text() : Promise.reject(new Error('HTTP ' + r.status)); })
      .then(function (t) { body.innerHTML = t; })
      .catch(function (err) { body.textContent = '불러오기 실패: ' + err.message; });
  }, true);

  // Diff before/after tabs.
  document.addEventListener('click', function (e) {
    var b = e.target.closest('.diff-tabs button');
    if (!b) return;
    var wrap = b.closest('.diff-wrap');
    if (!wrap) return;
    var msg = wrap.dataset.msg, file = wrap.dataset.file, view = b.dataset.view;
    wrap.innerHTML = '<span class="muted" style="padding:10px;display:block">불러오는 중…</span>';
    fetch('/diff?msg=' + encodeURIComponent(msg) + '&file=' + encodeURIComponent(file) + '&view=' + encodeURIComponent(view))
      .then(function (r) { return r.ok ? r.text() : Promise.reject(new Error('HTTP ' + r.status)); })
      .then(function (t) { wrap.innerHTML = t; })
      .catch(function (err) { wrap.textContent = '불러오기 실패: ' + err.message; });
  });

  // Scroll position restore.
  try { history.scrollRestoration = 'manual'; } catch (e) {}
  var sk = 'ocv.scroll:' + location.pathname + location.search;
  var last = 0;
  addEventListener('scroll', function () {
    var n = Date.now();
    if (n - last < 200) return;
    last = n;
    try { sessionStorage.setItem(sk, String(scrollY)); } catch (e) {}
  }, { passive: true });
  addEventListener('pagehide', function () {
    try { sessionStorage.setItem(sk, String(scrollY)); } catch (e) {}
  });

  addEventListener('DOMContentLoaded', function () {
    renderBm();
    if (location.hash && location.hash.indexOf('#msg-') === 0) {
      var el = document.getElementById(location.hash.slice(1));
      if (el) {
        el.scrollIntoView({ block: 'center' });
        el.classList.add('flash');
      } else {
        var b = document.getElementById('bm-missing');
        if (b) {
          b.hidden = false;
          b.textContent = '';
          b.appendChild(document.createTextNode('북마크한 메시지가 현재 필터에 없습니다. '));
          var a = document.createElement('a');
          a.href = location.pathname + location.hash;
          a.textContent = '필터 초기화 후 이동';
          b.appendChild(a);
        }
      }
    } else {
      var y = parseInt(sessionStorage.getItem(sk) || '0', 10);
      if (y > 0) requestAnimationFrame(function () { scrollTo(0, y); });
    }
  });
})();
</script>
</body></html>`

var projectsTmpl = mustTmpl(`
<h1>프로젝트</h1>
<form class="filters" method="get">
  <label>검색<input name="q" value="{{.PF.Q}}" placeholder="worktree 또는 이름"></label>
  <label>시작일<input type="date" name="from" value="{{.PF.From}}"></label>
  <label>종료일<input type="date" name="to" value="{{.PF.To}}"></label>
  <label>정렬
    <select name="sort">
      <option value="active" {{if eq .PF.Sort "active"}}selected{{end}}>최근 활성</option>
      <option value="sessions" {{if eq .PF.Sort "sessions"}}selected{{end}}>세션 수</option>
    </select>
  </label>
  <button type="submit">적용</button>
  <a class="reset" href="/">초기화</a>
</form>
<p class="count">{{len .Projects}}개 프로젝트 · DB: <span class="mono">{{.DBPath}}</span></p>
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
<tr><td colspan="4" class="muted">조건에 맞는 프로젝트가 없습니다.</td></tr>
{{end}}
</tbody></table>
`)

var sessionsTmpl = mustTmpl(`
<div class="back"><a href="/">← 프로젝트</a></div>
<h1>{{.Project.Worktree}}</h1>
<form class="filters" method="get">
  <label>제목 검색<input name="q" value="{{.SF.Q}}" placeholder="세션 제목"></label>
  <label>agent
    <select name="agent">
      <option value="">전체</option>
      {{range .SF.Agents}}<option value="{{.}}" {{if eq . $.SF.Agent}}selected{{end}}>{{.}}</option>{{end}}
    </select>
  </label>
  <label>model
    <select name="model">
      <option value="">전체</option>
      {{range .SF.Models}}<option value="{{.Value}}" {{if eq .Value $.SF.Model}}selected{{end}}>{{.Label}}</option>{{end}}
    </select>
  </label>
  <label>시작일<input type="date" name="from" value="{{.SF.From}}"></label>
  <label>종료일<input type="date" name="to" value="{{.SF.To}}"></label>
  <label>최소 비용<input name="minCost" value="{{.SF.MinCost}}" placeholder="0.01" size="6"></label>
  <label>최소 토큰<input name="minTokens" value="{{.SF.MinTokens}}" placeholder="1000" size="7"></label>
  <label>정렬
    <select name="sort">
      <option value="updated" {{if eq .SF.Sort "updated"}}selected{{end}}>최근 갱신</option>
      <option value="created" {{if eq .SF.Sort "created"}}selected{{end}}>최근 생성</option>
      <option value="cost" {{if eq .SF.Sort "cost"}}selected{{end}}>비용</option>
      <option value="tokens" {{if eq .SF.Sort "tokens"}}selected{{end}}>토큰</option>
    </select>
  </label>
  <button type="submit">적용</button>
  <a class="reset" href="/project/{{.Project.ID}}">초기화</a>
</form>
<p class="count">{{len .Sessions}}개 세션 · 마지막 활성 {{.Project.LastActive}}</p>
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
<tr><td colspan="6" class="muted">조건에 맞는 세션이 없습니다.</td></tr>
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
  {{if .Head.Hash}}<span>HEAD <span class="mono">{{.Head.Hash}}</span> · {{.Head.Subject}}{{if .Head.Date}} · {{.Head.Date}}{{end}}</span>{{end}}
</div>
<form class="filters" method="get">
  <input type="hidden" name="f" value="1">
  <label>검색<input name="q" value="{{.TF.Q}}" placeholder="텍스트/툴 입출력"></label>
  <label>시작일<input type="date" name="from" value="{{.TF.From}}"></label>
  <label>종료일<input type="date" name="to" value="{{.TF.To}}"></label>
  <label>툴
    <select name="tool">
      <option value="">전체</option>
      {{range .TF.ToolsList}}<option value="{{.}}" {{if eq . $.TF.Tool}}selected{{end}}>{{.}}</option>{{end}}
    </select>
  </label>
  <label class="chk"><input type="checkbox" name="type" value="user" {{if .TF.User}}checked{{end}}> user</label>
  <label class="chk"><input type="checkbox" name="type" value="assistant" {{if .TF.Assistant}}checked{{end}}> assistant</label>
  <label class="chk"><input type="checkbox" name="type" value="idle" {{if .TF.Idle}}checked{{end}}> idle</label>
  <label class="chk"><input type="checkbox" name="reasoning" value="1" {{if .TF.Reasoning}}checked{{end}}> reasoning</label>
  <label class="chk"><input type="checkbox" name="tools" value="1" {{if .TF.Tools}}checked{{end}}> tools</label>
  <button type="submit">적용</button>
  <a class="reset" href="/session/{{.Session.ID}}">초기화</a>
</form>
<p class="count">{{len .Messages}}개 표시 · 필터 결과 {{.FilteredTotal}}개{{if .OlderURL}} · <a href="{{.OlderURL}}">이전 메시지 더 보기</a>{{end}}{{if .NewerURL}} · <a href="{{.NewerURL}}">최신으로</a>{{end}}</p>
{{range .Messages}}
<div class="msg {{.Type}}" id="msg-{{.ID}}"
     data-session="{{$.Session.ID}}" data-msg="{{.ID}}" data-title="{{$.Session.Title}}"
     data-worktree="{{$.Project.Worktree}}" data-preview="{{.Preview}}">
  <div class="role">
    <button class="bm-toggle" type="button" title="북마크">☆</button>
    {{.Type}}{{if .At}} · {{.At}}{{end}}{{if .Agent}} · {{.Agent}}{{end}}{{if .Model}} · {{.Model}}{{end}}
  </div>
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
  {{if .Snapshot}}{{if ne .Snapshot.Start .Snapshot.End}}{{$msg := .ID}}
    <div class="changes">
      <div class="changes-head">변경 {{len .Snapshot.Files}}개 파일
        <span class="mono">{{.Snapshot.StartShort}} → {{.Snapshot.EndShort}}</span></div>
      {{range .Snapshot.Files}}
      <details class="file" data-msg="{{$msg}}" data-file="{{.}}">
        <summary class="mono">{{.}}</summary>
        <div class="file-body"><span class="muted">펼치면 로드합니다…</span></div>
      </details>
      {{else}}
      <div class="muted">변경 파일 정보가 없습니다.</div>
      {{end}}
    </div>
  {{end}}{{end}}
</div>
{{else}}
<div class="empty">조건에 맞는 메시지가 없습니다.</div>
{{end}}
`)

func mustTmpl(body string) *template.Template {
	return template.Must(template.New("page").Parse(pageStart + body + pageEnd))
}
