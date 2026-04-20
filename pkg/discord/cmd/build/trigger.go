package build

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const (
	fallbackDefaultBranch = "main"
	buildEmbedColor       = 0x7289DA
	// inlineClaimTimeout is how long handleBuild waits for the run-id lookup
	// before responding without a Run link. The background watcher keeps
	// trying for the full claimTimeout and relinks the embed when it lands.
	inlineClaimTimeout = 12 * time.Second
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
				// Get display name from workflows
				if allWorkflows, err := c.workflowFetcher.GetAllWorkflows(); err == nil {
					// Map client name to workflow name for special cases
					workflowName := getClientToWorkflowName(targetName)
					if workflow, exists := allWorkflows[workflowName]; exists {
						targetDisplayName = workflow.Name
					} else {
						targetDisplayName = targetName
					}
				} else {
					targetDisplayName = targetName
				}

				break
			}
		}
	case "tool":
		isClient = false

		for _, opt := range option.Options {
			if opt.Name == "workflow" {
				targetName = opt.StringValue()
				// Get display name from workflows
				if allWorkflows, err := c.workflowFetcher.GetAllWorkflows(); err == nil {
					if workflow, exists := allWorkflows[targetName]; exists {
						targetDisplayName = workflow.Name
					} else {
						targetDisplayName = targetName
					}
				} else {
					targetDisplayName = targetName
				}

				break
			}
		}
	}

	// Defer the ACK so we have up to 15 minutes to send the final embed.
	// Discord requires an ACK within 3s; deferring buys us time to dispatch
	// the workflow and inline-claim the run before showing anything.
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	}); err != nil {
		return fmt.Errorf("failed to send deferred ack: %w", err)
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
		// Get repository from workflows
		allWorkflows, err := c.workflowFetcher.GetAllWorkflows()
		if err != nil {
			c.log.WithError(err).Error("Failed to fetch workflows for repository resolution")

			if _, interactionErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: new(fmt.Sprintf("❌ Failed to fetch workflow data for **%s**", targetDisplayName)),
			}); interactionErr != nil {
				return fmt.Errorf("failed to edit response: %w", interactionErr)
			}

			return nil
		}

		// Map client name to workflow name for special cases
		workflowName := getClientToWorkflowName(targetName)
		if workflow, exists := allWorkflows[workflowName]; exists {
			repository = workflow.Repository
		}

		if repository == "" {
			// Repository is required but not found
			if _, interactionErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: new(fmt.Sprintf("❌ Repository not found for **%s**", targetDisplayName)),
			}); interactionErr != nil {
				return fmt.Errorf("failed to edit response: %w", interactionErr)
			}

			return nil
		}
	}

	if ref == "" {
		// Get branch from workflows
		allWorkflows, err := c.workflowFetcher.GetAllWorkflows()
		if err != nil {
			c.log.WithError(err).Error("Failed to fetch workflows for branch resolution")
			// Default to main if workflow fetch fails
			ref = fallbackDefaultBranch
		} else {
			// Map client name to workflow name for special cases
			workflowName := getClientToWorkflowName(targetName)
			if workflow, exists := allWorkflows[workflowName]; exists {
				ref = workflow.Branch
			}
		}

		if ref == "" {
			// Default to main if no branch specified
			ref = fallbackDefaultBranch
		}
	}

	// Use default build args if provided and user didn't specify any.
	if buildArgs == "" && c.HasBuildArgs(targetName) {
		buildArgs = c.GetDefaultBuildArgs(targetName)
	}

	// Generate a correlation ID so we can locate the resulting workflow run and
	// DM the invoker when it finishes.
	correlationID := uuid.NewString()

	// Trigger the workflow.
	workflowURL, err := c.triggerWorkflow(targetName, repository, ref, dockerTag, buildArgs, correlationID)
	if err != nil {
		if _, interactionErr := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: new(fmt.Sprintf("❌ Failed to trigger build for **%s**: %s", targetDisplayName, err)),
		}); interactionErr != nil {
			return fmt.Errorf("failed to edit response with error: %w (original error: %s)", interactionErr, err)
		}

		return nil // Already handled error by editing message.
	}

	// Build the claim request up-front; we'll attempt an inline claim before
	// responding so the embed can include the run URL when GitHub is fast.
	userID := interactionUserID(i)

	workflowName := getClientToWorkflowName(targetName)
	workflowFile := fmt.Sprintf("build-push-%s.yml", workflowName)

	var (
		upstreamRepo string
		manifests    []ManifestInfo
	)

	if allWorkflows, wfErr := c.workflowFetcher.GetAllWorkflows(); wfErr == nil {
		if workflow, exists := allWorkflows[workflowName]; exists {
			upstreamRepo = workflow.UpstreamRepo
			manifests = workflow.Manifests
		}
	}

	claimReq := ClaimRequest{
		UserID:          userID,
		TargetDisplay:   targetDisplayName,
		WorkflowFile:    workflowFile,
		CorrelationID:   correlationID,
		HTMLURL:         workflowURL,
		RepositoryInput: repository,
		RefInput:        ref,
		DockerTagInput:  dockerTag,
		UpstreamRepo:    upstreamRepo,
		Manifests:       manifests,
	}

	// Try to resolve the run URL inline (short timeout). If GitHub is slow,
	// we render without a link and let the background claim relink later.
	var (
		inlineRunURL  string
		inlineClaimed bool
	)

	if userID != "" {
		inlineCtx, inlineCancel := context.WithTimeout(context.Background(), inlineClaimTimeout)

		runURL, claimErr := c.watcher.Claim(inlineCtx, claimReq)

		inlineCancel()

		if claimErr == nil {
			inlineRunURL = runURL
			inlineClaimed = true
		} else {
			c.log.WithError(claimErr).WithFields(logrus.Fields{
				"workflow":       workflowFile,
				"correlation_id": correlationID,
			}).Debug("Inline claim timed out; falling back to background claim")
		}
	}

	embed := buildTriggeredEmbed(triggeredEmbedInput{
		targetName:        targetName,
		targetDisplayName: targetDisplayName,
		repository:        repository,
		ref:               ref,
		dockerTag:         dockerTag,
		buildArgs:         buildArgs,
		runURL:            inlineRunURL,
		isClient:          isClient,
		cartographoor:     c.bot.GetCartographoor(),
	})

	if _, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: new(""),
		Embeds:  &[]*discordgo.MessageEmbed{embed},
	}); err != nil {
		return fmt.Errorf("failed to edit response: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"workflow":     targetName,
		"repository":   repository,
		"ref":          ref,
		"docker_tag":   dockerTag,
		"build_args":   buildArgs,
		"inline_claim": inlineClaimed,
	}).Info("Build triggered")

	// If the inline claim didn't land (timeout or unknown user), keep claiming
	// in the background so the run still gets tracked for the completion DM.
	// We deliberately do NOT edit the triggered message again — the inline
	// embed is the user's single, final reply. They'll get the run URL via
	// the completion DM when the build finishes.
	if userID != "" && !inlineClaimed {
		go func() {
			claimCtx, cancel := context.WithTimeout(context.Background(), claimTimeout)
			defer cancel()

			if _, claimErr := c.watcher.Claim(claimCtx, claimReq); claimErr != nil {
				c.log.WithError(claimErr).WithFields(logrus.Fields{
					"workflow":       workflowFile,
					"correlation_id": correlationID,
					"user":           userID,
				}).Warn("Failed to claim build run for completion DM")
			}
		}()
	}

	return nil
}

