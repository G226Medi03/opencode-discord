package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
		`insert into project values('p2', 'D:/Coding/other', 'other', 1800000000000)`,
		`insert into session_v2 values('s1', 'p1', 'D:/Coding/demo', '테스트 세션', 'build',
			'{"id":"model-x","providerID":"prov"}', 0.0123, 100, 20, 1700000000000, 1700000005000)`,
		`insert into session_v2 values('s2', 'p1', 'D:/Coding/demo', '두 번째 세션', 'plan',
			'{"id":"model-y","providerID":"prov"}', 0.5, 1000, 200, 1700000000000, 1700000100000)`,
		`insert into session_v2 values('s3', 'p2', 'D:/Coding/other', '다른 프로젝트', 'build',
			'{"id":"model-x","providerID":"prov"}', 0.01, 10, 2, 1800000000000, 1800000005000)`,
		`insert into session_message values('m1', 's1', 'user', 1, 1700000001000,
			'{"time":{"created":1700000001000},"text":"안녕"}')`,
		`insert into session_message values('m2', 's1', 'assistant', 2, 1700000002000,
			'{"time":{"created":1700000002000},"agent":"build","model":{"id":"model-x","providerID":"prov"},"content":[{"type":"text","text":"답변"},{"type":"reasoning","text":"생각"},{"type":"tool","name":"shell","state":{"status":"completed","input":{"command":"dir"},"output":"ok"}}]}')`,
		`insert into session_message values('m3', 's1', 'assistant', 3, 1700000003000,
			'{"time":{"created":1700000003000},"agent":"build","content":[{"type":"text","text":"두번째 답변"}]}')`,
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return path
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openReadOnly(makeTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestQueries(t *testing.T) {
	db := openTestDB(t)

	ps, err := listProjects(db, projectFilter{Sort: "active"})
	if err != nil || len(ps) != 2 {
		t.Fatalf("projects: %#v %v", ps, err)
	}
	if ps[0].ID != "p2" {
		t.Fatalf("expected p2 first by activity: %#v", ps)
	}
	if ps[1].Sessions != 2 {
		t.Fatalf("p1 session count: %#v", ps[1])
	}

	ss, err := listSessions(db, "p1", sessionFilter{})
	if err != nil || len(ss) != 2 {
		t.Fatalf("sessions: %#v %v", ss, err)
	}
	if got := modelLabel(ss[0].Model); got != "prov/model-y" {
		t.Fatalf("model label: %q", got)
	}

	msgs, err := listMessages(db, "s1")
	if err != nil || len(msgs) != 3 {
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

func TestProjectFilters(t *testing.T) {
	db := openTestDB(t)
	ps, err := listProjects(db, projectFilter{Q: "demo"})
	if err != nil || len(ps) != 1 || ps[0].ID != "p1" {
		t.Fatalf("q=demo: %#v %v", ps, err)
	}
	ps, err = listProjects(db, projectFilter{Sort: "sessions"})
	if err != nil || len(ps) != 2 || ps[0].ID != "p1" {
		t.Fatalf("sort=sessions: %#v %v", ps, err)
	}
	ps, err = listProjects(db, projectFilter{From: 1750000000000})
	if err != nil || len(ps) != 1 || ps[0].ID != "p2" {
		t.Fatalf("from: %#v %v", ps, err)
	}
}

func TestSessionFilters(t *testing.T) {
	db := openTestDB(t)
	cases := []struct {
		name string
		f    sessionFilter
		want []string
	}{
		{"agent", sessionFilter{Agent: "plan"}, []string{"s2"}},
		{"model", sessionFilter{Model: `{"id":"model-x","providerID":"prov"}`}, []string{"s1"}},
		{"title", sessionFilter{Q: "두 번째"}, []string{"s2"}},
		{"from", sessionFilter{From: 1700000050000}, []string{"s2"}},
		{"to", sessionFilter{To: 1700000010000}, []string{"s1"}},
		{"minCost", sessionFilter{MinCost: 0.1}, []string{"s2"}},
		{"minTokens", sessionFilter{MinTokens: 500}, []string{"s2"}},
		{"sortCost", sessionFilter{Sort: "cost"}, []string{"s2", "s1"}},
		{"sortTokens", sessionFilter{Sort: "tokens"}, []string{"s2", "s1"}},
	}
	for _, c := range cases {
		ss, err := listSessions(db, "p1", c.f)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		var got []string
		for _, s := range ss {
			got = append(got, s.ID)
		}
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v want %v", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: got %v want %v", c.name, got, c.want)
			}
		}
	}
}

