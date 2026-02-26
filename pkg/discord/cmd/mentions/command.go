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
	guildRegistrations  map[string]string // Maps guild ID to registered command ID for updates
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

// getCommandDefinition returns the application command definition.
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

// Register registers the /mentions command with the given discord session (globally).
func (c *MentionsCommand) Register(session *discordgo.Session) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, "", c.getCommandDefinition())
	if err != nil {
		return err
	}

	if c.guildRegistrations == nil {
		c.guildRegistrations = make(map[string]string, 1)
	}

	c.guildRegistrations[""] = cmd.ID

	return nil
}

// RegisterWithGuild registers the /mentions command with a specific guild.
func (c *MentionsCommand) RegisterWithGuild(session *discordgo.Session, guildID string) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, guildID, c.getCommandDefinition())
	if err != nil {
		return fmt.Errorf("failed to register mentions command to guild %s: %w", guildID, err)
	}

	if c.guildRegistrations == nil {
		c.guildRegistrations = make(map[string]string, 2)
	}

	c.guildRegistrations[guildID] = cmd.ID

	c.log.WithField("guild", guildID).Info("Registered mentions command to guild")

	return nil
}

// UpdateChoices updates the command choices by editing the existing command with fresh network and client data.
func (c *MentionsCommand) UpdateChoices(session *discordgo.Session) error {
	if len(c.guildRegistrations) == 0 {
		c.log.Warn("No command registrations stored, cannot update choices")

		return nil
	}

	definition := c.getCommandDefinition()

	for guildID, commandID := range c.guildRegistrations {
		if _, err := session.ApplicationCommandEdit(session.State.User.ID, guildID, commandID, definition); err != nil {
			return fmt.Errorf("failed to update mentions command choices for guild %s: %w", guildID, err)
		}

		if guildID != "" {
			c.log.WithField("guild", guildID).Debug("Updated mentions command choices for guild")
		} else {
			c.log.Debug("Updated mentions command choices globally")
		}
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
