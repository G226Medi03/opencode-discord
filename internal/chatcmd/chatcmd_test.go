package chatcmd

import "testing"

func testRegistry() *Registry {
	r := New()
	r.Add(&Command{Name: "help", Description: "usage"})
	r.Add(&Command{Name: "status"})
	r.Add(&Command{Name: "cwd", ArgName: "path"})
	r.Add(&Command{Name: "cmd", ArgName: "command"})
	return r
}

func TestParse(t *testing.T) {
	r := testRegistry()
	cases := []struct {
		in   string
		name string
		rest string
		ok   bool
	}{
		{"!help", "help", "", true},
		{"/help", "help", "", true},
		{"!cwd", "cwd", "", true},
		{"!cwd D:\\Coding\\X", "cwd", "D:\\Coding\\X", true},
		{"/cwd D:\\Coding\\My Proj", "cwd", "D:\\Coding\\My Proj", true},
		{"!cmd dir /s", "cmd", "dir /s", true},
		{"/cmd", "cmd", "", true},
		{"!nope", "", "", false},
		{"/notacommand foo", "", "", false},
		{"hello world", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		name, rest, ok := r.Parse(c.in)
		if ok != c.ok || name != c.name || rest != c.rest {
			t.Errorf("Parse(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, name, rest, ok, c.name, c.rest, c.ok)
		}
	}
}

func TestTextArgs(t *testing.T) {
	r := testRegistry()
	if got := r.TextArgs("cwd", "D:\\a b"); got["path"] != "D:\\a b" {
		t.Errorf("cwd args = %v", got)
	}
	if got := r.TextArgs("cmd", "dir /s"); got["command"] != "dir /s" {
		t.Errorf("cmd args = %v", got)
	}
	if got := r.TextArgs("help", "x"); len(got) != 0 {
		t.Errorf("help args should be empty: %v", got)
	}
}

func TestSpecs(t *testing.T) {
	r := testRegistry()
	specs := r.Specs()
	if len(specs) != len(r.All()) {
		t.Fatalf("specs=%d all=%d", len(specs), len(r.All()))
	}
	if specs[0].Name != "help" {
		t.Fatalf("order not preserved: %q", specs[0].Name)
	}
}