func TestMessageFilters(t *testing.T) {
	db := openTestDB(t)
	msgs, err := listMessages(db, "s1")
	if err != nil {
		t.Fatal(err)
	}
	var all []messageView
	for _, m := range msgs {
		all = append(all, parseMessage(m))
	}
	if got := distinctTools(all); len(got) != 1 || got[0] != "shell" {
		t.Fatalf("tools: %#v", got)
	}

	// default: tools on, reasoning off, all types
	got := filterMessages(all, messageFilter{ShowTools: true})
	if len(got) != 3 {
		t.Fatalf("default: %d messages", len(got))
	}
	if len(got[1].Parts) != 2 || got[1].Parts[1].Kind != "tool" {
		t.Fatalf("reasoning should be hidden: %#v", got[1].Parts)
	}

	// only user messages
	got = filterMessages(all, messageFilter{TypesSet: true, Types: map[string]bool{"user": true}, ShowTools: true})
	if len(got) != 1 || got[0].Type != "user" {
		t.Fatalf("type filter: %#v", got)
	}

	// no types selected
	got = filterMessages(all, messageFilter{TypesSet: true, Types: map[string]bool{}, ShowTools: true})
	if len(got) != 0 {
		t.Fatalf("empty types should return nothing: %d", len(got))
	}

	// tool filter keeps only messages containing that tool
	got = filterMessages(all, messageFilter{Tool: "shell", ShowTools: false})
	if len(got) != 1 || got[0].Type != "assistant" || len(got[0].Parts) != 2 {
		t.Fatalf("tool filter: %#v", got)
	}
	if got[0].Parts[0].Kind != "text" || got[0].Parts[1].Kind != "tool" {
		t.Fatalf("tool filter parts: %#v", got[0].Parts)
	}

	// text search across reasoning only when reasoning is shown
	if got := filterMessages(all, messageFilter{Q: "생각", ShowTools: true}); len(got) != 0 {
		t.Fatalf("hidden reasoning should not match search: %d", len(got))
	}
	if got := filterMessages(all, messageFilter{Q: "생각", ShowReasoning: true, ShowTools: true}); len(got) != 1 {
		t.Fatalf("reasoning search: %d", len(got))
	}

	// date range
	got = filterMessages(all, messageFilter{From: 1700000002500, ShowTools: true})
	if len(got) != 1 || got[0].Type != "assistant" {
		t.Fatalf("date filter: %#v", got)
	}
}

func TestReadOnly(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`insert into project(id) values('x')`); err == nil {
		t.Fatal("write should be rejected on a read-only database")
	}
}

func TestDateHelpers(t *testing.T) {
	if got := dateStart(""); got != 0 {
		t.Fatalf("empty start: %d", got)
	}
	if got := dateStart("not-a-date"); got != 0 {
		t.Fatalf("invalid start: %d", got)
	}
	start := dateStart("2026-10-26")
	end := dateEnd("2026-10-26")
	if start == 0 || end <= start {
		t.Fatalf("range: %d..%d", start, end)
	}
	if parseInt64("bad") != 0 || parseFloat("bad") != 0 {
		t.Fatal("invalid numbers should be zero")
	}
}

// ---- snapshots / git -----------------------------------------------------

