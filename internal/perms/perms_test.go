package perms

import (
	"encoding/json"
	"testing"
)

var openOptions = []Option{
	{OptionID: "once", Name: "Allow once", Kind: "allow_once"},
	{OptionID: "always", Name: "Always allow", Kind: "allow_always"},
	{OptionID: "reject", Name: "Reject", Kind: "reject_once"},
}

func TestEvaluateModes(t *testing.T) {
	raw := json.RawMessage(`{"command":"ls"}`)
	if got := Evaluate(Policy{Mode: "auto"}, "run_commands", "execute", raw); got != DecideAllow {
		t.Fatalf("auto: %s", got)
	}
	if got := Evaluate(Policy{Mode: "deny"}, "run_commands", "execute", raw); got != DecideDeny {
		t.Fatalf("deny: %s", got)
	}
	if got := Evaluate(Policy{Mode: "ask"}, "run_commands", "execute", raw); got != DecideAsk {
		t.Fatalf("ask: %s", got)
	}
}

func TestEvaluateDenyWins(t *testing.T) {
	raw := json.RawMessage(`{"command":"rm -rf /"}`)
	p := Policy{Mode: "auto", Allow: []string{"run_commands"}, Deny: []string{"rm\\s+-rf"}}
	if got := Evaluate(p, "run_commands", "execute", raw); got != DecideDeny {
		t.Fatalf("deny should win: %s", got)
	}
}

func TestEvaluateAllowPattern(t *testing.T) {
	raw := json.RawMessage(`{"path":"a.txt"}`)
	p := Policy{Mode: "ask", Allow: []string{"^read_file"}}
	if got := Evaluate(p, "read_file", "read", raw); got != DecideAllow {
		t.Fatalf("allow pattern: %s", got)
	}
}

func TestPickOptionID(t *testing.T) {
	if id, ok := PickOptionID(openOptions, DecideAllow); !ok || id != "once" {
		t.Fatalf("allow -> %q %v", id, ok)
	}
	if id, ok := PickOptionID(openOptions, DecideAllowAlways); !ok || id != "always" {
		t.Fatalf("always -> %q %v", id, ok)
	}
	if id, ok := PickOptionID(openOptions, DecideDeny); !ok || id != "reject" {
		t.Fatalf("deny -> %q %v", id, ok)
	}
}

func TestSummarize(t *testing.T) {
	raw := json.RawMessage(`{"commands":["echo hi","pwd"]}`)
	s := SummarizeToolCall("run_commands", "execute", raw)
	if s.Title != "run_commands" || s.Detail != "echo hi\npwd" {
		t.Fatalf("summary: %+v", s)
	}
}
