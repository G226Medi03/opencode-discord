// Package main implements oc-viewer: a read-only local web viewer for the
// OpenCode session database.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

type projectFilter struct {
	Q        string
	From, To int64
	Sort     string
}

type sessionFilter struct {
	Q, Agent, Model string
	From, To        int64
	MinCost         float64
	MinTokens       int64
	Sort            string
}

type modelOption struct {
	Value string
	Label string
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

const projectCols = `
		coalesce(p.id, ''), coalesce(p.worktree, ''), coalesce(p.name, ''),
		coalesce(p.time_active, 0),
		(select count(*) from session_v2 s where s.project_id = p.id)`

func listProjects(db *sql.DB, f projectFilter) ([]project, error) {
	var where []string
	var args []any
	if q := strings.TrimSpace(f.Q); q != "" {
		where = append(where, "(coalesce(p.worktree,'') like ? or coalesce(p.name,'') like ?)")
		like := "%" + q + "%"
		args = append(args, like, like)
	}
	if f.From > 0 {
		where = append(where, "coalesce(p.time_active,0) >= ?")
		args = append(args, f.From)
	}
	if f.To > 0 {
		where = append(where, "coalesce(p.time_active,0) <= ?")
		args = append(args, f.To)
	}
	order := "coalesce(p.time_active,0) desc"
	if f.Sort == "sessions" {
		order = "session_count desc, coalesce(p.time_active,0) desc"
	}
	q := "select " + projectCols + " as session_count from project p"
	if len(where) > 0 {
		q += " where " + strings.Join(where, " and ")
	}
	q += " order by " + order

	rows, err := db.Query(q, args...)
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
	q := "select " + projectCols + " from project p where p.id = ?"
	var p project
	err := db.QueryRow(q, id).Scan(&p.ID, &p.Worktree, &p.Name, &p.TimeActive, &p.Sessions)
	return p, err
}

const sessionCols = `
		id, coalesce(project_id, ''), coalesce(directory, ''), coalesce(title, ''),
		coalesce(agent, ''), coalesce(model, ''), coalesce(cost, 0),
		coalesce(tokens_input, 0), coalesce(tokens_output, 0),
		coalesce(time_created, 0), coalesce(time_updated, 0)`

func listSessions(db *sql.DB, projectID string, f sessionFilter) ([]session, error) {
	where := []string{"project_id = ?"}
	args := []any{projectID}
	if q := strings.TrimSpace(f.Q); q != "" {
		where = append(where, "coalesce(title,'') like ?")
		args = append(args, "%"+q+"%")
	}
	if f.Agent != "" {
		where = append(where, "agent = ?")
		args = append(args, f.Agent)
	}
	if f.Model != "" {
		where = append(where, "model = ?")
		args = append(args, f.Model)
	}
	if f.From > 0 {
		where = append(where, "coalesce(time_updated,0) >= ?")
		args = append(args, f.From)
	}
	if f.To > 0 {
		where = append(where, "coalesce(time_updated,0) <= ?")
		args = append(args, f.To)
	}
	if f.MinCost > 0 {
		where = append(where, "coalesce(cost,0) >= ?")
		args = append(args, f.MinCost)
	}
	if f.MinTokens > 0 {
		where = append(where, "(coalesce(tokens_input,0)+coalesce(tokens_output,0)) >= ?")
		args = append(args, f.MinTokens)
	}
	order := "coalesce(time_updated,0) desc"
	switch f.Sort {
	case "created":
		order = "coalesce(time_created,0) desc"
	case "cost":
		order = "coalesce(cost,0) desc"
	case "tokens":
		order = "(coalesce(tokens_input,0)+coalesce(tokens_output,0)) desc"
	}
	q := "select " + sessionCols + " from session_v2 where " + strings.Join(where, " and ") +
		" order by " + order + " limit 500"
	return scanSessions(db, q, args...)
}

func getSession(db *sql.DB, id string) (session, error) {
	q := "select " + sessionCols + " from session_v2 where id = ?"
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

func distinctAgents(db *sql.DB, projectID string) ([]string, error) {
	rows, err := db.Query(`select distinct agent from session_v2
		where project_id = ? and coalesce(agent,'') <> '' order by agent`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func distinctModels(db *sql.DB, projectID string) ([]modelOption, error) {
	rows, err := db.Query(`select distinct model from session_v2
		where project_id = ? and coalesce(model,'') <> '' order by model`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []modelOption
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		label := modelLabel(raw)
		if seen[label] {
			continue
		}
		seen[label] = true
		out = append(out, modelOption{Value: raw, Label: label})
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
