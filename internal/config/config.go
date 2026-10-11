// Package config loads .env, bot-config.json, channel-policy.json and
// channel-cwd.json into a single Config value. The .env key conventions match
// load-env.ps1 so the run scripts and the binary agree.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// BotConfig is the runtime behaviour policy (bot-config.json + OCD_* env).
type BotConfig struct {
	Permissions          string   `json:"permissions"`
	PermissionTimeoutSec int      `json:"permissionTimeoutSec"`
	RememberAllowAlways  bool     `json:"rememberAllowAlways"`
	Threads              bool     `json:"threads"`
	Stream               bool     `json:"stream"`
	StreamEditMS         int      `json:"streamEditMs"`
	MaxChunk             int      `json:"maxChunk"`
	ShowTools            bool     `json:"showTools"`
	IdleShutdownMin      int      `json:"idleShutdownMin"`
	MaxAgents            int      `json:"maxAgents"`
	AllowPatterns        []string `json:"allowPatterns"`
	DenyPatterns         []string `json:"denyPatterns"`
	OpenCodeArgs         []string `json:"opencodeArgs"`
	RequireMention       *bool    `json:"requireMention"`
	CmdTimeoutSec        int      `json:"cmdTimeoutSec"`
}

// Config is the fully resolved application configuration.
type Config struct {
	Root string

	Token          string
	AppID          string
	PublicKey      string
	OwnerUserID    string
	MentionRoleIDs []string
	GuildID        string

	RespondWithoutMention bool
	AllowedChannelIDs     []string
	AllowedCategoryIDs    []string

	CwdRoot      string
	CwdOverrides map[string]string

	OpenCodeBin string

	Bot BotConfig

	LogDir         string
	StatePath      string
	EnvKeys        []string
	DeployCommands bool
}

func defaults() BotConfig {
	return BotConfig{
		Permissions:          "ask",
		PermissionTimeoutSec: 180,
		RememberAllowAlways:  true,
		Threads:              false,
		Stream:               true,
		StreamEditMS:         1300,
		MaxChunk:             1900,
		ShowTools:            true,
		IdleShutdownMin:      30,
		MaxAgents:            8,
		CmdTimeoutSec:        60,
	}
}

// ParseDotEnv parses a .env file body.
func ParseDotEnv(text string) map[string]string {
	out := map[string]string{}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(line[len("export "):])
		} else if strings.HasPrefix(line, "set ") {
			line = strings.TrimSpace(line[len("set "):])
		}
		eq := strings.Index(line, "=")
		if eq < 1 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if !isEnvKey(key) {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		quoted := strings.HasPrefix(val, `"`) || strings.HasPrefix(val, "'")
		if !quoted {
			if h := strings.Index(val, "#"); h >= 0 {
				val = strings.TrimSpace(val[:h])
			}
		}
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') && val[len(val)-1] == val[0] {
			val = val[1 : len(val)-1]
		}
		out[key] = val
	}
	return out
}

func isEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// LoadDotEnv applies values from file into the process environment (overriding).
func LoadDotEnv(file string) []string {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	vars := ParseDotEnv(string(b))
	applied := make([]string, 0, len(vars))
	for k, v := range vars {
		_ = os.Setenv(k, v)
		applied = append(applied, k)
	}
	return applied
}

func readJSON(path string, dst any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	b = []byte(strings.TrimPrefix(string(b), "\uFEFF"))
	return json.Unmarshal(b, dst) == nil
}

func boolEnv(v string) (bool, bool) {
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	default:
		return false, true
	}
}

// findRoot resolves the repository root.
func findRoot(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("OCD_ROOT"); v != "" {
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".env")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	wd, _ := os.Getwd()
	return wd
}

// Load builds the configuration for the given (optional) explicit root.
func Load(explicitRoot string) *Config {
	root := findRoot(explicitRoot)

	envPath := filepath.Join(root, ".env")
	envKeys := LoadDotEnv(envPath)

	botRaw := defaults()
	var fileCfg botFile
	botCfgPath := filepath.Join(root, "bot-config.json")
	readJSON(botCfgPath, &fileCfg)
	fileCfg.merge(&botRaw)
	applyBotEnv(&botRaw)

	var policy struct {
		GuildID               string   `json:"guildId"`
		RespondWithoutMention *bool    `json:"respondWithoutMention"`
		Channels              []string `json:"channels"`
		Resolved              struct {
			ChannelIDs  []string          `json:"channelIds"`
			CategoryIDs map[string]string `json:"categoryIds"`
		} `json:"resolved"`
	}
	_ = readJSON(filepath.Join(root, "channel-policy.json"), &policy)

	allowedCategories := make([]string, 0, len(policy.Resolved.CategoryIDs))
	for _, id := range policy.Resolved.CategoryIDs {
		if strings.TrimSpace(id) != "" {
			allowedCategories = append(allowedCategories, id)
		}
	}

	overrides := map[string]string{}
	var cwdRaw map[string]json.RawMessage
	if readJSON(filepath.Join(root, "channel-cwd.json"), &cwdRaw) {
		for k, v := range cwdRaw {
			if strings.HasPrefix(k, "_") {
				continue
			}
			var s string
			if json.Unmarshal(v, &s) == nil && strings.TrimSpace(s) != "" {
				overrides[strings.ToLower(k)] = s
			}
		}
	}

	cwdRoot := firstNonEmpty(os.Getenv("OCD_CWD_ROOT"), `D:\Coding`)

	respondWithoutMention := true
	if policy.RespondWithoutMention != nil {
		respondWithoutMention = *policy.RespondWithoutMention
	}

	guildID := firstNonEmpty(os.Getenv("DISCORD_GUILD_ID"), policy.GuildID)

	deploy, _ := boolEnv(os.Getenv("OCD_DEPLOY_COMMANDS"))

	return &Config{
		Root:                  root,
		Token:                 os.Getenv("DISCORD_BOT_TOKEN"),
		AppID:                 os.Getenv("DISCORD_APP_ID"),
		PublicKey:             os.Getenv("DISCORD_PUBLIC_KEY"),
		OwnerUserID:           os.Getenv("DISCORD_OWNER_USER_ID"),
		MentionRoleIDs:        splitCSV(os.Getenv("DISCORD_MENTION_ROLE_IDS")),
		GuildID:               guildID,
		RespondWithoutMention: respondWithoutMention,
		AllowedChannelIDs:     policy.Resolved.ChannelIDs,
		AllowedCategoryIDs:    allowedCategories,
		CwdRoot:               cwdRoot,
		CwdOverrides:          overrides,
		OpenCodeBin:           os.Getenv("OPENCODE_BIN"),
		Bot:                   botRaw,
		LogDir:                filepath.Join(root, "logs"),
		StatePath:             filepath.Join(root, "state", "bindings.json"),
		EnvKeys:               envKeys,
		DeployCommands:        deploy,
	}
}

