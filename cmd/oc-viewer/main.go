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
	ps, err := listProjects(s.db)
	if err != nil {
		http.Error(w, "projects: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := pageData{Title: "프로젝트", DBPath: s.dbPath}
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
	ss, err := listSessions(s.db, id)
	if err != nil {
		http.Error(w, "sessions: "+err.Error(), http.StatusInternalServerError)
		return
	}
	data := pageData{Title: p.Worktree, DBPath: s.dbPath, Project: toProjectView(p)}
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
	data := pageData{Title: sess.Title, DBPath: s.dbPath, Session: toSessionView(sess)}
	for _, m := range msgs {
		data.Messages = append(data.Messages, parseMessage(m))
	}
	render(w, sessionTmpl, data)
}

func render(w http.ResponseWriter, t *template.Template, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, data); err != nil {
		log.Printf("oc-viewer: render: %v", err)
	}
}
