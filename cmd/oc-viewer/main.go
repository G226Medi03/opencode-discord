package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type server struct {
	db     *sql.DB
	dbPath string
}

func main() {
	dbPath := flag.String("db", defaultDBPath(), "OpenCode SQLite database path")
	addr := flag.String("addr", "127.0.0.1:8765", "listen address (localhost only by default)")
	flag.Parse()

	db, err := openReadOnly(*dbPath)
	if err != nil {
		log.Fatalf("oc-viewer: %v", err)
	}
	defer db.Close()

	s := &server{db: db, dbPath: *dbPath}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleProjects)
	mux.HandleFunc("GET /project/{id}", s.handleProject)
	mux.HandleFunc("GET /session/{id}", s.handleSession)
	mux.HandleFunc("GET /diff", s.handleDiff)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("oc-viewer: db=%s addr=http://%s", *dbPath, *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("oc-viewer: %v", err)
	}
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "opencode.db"
	}
	return filepath.Join(home, ".local", "share", "opencode", "opencode.db")
}

func (s *server) handleProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pf := projectForm{Q: q.Get("q"), From: q.Get("from"), To: q.Get("to"), Sort: q.Get("sort")}
	if pf.Sort == "" {
		pf.Sort = "active"
	}
	ps, err := listProjects(s.db, projectFilter{
		Q: pf.Q, From: dateStart(pf.From), To: dateEnd(pf.To), Sort: pf.Sort,
	})
	if err != nil {
		http.Error(w, "projects: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := pageData{Title: "프로젝트", DBPath: s.dbPath, PF: pf}
	for _, p := range ps {
		data.Projects = append(data.Projects, toProjectView(p))
	}
	render(w, projectsTmpl, data)
}

func (s *server) handleProject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := getProject(s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "project: "+err.Error(), http.StatusInternalServerError)
		return
	}
	q := r.URL.Query()
	sf := sessionForm{
		Q: q.Get("q"), Agent: q.Get("agent"), Model: q.Get("model"),
		From: q.Get("from"), To: q.Get("to"), Sort: q.Get("sort"),
		MinCost: q.Get("minCost"), MinTokens: q.Get("minTokens"),
	}
	if sf.Sort == "" {
		sf.Sort = "updated"
	}
	agents, err := distinctAgents(s.db, id)
	if err != nil {
		http.Error(w, "agents: "+err.Error(), http.StatusInternalServerError)
		return
	}
	models, err := distinctModels(s.db, id)
	if err != nil {
		http.Error(w, "models: "+err.Error(), http.StatusInternalServerError)
		return
	}
	sf.Agents, sf.Models = agents, models

	ss, err := listSessions(s.db, id, sessionFilter{
		Q: sf.Q, Agent: sf.Agent, Model: sf.Model,
		From: dateStart(sf.From), To: dateEnd(sf.To),
		MinCost: parseFloat(sf.MinCost), MinTokens: parseInt64(sf.MinTokens),
		Sort: sf.Sort,
	})
	if err != nil {
		http.Error(w, "sessions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := pageData{Title: p.Worktree, DBPath: s.dbPath, Project: toProjectView(p), SF: sf}
	for _, sess := range ss {
		data.Sessions = append(data.Sessions, toSessionView(sess))
	}
	render(w, sessionsTmpl, data)
}

func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, err := getSession(s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	proj, err := getProject(s.db, sess.ProjectID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "project: "+err.Error(), http.StatusInternalServerError)
		return
	}
	msgs, err := listMessages(s.db, id)
	if err != nil {
		http.Error(w, "messages: "+err.Error(), http.StatusInternalServerError)
		return
	}
	all := make([]messageView, 0, len(msgs))
	for _, m := range msgs {
		all = append(all, parseMessage(m))
	}

	q := r.URL.Query()
	submitted := q.Get("f") == "1"
	tf := timelineForm{Q: q.Get("q"), From: q.Get("from"), To: q.Get("to"), Tool: q.Get("tool")}
	types := map[string]bool{}
	if submitted {
		for _, t := range q["type"] {
			types[t] = true
		}
		tf.User, tf.Assistant, tf.Idle = types["user"], types["assistant"], types["idle"]
		tf.Reasoning = q.Get("reasoning") == "1"
		tf.Tools = q.Get("tools") == "1"
	} else {
		tf.User, tf.Assistant, tf.Idle = true, true, true
		tf.Tools = true
	}
	tf.ToolsList = distinctTools(all)

	filtered := filterMessages(all, messageFilter{
		Q: tf.Q, Types: types, TypesSet: submitted,
		ShowReasoning: tf.Reasoning, ShowTools: tf.Tools, Tool: tf.Tool,
		From: dateStart(tf.From), To: dateEnd(tf.To),
	})

	data := pageData{
		Title: sess.Title, DBPath: s.dbPath,
		Project: toProjectView(proj), Session: toSessionView(sess),
		Messages: filtered, TF: tf, Head: headCommit(proj.Worktree),
	}
	render(w, sessionTmpl, data)
}

func render(w http.ResponseWriter, t *template.Template, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("oc-viewer: render: %v", err)
	}
}

// ---- diffs ---------------------------------------------------------------

type changesEntry struct {
	list []fileChange
	at   time.Time
}

var changesCache = struct {
	sync.Mutex
	m map[string]changesEntry
}{m: map[string]changesEntry{}}

type fragmentEntry struct {
	body string
	at   time.Time
}

