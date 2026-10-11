package config

import "testing"

func TestParseDotEnv(t *testing.T) {
	body := "" +
		"# comment\n" +
		"DISCORD_BOT_TOKEN=abc.def.ghi\n" +
		"export DISCORD_APP_ID=123\n" +
		"set QUOTED=\"hello world\"\n" +
		"INLINE=value  # trailing comment\n" +
		"SINGLE='x y'\n" +
		"EMPTY=\n" +
		"BAD LINE\n"
	got := ParseDotEnv(body)

	cases := map[string]string{
		"DISCORD_BOT_TOKEN": "abc.def.ghi",
		"DISCORD_APP_ID":    "123",
		"QUOTED":            "hello world",
		"INLINE":            "value",
		"SINGLE":            "x y",
		"EMPTY":             "",
	}
	for k, want := range cases {
		if got[k] != want {
			t.Errorf("%s = %q, want %q", k, got[k], want)
		}
	}
	if _, ok := got["BAD LINE"]; ok {
		t.Errorf("malformed line should be ignored")
	}
	if _, ok := got["# comment"]; ok {
		t.Errorf("comment should be ignored")
	}
}

func TestDefaultsShape(t *testing.T) {
	b := defaults()
	if b.Permissions != "ask" || b.MaxChunk != 1900 || b.StreamEditMS != 1300 {
		t.Fatalf("unexpected defaults: %+v", b)
	}
}
