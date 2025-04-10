package discord

import (
	"strings"

	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
)

// Config represents the configuration for the Discord bot.
type Config struct {
	DiscordToken string `yaml:"discordToken"`
	GithubToken  string `yaml:"githubToken"`
}

// AsRoleConfig returns the role configuration.
func (c *Config) AsRoleConfig() *common.RoleConfig {
	// Create admin roles map.
	adminRoles := make(map[string]bool)
	for _, role := range clients.AdminRoles {
		adminRoles[strings.ToLower(role)] = true
	}

	return &common.RoleConfig{
		AdminRoles:  adminRoles,
		ClientRoles: clients.TeamRoles,
	}
}
