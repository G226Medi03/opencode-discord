package main

import (
	"database/sql"
	"errors"
	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
		Session: toSessionView(sess), Messages: filtered, TF: tf,
	}
	render(w, sessionTmpl, data)
}

func render(w http.ResponseWriter, t *template.Template, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("oc-viewer: render: %v", err)
	}
}

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
