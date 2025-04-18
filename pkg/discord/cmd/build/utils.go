package build

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
)

// getClientChoices returns the choices for client selection.
func (c *BuildCommand) getClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)

	// Add consensus clients
	for _, client := range clients.CLClients {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  client,
			Value: client,
		})
	}

	// Add execution clients
	for _, client := range clients.ELClients {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  client,
			Value: client,
		})
	}

	return choices
}

// hasPermission checks if a member has permission to execute the build command.
// For the build command, any user with any team role or admin role can trigger builds.
func (c *BuildCommand) hasPermission(member *discordgo.Member, session *discordgo.Session, guildID string, config *common.RoleConfig) bool {
	// Check admin roles first.
	for _, roleID := range member.Roles {
		role, err := session.State.Role(guildID, roleID)
		if err != nil {
			continue
		}

		roleName := strings.ToLower(role.Name)
		if config.AdminRoles[roleName] {
			return true
		}
	}

	// Check if user has any team role.
	for _, roleID := range member.Roles {
		role, err := session.State.Role(guildID, roleID)
		if err != nil {
			continue
		}

		roleName := strings.ToLower(role.Name)
		for _, teamRole := range config.ClientRoles {
			if strings.EqualFold(teamRole, roleName) {
				return true
			}
		}
	}

	return false
}

// stringPtr returns a pointer to the given string.
func stringPtr(s string) *string {
	return &s
}
