// Package discordaccess discovers guild channels the bot has explicitly been
// invited to. A channel counts as invited when the bot's member or one of its
// roles is granted ViewChannel by a channel or category permission overwrite.
// Public channels that are merely visible through @everyone are not included.
package discordaccess

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	discordAPI = "https://discord.com/api/v10"

	permViewChannel   uint64 = 1 << 10
	permAdministrator uint64 = 1 << 3
)

// Role is a Discord guild role.
type Role struct {
	ID          string `json:"id"`
	Permissions string `json:"permissions"`
}

// Overwrite is a channel or category permission overwrite.
type Overwrite struct {
	ID    string `json:"id"`
	Type  int    `json:"type"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

// Channel is the subset of a Discord channel needed for access checks.
type Channel struct {
	ID                   string      `json:"id"`
	Name                 string      `json:"name"`
	Type                 int         `json:"type"`
	ParentID             string      `json:"parent_id"`
	PermissionOverwrites []Overwrite `json:"permission_overwrites"`
}

type user struct {
	ID string `json:"id"`
}

type member struct {
	Roles []string `json:"roles"`
}

// InvitedChannels returns the guild channels where the bot has explicit
// ViewChannel access. It uses REST only, so it can run before the gateway
// connects. A failure leaves the caller's static policy untouched.
func InvitedChannels(token, guildID string) ([]string, error) {
	token = strings.TrimSpace(token)
	guildID = strings.TrimSpace(guildID)
	if token == "" || guildID == "" {
		return nil, fmt.Errorf("discord token/guild id is empty")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	var me user
	if err := getJSON(client, token, "/users/@me", &me); err != nil {
		return nil, err
	}
	if me.ID == "" {
		return nil, fmt.Errorf("discord /users/@me returned no id")
	}

	var mem member
	if err := getJSON(client, token, "/guilds/"+guildID+"/members/"+me.ID, &mem); err != nil {
		return nil, err
	}
	var roles []Role
	if err := getJSON(client, token, "/guilds/"+guildID+"/roles", &roles); err != nil {
		return nil, err
	}
	var channels []Channel
	if err := getJSON(client, token, "/guilds/"+guildID+"/channels", &channels); err != nil {
		return nil, err
	}

	return InvitedFrom(guildID, me.ID, mem.Roles, roles, channels), nil
}

func getJSON(client *http.Client, token, path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, discordAPI+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+token)
	req.Header.Set("User-Agent", "oc-discord-channel-access")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("discord GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discord GET %s: %s", path, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("discord GET %s: decode: %w", path, err)
	}
	return nil
}

// InvitedFrom is the pure part of InvitedChannels, kept separate for tests.
func InvitedFrom(guildID, botID string, memberRoles []string, roles []Role, channels []Channel) []string {
	rolePerm := make(map[string]uint64, len(roles))
	for _, r := range roles {
		rolePerm[r.ID] = parsePerm(r.Permissions)
	}
	roleSet := make(map[string]bool, len(memberRoles))
	for _, id := range memberRoles {
		roleSet[id] = true
	}

	base := rolePerm[guildID]
	for _, id := range memberRoles {
		base |= rolePerm[id]
	}
	admin := base&permAdministrator != 0

	categories := make(map[string]Channel)
	for _, c := range channels {
		if c.Type == 4 {
			categories[c.ID] = c
		}
	}

	var out []string
	for _, c := range channels {
		// Text, announcement and forum channels. Threads inherit access from
		// their parent and are handled by the caller's parent check.
		if c.Type != 0 && c.Type != 5 && c.Type != 15 {
			continue
		}

		perm := base
		explicit := false
		if cat, ok := categories[c.ParentID]; ok {
			perm, explicit = applyScope(perm, false, cat.PermissionOverwrites, guildID, botID, roleSet)
		}
		perm, more := applyScope(perm, explicit, c.PermissionOverwrites, guildID, botID, roleSet)
		explicit = explicit || more

		if (admin || perm&permViewChannel != 0) && explicit {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return out
}

// applyScope applies one category/channel overwrite scope. Role overwrites are
// combined (allow wins) before the bot member overwrite, matching Discord's
// resolution order. It also reports whether the bot member or one of its roles
// was explicitly granted ViewChannel in this scope.
func applyScope(perm uint64, explicit bool, ows []Overwrite, guildID, botID string, roles map[string]bool) (uint64, bool) {
	var roleAllow, roleDeny uint64
	var memberOW *Overwrite
	for i := range ows {
		o := ows[i]
		allow, deny := parsePerm(o.Allow), parsePerm(o.Deny)
		switch {
		case o.ID == guildID:
			perm = (perm &^ deny) | allow
		case o.ID == botID:
			memberOW = &ows[i]
			if allow&permViewChannel != 0 {
				explicit = true
			}
		case roles[o.ID]:
			roleAllow |= allow
			roleDeny |= deny
			if allow&permViewChannel != 0 {
				explicit = true
			}
		}
	}
	if roleAllow != 0 || roleDeny != 0 {
		perm = (perm &^ roleDeny) | roleAllow
	}
	if memberOW != nil {
		perm = (perm &^ parsePerm(memberOW.Deny)) | parsePerm(memberOW.Allow)
	}
	return perm, explicit
}

func parsePerm(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}
