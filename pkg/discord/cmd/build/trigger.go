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

// handleBuild handles the build subcommands (client-cl, client-el, tool).
//
//nolint:gocyclo // Not that bad, switch statement throwing it.
func (c *BuildCommand) handleBuild(s *discordgo.Session, i *discordgo.InteractionCreate, option *discordgo.ApplicationCommandInteractionDataOption) error {
	// Determine what type of build this is.
	var (
		targetName, targetDisplayName string
		isClient                      bool
	)

	switch option.Name {
	case "client-cl", "client-el":
		isClient = true

		for _, opt := range option.Options {
			if opt.Name == "client" {
				targetName = opt.StringValue()
				targetDisplayName = targetName

				break
			}
		}
	case "tool":
		isClient = false

		for _, opt := range option.Options {
			if opt.Name == "workflow" {
				targetName = opt.StringValue()
				if workflow, exists := AdditionalWorkflows[targetName]; exists {
					targetDisplayName = workflow.Name
				} else {
					targetDisplayName = targetName
				}

				break
			}
		}
	}

	// Send immediate response, discord requires an ACK with 3s, sometimes triggering
	// the build can blow out, and then things will fall apart.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("🔄 Triggering build for **%s**...", targetDisplayName),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		return fmt.Errorf("failed to send initial response: %w", err)
	}

	// Get optional parameters.
	var repository, ref, dockerTag, buildArgs string

	for _, opt := range option.Options {
		switch opt.Name {
		case "repository":
			repository = opt.StringValue()
		case "ref":
			ref = opt.StringValue()
		case "docker_tag":
			dockerTag = opt.StringValue()
		case "build_args":
			buildArgs = opt.StringValue()
		}
	}

	// Use defaults if not provided.
	if repository == "" {
		if isClient {
			if repo, exists := clients.DefaultRepositories[targetName]; exists {
				repository = repo
			} else {
				// For unknown clients, repository is required.
				if _, interactionErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: stringPtr(fmt.Sprintf("❌ Repository is required for **%s**", targetDisplayName)),
				}); interactionErr != nil {
					return fmt.Errorf("failed to edit response: %w", interactionErr)
				}

				return nil
			}
		} else {
			// For tools, get from AdditionalWorkflows.
			if workflow, exists := AdditionalWorkflows[targetName]; exists {
				repository = workflow.Repository
			} else {
				// This should never happen with the dropdown selection.
				if _, interactionErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: stringPtr(fmt.Sprintf("❌ Unknown tool workflow: **%s**", targetName)),
				}); interactionErr != nil {
					return fmt.Errorf("failed to edit response: %w", interactionErr)
				}

				return nil
			}
		}
	}

	if ref == "" {
		if isClient {
			if branch, exists := clients.DefaultBranches[targetName]; exists {
				ref = branch
			} else {
				// For unknown clients, default to main.
				ref = "main"
			}
		} else {
			// For tools, get from AdditionalWorkflows.
			if workflow, exists := AdditionalWorkflows[targetName]; exists {
				ref = workflow.Branch
			} else {
				// This should never happen with the dropdown selection.
				ref = "main"
			}
		}
	}

	// Use default build args if provided and user didn't specify any.
	if buildArgs == "" && HasBuildArgs(targetName) {
		buildArgs = GetDefaultBuildArgs(targetName)
	}

	// Trigger the workflow.
	workflowURL, err := c.triggerWorkflow(targetName, repository, ref, dockerTag, buildArgs)
	if err != nil {
		if _, interactionErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("❌ Failed to trigger build for **%s**: %s", targetDisplayName, err)),
		}); interactionErr != nil {
			return fmt.Errorf("failed to edit response with error: %w (original error: %s)", interactionErr, err)
		}

		return nil // Already handled error by editing message.
	}

	// Create success embed.
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🏗️ Build Triggered: %s", targetDisplayName),
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

	// Add build args if specified.
	if buildArgs != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Build Args",
			Value:  fmt.Sprintf("`%s`", buildArgs),
			Inline: true,
		})
	}

	// Add thumbnail.
	if isClient && (clients.IsELClient(targetName) || clients.IsCLClient(targetName)) {
		if logo := clients.GetClientLogo(targetName); logo != "" {
			embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
				URL: logo,
			}
		}
	} else {
		// Default logo for non-client workflows.
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: "https://pbs.twimg.com/profile_images/1772523356123959296/e9mkwqVp_400x400.jpg",
		}
	}

	// Edit message with success embed.
	if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(""),
		Embeds:  &[]*discordgo.MessageEmbed{embed},
	}); err != nil {
		return fmt.Errorf("failed to edit response: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"workflow":   targetName,
		"repository": repository,
		"ref":        ref,
		"docker_tag": dockerTag,
		"build_args": buildArgs,
	}).Info("Build triggered")

	return nil
}

// triggerWorkflow triggers the GitHub workflow for the given build target.
func (c *BuildCommand) triggerWorkflow(buildTarget, repository, ref, dockerTag string, buildArgs string) (string, error) {
	// Prepare the workflow inputs.
	inputs := map[string]interface{}{
		"repository": repository,
		"ref":        ref,
	}

	if dockerTag != "" {
		inputs["docker_tag"] = dockerTag
	}

	if buildArgs != "" {
		inputs["build_args"] = buildArgs
	}

	body := map[string]interface{}{
		"ref":    "master", // `master` of DefaultRepository.
		"inputs": inputs,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Determine the workflow path based on the build target
	workflowName := buildTarget

	// Special case mapping for clients with different repo/workflow names
	switch buildTarget {
	case clients.CLNimbus:
		// See: https://github.com/status-im/nimbus-eth2
		workflowName = "nimbus-eth2"
	case clients.ELNimbusel:
		// See: https://github.com/status-im/nimbus-eth1
		workflowName = "nimbus-eth1"
	default:
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/actions/workflows/build-push-%s.yml/dispatches", DefaultRepository, workflowName)

	req, err := http.NewRequest(
		"POST",
		url,
		strings.NewReader(string(jsonBody)),
	)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+c.githubToken)
	req.Header.Set("Content-Type", "application/json")

	// Use the HTTP client
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("workflow trigger failed with status: %d", resp.StatusCode)
	}

	return fmt.Sprintf("https://github.com/%s/actions/workflows/build-push-%s.yml", DefaultRepository, workflowName), nil
}
