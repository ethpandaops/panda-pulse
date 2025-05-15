package build

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
)

// AdditionalWorkflows contains information about non-client workflows.
var AdditionalWorkflows = map[string]struct {
	Repository   string
	Branch       string
	Name         string
	BuildArgs    string
	HasBuildArgs bool
}{
	"rustic-builder": {
		Repository: "pawanjay176/rustic-builder",
		Branch:     "main",
		Name:       "rustic-builder",
	},
	"beacon-metrics-gazer": {
		Repository: "dapplion/beacon-metrics-gazer",
		Branch:     "master",
		Name:       "beacon-metrics-gazer",
	},
	"consensus-monitor": {
		Repository: "ralexstokes/ethereum_consensus_monitor",
		Branch:     "main",
		Name:       "consensus-monitor",
	},
	"eleel": {
		Repository: "sigp/eleel",
		Branch:     "main",
		Name:       "eleel",
	},
	"ethereum-genesis-generator": {
		Repository: "ethpandaops/ethereum-genesis-generator",
		Branch:     "master",
		Name:       "ethereum-genesis-generator",
	},
	"execution-monitor": {
		Repository: "ethereum/nodemonitor",
		Branch:     "master",
		Name:       "execution-monitor",
	},
	"flashbots-builder": {
		Repository: "flashbots/builder",
		Branch:     "main",
		Name:       "flashbots-builder",
	},
	"goomy-blob": {
		Repository: "ethpandaops/goomy-blob",
		Branch:     "master",
		Name:       "goomy-blob",
	},
	"goteth": {
		Repository: "migalabs/goteth",
		Branch:     "master",
		Name:       "goteth",
	},
	"mev-boost": {
		Repository: "flashbots/mev-boost",
		Branch:     "develop",
		Name:       "mev-boost",
	},
	"mev-boost-relay": {
		Repository: "flashbots/mev-boost-relay",
		Branch:     "main",
		Name:       "mev-boost-relay",
	},
	"mev-rs": {
		Repository:   "ralexstokes/mev-rs",
		Branch:       "main",
		Name:         "mev-rs",
		HasBuildArgs: true,
	},
	"reth-rbuilder": {
		Repository:   "flashbots/rbuilder",
		Branch:       "develop",
		Name:         "reth-rbuilder",
		BuildArgs:    "RBUILDER_BIN=reth-rbuilder",
		HasBuildArgs: true,
	},
	"tx-fuzz": {
		Repository: "MariusVanDerWijden/tx-fuzz",
		Branch:     "master",
		Name:       "tx-fuzz",
	},
	"armiarma": {
		Repository: "ethpandaops/armiarma",
		Branch:     "master",
		Name:       "armiarma",
	},
	"goevmlab": {
		Repository: "holiman/goevmlab",
		Branch:     "master",
		Name:       "goevmlab",
	},
}

// HasBuildArgs returns whether the given workflow or client supports build arguments.
func (c *BuildCommand) HasBuildArgs(target string) bool {
	// Check client workflows first
	if c.bot.GetCartographoor().ClientSupportsBuildArgs(target) {
		return true
	}

	// Check additional workflows
	if workflow, exists := AdditionalWorkflows[target]; exists {
		return workflow.HasBuildArgs
	}

	return false
}

// GetDefaultBuildArgs returns the default build arguments for a workflow or client, if any.
func (c *BuildCommand) GetDefaultBuildArgs(target string) string {
	// Check client workflows first.
	clientBuildArgs := c.bot.GetCartographoor().GetClientDefaultBuildArgs(target)
	if clientBuildArgs != "" {
		return clientBuildArgs
	}

	// Check additional workflows.
	if workflow, exists := AdditionalWorkflows[target]; exists && workflow.BuildArgs != "" {
		return workflow.BuildArgs
	}

	return ""
}

// getCLClientChoices returns the choices for consensus layer client selection.
func (c *BuildCommand) getCLClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)
	cartographoor := c.bot.GetCartographoor()

	// Add consensus clients
	for _, client := range cartographoor.GetCLClients() {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  cartographoor.GetClientDisplayName(client),
			Value: client,
		})
	}

	return choices
}

// getELClientChoices returns the choices for execution layer client selection.
func (c *BuildCommand) getELClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)
	cartographoor := c.bot.GetCartographoor()

	// Add execution clients
	for _, client := range cartographoor.GetELClients() {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  cartographoor.GetClientDisplayName(client),
			Value: client,
		})
	}

	return choices
}

// getToolsChoices returns the choices for tool workflow selection.
func (c *BuildCommand) getToolsChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)

	// Add additional workflow choices
	for key, workflow := range AdditionalWorkflows {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  workflow.Name,
			Value: key,
		})
	}

	return choices
}

// hasPermission checks if a member has permission to execute the build command.
// For the build command, any user with any team role or admin role can trigger builds.
func (c *BuildCommand) hasPermission(member *discordgo.Member, session *discordgo.Session, guildID string, config *common.RoleConfig) bool {
	// Check admin roles first.
	for _, roleName := range common.GetRoleNames(member, session, guildID) {
		if config.AdminRoles[strings.ToLower(roleName)] {
			return true
		}
	}

	// Check if user has any team role.
	for _, roleName := range common.GetRoleNames(member, session, guildID) {
		for _, teamRole := range config.ClientRoles {
			if strings.EqualFold(teamRole, strings.ToLower(roleName)) {
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
