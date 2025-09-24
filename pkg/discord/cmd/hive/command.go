package hive

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/queue"
	"github.com/sirupsen/logrus"
)

const (
	threadAutoArchiveDuration = 60 // 1 hour.
	threadDateFormat          = "2006-01-02"
	optionNameNetwork         = "network"
	optionNameSuite           = "suite"
)

// HiveCommand handles the /hive command.
type HiveCommand struct {
	log       *logrus.Logger
	bot       common.BotContext
	queue     *queue.AlertQueue
	commandID string // Store the registered command ID for updates
	guildID   string // Store the guild ID for guild-specific registration
}

// NewHiveCommand creates a new hive command.
func NewHiveCommand(log *logrus.Logger, bot common.BotContext) *HiveCommand {
	cmd := &HiveCommand{
		log: log,
		bot: bot,
	}

	return cmd
}

// Name returns the name of the command.
func (c *HiveCommand) Name() string {
	return "hive"
}

// Queue returns the alert queue.
func (c *HiveCommand) Queue() *queue.AlertQueue {
	return c.queue
}

// getCommandDefinition returns the application command definition.
func (c *HiveCommand) getCommandDefinition() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        c.Name(),
		Description: "Manage Hive test summaries",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "register",
				Description: "Register a Hive summary alert",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:         "network",
						Description:  "The network to monitor",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     true,
						Autocomplete: true,
					},
					{
						Name:        "channel",
						Description: "Channel to send alerts to",
						Type:        discordgo.ApplicationCommandOptionChannel,
						Required:    true,
						ChannelTypes: []discordgo.ChannelType{
							discordgo.ChannelTypeGuildText,
						},
					},
					{
						Name:         "suite",
						Description:  "Filter by specific test suite (optional)",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     false,
						Autocomplete: true,
					},
					{
						Name:        "schedule",
						Description: "The schedule to run the check (cron format)",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    false,
					},
				},
			},
			{
				Name:        "deregister",
				Description: "Deregister a Hive summary alert",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:         "network",
						Description:  "The network to stop monitoring",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     true,
						Autocomplete: true,
					},
					{
						Name:         "suite",
						Description:  "Filter by specific test suite (optional)",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     false,
						Autocomplete: true,
					},
				},
			},
			{
				Name:        "list",
				Description: "List all registered Hive summary alerts",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
			},
			{
				Name:        "run",
				Description: "Run a Hive summary check immediately",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:         "network",
						Description:  "The network to check",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     true,
						Autocomplete: true,
					},
					{
						Name:         "suite",
						Description:  "Filter by specific test suite (optional)",
						Type:         discordgo.ApplicationCommandOptionString,
						Required:     false,
						Autocomplete: true,
					},
				},
			},
		},
	}
}

// Register registers the command with Discord (globally).
func (c *HiveCommand) Register(session *discordgo.Session) error {
	cmd, err := session.ApplicationCommandCreate(
		session.State.User.ID,
		"",
		c.getCommandDefinition(),
	)
	if err != nil {
		return err
	}

	// Store the command ID for future updates
	c.commandID = cmd.ID
	c.guildID = "" // Global command

	return nil
}

// RegisterWithGuild registers the /hive command with a specific guild.
func (c *HiveCommand) RegisterWithGuild(session *discordgo.Session, guildID string) error {
	cmd, err := session.ApplicationCommandCreate(session.State.User.ID, guildID, c.getCommandDefinition())
	if err != nil {
		return fmt.Errorf("failed to register hive command to guild %s: %w", guildID, err)
	}

	// Store the command ID and guild ID for future updates
	c.commandID = cmd.ID
	c.guildID = guildID

	c.log.WithField("guild", guildID).Info("Registered hive command to guild")

	return nil
}

// UpdateChoices updates the command choices by editing the existing command with fresh network data.
func (c *HiveCommand) UpdateChoices(session *discordgo.Session) error {
	// If we don't have a command ID, we can't update choices
	if c.commandID == "" {
		c.log.Warn("No command ID stored, cannot update choices")

		return nil
	}

	// Use the stored guild ID (empty string for global commands)
	_, err := session.ApplicationCommandEdit(session.State.User.ID, c.guildID, c.commandID, c.getCommandDefinition())
	if err != nil {
		return fmt.Errorf("failed to update hive command choices for guild %s: %w", c.guildID, err)
	}

	if c.guildID != "" {
		c.log.WithField("guild", c.guildID).Debug("Updated hive command choices for guild")
	} else {
		c.log.Debug("Updated hive command choices globally")
	}

	return nil
}

