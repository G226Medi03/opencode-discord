package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func makeTestDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`create table project(id text primary key, worktree text, name text, time_active integer)`,
		`create table session_v2(id text primary key, project_id text, directory text, title text,
			agent text, model text, cost real, tokens_input integer, tokens_output integer,
			time_created integer, time_updated integer)`,
		`create table session_message(id text primary key, session_id text, type text, seq integer,
			time_created integer, data text)`,
		`insert into project values('p1', 'D:/Coding/demo', 'demo', 1700000000000)`,
		`insert into session_v2 values('s1', 'p1', 'D:/Coding/demo', '테스트 세션', 'build',
			'{"id":"model-x","providerID":"prov"}', 0.0123, 100, 20, 1700000000000, 1700000005000)`,
		`insert into session_message values('m1', 's1', 'user', 1, 1700000001000,
			'{"time":{"created":1700000001000},"text":"안녕"}')`,
		`insert into session_message values('m2', 's1', 'assistant', 2, 1700000002000,
			'{"time":{"created":1700000002000},"agent":"build","model":{"id":"model-x","providerID":"prov"},"content":[{"type":"text","text":"답변"},{"type":"reasoning","text":"생각"},{"type":"tool","name":"shell","state":{"status":"completed","input":{"command":"dir"},"output":"ok"}}]}')`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return path
}

func TestQueries(t *testing.T) {
	path := makeTestDB(t)
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ps, err := listProjects(db)
	if err != nil || len(ps) != 1 {
		t.Fatalf("projects: %#v %v", ps, err)
	}
	if ps[0].Worktree != "D:/Coding/demo" || ps[0].Sessions != 1 {
		t.Fatalf("project: %#v", ps[0])
	}

	ss, err := listSessions(db, "p1")
	if err != nil || len(ss) != 1 {
		t.Fatalf("sessions: %#v %v", ss, err)
	}
	if got := modelLabel(ss[0].Model); got != "prov/model-x" {
		t.Fatalf("model label: %q", got)
	}

	msgs, err := listMessages(db, "s1")
	if err != nil || len(msgs) != 2 {
		t.Fatalf("messages: %#v %v", msgs, err)
	}
	um := parseMessage(msgs[0])
	if um.Type != "user" || um.Text != "안녕" {
		t.Fatalf("user: %#v", um)
	}
	am := parseMessage(msgs[1])
	if am.Agent != "build" || am.Model != "prov/model-x" {
		t.Fatalf("assistant meta: %#v", am)
	}
	if len(am.Parts) != 3 {
		t.Fatalf("parts: %#v", am.Parts)
	}
	if am.Parts[0].Kind != "text" || am.Parts[0].Text != "답변" {
		t.Fatalf("text part: %#v", am.Parts[0])
	}
	if am.Parts[2].Kind != "tool" || am.Parts[2].Name != "shell" || am.Parts[2].Status != "completed" {
		t.Fatalf("tool part: %#v", am.Parts[2])
	}
}

func TestReadOnly(t *testing.T) {
	path := makeTestDB(t)
	db, err := openReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`insert into project(id) values('x')`); err == nil {
		t.Fatal("write should be rejected on a read-only database")
	}
}