// botFile mirrors BotConfig with pointers so we can tell which keys the file
// actually set (vs. Go zero values).
type botFile struct {
	Permissions          *string   `json:"permissions"`
	PermissionTimeoutSec *int      `json:"permissionTimeoutSec"`
	RememberAllowAlways  *bool     `json:"rememberAllowAlways"`
	Threads              *bool     `json:"threads"`
	Stream               *bool     `json:"stream"`
	StreamEditMS         *int      `json:"streamEditMs"`
	MaxChunk             *int      `json:"maxChunk"`
	ShowTools            *bool     `json:"showTools"`
	IdleShutdownMin      *int      `json:"idleShutdownMin"`
	MaxAgents            *int      `json:"maxAgents"`
	AllowPatterns        *[]string `json:"allowPatterns"`
	DenyPatterns         *[]string `json:"denyPatterns"`
	OpenCodeArgs         *[]string `json:"opencodeArgs"`
	RequireMention       *bool     `json:"requireMention"`
	CmdTimeoutSec        *int      `json:"cmdTimeoutSec"`
}

func (f botFile) merge(b *BotConfig) {
	if f.Permissions != nil {
		b.Permissions = *f.Permissions
	}
	if f.PermissionTimeoutSec != nil {
		b.PermissionTimeoutSec = *f.PermissionTimeoutSec
	}
	if f.RememberAllowAlways != nil {
		b.RememberAllowAlways = *f.RememberAllowAlways
	}
	if f.Threads != nil {
		b.Threads = *f.Threads
	}
	if f.Stream != nil {
		b.Stream = *f.Stream
	}
	if f.StreamEditMS != nil {
		b.StreamEditMS = *f.StreamEditMS
	}
	if f.MaxChunk != nil {
		b.MaxChunk = *f.MaxChunk
	}
	if f.ShowTools != nil {
		b.ShowTools = *f.ShowTools
	}
	if f.IdleShutdownMin != nil {
		b.IdleShutdownMin = *f.IdleShutdownMin
	}
	if f.MaxAgents != nil {
		b.MaxAgents = *f.MaxAgents
	}
	if f.AllowPatterns != nil {
		b.AllowPatterns = *f.AllowPatterns
	}
	if f.DenyPatterns != nil {
		b.DenyPatterns = *f.DenyPatterns
	}
	if f.OpenCodeArgs != nil {
		b.OpenCodeArgs = *f.OpenCodeArgs
	}
	if f.RequireMention != nil {
		b.RequireMention = f.RequireMention
	}
	if f.CmdTimeoutSec != nil {
		b.CmdTimeoutSec = *f.CmdTimeoutSec
	}
}

func applyBotEnv(b *BotConfig) {
	if v := os.Getenv("OCD_PERMISSIONS"); v != "" {
		b.Permissions = v
	}
	if v := os.Getenv("OCD_PERMISSION_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			b.PermissionTimeoutSec = n
		}
	}
	if v, ok := boolEnv(os.Getenv("OCD_THREADS")); ok {
		b.Threads = v
	}
	if v, ok := boolEnv(os.Getenv("OCD_STREAM")); ok {
		b.Stream = v
	}
	if v := os.Getenv("OCD_STREAM_EDIT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			b.StreamEditMS = n
		}
	}
	if v := os.Getenv("OCD_MAX_CHUNK"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			b.MaxChunk = n
		}
	}
	if v, ok := boolEnv(os.Getenv("OCD_SHOW_TOOLS")); ok {
		b.ShowTools = v
	}
	if v := os.Getenv("OCD_IDLE_SHUTDOWN_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			b.IdleShutdownMin = n
		}
	}
	if v := os.Getenv("OCD_MAX_AGENTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			b.MaxAgents = n
		}
	}
	if v, ok := boolEnv(os.Getenv("OCD_REQUIRE_MENTION")); ok {
		b.RequireMention = &v
	}
	if v := os.Getenv("OCD_CMD_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			b.CmdTimeoutSec = n
		}
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