// Handle handles the command.
func (c *HiveCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Handle autocomplete interactions
	if i.Type == discordgo.InteractionApplicationCommandAutocomplete {
		// Find the focused option
		data := i.ApplicationCommandData()
		if data.Name == c.Name() {
			focusedOption := c.findFocusedOption(data.Options)
			if focusedOption != nil {
				switch focusedOption.Name {
				case optionNameNetwork:
					c.handleNetworkAutocomplete(s, i)
				case optionNameSuite:
					c.handleSuiteAutocomplete(s, i)
				}
			}
		}

		return
	}

	// Only respond to application commands
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	// Get the subcommand
	if len(data.Options) == 0 {
		c.respondWithError(s, i, "No subcommand provided")

		return
	}

	subCmd := data.Options[0]
	switch subCmd.Name {
	case "register":
		c.handleRegister(s, i, subCmd)
	case "deregister":
		c.handleDeregister(s, i, subCmd)
	case "list":
		if err := c.handleList(s, i, subCmd); err != nil {
			c.respondWithError(s, i, err.Error())
		}
	case "run":
		c.handleRun(s, i, subCmd)
	default:
		c.respondWithError(s, i, fmt.Sprintf("Unknown subcommand: %s", subCmd.Name))
	}
}

// RunHiveSummary runs a Hive summary check for a given alert.
func (c *HiveCommand) RunHiveSummary(ctx context.Context, alert *hive.HiveSummaryAlert) error {
	c.log.WithFields(logrus.Fields{
		"network": alert.Network,
		"channel": alert.DiscordChannel,
		"guild":   alert.DiscordGuildID,
	}).Info("Running Hive summary check")

	// Fetch test results from Hive
	results, err := c.bot.GetHive().FetchTestResults(ctx, alert.Network, alert.Suite)
	if err != nil {
		return fmt.Errorf("failed to fetch test results: %w", err)
	}

	// Process results into a summary
	summary := c.bot.GetHive().ProcessSummary(results)
	if summary == nil {
		return fmt.Errorf("failed to process summary: no results available")
	}

	// Get previous summary for comparison.
	prevSummary, err := c.bot.GetHiveSummaryRepo().GetPreviousSummaryResultWithSuite(ctx, alert.Network, alert.Suite)
	if err != nil {
		c.log.WithError(err).Warn("Failed to get previous summary, continuing without comparison")
	} else if prevSummary != nil {
		// Skip if we're comparing with the same summary.
		if summary.Timestamp.Equal(prevSummary.Timestamp) {
			prevSummary = nil
		}
	}

	// Store the new summary.
	if err := c.bot.GetHiveSummaryRepo().StoreSummaryResultWithSuite(ctx, summary, alert.Suite); err != nil {
		c.log.WithError(err).Warn("Failed to store summary, continuing")
	}

	// Send the summary to Discord.
	if err := c.sendHiveSummary(ctx, alert, summary, prevSummary, results); err != nil {
		return fmt.Errorf("failed to send summary: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"result_count": len(results),
		"client_count": len(summary.ClientResults),
		"clients":      fmt.Sprintf("%v", getClientNames(summary)),
	}).Info("Processed Hive client test results, sent notification")

	return nil
}

// handleNetworkAutocomplete handles autocomplete for network selection using Hive discovery.
func (c *HiveCommand) handleNetworkAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	// Find the focused option
	focusedOption := c.findFocusedOption(data.Options)
	if focusedOption == nil || focusedOption.Name != optionNameNetwork {
		return
	}

	// Get the current input value
	inputValue := ""
	if focusedOption.Value != nil {
		inputValue = strings.ToLower(fmt.Sprintf("%v", focusedOption.Value))
	}

	// Fetch available networks from Hive discovery
	choices := c.buildHiveNetworkChoices(inputValue)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
	if err != nil {
		c.log.WithError(err).Error("Failed to respond to autocomplete")
	}
}

