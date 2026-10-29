package bridge

import "testing"

func TestUnknownBangCommand(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"!foo bar", "!foo", true},
		{"  !FOO  ", "!foo", true},
		{"!", "!", true},
		{"!   ", "!", true},
		{"/foo", "", false},
		{"hello", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := unknownBangCommand(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("unknownBangCommand(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
