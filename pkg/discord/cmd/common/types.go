package common

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/cartographoor"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/scheduler"
	"github.com/ethpandaops/panda-pulse/pkg/store"
)

// RoleConfig defines the roles required for each permission level.
type RoleConfig struct {
	AdminRoles  map[string]bool     // Map of admin role names that have full access
	ClientRoles map[string][]string // Map of client names to their team role names
}

// Command represents a Discord slash command.
type Command interface {
	// Name returns the name of the command.
	Name() string
	// Register registers the command with the given session.
	Register(*discordgo.Session) error
	// Handle handles the command.
	Handle(*discordgo.Session, *discordgo.InteractionCreate)
}

// BotContext provides access to bot functionality needed by commands.
type BotContext interface {
	// GetSession returns the Discord session.
	GetSession() *discordgo.Session
	// GetScheduler returns the scheduler.
	GetScheduler() *scheduler.Scheduler
	// GetMonitorRepo returns the monitor repository.
	GetMonitorRepo() *store.MonitorRepo
	// GetChecksRepo returns the checks repository.
	GetChecksRepo() *store.ChecksRepo
	// GetMentionsRepo returns the mentions repository.
	GetMentionsRepo() *store.MentionsRepo
	// GetHiveSummaryRepo returns the Hive summary repository.
	GetHiveSummaryRepo() *store.HiveSummaryRepo
	// GetGrafana returns the Grafana client.
	GetGrafana() grafana.Client
	// GetHive returns the Hive client.
	GetHive() hive.Hive
	// GetCartographoor returns the cartographoor service.
	GetCartographoor() *cartographoor.Service
	// GetRoleConfig returns the role configuration.
	GetRoleConfig() *RoleConfig
}

// GetRoleNames returns the plain-english names of the roles a member has.
func GetRoleNames(member *discordgo.Member, session *discordgo.Session, guildID string) []string {
	roleNames := make([]string, 0)

	for _, roleID := range member.Roles {
		role, err := session.State.Role(guildID, roleID)
		if err != nil {
			continue
		}

		roleNames = append(roleNames, role.Name)
	}

	return roleNames
}

// HasPermission checks if a member has permission to execute a command.
func HasPermission(member *discordgo.Member, session *discordgo.Session, guildID string, config *RoleConfig, cmdData *discordgo.ApplicationCommandInteractionData) bool {
	// Check admin roles first and let it through to the keeper.
	for _, roleID := range member.Roles {
		role, err := session.State.Role(guildID, roleID)
		if err != nil {
			continue
		}

		if config.AdminRoles[strings.ToLower(role.Name)] {
			return true
		}
	}

	// For client team members, we need to check if they're trying to access their own client.
	clientArg := findClientArgument(cmdData)
	if clientArg != "" {
		// Get the required team roles for this client.
		requiredRoles := config.ClientRoles[strings.ToLower(clientArg)]
		if len(requiredRoles) == 0 {
			return false // Unknown client.
		}

		// Check if user has any of the required team roles.
		for _, roleName := range GetRoleNames(member, session, guildID) {
			for _, requiredRole := range requiredRoles {
				if strings.EqualFold(roleName, requiredRole) {
					return true
				}
			}
		}

		return false // User doesn't have the required team role.
	}

	// If no client is specified in the command, only admins can execute it.
	return false
}

// findClientArgument looks for a client argument in the command data.
func findClientArgument(data *discordgo.ApplicationCommandInteractionData) string {
	if data == nil || len(data.Options) == 0 {
		return ""
	}

	// Check subcommand options.
	subCmd := data.Options[0]
	for _, opt := range subCmd.Options {
		if opt.Name == "client" {
			return opt.StringValue()
		}
	}

	return ""
}

// NoPermissionError returns a formatted error message for permission denied.
func NoPermissionError(command string) error {
	return fmt.Errorf("🚫 Sorry, you do not have permission to use the `/%s` command", command)
}
