// Package main implements oc-viewer: a read-only local web viewer for the
// OpenCode session database.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// project mirrors a row of the OpenCode project table.
type project struct {
	ID         string
	Worktree   string
	Name       string
	TimeActive int64
	Sessions   int
}

// session mirrors a row of session_v2.
type session struct {
	ID        string
	ProjectID string
	Directory string
	Title     string
	Agent     string
	Model     string
	Cost      float64
	TokensIn  int64
	TokensOut int64
	Created   int64
	Updated   int64
}

// message is one session_message row. Data holds the JSON payload.
type message struct {
	ID      string
	Type    string
	Seq     int64
	Created int64
	Data    []byte
}

// openReadOnly opens the SQLite database without allowing writes.
func openReadOnly(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("db not found: %w", err)
	}
	dsn := "file:" + filepath.ToSlash(abs) + "?mode=ro"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func listProjects(db *sql.DB) ([]project, error) {
	const q = `
		select coalesce(p.id, ''), coalesce(p.worktree, ''), coalesce(p.name, ''),
		       coalesce(p.time_active, 0),
		       (select count(*) from session_v2 s where s.project_id = p.id)
		from project p
		order by coalesce(p.time_active, 0) desc`
	rows, err := db.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []project
	for rows.Next() {
		var p project
		if err := rows.Scan(&p.ID, &p.Worktree, &p.Name, &p.TimeActive, &p.Sessions); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func getProject(db *sql.DB, id string) (project, error) {
	const q = `
		select coalesce(p.id, ''), coalesce(p.worktree, ''), coalesce(p.name, ''),
		       coalesce(p.time_active, 0),
		       (select count(*) from session_v2 s where s.project_id = p.id)
		from project p where p.id = ?`
	var p project
	err := db.QueryRow(q, id).Scan(&p.ID, &p.Worktree, &p.Name, &p.TimeActive, &p.Sessions)
	return p, err
}

func listSessions(db *sql.DB, projectID string) ([]session, error) {
	const q = `
		select id, coalesce(project_id, ''), coalesce(directory, ''), coalesce(title, ''),
		       coalesce(agent, ''), coalesce(model, ''), coalesce(cost, 0),
		       coalesce(tokens_input, 0), coalesce(tokens_output, 0),
		       coalesce(time_created, 0), coalesce(time_updated, 0)
		from session_v2
		where project_id = ?
		order by coalesce(time_updated, 0) desc
		limit 500`
	return scanSessions(db, q, projectID)
}

func getSession(db *sql.DB, id string) (session, error) {
	const q = `
		select id, coalesce(project_id, ''), coalesce(directory, ''), coalesce(title, ''),
		       coalesce(agent, ''), coalesce(model, ''), coalesce(cost, 0),
		       coalesce(tokens_input, 0), coalesce(tokens_output, 0),
		       coalesce(time_created, 0), coalesce(time_updated, 0)
		from session_v2 where id = ?`
	var s session
	err := db.QueryRow(q, id).Scan(&s.ID, &s.ProjectID, &s.Directory, &s.Title, &s.Agent,
		&s.Model, &s.Cost, &s.TokensIn, &s.TokensOut, &s.Created, &s.Updated)
	return s, err
}

func scanSessions(db *sql.DB, q string, args ...any) ([]session, error) {
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []session
	for rows.Next() {
		var s session
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.Directory, &s.Title, &s.Agent,
			&s.Model, &s.Cost, &s.TokensIn, &s.TokensOut, &s.Created, &s.Updated); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func listMessages(db *sql.DB, sessionID string) ([]message, error) {
	const q = `
		select id, coalesce(type, ''), coalesce(seq, 0), coalesce(time_created, 0),
		       coalesce(data, '{}')
		from session_message
		where session_id = ?
		order by coalesce(seq, 0) asc
		limit 2000`
	rows, err := db.Query(q, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []message
	for rows.Next() {
		var m message
		if err := rows.Scan(&m.ID, &m.Type, &m.Seq, &m.Created, &m.Data); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
