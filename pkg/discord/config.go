package discord

import (
	"strings"

	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
)

// Config represents the configuration for the Discord bot.
type Config struct {
	DiscordToken string   `yaml:"discordToken"`
	GithubToken  string   `yaml:"githubToken"`
	GuildIDs     []string `yaml:"guildIds"` // Optional: if set, commands will be registered to these guilds only
}

// AsRoleConfig returns the role configuration.
func (c *Config) AsRoleConfig() *common.RoleConfig {
	// Create admin roles map by flattening all role name variants.
	adminRoles := make(map[string]bool)

	for _, roles := range clients.AdminRoles {
		for _, role := range roles {
			adminRoles[strings.ToLower(role)] = true
		}
	}

	return &common.RoleConfig{
		AdminRoles:  adminRoles,
		ClientRoles: clients.TeamRoles,
	}
}
