package hive

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/queue"
	"github.com/sirupsen/logrus"
)

const (
	threadAutoArchiveDuration = 60 // 1 hour.
	threadDateFormat          = "2006-01-02"
)

// HiveCommand handles the /hive command.
type HiveCommand struct {
	log   *logrus.Logger
	bot   common.BotContext
	queue *queue.AlertQueue
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

// Register registers the command with Discord.
func (c *HiveCommand) Register(session *discordgo.Session) error {
	// Get network choices for dropdowns
	networkChoices := c.getNetworkChoices()

	_, err := session.ApplicationCommandCreate(
		session.State.User.ID,
		"",
		&discordgo.ApplicationCommand{
			Name:        c.Name(),
			Description: "Manage Hive test summaries",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "register",
					Description: "Register a Hive summary alert",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "network",
							Description: "The network to monitor",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    true,
							Choices:     networkChoices,
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
							Name:        "network",
							Description: "The network to stop monitoring",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    true,
							Choices:     networkChoices,
						},
					},
				},
				{
					Name:        "run",
					Description: "Run a Hive summary check",
					Type:        discordgo.ApplicationCommandOptionSubCommand,
					Options: []*discordgo.ApplicationCommandOption{
						{
							Name:        "network",
							Description: "The network to check",
							Type:        discordgo.ApplicationCommandOptionString,
							Required:    true,
							Choices:     networkChoices,
						},
					},
				},
			},
		},
	)

	return err
}

// Handle handles the command.
func (c *HiveCommand) Handle(s *discordgo.Session, i *discordgo.InteractionCreate) {
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
	results, err := c.bot.GetHive().FetchTestResults(ctx, alert.Network)
	if err != nil {
		return fmt.Errorf("failed to fetch test results: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"network":     alert.Network,
		"resultCount": len(results),
	}).Info("Fetched test results")

	// Debug: Print out the first few results to see what we're getting
	if len(results) > 0 {
		for i := 0; i < min(5, len(results)); i++ {
			c.log.WithFields(logrus.Fields{
				"index":       i,
				"name":        results[i].Name,
				"client":      results[i].Client,
				"version":     cleanVersionString(results[i].Version),
				"testCount":   results[i].NTests,
				"passCount":   results[i].Passes,
				"failCount":   results[i].Fails,
				"testSuiteID": results[i].TestSuiteID,
				"fileName":    results[i].FileName,
				"timestamp":   results[i].Timestamp.Format(time.RFC3339),
			}).Info("Sample test result")
		}
	}

	// Process results into a summary
	summary := c.bot.GetHive().ProcessSummary(results)
	if summary == nil {
		return fmt.Errorf("failed to process summary: no results available")
	}

	// Debug: Print out the client results
	c.log.WithFields(logrus.Fields{
		"clientCount": len(summary.ClientResults),
		"clients":     fmt.Sprintf("%v", getClientNames(summary)),
	}).Info("Processed client results")

	// Get previous summary for comparison
	prevSummary, err := c.bot.GetHiveSummaryRepo().GetPreviousSummaryResult(ctx, alert.Network)
	if err != nil {
		c.log.WithError(err).Warn("Failed to get previous summary, continuing without comparison")
	} else if prevSummary != nil {
		c.log.WithFields(logrus.Fields{
			"currentDate":  summary.Timestamp.Format("2006-01-02"),
			"previousDate": prevSummary.Timestamp.Format("2006-01-02"),
		}).Info("Comparing with previous summary")

		// Skip if we're comparing with the same summary
		if summary.Timestamp.Equal(prevSummary.Timestamp) {
			c.log.Warn("Current and previous summaries have the same timestamp, skipping comparison")
			prevSummary = nil
		}
	}

	// Store the new summary
	if err := c.bot.GetHiveSummaryRepo().StoreSummaryResult(ctx, summary); err != nil {
		c.log.WithError(err).Warn("Failed to store summary, continuing")
	}

	// Send the summary to Discord
	if err := c.sendHiveSummary(ctx, alert, summary, prevSummary, results); err != nil {
		return fmt.Errorf("failed to send summary: %w", err)
	}

	return nil
}

// Helper function for min since Go <1.21 doesn't have it in the standard library
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Helper functions
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

func getClientNames(summary *hive.SummaryResult) []string {
	names := make([]string, 0, len(summary.ClientResults))
	for name := range summary.ClientResults {
		names = append(names, name)
	}
	return names
}
