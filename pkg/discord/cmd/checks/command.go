package checks

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/sirupsen/logrus"
)

// ChecksCommand handles the /checks command.
type ChecksCommand struct {
	log *logrus.Logger
	bot common.BotContext
}

// NewChecksCommand creates a new ChecksCommand.
func NewChecksCommand(log *logrus.Logger, bot common.BotContext) *ChecksCommand {
	return &ChecksCommand{
		log: log,
		bot: bot,
	}
}

// Name returns the name of the command.
func (c *ChecksCommand) Name() string {
	return "checks"
}

// Register registers the /checks command with the given discord session.
func (c *ChecksCommand) Register(session *discordgo.Session) error {
	var (
		networkChoices = c.getNetworkChoices()
		clientChoices  = c.getClientChoices()
	)

	if _, err := session.ApplicationCommandCreate(session.State.User.ID, "", &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: "Manage network client health checks",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "run",
				Description: "Run a specific health check",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "network",
						Description: "Network to check",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     networkChoices,
					},
					{
						Name:        "client",
						Description: "Client to check",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     clientChoices,
					},
				},
			},
			{
				Name:        "register",
				Description: "Register health checks",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "network",
						Description: "Network to monitor",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     networkChoices,
					},
					{
						Name:        "channel",
						Description: "Channel to send alerts to",
						Type:        discordgo.ApplicationCommandOptionChannel,
						Required:    true,
					},
					{
						Name:        "client",
						Description: "Specific client to monitor (optional)",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    false,
						Choices:     clientChoices,
					},
				},
			},
			{
				Name:        "deregister",
				Description: "Deregister health checks",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "network",
						Description: "Network to stop monitoring",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     networkChoices,
					},
					{
						Name:        "client",
						Description: "Specific client to stop monitoring (optional)",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    false,
						Choices:     clientChoices,
					},
				},
			},
			{
				Name:        "list",
				Description: "List registered health checks",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "network",
						Description: "Network to list checks for (optional)",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    false,
						Choices:     networkChoices,
					},
				},
			},
		},
	}); err != nil {
		return fmt.Errorf("failed to register checks command: %w", err)
	}

	return nil
}

// Handle handles the /checks command.
func (c *ChecksCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	var err error
	switch data.Options[0].Name {
	case "run":
		err = c.handleRun(s, i, data.Options[0])
	case "register":
		err = c.handleRegister(s, i, data.Options[0])
	case "deregister":
		err = c.handleDeregister(s, i, data.Options[0])
	case "list":
		err = c.handleList(s, i, data.Options[0])
	}

	if err != nil {
		c.log.Errorf("Command failed: %v", err)

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Command failed: %v", err),
			},
		})
	}
}
