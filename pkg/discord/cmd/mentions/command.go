package mentions

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/sirupsen/logrus"
)

// MentionsCommand handles the /mentions command.
type MentionsCommand struct {
	log                 *logrus.Logger
	bot                 common.BotContext
	autocompleteHandler *common.AutocompleteHandler
	commandID           string // Store the registered command ID for updates
}

// NewMentionsCommand creates a new MentionsCommand.
func NewMentionsCommand(log *logrus.Logger, bot common.BotContext) *MentionsCommand {
	return &MentionsCommand{
		log:                 log,
		bot:                 bot,
		autocompleteHandler: common.NewAutocompleteHandler(bot, log),
	}
}

// Name returns the name of the command.
func (c *MentionsCommand) Name() string {
	return "mentions"
}

// getCommandDefinition returns the application command definition with current choices.
func (c *MentionsCommand) getCommandDefinition() *discordgo.ApplicationCommand {
	clientChoices := c.getClientChoices()

	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: "Manage client team mentions",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "add",
				Description: "Add mentions for user(s), for a specific client on a specific network",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:         "network",
						Description:  "Network to add mentions for",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     true,
						Autocomplete: true,
					},
					{
						Name:        "client",
						Description: "Client to add mentions for",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     clientChoices,
					},
					{
						Name:        "handles",
						Description: "Handles of users or roles to add mentions for (space separated)",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
					},
				},
			},
			{
				Name:        "remove",
				Description: "Remove mentions for user(s), for a specific client on a specific network",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:         "network",
						Description:  "Network to remove mentions from",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     true,
						Autocomplete: true,
					},
					{
						Name:        "client",
						Description: "Client to remove mentions from",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     clientChoices,
					},
					{
						Name:        "handles",
						Description: "Handles of users or roles to remove mentions for (space separated)",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
					},
				},
			},
			{
				Name:        "list",
				Description: "Returns a list of which clients have which mentions enabled",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:         "network",
						Description:  "Network to list mentions for (optional)",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     false,
						Autocomplete: true,
					},
				},
			},
			{
				Name:        "enable",
				Description: "Enable all mentions for a specific client on a specific network",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:         "network",
						Description:  "Network to enable mentions for",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     true,
						Autocomplete: true,
					},
					{
						Name:        "client",
						Description: "Client to enable mentions for",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     clientChoices,
					},
				},
			},
			{
				Name:        "disable",
				Description: "Disable all mentions for a specific client on a specific network",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:         "network",
						Description:  "Network to disable mentions for",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     true,
						Autocomplete: true,
					},
					{
						Name:        "client",
						Description: "Client to disable mentions for",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
						Choices:     clientChoices,
					},
				},
			},
		},
	}
}

// Register registers the /mentions command with the given discord session.
func (c *MentionsCommand) Register(session *discordgo.Session) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, "", c.getCommandDefinition())
	if err != nil {
		return err
	}

	// Store the command ID for future updates
	c.commandID = cmd.ID

	return nil
}

// UpdateChoices updates the command choices by editing the existing command with fresh network and client data.
func (c *MentionsCommand) UpdateChoices(session *discordgo.Session) error {
	// If we don't have a command ID, we can't update choices
	if c.commandID == "" {
		c.log.Warn("No command ID stored, cannot update choices")

		return nil
	}

	// Use the same command definition as Register
	_, err := session.ApplicationCommandEdit(session.State.User.ID, "", c.commandID, c.getCommandDefinition())
	if err != nil {
		return fmt.Errorf("failed to update mentions command choices: %w", err)
	}

	return nil
}

// Handle handles the /mentions command.
func (c *MentionsCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Handle autocomplete interactions
	if i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		c.autocompleteHandler.HandleNetworkAutocomplete(s, i, c.Name())

		return
	}

	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	var err error

	switch data.Options[0].Name {
	case "add":
		err = c.handleAdd(s, i, data.Options[0])
	case "remove":
		err = c.handleRemove(s, i, data.Options[0])
	case "list":
		err = c.handleList(s, i, data.Options[0])
	case "enable":
		err = c.handleEnable(s, i, data.Options[0])
	case "disable":
		err = c.handleDisable(s, i, data.Options[0])
	}

	if err != nil {
		c.log.Errorf("Command failed: %v", err)

		respErr := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Command failed: %v", err),
			},
		})
		if respErr != nil {
			c.log.Errorf("Failed to respond to interaction: %v", respErr)
		}
	}
}