// triggeredEmbedInput bundles the values used to render the
// "Build Triggered" embed, including an optional run URL that is included as
// a "Run" field only when non-empty.
type triggeredEmbedInput struct {
	targetName        string
	targetDisplayName string
	repository        string
	ref               string
	dockerTag         string
	buildArgs         string
	runURL            string
	isClient          bool
	cartographoor     cartographoorService
}

// cartographoorService captures the subset of *cartographoor.Service that
// buildTriggeredEmbed needs, so the helper stays decoupled from the bot.
type cartographoorService interface {
	IsELClient(string) bool
	IsCLClient(string) bool
	GetClientLogo(string) string
}

func buildTriggeredEmbed(in triggeredEmbedInput) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title: fmt.Sprintf("🏗️ Build Triggered: %s", in.targetDisplayName),
		Color: buildEmbedColor,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "Repository",
				Value:  fmt.Sprintf("`%s`", in.repository),
				Inline: true,
			},
			{
				Name:   "Branch/Tag",
				Value:  fmt.Sprintf("`%s`", in.ref),
				Inline: true,
			},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if in.runURL != "" {
		embed.URL = in.runURL
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Run",
			Value:  fmt.Sprintf("[View on GitHub](%s)", in.runURL),
			Inline: false,
		})
	}

	if in.dockerTag != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Docker Tag",
			Value:  fmt.Sprintf("`%s`", in.dockerTag),
			Inline: true,
		})
	}

	if in.buildArgs != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "Build Args",
			Value:  fmt.Sprintf("`%s`", in.buildArgs),
			Inline: true,
		})
	}

	if in.isClient && in.cartographoor != nil && (in.cartographoor.IsELClient(in.targetName) || in.cartographoor.IsCLClient(in.targetName)) {
		if logo := in.cartographoor.GetClientLogo(in.targetName); logo != "" {
			embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: logo}
		}
	} else {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: "https://pbs.twimg.com/profile_images/1772523356123959296/e9mkwqVp_400x400.jpg",
		}
	}

	return embed
}

// triggerWorkflow triggers the GitHub workflow for the given build target.
func (c *BuildCommand) triggerWorkflow(buildTarget, repository, ref, dockerTag, buildArgs, correlationID string) (string, error) {
	// Prepare the workflow inputs.
	inputs := map[string]any{
		"repository": repository,
		"ref":        ref,
	}

	if dockerTag != "" {
		inputs["docker_tag"] = dockerTag
	}

	if buildArgs != "" {
		inputs["build_args"] = buildArgs
	}

	if correlationID != "" {
		inputs["correlation_id"] = correlationID
	}

	body := map[string]any{
		"ref":    "master", // `master` of DefaultRepository.
		"inputs": inputs,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Determine the workflow path based on the build target
	// Use helper function to handle client-to-workflow name mapping
	workflowName := getClientToWorkflowName(buildTarget)

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

// interactionUserID extracts the invoking user's Discord ID from an interaction,
// handling both guild and DM contexts.
func interactionUserID(i *discordgo.InteractionCreate) string {
	if i == nil {
		return ""
	}

	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}

	if i.User != nil {
		return i.User.ID
	}

	return ""
}
