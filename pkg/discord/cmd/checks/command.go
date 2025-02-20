package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/ethpandaops/panda-pulse/pkg/discord/message"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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
			{
				Name:        "debug",
				Description: "Show debug logs for a specific check",
				Type:        discordgo.ApplicationCommandOptionSubCommand,
				Options: []*discordgo.ApplicationCommandOption{
					{
						Name:        "id",
						Description: "Check ID to debug",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    true,
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
	case "debug":
		err = c.handleDebug(s, i, data.Options[0])
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

// RunChecks runs the health checks for a given alert.
func (c *ChecksCommand) RunChecks(ctx context.Context, alert *store.MonitorAlert) (bool, error) {
	// Determine client type if not set.
	if alert.ClientType == clients.ClientTypeAll {
		return false, fmt.Errorf("running checks for all clients is not supported")
	}

	var (
		consensusNode string
		executionNode string
	)

	if clients.IsELClient(alert.Client) {
		executionNode = alert.Client
	} else {
		consensusNode = alert.Client
	}

	var (
		gc     = c.bot.GetGrafana()
		runner = checks.NewDefaultRunner(checks.Config{
			Network:       alert.Network,
			ConsensusNode: consensusNode,
			ExecutionNode: executionNode,
		})
	)

	// Register the checks to run.
	runner.RegisterCheck(checks.NewCLSyncCheck(gc))
	runner.RegisterCheck(checks.NewHeadSlotCheck(gc))
	runner.RegisterCheck(checks.NewCLFinalizedEpochCheck(gc))
	runner.RegisterCheck(checks.NewELSyncCheck(gc))
	runner.RegisterCheck(checks.NewELBlockHeightCheck(gc))

	// Run the checks.
	if err := runner.RunChecks(ctx); err != nil {
		return false, fmt.Errorf("failed to run checks: %w", err)
	}

	// Persist the check output, for debugging purposes later if needed.
	if err := c.bot.GetChecksRepo().Persist(ctx, &store.CheckArtifact{
		Network:   alert.Network,
		Client:    alert.Client,
		CheckID:   runner.GetID(),
		Type:      "log",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Content:   runner.GetLog().GetBuffer().Bytes(),
	}); err != nil {
		return false, fmt.Errorf("failed to persist check artifact: %w", err)
	}

	hiveAvailable, err := c.bot.GetHive().IsAvailable(context.Background(), alert.Network)
	if err != nil {
		return false, fmt.Errorf("failed to check hive availability: %w", err)
	}

	if hiveAvailable {
		hiveContent, hiveErr := c.bot.GetHive().Snapshot(context.Background(), hive.SnapshotConfig{
			Network:       alert.Network,
			ConsensusNode: consensusNode,
			ExecutionNode: executionNode,
		})
		if hiveErr != nil {
			if strings.Contains(hiveErr.Error(), "context deadline exceeded") {
				c.log.WithFields(logrus.Fields{
					"network":       alert.Network,
					"consensusNode": consensusNode,
					"executionNode": executionNode,
				}).WithError(hiveErr).Error("hive screenshot timed out")
			} else {
				return false, fmt.Errorf("failed to snapshot test coverage: %w", hiveErr)
			}
		}

		// Persist the hive output, so we can include in our discord embed.
		if len(hiveContent) > 0 {
			if persistErr := c.bot.GetChecksRepo().Persist(ctx, &store.CheckArtifact{
				Network:   alert.Network,
				Client:    alert.Client,
				CheckID:   runner.GetID(),
				Type:      "png",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
				Content:   hiveContent,
			}); persistErr != nil {
				return false, fmt.Errorf("failed to persist check artifact: %w", persistErr)
			}
		}
	}

	// Send the results to Discord.
	alertSent, err := c.sendResults(alert, runner, hiveAvailable)
	if err != nil {
		return alertSent, fmt.Errorf("failed to send discord notification: %w", err)
	}

	return alertSent, nil
}

// sendResults sends the analysis results to Discord.
func (c *ChecksCommand) sendResults(alert *store.MonitorAlert, runner checks.Runner, hiveAvailable bool) (bool, error) {
	var (
		hasFailures          = false
		isRootCause          = false
		hasUnexplainedIssues = false
		checkID              = runner.GetID()
		analysis             = runner.GetAnalysis()
		results              = runner.GetResults()
	)

	// Check if this client is a root cause.
	for _, rootCause := range analysis.RootCause {
		if rootCause == alert.Client {
			isRootCause = true

			break
		}
	}

	// Check for unexplained issues specific to this client.
	for _, issue := range analysis.UnexplainedIssues {
		if strings.Contains(issue, alert.Client) {
			hasUnexplainedIssues = true

			break
		}
	}

	// If they are neither, or if unexplained alerts are disabled, we're done.
	if !isRootCause && !hasUnexplainedIssues {
		return false, nil
	}

	for _, result := range results {
		if result.Status == checks.StatusFail {
			hasFailures = true

			break
		}
	}

	// Sanity check they're failures.
	if !hasFailures {
		return false, nil
	}

	// Use the new builder.
	builder := message.NewAlertMessageBuilder(&message.Config{
		Alert:          alert,
		CheckID:        checkID,
		Results:        results,
		HiveAvailable:  hiveAvailable,
		GrafanaBaseURL: c.bot.GetGrafana().GetBaseURL(),
		HiveBaseURL:    c.bot.GetHive().GetBaseURL(),
	})

	// Create the main message.
	msg, err := c.createMainMessage(alert, builder)
	if err != nil {
		return false, fmt.Errorf("failed to create main message: %w", err)
	}

	// Create a thread off our main message.
	thread, err := c.createThread(msg.ID, alert)
	if err != nil {
		return true, err
	}

	// Populate the thread.
	if err := c.sendThreadMessages(thread.ID, alert, results, builder); err != nil {
		return true, err
	}

	// If hive is available, pop a screenshot of the test coverage into the thread.
	if hiveAvailable {
		screenshot, err := c.bot.GetChecksRepo().GetArtifact(context.Background(), alert.Network, alert.Client, checkID, "png")
		if err == nil && screenshot != nil && len(screenshot.Content) > 0 {
			if _, err := c.bot.GetSession().ChannelMessageSendComplex(thread.ID, builder.BuildHiveMessage(screenshot.Content)); err != nil {
				return true, fmt.Errorf("failed to send hive screenshot: %w", err)
			}
		}
	}

	return true, nil
}

// createMainMessage creates the main message with embed and buttons.
func (c *ChecksCommand) createMainMessage(alert *store.MonitorAlert, builder *message.AlertMessageBuilder) (*discordgo.Message, error) {
	// Send main message.
	mainMsg, err := c.bot.GetSession().ChannelMessageSendComplex(alert.DiscordChannel, builder.BuildMainMessage())
	if err != nil {
		return nil, fmt.Errorf("failed to send Discord message: %w", err)
	}

	return mainMsg, nil
}

// createThread creates a new thread for the given message.
func (c *ChecksCommand) createThread(messageID string, alert *store.MonitorAlert) (*discordgo.Channel, error) {
	threadName := fmt.Sprintf("Issues - %s", time.Now().Format("2006-01-02"))
	if alert.Client != "" {
		threadName = fmt.Sprintf(
			"%s Issues - %s",
			cases.Title(language.English, cases.Compact).String(alert.Client),
			time.Now().Format("2006-01-02"),
		)
	}

	return c.bot.GetSession().MessageThreadStartComplex(alert.DiscordChannel, messageID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: 60,
		Invitable:           false,
	})
}

// sendThreadMessages sends category-specific issues to the thread.
func (c *ChecksCommand) sendThreadMessages(threadID string, alert *store.MonitorAlert, results []*checks.Result, builder *message.AlertMessageBuilder) error {
	categories := groupResultsByCategory(results)

	for _, category := range orderedCategories {
		cat, exists := categories[category]
		if !exists || !cat.hasFailed {
			continue
		}

		messages := builder.BuildThreadMessages(category, cat.failedChecks)
		for _, msg := range messages {
			if _, err := c.bot.GetSession().ChannelMessageSend(threadID, msg); err != nil {
				return fmt.Errorf("failed to send category message: %w", err)
			}
		}
	}

	return nil
}

// Helper function to group results by category.
func groupResultsByCategory(results []*checks.Result) map[checks.Category]*categoryResults {
	categories := make(map[checks.Category]*categoryResults)

	for _, result := range results {
		if result.Status != checks.StatusFail {
			continue
		}

		if _, exists := categories[result.Category]; !exists {
			categories[result.Category] = &categoryResults{
				failedChecks: make([]*checks.Result, 0),
			}
		}

		cat := categories[result.Category]
		cat.failedChecks = append(cat.failedChecks, result)
		cat.hasFailed = true
	}

	return categories
}
