package hive

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/sirupsen/logrus"
)

const (
	triggerEmbedColor  = 0x7289DA
	defaultTriggerRef  = "master"
	optionNameClient   = "client"
	optionNameRepo     = "repository"
	optionNameWorkflow = "workflow"
	optionNameRef      = "ref"
)

// handleTrigger handles the /hive trigger subcommand.
func (c *HiveCommand) handleTrigger(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
	option *discordgo.ApplicationCommandInteractionDataOption,
) {
	if !c.hasTriggerPermission(i.Member, s, i.GuildID, c.bot.GetRoleConfig()) {
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: common.NoPermissionError("hive trigger").Error(),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		}); err != nil {
			c.log.WithError(err).Error("Failed to respond with permission error")
		}

		return
	}

	var repository, workflow, client, ref string

	for _, opt := range option.Options {
		switch opt.Name {
		case optionNameRepo:
			repository = opt.StringValue()
		case optionNameWorkflow:
			workflow = opt.StringValue()
		case optionNameClient:
			client = opt.StringValue()
		case optionNameRef:
			ref = opt.StringValue()
		}
	}

	if ref == "" {
		ref = defaultTriggerRef
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf(
				"🔄 Triggering Hive workflow **%s** for client **%s**...",
				workflow, client,
			),
			Flags: discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		c.log.WithError(err).Error("Failed to send initial response")

		return
	}

	if err := c.triggerHiveWorkflow(repository, workflow, ref, client); err != nil {
		if _, editErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf(
				"❌ Failed to trigger Hive workflow **%s**: %s",
				workflow, err,
			)),
		}); editErr != nil {
			c.log.WithError(editErr).Error("Failed to edit response with error")
		}

		return
	}

	workflowURL := fmt.Sprintf(
		"https://github.com/%s/actions/workflows/%s",
		repository, workflow,
	)

	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🐝 Hive Workflow Triggered: %s", workflow),
		Color: triggerEmbedColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Repository",
				Value:  fmt.Sprintf("`%s`", repository),
				Inline: true,
			},
			{
				Name:   "Workflow",
				Value:  fmt.Sprintf("`%s`", workflow),
				Inline: true,
			},
			{
				Name:   "Client",
				Value:  fmt.Sprintf("`%s`", client),
				Inline: true,
			},
			{
				Name:   "Branch/Tag",
				Value:  fmt.Sprintf("`%s`", ref),
				Inline: true,
			},
			{
				Name:   "Actions",
				Value:  fmt.Sprintf("[View Workflow Runs](%s)", workflowURL),
				Inline: false,
			},
		},
		URL:       workflowURL,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(""),
		Embeds:  &[]*discordgo.MessageEmbed{embed},
	}); err != nil {
		c.log.WithError(err).Error("Failed to edit response with embed")
	}

	c.log.WithFields(logrus.Fields{
		"repository": repository,
		"workflow":   workflow,
		"client":     client,
		"ref":        ref,
	}).Info("Hive workflow triggered")
}

// triggerHiveWorkflow triggers a GitHub Actions workflow dispatch for Hive tests.
func (c *HiveCommand) triggerHiveWorkflow(
	repository, workflow, ref, client string,
) error {
	body := map[string]any{
		"ref": ref,
		"inputs": map[string]string{
			"client": client,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	url := fmt.Sprintf(
		"https://api.github.com/repos/%s/actions/workflows/%s/dispatches",
		repository, workflow,
	)

	req, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+c.githubToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req) //nolint:gosec // URL is from trusted configuration
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("workflow trigger failed with status: %d", resp.StatusCode)
	}

	return nil
}

// hasTriggerPermission checks if a member has permission to trigger Hive workflows.
// Any user with any team role or admin role can trigger workflows.
func (c *HiveCommand) hasTriggerPermission(
	member *discordgo.Member,
	session *discordgo.Session,
	guildID string,
	config *common.RoleConfig,
) bool {
	for _, roleName := range common.GetRoleNames(member, session, guildID) {
		if config.AdminRoles[strings.ToLower(roleName)] {
			return true
		}
	}

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

// handleClientAutocomplete handles autocomplete for client selection in the trigger subcommand.
func (c *HiveCommand) handleClientAutocomplete(
	s *discordgo.Session,
	i *discordgo.InteractionCreate,
) {
	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	focusedOption := c.findFocusedOption(data.Options)
	if focusedOption == nil || focusedOption.Name != optionNameClient {
		return
	}

	inputValue := ""
	if focusedOption.Value != nil {
		inputValue = strings.ToLower(fmt.Sprintf("%v", focusedOption.Value))
	}

	cartographoor := c.bot.GetCartographoor()
	allClients := cartographoor.GetAllClients()
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)

	for _, client := range allClients {
		displayName := cartographoor.GetClientDisplayName(client)
		if inputValue == "" ||
			strings.Contains(strings.ToLower(client), inputValue) ||
			strings.Contains(strings.ToLower(displayName), inputValue) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  displayName,
				Value: client,
			})

			if len(choices) >= 25 {
				break
			}
		}
	}

	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	}); err != nil {
		c.log.WithError(err).Error("Failed to respond to client autocomplete")
	}
}
