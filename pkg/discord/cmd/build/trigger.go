package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/sirupsen/logrus"
)

const (
	buildEmbedColor = 0x7289DA
)

// handleTrigger handles the trigger subcommand.
func (c *BuildCommand) handleTrigger(s *discordgo.Session, i *discordgo.InteractionCreate, option *discordgo.ApplicationCommandInteractionDataOption) error {
	// Send immediate response, discord requires an ACK with 3s, sometimes triggering
	// the build can blow out, and then things will fall apart.
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("🔄 Triggering build for **%s**...", option.Options[0].StringValue()),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send initial response: %w", err)
	}

	// Get the client from the options.
	client := option.Options[0].StringValue()

	// Get optional parameters.
	var repository, ref, dockerTag string

	for _, opt := range option.Options[1:] {
		switch opt.Name {
		case "repository":
			repository = opt.StringValue()
		case "ref":
			ref = opt.StringValue()
		case "docker_tag":
			dockerTag = opt.StringValue()
		}
	}

	// Use defaults if not provided.
	if repository == "" {
		repository = clients.DefaultRepositories[client]
	}

	if ref == "" {
		ref = clients.DefaultBranches[client]
	}

	c.log.WithFields(logrus.Fields{
		"command":    "/build trigger",
		"repository": repository,
		"ref":        ref,
		"docker_tag": dockerTag,
		"user":       i.Member.User.Username,
	}).Info("Received command")

	// Trigger the workflow.
	workflowURL, err := c.triggerWorkflow(client, repository, ref, dockerTag)
	if err != nil {
		if _, interactionErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("❌ Failed to trigger build for **%s**: %s", client, err)),
		}); interactionErr != nil {
			return fmt.Errorf("failed to edit response with error: %w (original error: %s)", interactionErr, err)
		}

		return nil // Already handled error by editing message
	}

	// Create success embed.
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🏗️ Build Triggered: %s", client),
		Color: buildEmbedColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Repository",
				Value:  fmt.Sprintf("`%s`", repository),
				Inline: true,
			},
			{
				Name:   "Branch/Tag",
				Value:  fmt.Sprintf("`%s`", ref),
				Inline: true,
			},
			{
				Name:   "Workflow",
				Value:  fmt.Sprintf("[View Build Status](%s)", workflowURL),
				Inline: false,
			},
		},
		URL:       workflowURL,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Add docker tag if specified.
	if dockerTag != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Docker Tag",
			Value:  fmt.Sprintf("`%s`", dockerTag),
			Inline: true,
		})
	}

	// Add client logo if available.
	if logo := clients.GetClientLogo(client); logo != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: logo,
		}
	}

	// Edit message with success embed.
	if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(""),
		Embeds:  &[]*discordgo.MessageEmbed{embed},
	}); err != nil {
		return fmt.Errorf("failed to edit response: %w", err)
	}

	return nil
}

// triggerWorkflow triggers the GitHub workflow for the given client.
func (c *BuildCommand) triggerWorkflow(clientName, repository, ref, dockerTag string) (string, error) {
	// Prepare the workflow inputs.
	inputs := map[string]interface{}{
		"repository": repository,
		"ref":        ref,
	}

	if dockerTag != "" {
		inputs["docker_tag"] = dockerTag
	}

	body := map[string]interface{}{
		"ref":    "master", // `master` of DefaultRepository.
		"inputs": inputs,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Map client names to their workflow names
	workflowClientName := clientName

	switch clientName {
	case clients.CLNimbus:
		workflowClientName = "nimbus-eth1"
	case clients.ELNimbusel:
		workflowClientName = "nimbus-eth2"
	default:
	}

	req, err := http.NewRequest(
		"POST",
		fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/build-push-%s.yml/dispatches", DefaultRepository, workflowClientName),
		strings.NewReader(string(jsonBody)),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+c.githubToken)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("workflow trigger failed with status: %d", resp.StatusCode)
	}

	return fmt.Sprintf("https://github.com/%s/actions/workflows/build-push-%s.yml", DefaultRepository, workflowClientName), nil
}
