package build

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultRepository is the default repository for the eth-client-docker-image-builder.
	DefaultRepository = "ethpandaops/eth-client-docker-image-builder"
)

// BuildCommand handles the /build command.
type BuildCommand struct {
	log         *logrus.Logger
	bot         common.BotContext
	githubToken string
}

// NewBuildCommand creates a new build command.
func NewBuildCommand(log *logrus.Logger, bot common.BotContext, githubToken string) *BuildCommand {
	return &BuildCommand{
		log:         log,
		bot:         bot,
		githubToken: githubToken,
	}
}

// Name returns the name of the command.
func (c *BuildCommand) Name() string {
	return "build"
}

// Register registers the /build command with the given discord session.
func (c *BuildCommand) Register(session *discordgo.Session) error {
	var (
		clientChoices = c.getClientChoices()
	)

	if _, err := session.ApplicationCommandCreate(session.State.User.ID, "", &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: "Trigger client docker image builds",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "trigger",
				Description: "Trigger a build for a specific client",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "client",
						Description: "Client to build",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     clientChoices,
					},
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
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to register build command: %w", err)
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
	case "trigger":
		err = c.handleTrigger(s, i, data.Options[0])
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
