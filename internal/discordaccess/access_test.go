package discordaccess

import (
	"reflect"
	"testing"
)

const (
	everyoneRole = "guild"
	botID        = "bot"
	botRole      = "bot-role"
	otherRole    = "other-role"
)

func TestInvitedFromExplicitBotMember(t *testing.T) {
	channels := []Channel{
		{ID: "private", Type: 0, ParentID: "cat", PermissionOverwrites: []Overwrite{
			{ID: botID, Allow: "1024"},
		}},
	}
	roles := []Role{{ID: everyoneRole, Permissions: "0"}}
	got := InvitedFrom(everyoneRole, botID, nil, roles, channels)
	if want := []string{"private"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestInvitedFromPublicChannelIsNotInvited(t *testing.T) {
	channels := []Channel{
		{ID: "public", Type: 0},
	}
	roles := []Role{{ID: everyoneRole, Permissions: "1024"}}
	got := InvitedFrom(everyoneRole, botID, nil, roles, channels)
	if len(got) != 0 {
		t.Fatalf("public channel should be excluded, got %v", got)
	}
}

func TestInvitedFromRoleOverwrite(t *testing.T) {
	channels := []Channel{
		{ID: "role", Type: 0, PermissionOverwrites: []Overwrite{
			{ID: botRole, Allow: "1024"},
		}},
	}
	roles := []Role{
		{ID: everyoneRole, Permissions: "0"},
		{ID: botRole, Permissions: "0"},
	}
	got := InvitedFrom(everyoneRole, botID, []string{botRole}, roles, channels)
	if want := []string{"role"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestInvitedFromCategoryOverwrite(t *testing.T) {
	channels := []Channel{
		{ID: "cat", Type: 4, PermissionOverwrites: []Overwrite{
			{ID: botRole, Allow: "1024"},
		}},
		{ID: "child", Type: 0, ParentID: "cat"},
	}
	roles := []Role{
		{ID: everyoneRole, Permissions: "0"},
		{ID: botRole, Permissions: "0"},
	}
	got := InvitedFrom(everyoneRole, botID, []string{botRole}, roles, channels)
	if want := []string{"child"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestInvitedFromMemberDenyWins(t *testing.T) {
	channels := []Channel{
		{ID: "denied", Type: 0, PermissionOverwrites: []Overwrite{
			{ID: botRole, Allow: "1024"},
			{ID: botID, Deny: "1024"},
		}},
	}
	roles := []Role{
		{ID: everyoneRole, Permissions: "0"},
		{ID: botRole, Permissions: "0"},
	}
	got := InvitedFrom(everyoneRole, botID, []string{botRole}, roles, channels)
	if len(got) != 0 {
		t.Fatalf("member deny should win, got %v", got)
	}
}

func TestInvitedFromIgnoresOtherRoles(t *testing.T) {
	channels := []Channel{
		{ID: "other", Type: 0, PermissionOverwrites: []Overwrite{
			{ID: otherRole, Allow: "1024"},
		}},
	}
	roles := []Role{
		{ID: everyoneRole, Permissions: "0"},
		{ID: otherRole, Permissions: "0"},
	}
	got := InvitedFrom(everyoneRole, botID, nil, roles, channels)
	if len(got) != 0 {
		t.Fatalf("other role should not invite the bot, got %v", got)
	}
}
