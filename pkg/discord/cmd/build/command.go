package build

import (
	"fmt"
	"net/http"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultRepository is the default repository for the eth-client-docker-image-builder.
	DefaultRepository = "ethpandaops/eth-client-docker-image-builder"

	// Subcommand names.
	subcommandClientCL = "client-cl"
	subcommandClientEL = "client-el"
	subcommandTool     = "tool"

	// Option names.
	optionClient   = "client"
	optionWorkflow = "workflow"
)

// BuildCommand handles the /build command.
type BuildCommand struct {
	log                *logrus.Logger
	bot                common.BotContext
	githubToken        string
	httpClient         *http.Client
	workflowFetcher    *WorkflowFetcher
	guildRegistrations map[string]string // Maps guild ID to registered command ID for updates
}

// NewBuildCommand creates a new build command.
func NewBuildCommand(log *logrus.Logger, bot common.BotContext, githubToken string, client *http.Client) *BuildCommand {
	return &BuildCommand{
		log:             log,
		bot:             bot,
		githubToken:     githubToken,
		httpClient:      client,
		workflowFetcher: NewWorkflowFetcher(client, githubToken, log, bot),
	}
}

// Name returns the name of the command.
func (c *BuildCommand) Name() string {
	return "build"
}

// getCommandDefinition returns the application command definition.
func (c *BuildCommand) getCommandDefinition() *discordgo.ApplicationCommand {
	var (
		clClientChoices = c.getCLClientChoices()
		elClientChoices = c.getELClientChoices()
		toolsChoices    = c.getToolsChoices()
	)

	// Options that are common to all subcommands
	commonOptions := []*discordgo.ApplicationCommandOption{
		{
			Name:        "repository",
			Description: "Source repository to build from",
			Type:        discordgo.ApplicationCommandOptionString,
			Required:    false,
		},
		{
			Name:        "ref",
			Description: "Branch, tag or SHA to build from",
			Type:        discordgo.ApplicationCommandOptionString,
			Required:    false,
		},
		{
			Name:        "docker_tag",
			Description: "Override target docker tag",
			Type:        discordgo.ApplicationCommandOptionString,
			Required:    false,
		},
		{
			Name:        "build_args",
			Description: "Build arguments to pass to the Docker build (key=value,...)",
			Type:        discordgo.ApplicationCommandOptionString,
			Required:    false,
		},
	}

	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: "Trigger docker image builds",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        subcommandClientCL,
				Description: "Trigger a build for a consensus layer client",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: append([]*discordgo.ApplicationCommandOption{
					{
						Name:        optionClient,
						Description: "Consensus client to build",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     clClientChoices,
					},
				}, commonOptions...),
			},
			{
				Name:        subcommandClientEL,
				Description: "Trigger a build for an execution layer client",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: append([]*discordgo.ApplicationCommandOption{
					{
						Name:        optionClient,
						Description: "Execution client to build",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     elClientChoices,
					},
				}, commonOptions...),
			},
			{
				Name:        subcommandTool,
				Description: "Trigger a build for a tool or utility",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: append([]*discordgo.ApplicationCommandOption{
					{
						Name:        optionWorkflow,
						Description: "Tool workflow to build",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     toolsChoices,
					},
				}, commonOptions...),
			},
		},
	}
}

// Register registers the /build command with the given discord session (globally).
func (c *BuildCommand) Register(session *discordgo.Session) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, "", c.getCommandDefinition())
	if err != nil {
		return fmt.Errorf("failed to register build command: %w", err)
	}

	if c.guildRegistrations == nil {
		c.guildRegistrations = make(map[string]string, 1)
	}

	c.guildRegistrations[""] = cmd.ID

	return nil
}

// RegisterWithGuild registers the /build command with a specific guild.
func (c *BuildCommand) RegisterWithGuild(session *discordgo.Session, guildID string) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, guildID, c.getCommandDefinition())
	if err != nil {
		return fmt.Errorf("failed to register build command to guild %s: %w", guildID, err)
	}

	if c.guildRegistrations == nil {
		c.guildRegistrations = make(map[string]string, 2)
	}

	c.guildRegistrations[guildID] = cmd.ID

	c.log.WithField("guild", guildID).Info("Registered build command to guild")

	return nil
}

// UpdateChoices updates the command choices by editing the existing command with fresh client and tool data.
func (c *BuildCommand) UpdateChoices(session *discordgo.Session) error {
	if len(c.guildRegistrations) == 0 {
		c.log.Warn("No command registrations stored, cannot update choices")

		return nil
	}

	// Refresh the workflow cache to get latest workflows from GitHub.
	if err := c.workflowFetcher.RefreshCache(); err != nil {
		c.log.WithError(err).Warn("Failed to refresh workflow cache, using existing data")
	}

	definition := c.getCommandDefinition()

	for guildID, commandID := range c.guildRegistrations {
		if _, err := session.ApplicationCommandEdit(session.State.User.ID, guildID, commandID, definition); err != nil {
			return fmt.Errorf("failed to update build command choices for guild %s: %w", guildID, err)
		}

		if guildID != "" {
			c.log.WithField("guild", guildID).Debug("Updated build command choices for guild")
		} else {
			c.log.Debug("Updated build command choices globally")
		}
	}

	return nil
}

// Handle handles the /build command.
func (c *BuildCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	logCtx := logrus.Fields{
		"subcommand": data.Options[0].Name,
		"command":    c.Name(),
		"guild":      i.GuildID,
		"user":       i.Member.User.Username,
		"roles":      common.GetRoleNames(i.Member, s, i.GuildID),
	}

	// Check permissions before executing command.
	if !c.hasPermission(i.Member, s, i.GuildID, c.bot.GetRoleConfig()) {
		if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: common.NoPermissionError(fmt.Sprintf("%s %s", c.Name(), data.Options[0].Name)).Error(),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		}); err != nil {
			c.log.WithError(err).Error("Failed to respond with permission error")
		}

		c.log.WithFields(logCtx).Error("Permission denied")

		return
	}

	var err error

	switch data.Options[0].Name {
	case subcommandClientCL, subcommandClientEL, subcommandTool:
		err = c.handleBuild(s, i, data.Options[0])
	}

	if err != nil {
		c.log.Errorf("Command failed: %v", err)

		respErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Command failed: %v", err),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		if respErr != nil {
			c.log.Errorf("Failed to respond to interaction: %v", respErr)
		}
	}
}
