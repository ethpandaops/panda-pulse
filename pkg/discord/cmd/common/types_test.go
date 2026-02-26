package common

import (
	"testing"

	"github.com/bwmarrin/discordgo"
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

// newMember creates a discordgo member with the given role IDs.
func newMember(roleIDs ...string) *discordgo.Member {
	return &discordgo.Member{
		Roles: roleIDs,
	}
}

// newCmdDataWithClient creates interaction data with a client subcommand option.
func newCmdDataWithClient(client string) *discordgo.ApplicationCommandInteractionData {
	return &discordgo.ApplicationCommandInteractionData{
		Options: []*discordgo.ApplicationCommandInteractionDataOption{
			{
				Name: "run",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{
						Name:  "client",
						Type:  discordgo.ApplicationCommandOptionString,
						Value: client,
					},
				},
			},
		},
	}
}

func TestHasPermission(t *testing.T) {
	multiServerConfig := &RoleConfig{
		AdminRoles: map[string]bool{
			"ef":     true,
			"admin":  true,
			"mod":    true,
			"epf":    true,
			"eels":   true,
			"steel":  true,
			"pandas": true,
		},
		ClientRoles: map[string][]string{
			"lighthouse": {"sigmaprime", "lighthouse"},
			"prysm":      {"prysmatic", "prysm"},
			"lodestar":   {"chainsafe", "lodestar"},
			"nimbus":     {"nimbus"},
			"teku":       {"teku"},
			"geth":       {"geth"},
			"ethrex":     {"ethrex"},
		},
	}

	t.Run("admin role grants access to any command", func(t *testing.T) {
		tests := []struct {
			name     string
			roleName string
		}{
			{name: "original ef role", roleName: "ef"},
			{name: "admin role", roleName: "Admin"},
			{name: "mod role", roleName: "mod"},
			{name: "new server EELS role", roleName: "EELS"},
			{name: "new server STEEL role", roleName: "STEEL"},
			{name: "new server Pandas role", roleName: "Pandas"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				roles := []*discordgo.Role{
					{ID: "role-1", Name: tt.roleName},
				}
				session := newTestSession(t, roles)
				member := newMember("role-1")

				// Admin should have access even without a client argument.
				assert.True(t, HasPermission(member, session, testGuildID, multiServerConfig, nil))

				// Admin should also have access with a client argument.
				cmdData := newCmdDataWithClient("lighthouse")
				assert.True(t, HasPermission(member, session, testGuildID, multiServerConfig, cmdData))
			})
		}
	})

	t.Run("original server team roles grant access", func(t *testing.T) {
		tests := []struct {
			name       string
			roleName   string
			clientName string
		}{
			{name: "sigmaprime for lighthouse", roleName: "sigmaprime", clientName: "lighthouse"},
			{name: "prysmatic for prysm", roleName: "prysmatic", clientName: "prysm"},
			{name: "chainsafe for lodestar", roleName: "chainsafe", clientName: "lodestar"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				roles := []*discordgo.Role{
					{ID: "role-1", Name: tt.roleName},
				}
				session := newTestSession(t, roles)
				member := newMember("role-1")
				cmdData := newCmdDataWithClient(tt.clientName)

				assert.True(t, HasPermission(member, session, testGuildID, multiServerConfig, cmdData))
			})
		}
	})

	t.Run("new server team roles grant access", func(t *testing.T) {
		tests := []struct {
			name       string
			roleName   string
			clientName string
		}{
			{name: "Lighthouse role for lighthouse", roleName: "Lighthouse", clientName: "lighthouse"},
			{name: "Prysm role for prysm", roleName: "Prysm", clientName: "prysm"},
			{name: "Lodestar role for lodestar", roleName: "Lodestar", clientName: "lodestar"},
			{name: "Ethrex role for ethrex", roleName: "Ethrex", clientName: "ethrex"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				roles := []*discordgo.Role{
					{ID: "role-1", Name: tt.roleName},
				}
				session := newTestSession(t, roles)
				member := newMember("role-1")
				cmdData := newCmdDataWithClient(tt.clientName)

				assert.True(t, HasPermission(member, session, testGuildID, multiServerConfig, cmdData))
			})
		}
	})

	t.Run("team role denied for wrong client", func(t *testing.T) {
		roles := []*discordgo.Role{
			{ID: "role-1", Name: "Lighthouse"},
		}
		session := newTestSession(t, roles)
		member := newMember("role-1")

		// Lighthouse role should not grant access to prysm.
		cmdData := newCmdDataWithClient("prysm")
		assert.False(t, HasPermission(member, session, testGuildID, multiServerConfig, cmdData))
	})

	t.Run("non-admin denied without client argument", func(t *testing.T) {
		roles := []*discordgo.Role{
			{ID: "role-1", Name: "Lighthouse"},
		}
		session := newTestSession(t, roles)
		member := newMember("role-1")

		assert.False(t, HasPermission(member, session, testGuildID, multiServerConfig, nil))
	})

	t.Run("unknown client denied", func(t *testing.T) {
		roles := []*discordgo.Role{
			{ID: "role-1", Name: "Lighthouse"},
		}
		session := newTestSession(t, roles)
		member := newMember("role-1")

		cmdData := newCmdDataWithClient("unknownclient")
		assert.False(t, HasPermission(member, session, testGuildID, multiServerConfig, cmdData))
	})

	t.Run("user with no roles denied", func(t *testing.T) {
		session := newTestSession(t, nil)
		member := newMember()

		cmdData := newCmdDataWithClient("lighthouse")
		assert.False(t, HasPermission(member, session, testGuildID, multiServerConfig, cmdData))
	})

	t.Run("case insensitive role matching", func(t *testing.T) {
		roles := []*discordgo.Role{
			{ID: "role-1", Name: "SIGMAPRIME"},
		}
		session := newTestSession(t, roles)
		member := newMember("role-1")

		cmdData := newCmdDataWithClient("lighthouse")
		assert.True(t, HasPermission(member, session, testGuildID, multiServerConfig, cmdData))
	})
}

func TestFindClientArgument(t *testing.T) {
	t.Run("returns client from subcommand options", func(t *testing.T) {
		data := newCmdDataWithClient("lighthouse")
		assert.Equal(t, "lighthouse", findClientArgument(data))
	})

	t.Run("returns empty for nil data", func(t *testing.T) {
		assert.Equal(t, "", findClientArgument(nil))
	})

	t.Run("returns empty when no client option", func(t *testing.T) {
		data := &discordgo.ApplicationCommandInteractionData{
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{
					Name: "run",
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{
							Name:  "network",
							Type:  discordgo.ApplicationCommandOptionString,
							Value: "mainnet",
						},
					},
				},
			},
		}
		assert.Equal(t, "", findClientArgument(data))
	})

	t.Run("returns empty for no options", func(t *testing.T) {
		data := &discordgo.ApplicationCommandInteractionData{}
		assert.Equal(t, "", findClientArgument(data))
	})
}