func TestSnapshotFromData(t *testing.T) {
	data := []byte(`{"snapshot":{"start":"0123456789abcdef","end":"fedcba9876543210","files":["a.go","b.go"]}}`)
	s := snapshotFromData(data)
	if s.Start != "0123456789abcdef" || s.End != "fedcba9876543210" {
		t.Fatalf("snapshot: %#v", s)
	}
	if s.StartShort != "01234567" || s.EndShort != "fedcba98" {
		t.Fatalf("shorts: %#v", s)
	}
	if len(s.Files) != 2 || s.Files[0] != "a.go" {
		t.Fatalf("files: %#v", s.Files)
	}
	if empty := snapshotFromData([]byte(`{}`)); empty.Start != "" {
		t.Fatalf("expected empty snapshot: %#v", empty)
	}
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGitHelpers(t *testing.T) {
	dir := t.TempDir()
	gitTest(t, dir, "init", "-q")
	gitTest(t, dir, "config", "user.email", "test@example.com")
	gitTest(t, dir, "config", "user.name", "test")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", ".")
	gitTest(t, dir, "commit", "-qm", "one")
	tree1 := gitTest(t, dir, "rev-parse", "HEAD^{tree}")

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", ".")
	gitTest(t, dir, "commit", "-qm", "two")
	tree2 := gitTest(t, dir, "rev-parse", "HEAD^{tree}")

	shadow := filepath.Join(dir, ".git")
	changes, err := changedFiles(shadow, tree1, tree2)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes: %#v", changes)
	}
	a, ok := findChange(changes, "a.txt")
	if !ok || a.Status != "M" || a.Additions != 1 || a.Deletions != 1 {
		t.Fatalf("a.txt: %#v ok=%v", a, ok)
	}
	b, ok := findChange(changes, "b.txt")
	if !ok || b.Status != "A" || b.Additions != 1 {
		t.Fatalf("b.txt: %#v ok=%v", b, ok)
	}
	if _, ok := findChange(changes, "missing.txt"); ok {
		t.Fatal("missing file should not be found")
	}

	content, _, binary, err := fileAt(shadow, tree2, "a.txt")
	if err != nil || binary || content != "two\n" {
		t.Fatalf("fileAt after: %q binary=%v err=%v", content, binary, err)
	}
	if _, _, _, err := fileAt(shadow, tree1, "b.txt"); err == nil {
		t.Fatal("b.txt should not exist at tree1")
	}

	diff, truncated, err := fileDiff(shadow, tree1, tree2, "a.txt")
	if err != nil || truncated {
		t.Fatalf("diff err=%v truncated=%v", err, truncated)
	}
	if !strings.Contains(diff, "-one") || !strings.Contains(diff, "+two") {
		t.Fatalf("unexpected diff:\n%s", diff)
	}
}

func TestFindSnapshotRepoIn(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "abc123")
	if err := os.MkdirAll(filepath.Join(repo, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "HEAD"), []byte("ref: refs/heads/master\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := findSnapshotRepoIn(base)
	if err != nil || got != repo {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := findSnapshotRepoIn(t.TempDir()); err == nil {
		t.Fatal("expected error for empty base")
	}
}

func TestColorizeDiff(t *testing.T) {
	got := string(colorizeDiff("@@ -1 +1 @@\n-old <x>\n+new <y>\n context"))
	for _, want := range []string{`class="hunk"`, `class="del"`, `class="add"`, "&lt;x&gt;"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "<x>") {
		t.Fatalf("diff was not escaped:\n%s", got)
	}
}

func TestTailSlice(t *testing.T) {
	var in []messageView
	for i := 0; i < 1500; i++ {
		in = append(in, messageView{ID: strconv.Itoa(i)})
	}
	got := tailSlice(in, 1000)
	if len(got) != 1000 || got[0].ID != "500" || got[999].ID != "1499" {
		t.Fatalf("tail: len=%d first=%q last=%q", len(got), got[0].ID, got[len(got)-1].ID)
	}
	if all := tailSlice(in, 5000); len(all) != 1500 {
		t.Fatalf("large tail: %d", len(all))
	}
	if def := tailSlice(in, 0); len(def) != 1000 {
		t.Fatalf("default tail: %d", len(def))
	}
}
