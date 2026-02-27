package build

import (
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
)

// getAdditionalWorkflows returns workflow information, dynamically fetched from GitHub.
func (c *BuildCommand) getAdditionalWorkflows() map[string]WorkflowInfo {
	workflows, err := c.workflowFetcher.GetToolWorkflows()
	if err != nil {
		c.log.WithError(err).Error("Failed to fetch dynamic workflows")

		return make(map[string]WorkflowInfo)
	}

	return workflows
}

// getClientWorkflows returns workflows for clients that exist in both Cartographoor and GitHub workflows.
func (c *BuildCommand) getClientWorkflows(clientType string) map[string]WorkflowInfo {
	allWorkflows, err := c.workflowFetcher.GetAllWorkflows()
	if err != nil {
		c.log.WithError(err).Error("Failed to fetch all workflows")

		return make(map[string]WorkflowInfo)
	}

	cartographoor := c.bot.GetCartographoor()

	var clients []string

	switch clientType {
	case "execution":
		clients = cartographoor.GetELClients()
	case "consensus":
		clients = cartographoor.GetCLClients()
	default:
		return make(map[string]WorkflowInfo)
	}

	// Filter workflows to only include clients that exist in both Cartographoor and GitHub workflows.
	clientWorkflows := make(map[string]WorkflowInfo)

	for _, client := range clients {
		// Map client name to workflow name for special cases
		workflowName := getClientToWorkflowName(client)

		if workflow, exists := allWorkflows[workflowName]; exists {
			// Use Cartographoor display name but keep all other workflow data unchanged.
			workflowCopy := workflow
			workflowCopy.Name = cartographoor.GetClientDisplayName(client)
			clientWorkflows[client] = workflowCopy
		}
	}

	return clientWorkflows
}

// HasBuildArgs returns whether the given workflow or client supports build arguments.
func (c *BuildCommand) HasBuildArgs(target string) bool {
	// Check all workflows (clients and tools).
	allWorkflows, err := c.workflowFetcher.GetAllWorkflows()
	if err != nil {
		c.log.WithError(err).Error("Failed to fetch workflows for build args check")

		return false
	}

	// Map client name to workflow name for special cases
	workflowName := getClientToWorkflowName(target)

	if workflow, exists := allWorkflows[workflowName]; exists {
		return workflow.HasBuildArgs
	}

	return false
}

// GetDefaultBuildArgs returns the default build arguments for a workflow or client, if any.
func (c *BuildCommand) GetDefaultBuildArgs(target string) string {
	// Check all workflows (clients and tools)
	allWorkflows, err := c.workflowFetcher.GetAllWorkflows()
	if err != nil {
		c.log.WithError(err).Error("Failed to fetch workflows for build args")

		return ""
	}

	// Map client name to workflow name for special cases
	workflowName := getClientToWorkflowName(target)

	if workflow, exists := allWorkflows[workflowName]; exists && workflow.BuildArgs != "" {
		return workflow.BuildArgs
	}

	return ""
}

// getCLClientChoices returns the choices for consensus layer client selection.
func (c *BuildCommand) getCLClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	clientWorkflows := c.getClientWorkflows("consensus")
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(clientWorkflows))

	for client, workflow := range clientWorkflows {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  truncateChoiceName(workflow.Name),
			Value: client,
		})
	}

	return choices
}

// getELClientChoices returns the choices for execution layer client selection.
func (c *BuildCommand) getELClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	clientWorkflows := c.getClientWorkflows("execution")
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(clientWorkflows))

	for client, workflow := range clientWorkflows {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  truncateChoiceName(workflow.Name),
			Value: client,
		})
	}

	return choices
}

// getToolsChoices returns the choices for tool workflow selection.
func (c *BuildCommand) getToolsChoices() []*discordgo.ApplicationCommandOptionChoice {
	workflows := c.getAdditionalWorkflows()
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(workflows))

	for key, workflow := range workflows {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  truncateChoiceName(workflow.Name),
			Value: key,
		})
	}

	return choices
}

// maxChoiceNameLength is the maximum length for a Discord slash command choice name.
const maxChoiceNameLength = 25

// truncateChoiceName truncates a choice name to fit Discord's 25-character limit.
func truncateChoiceName(name string) string {
	if len(name) <= maxChoiceNameLength {
		return name
	}

	return name[:maxChoiceNameLength]
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
		for _, teamRoles := range config.ClientRoles {
			for _, teamRole := range teamRoles {
				if strings.EqualFold(teamRole, roleName) {
					return true
				}
			}
		}
	}

	return false
}

// getClientToWorkflowName maps client names to their corresponding workflow names.
func getClientToWorkflowName(clientName string) string {
	// Special case mapping for clients with different repo/workflow names
	switch clientName {
	case "nimbus":
		return "nimbus-eth2"
	case "nimbusel":
		return "nimbus-eth1"
	default:
		return clientName
	}
}

// stringPtr returns a pointer to the given string.
func stringPtr(s string) *string {
	return &s
}
