package deploy

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/sirupsen/logrus"
)

// DeployCommand handles the /deploy command.
type DeployCommand struct {
	log        *logrus.Logger
	bot        common.BotContext
	httpClient *http.Client
}

// NewDeployCommand creates a new deploy command.
func NewDeployCommand(log *logrus.Logger, bot common.BotContext, client *http.Client) *DeployCommand {
	return &DeployCommand{
		log:        log,
		bot:        bot,
		httpClient: client,
	}
}

// Name returns the name of the command.
func (c *DeployCommand) Name() string {
	return "deploy"
}

// Register registers the /deploy command with the given discord session.
func (c *DeployCommand) Register(session *discordgo.Session) error {
	if _, err := session.ApplicationCommandCreate(session.State.User.ID, "", &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: "Deploy Docker image to network nodes",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "network",
				Description: "Network to deploy to (e.g., pectra-devnet-6)",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
			},
			{
				Name:        "client",
				Description: "Client to deploy (e.g., grandine, lighthouse, etc.)",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
			},
			{
				Name:        "docker_tag",
				Description: "Docker tag to deploy",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
			},
			{
				Name:        "dry_run",
				Description: "Only show what would be done, without executing (default: false)",
				Type:        discordgo.ApplicationCommandOptionBoolean,
				Required:    false,
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to register deploy command: %w", err)
	}

	return nil
}

// Handle handles the /deploy command.
func (c *DeployCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	logCtx := logrus.Fields{
		"command": c.Name(),
		"guild":   i.GuildID,
		"user":    i.Member.User.Username,
		"roles":   common.GetRoleNames(i.Member, s, i.GuildID),
	}

	// Check permissions
	if !c.hasPermission(i.Member, s, i.GuildID, c.bot.GetRoleConfig()) {
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: common.NoPermissionError(c.Name()).Error(),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		}); err != nil {
			c.log.WithError(err).Error("Failed to respond with permission error")
		}

		c.log.WithFields(logCtx).Error("Permission denied")
		return
	}

	// Acknowledge the interaction immediately
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})
	if err != nil {
		c.log.WithError(err).Error("Failed to acknowledge interaction")
		return
	}

	// Extract parameters
	var network, client, dockerTag string
	var dryRun bool

	for _, opt := range data.Options {
		switch opt.Name {
		case "network":
			network = opt.StringValue()
		case "client":
			client = opt.StringValue()
		case "docker_tag":
			dockerTag = opt.StringValue()
		case "dry_run":
			dryRun = opt.BoolValue()
		}
	}

	// Initial message to the user
	_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content: stringPtr(fmt.Sprintf("🔄 Preparing deployment of `%s` for client `%s` on network `%s`...",
			dockerTag, client, network)),
	})
	if err != nil {
		c.log.WithError(err).Error("Failed to update initial message")
	}

	// If dry run, just list what would be done without executing
	if dryRun {
		dryRunMsg, err := c.prepareDryRun(network, client, dockerTag)
		if err != nil {
			c.log.WithFields(logCtx).WithError(err).Error("Dry run preparation failed")

			_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: stringPtr(fmt.Sprintf("❌ Dry run failed: %v", err)),
			})

			return
		}

		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("🔍 **[DRY RUN]** Here's what would be deployed:\n\n%s", dryRunMsg)),
		})
		if err != nil {
			c.log.WithError(err).Error("Failed to update dry run message")
		}

		return
	}

	// Process the deployment
	progressChan := make(chan string)
	resultChan := make(chan struct {
		message string
		err     error
	})

	go func() {
		// Launch the deployment in a goroutine
		result, err := c.deployWithProgress(network, client, dockerTag, progressChan)
		resultChan <- struct {
			message string
			err     error
		}{message: result, err: err}
	}()

	// Set up a ticker to update the Discord message with progress
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	latestProgress := fmt.Sprintf("🔄 Starting deployment of `%s` for client `%s` on network `%s`...",
		dockerTag, client, network)

	for {
		select {
		case progress := <-progressChan:
			// Update the progress message
			latestProgress = progress

			_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: stringPtr(latestProgress),
			})
			if err != nil {
				c.log.WithError(err).Error("Failed to update progress message")
			}

		case result := <-resultChan:
			// Deployment completed
			if result.err != nil {
				c.log.WithFields(logCtx).WithError(result.err).Error("Deployment failed")

				_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: stringPtr(fmt.Sprintf("❌ Deployment failed: %v", result.err)),
				})
				if err != nil {
					c.log.WithError(err).Error("Failed to update failure message")
				}
			} else {
				// Success
				_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: stringPtr(result.message),
				})
				if err != nil {
					c.log.WithError(err).Error("Failed to update success message")
				}
			}

			return

		case <-ticker.C:
			// Regularly update the message with the latest progress
			_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
				Content: stringPtr(latestProgress),
			})
			if err != nil {
				c.log.WithError(err).Error("Failed to refresh progress message")
			}
		}
	}
}

// hasPermission checks if a member has permission to execute the deploy command.
func (c *DeployCommand) hasPermission(member *discordgo.Member, session *discordgo.Session, guildID string, config *common.RoleConfig) bool {
	// Check admin roles first
	for _, roleID := range member.Roles {
		role, err := session.State.Role(guildID, roleID)
		if err != nil {
			continue
		}

		if config.AdminRoles[strings.ToLower(role.Name)] {
			return true
		}
	}

	// For now, only admins can use the deploy command
	return false
}

// stringPtr converts a string to a string pointer.
func stringPtr(s string) *string {
	return &s
}
