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
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)

	// Get consensus clients that have workflows
	clientWorkflows := c.getClientWorkflows("consensus")
	for client, workflow := range clientWorkflows {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  workflow.Name,
			Value: client,
		})
	}

	return choices
}

// getELClientChoices returns the choices for execution layer client selection.
func (c *BuildCommand) getELClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)

	// Get execution clients that have workflows
	clientWorkflows := c.getClientWorkflows("execution")
	for client, workflow := range clientWorkflows {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  workflow.Name,
			Value: client,
		})
	}

	return choices
}

// getToolsChoices returns the choices for tool workflow selection.
func (c *BuildCommand) getToolsChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0)

	// Add additional workflow choices
	workflows := c.getAdditionalWorkflows()
	for key, workflow := range workflows {
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

// isOfficialRepository compares two repository paths to determine if they are the same.
// Examples:
//
//	isOfficialRepository("ethereum/go-ethereum", "ethereum/go-ethereum") → true
//	isOfficialRepository("mattevans/go-ethereum", "ethereum/go-ethereum") → false
func isOfficialRepository(providedRepo, officialRepo string) bool {
	return strings.EqualFold(providedRepo, officialRepo)
}

// extractOrganization extracts the organization/username from a repository path.
// Examples:
//
//	extractOrganization("ethereum/go-ethereum") → "ethereum"
//	extractOrganization("mattevans/prysm") → "mattevans"
//	extractOrganization("go-ethereum") → ""
func extractOrganization(repository string) string {
	parts := strings.Split(repository, "/")
	if len(parts) >= 2 {
		return parts[0]
	}

	return ""
}

// shouldPrependOrganization determines if organization should be prepended to docker tag.
// Returns true only if:
// 1. dockerTag is empty (not provided by user).
// 2. providedRepo is different from officialRepo.
// 3. providedRepo has a valid organization prefix.
func shouldPrependOrganization(providedRepo, officialRepo, dockerTag string) bool {
	if dockerTag != "" {
		return false
	}

	if isOfficialRepository(providedRepo, officialRepo) {
		return false
	}

	return extractOrganization(providedRepo) != ""
}
