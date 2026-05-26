package build

import (
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testGuildID = "guild-1"

// newTestSession creates a discordgo session with state pre-populated with the given roles.
func newTestSession(t *testing.T, roles []*discordgo.Role) *discordgo.Session {
	t.Helper()

	session, err := discordgo.New("Bot fake-token")
	require.NoError(t, err)

	session.StateEnabled = true
	session.State = discordgo.NewState()

	guild := &discordgo.Guild{
		ID:    testGuildID,
		Roles: roles,
	}
	require.NoError(t, session.State.GuildAdd(guild))

	return session
}

// TestBuildHasPermission pins down the build command's intentionally-permissive
// rule: any user with any team role (or admin role) can trigger a build for any
// target, regardless of which client the build is for. This is the rule the
// dispatcher in pkg/discord/bot.go defers to via commandSelfChecksPermission.
func TestBuildHasPermission(t *testing.T) {
	cfg := &common.RoleConfig{
		AdminRoles: map[string]bool{
			"ef":    true,
			"admin": true,
		},
		ClientRoles: map[string][]string{
			"lighthouse": {"sigmaprime", "lighthouse"},
			"prysm":      {"prysmatic", "prysm"},
			"geth":       {"geth"},
			"reth":       {"reth"},
		},
	}

	cmd := &BuildCommand{}

	tests := []struct {
		name     string
		roleName string
		want     bool
	}{
		// Regression guard: a geth-tagged user (EL client team) must be able
		// to trigger any build — including CL clients like /build client-cl
		// lighthouse. Before the dispatcher fix, the strict per-client check
		// in common.HasPermission was rejecting these.
		{name: "geth role allowed (EL team triggering anything)", roleName: "geth", want: true},
		{name: "reth role allowed", roleName: "reth", want: true},
		{name: "lighthouse role allowed (CL team triggering anything)", roleName: "lighthouse", want: true},
		{name: "sigmaprime alias allowed", roleName: "sigmaprime", want: true},
		{name: "case-insensitive role match", roleName: "GETH", want: true},

		// Admin roles always pass.
		{name: "admin role allowed", roleName: "admin", want: true},
		{name: "ef admin role allowed", roleName: "ef", want: true},

		// Users with no team or admin role are still rejected.
		{name: "unrelated role denied", roleName: "random-role", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roles := []*discordgo.Role{{ID: "role-1", Name: tt.roleName}}
			session := newTestSession(t, roles)
			member := &discordgo.Member{Roles: []string{"role-1"}}

			assert.Equal(t, tt.want, cmd.hasPermission(member, session, testGuildID, cfg))
		})
	}

	t.Run("member with no roles denied", func(t *testing.T) {
		session := newTestSession(t, nil)
		member := &discordgo.Member{Roles: []string{}}

		assert.False(t, cmd.hasPermission(member, session, testGuildID, cfg))
	})
}