var fragmentCache = struct {
	sync.Mutex
	m map[string]fragmentEntry
}{m: map[string]fragmentEntry{}}

func changesCached(msgID, shadow, start, end string) ([]fileChange, error) {
	changesCache.Lock()
	if e, ok := changesCache.m[msgID]; ok && time.Since(e.at) < 5*time.Minute {
		changesCache.Unlock()
		return e.list, nil
	}
	changesCache.Unlock()
	list, err := changedFiles(shadow, start, end)
	if err != nil {
		return nil, err
	}
	changesCache.Lock()
	changesCache.m[msgID] = changesEntry{list: list, at: time.Now()}
	changesCache.Unlock()
	return list, nil
}

func (s *server) handleDiff(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	msgID := q.Get("msg")
	file := q.Get("file")
	view := q.Get("view")
	if view != "before" && view != "after" {
		view = "diff"
	}
	if msgID == "" || file == "" {
		http.Error(w, "msg and file are required", http.StatusBadRequest)
		return
	}
	key := msgID + "|" + file + "|" + view
	fragmentCache.Lock()
	if e, ok := fragmentCache.m[key]; ok && time.Since(e.at) < 5*time.Minute {
		fragmentCache.Unlock()
		writeHTML(w, e.body)
		return
	}
	fragmentCache.Unlock()

	var sessionID string
	var data []byte
	err := s.db.QueryRow(`select session_id, coalesce(data,'{}') from session_message where id = ?`, msgID).
		Scan(&sessionID, &data)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "message: "+err.Error(), http.StatusInternalServerError)
		return
	}
	snap := snapshotFromData(data)
	if snap.Start == "" || snap.End == "" || snap.Start == snap.End {
		http.Error(w, "message has no snapshot", http.StatusBadRequest)
		return
	}
	sess, err := getSession(s.db, sessionID)
	if err != nil {
		http.Error(w, "session: "+err.Error(), http.StatusInternalServerError)
		return
	}
	shadow, err := findSnapshotRepo(sess.ProjectID)
	if err != nil {
		http.Error(w, "snapshot repo: "+err.Error(), http.StatusInternalServerError)
		return
	}
	changes, err := changesCached(msgID, shadow, snap.Start, snap.End)
	if err != nil {
		http.Error(w, "changes: "+err.Error(), http.StatusInternalServerError)
		return
	}
	ch, ok := findChange(changes, file)
	if !ok {
		http.Error(w, "file is not part of this snapshot", http.StatusForbidden)
		return
	}

	inner := template.HTML("")
	binary := ch.Binary
	truncated := false
	switch view {
	case "diff":
		d, tr, err := fileDiff(shadow, snap.Start, snap.End, file)
		if err != nil {
			http.Error(w, "diff: "+err.Error(), http.StatusInternalServerError)
			return
		}
		truncated = tr
		if strings.ContainsRune(d, 0) {
			binary = true
		} else {
			inner = colorizeDiff(d)
		}
	case "before", "after":
		tree := snap.Start
		if view == "after" {
			tree = snap.End
		}
		c, tr, bin, err := fileAt(shadow, tree, file)
		if err != nil {
			c = "(이 시점에 파일이 없습니다)"
			tr, bin = false, false
		}
		truncated, binary = tr, bin
		if !binary {
			inner = template.HTML(html.EscapeString(c))
		}
	}
	body := renderFileFragment(ch, msgID, file, view, inner, truncated, binary)
	fragmentCache.Lock()
	fragmentCache.m[key] = fragmentEntry{body: body, at: time.Now()}
	fragmentCache.Unlock()
	writeHTML(w, body)
}

func writeHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func renderFileFragment(ch fileChange, msgID, file, view string, inner template.HTML, truncated, binary bool) string {
	var b strings.Builder
	status := ch.Status
	if status == "" {
		status = "?"
	}
	fmt.Fprintf(&b, `<div class="diff-wrap" data-msg="%s" data-file="%s">`,
		template.HTMLEscapeString(msgID), template.HTMLEscapeString(file))
	fmt.Fprintf(&b, `<div class="diff-head"><span class="st st-%s">%s</span> <span class="mono">%s</span>`,
		status, status, template.HTMLEscapeString(file))
	if binary {
		b.WriteString(` <span class="muted">binary file</span>`)
	} else {
		fmt.Fprintf(&b, ` <span class="muted">+%d −%d</span>`, ch.Additions, ch.Deletions)
	}
	b.WriteString(`</div>`)

	if !binary {
		b.WriteString(`<div class="diff-tabs">`)
		for _, v := range []string{"diff", "before", "after"} {
			cls := ""
			if v == view {
				cls = " active"
			}
			label := map[string]string{"diff": "Diff", "before": "Before", "after": "After"}[v]
			fmt.Fprintf(&b, `<button type="button" data-view="%s" class="%s">%s</button>`, v, cls, label)
		}
		b.WriteString(`</div>`)
	}
	if binary {
		b.WriteString(`<div class="muted" style="padding:10px">binary file</div>`)
	} else {
		b.WriteString(`<pre class="diff">`)
		b.WriteString(string(inner))
		b.WriteString(`</pre>`)
	}
	if truncated {
		b.WriteString(`<div class="muted" style="padding:6px 10px">… 200KB에서 잘림</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

// ---- query helpers -------------------------------------------------------

func dateStart(s string) int64 {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func dateEnd(s string) int64 {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return 0
	}
	return t.AddDate(0, 0, 1).Add(-time.Millisecond).UnixMilli()
}

func parseInt64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func parseFloat(s string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return n
}
