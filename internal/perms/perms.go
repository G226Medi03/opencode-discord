// Package perms implements the tool-permission policy for ACP
// session/request_permission.
package perms

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Option mirrors an ACP permission option.
type Option struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// Policy is the resolved permission configuration.
type Policy struct {
	Mode  string // auto | ask | deny
	Allow []string
	Deny  []string
}

// Decision values.
const (
	DecideAllow       = "allow"
	DecideAllowAlways = "allowAlways"
	DecideDeny        = "deny"
	DecideAsk         = "ask"
)

func compile(pattern string) *regexp.Regexp {
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil
	}
	return re
}

// Evaluate decides what to do for a tool call. deny patterns win, then allow
// patterns, then the mode.
func Evaluate(p Policy, title, kind string, rawInput json.RawMessage) string {
	text := title + " " + kind + " " + string(rawInput)
	for _, pat := range p.Deny {
		if re := compile(pat); re != nil && re.MatchString(text) {
			return DecideDeny
		}
	}
	for _, pat := range p.Allow {
		if re := compile(pat); re != nil && re.MatchString(text) {
			return DecideAllow
		}
	}
	switch p.Mode {
	case "auto":
		return DecideAllow
	case "deny":
		return DecideDeny
	default:
		return DecideAsk
	}
}

func findByKind(options []Option, kind string) (string, bool) {
	for _, o := range options {
		if o.Kind == kind {
			return o.OptionID, true
		}
	}
	return "", false
}

func findByRegexp(options []Option, re *regexp.Regexp) (string, bool) {
	for _, o := range options {
		if re.MatchString(o.OptionID) {
			return o.OptionID, true
		}
	}
	return "", false
}

// PickOptionID chooses an option id for the wanted decision.
func PickOptionID(options []Option, wanted string) (string, bool) {
	allowOnce, _ := findByKind(options, "allow_once")
	if allowOnce == "" {
		allowOnce, _ = findByRegexp(options, regexp.MustCompile(`(?i)allow_once|allow$`))
	}
	if allowOnce == "" {
		allowOnce, _ = findByRegexp(options, regexp.MustCompile(`(?i)allow`))
	}
	allowAlways, _ := findByKind(options, "allow_always")
	if allowAlways == "" {
		allowAlways, _ = findByRegexp(options, regexp.MustCompile(`(?i)allow_always|always`))
	}
	reject, _ := findByKind(options, "reject_once")
	if reject == "" {
		reject, _ = findByKind(options, "reject_always")
	}
	if reject == "" {
		reject, _ = findByRegexp(options, regexp.MustCompile(`(?i)reject|deny`))
	}

	switch wanted {
	case DecideAllow:
		if allowOnce != "" {
			return allowOnce, true
		}
		return allowAlways, allowAlways != ""
	case DecideAllowAlways:
		if allowAlways != "" {
			return allowAlways, true
		}
		return allowOnce, allowOnce != ""
	case DecideDeny:
		return reject, reject != ""
	}
	return "", false
}

// Selected builds a "selected" outcome.
func Selected(optionID string) map[string]any {
	return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": optionID}}
}

// Cancelled builds a "cancelled" outcome.
func Cancelled() map[string]any {
	return map[string]any{"outcome": map[string]any{"outcome": "cancelled"}}
}

// Summary is a short human-readable description of a tool call.
type Summary struct {
	Title  string
	Detail string
	Kind   string
}

// SummarizeToolCall builds a Summary from a tool call's title/kind/rawInput.
func SummarizeToolCall(title, kind string, rawInput json.RawMessage) Summary {
	if strings.TrimSpace(title) == "" {
		title = "tool"
	}
	detail := ""
	var obj map[string]any
	if len(rawInput) > 0 && json.Unmarshal(rawInput, &obj) == nil {
		if cmds, ok := obj["commands"].([]any); ok {
			parts := make([]string, 0, len(cmds))
			for _, c := range cmds {
				parts = append(parts, toStr(c))
			}
			detail = strings.Join(parts, "\n")
		} else if p, ok := obj["path"].(string); ok {
			detail = p
		} else if p, ok := obj["file_path"].(string); ok {
			detail = p
		} else {
			b, _ := json.Marshal(obj)
			detail = string(b)
		}
	} else if len(rawInput) > 0 {
		detail = string(rawInput)
	}
	if len(detail) > 700 {
		detail = detail[:700] + " …"
	}
	if len(title) > 200 {
		title = title[:200]
	}
	return Summary{Title: title, Detail: detail, Kind: kind}
}

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}