// findFocusedOption finds the currently focused option in the interaction data.
func (c *HiveCommand) findFocusedOption(options []*discordgo.ApplicationCommandInteractionDataOption) *discordgo.ApplicationCommandInteractionDataOption {
	for _, option := range options {
		if option.Type == discordgo.ApplicationCommandOptionSubCommand {
			for _, subOption := range option.Options {
				if subOption.Focused {
					return subOption
				}
			}
		}

		if option.Focused {
			return option
		}
	}

	return nil
}

// buildHiveNetworkChoices builds the autocomplete choices for networks from Hive discovery.
func (c *HiveCommand) buildHiveNetworkChoices(inputValue string) []*discordgo.ApplicationCommandOptionChoice {
	// Fetch networks from Hive discovery
	ctx := context.Background()

	networks, err := c.bot.GetHive().FetchAvailableNetworks(ctx)
	if err != nil {
		c.log.WithError(err).Warn("Failed to fetch Hive networks, falling back to empty list")

		return []*discordgo.ApplicationCommandOptionChoice{}
	}

	// Build choices - max 25 per Discord limits
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)

	for _, network := range networks {
		if inputValue == "" || strings.Contains(strings.ToLower(network), inputValue) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  network,
				Value: network,
			})
			if len(choices) >= 25 {
				break
			}
		}
	}

	return choices
}

// handleSuiteAutocomplete handles autocomplete for suite selection.
func (c *HiveCommand) handleSuiteAutocomplete(s *discordgo.Session, i *discordgo.InteractionCreate) {
	data := i.ApplicationCommandData()
	if data.Name != c.Name() {
		return
	}

	// Find the focused option
	focusedOption := c.findFocusedOption(data.Options)
	if focusedOption == nil || focusedOption.Name != optionNameSuite {
		return
	}

	// Get the current input value
	inputValue := ""
	if focusedOption.Value != nil {
		inputValue = strings.ToLower(fmt.Sprintf("%v", focusedOption.Value))
	}

	// Find the network value from the options to fetch suites for that network
	network := ""

	if len(data.Options) > 0 && data.Options[0].Type == discordgo.ApplicationCommandOptionSubCommand {
		for _, opt := range data.Options[0].Options {
			if opt.Name == optionNameNetwork && opt.Value != nil {
				network = fmt.Sprintf("%v", opt.Value)

				break
			}
		}
	}

	// If no network specified, return empty choices
	if network == "" {
		err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionApplicationCommandAutocompleteResult,
			Data: &discordgo.InteractionResponseData{
				Choices: []*discordgo.ApplicationCommandOptionChoice{},
			},
		})
		if err != nil {
			c.log.WithError(err).Error("Failed to respond to suite autocomplete")
		}

		return
	}

	// Fetch available suites for the network
	choices := c.buildHiveSuiteChoices(network, inputValue)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
	if err != nil {
		c.log.WithError(err).Error("Failed to respond to suite autocomplete")
	}
}

// buildHiveSuiteChoices builds the autocomplete choices for suites from a specific network.
func (c *HiveCommand) buildHiveSuiteChoices(network, inputValue string) []*discordgo.ApplicationCommandOptionChoice {
	// Fetch suites from Hive for the specific network
	ctx := context.Background()

	suites, err := c.bot.GetHive().FetchAvailableSuites(ctx, network)
	if err != nil {
		c.log.WithError(err).Warn("Failed to fetch Hive suites, falling back to empty list")

		return []*discordgo.ApplicationCommandOptionChoice{}
	}

	// Build choices - max 25 per Discord limits
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)

	for _, suite := range suites {
		if inputValue == "" || strings.Contains(strings.ToLower(suite), inputValue) {
			choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
				Name:  suite,
				Value: suite,
			})
			if len(choices) >= 25 {
				break
			}
		}
	}

	return choices
}

// respondWithError responds to the interaction with an error message.
func (c *HiveCommand) respondWithError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		c.log.WithError(err).Error("Failed to respond with error")
	}
}

// getClientNames returns the names of the clients in the summary.
func getClientNames(summary *hive.SummaryResult) []string {
	names := make([]string, 0, len(summary.ClientResults))

	for name := range summary.ClientResults {
		names = append(names, name)
	}

	return names
}
