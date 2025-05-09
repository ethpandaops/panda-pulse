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
	"github.com/ethpandaops/panda-pulse/pkg/queue"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"github.com/sirupsen/logrus"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	threadAutoArchiveDuration = 60 // 1 hour.
	threadDateFormat          = "2006-01-02"
	// DefaultCheckSchedule defines when checks should run (daily at 7am UTC).
	DefaultCheckSchedule = "0 7 * * *"
)

// ChecksCommand handles the /checks command.
type ChecksCommand struct {
	log   *logrus.Logger
	bot   common.BotContext
	queue *queue.AlertQueue
}

// NewChecksCommand creates a new checks command.
func NewChecksCommand(log *logrus.Logger, bot common.BotContext) *ChecksCommand {
	cmd := &ChecksCommand{
		log: log,
		bot: bot,
	}

	cmd.queue = queue.NewAlertQueue(
		log,
		cmd.RunChecks,
		queue.NewMetrics("panda_pulse"),
	)

	return cmd
}

// Name returns the name of the command.
func (c *ChecksCommand) Name() string {
	return "checks"
}

// Queue returns the queue instance.
func (c *ChecksCommand) Queue() *queue.AlertQueue {
	return c.queue
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
				Description: "Run a specific health check for a network and client",
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
				Description: "Register health checks for a network (and optional client)",
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
						ChannelTypes: []discordgo.ChannelType{
							discordgo.ChannelTypeGuildText,
						},
					},
					{
						Name:        "client",
						Description: "Specific client to monitor (optional)",
						Type:        discordgo.ApplicationCommandOptionString,
						Required:    false,
						Choices:     clientChoices,
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
				Description: "Deregister health checks for a network (and optional client)",
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
				Description: "List all registered health checks",
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
	if alert.ClientType == clients.ClientTypeAll {
		return false, fmt.Errorf("running checks for all clients is not supported")
	}

	runner, err := c.setupRunner(alert)
	if err != nil {
		return false, err
	}

	if err := runner.RunChecks(ctx); err != nil {
		return false, fmt.Errorf("failed to run checks: %w", err)
	}

	if err := c.persistCheckResults(ctx, alert, runner); err != nil {
		return false, err
	}

	return c.sendResults(ctx, alert, runner)
}

// setupRunner creates and configures a new checks runner.
func (c *ChecksCommand) setupRunner(alert *store.MonitorAlert) (checks.Runner, error) {
	var consensusNode, executionNode string

	if c.bot.GetClientsService().IsELClient(alert.Client) {
		executionNode = alert.Client
	} else {
		consensusNode = alert.Client
	}

	runner := checks.NewDefaultRunner(checks.Config{
		Network:       alert.Network,
		ConsensusNode: consensusNode,
		ExecutionNode: executionNode,
	}, c.bot.GetClientsService())

	runner.RegisterCheck(checks.NewCLSyncCheck(c.bot.GetGrafana()))
	runner.RegisterCheck(checks.NewHeadSlotCheck(c.bot.GetGrafana()))
	runner.RegisterCheck(checks.NewCLFinalizedEpochCheck(c.bot.GetGrafana()))
	runner.RegisterCheck(checks.NewELSyncCheck(c.bot.GetGrafana()))
	runner.RegisterCheck(checks.NewELBlockHeightCheck(c.bot.GetGrafana()))

	return runner, nil
}

// persistCheckResults persists the check results to storage.
func (c *ChecksCommand) persistCheckResults(ctx context.Context, alert *store.MonitorAlert, runner checks.Runner) error {
	now := time.Now()

	return c.bot.GetChecksRepo().Persist(ctx, &store.CheckArtifact{
		Network:   alert.Network,
		Client:    alert.Client,
		CheckID:   runner.GetID(),
		Type:      "log",
		CreatedAt: now,
		UpdatedAt: now,
		Content:   runner.GetLog().GetBuffer().Bytes(),
	})
}

// sendResults sends the analysis results to Discord.
func (c *ChecksCommand) sendResults(ctx context.Context, alert *store.MonitorAlert, runner checks.Runner) (bool, error) {
	var (
		hasFailures          = false
		isRootCause          = false
		hasUnexplainedIssues = false
		checkID              = runner.GetID()
		analysis             = runner.GetAnalysis()
		results              = runner.GetResults()
	)

	// Check if Hive is available for this network
	isHiveAvailable, _ := c.bot.GetHive().IsAvailable(context.Background(), alert.Network)

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

	// If they are neither, we're done.
	if !isRootCause && !hasUnexplainedIssues {
		c.log.WithFields(logrus.Fields{
			"network": alert.Network,
			"client":  alert.Client,
		}).Info("No issues detected, skipped notification")

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
		c.log.WithFields(logrus.Fields{
			"network": alert.Network,
			"client":  alert.Client,
		}).Info("No failures detected, skipped notification")

		return false, nil
	}

	// Get mentions for this client/network.
	mentions, err := c.bot.GetMentionsRepo().Get(context.Background(), alert.Network, alert.Client, alert.DiscordGuildID)
	if err != nil {
		c.log.WithError(err).Error("Failed to get mentions")
	}

	// Use the new builder.
	builder := message.NewAlertMessageBuilder(&message.Config{
		Alert:          alert,
		CheckID:        checkID,
		Results:        results,
		HiveAvailable:  isHiveAvailable,
		GrafanaBaseURL: c.bot.GetGrafana().GetBaseURL(),
		HiveBaseURL:    c.bot.GetHive().GetBaseURL(),
		RootCauses:     analysis.RootCause,
		ClientsService: c.bot.GetClientsService(),
	})

	// Process the data to detect infrastructure issues.
	// We need to populate this field by calling the category-specific methods.
	categories := groupResultsByCategory(results)

	for _, category := range orderedCategories {
		cat, exists := categories[category]
		if !exists || !cat.hasFailed {
			continue
		}

		builder.BuildThreadMessages(category, cat.failedChecks)
	}

	// Check if all issues are infrastructure or unrelated only.
	if builder.HasOnlyInfraOrUnrelatedIssues() {
		c.log.WithFields(logrus.Fields{
			"network": alert.Network,
			"client":  alert.Client,
		}).Info("Only infrastructure or unrelated issues detected, skipped notification")

		return false, nil
	}

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
	if isHiveAvailable {
		// Get a screenshot of the test coverage.
		var consensusNode, executionNode string

		if c.bot.GetClientsService().IsELClient(alert.Client) {
			executionNode = alert.Client
		} else {
			consensusNode = alert.Client
		}

		content, err := c.bot.GetHive().Snapshot(ctx, hive.SnapshotConfig{
			Network:       alert.Network,
			ConsensusNode: consensusNode,
			ExecutionNode: executionNode,
		})
		if err != nil {
			if strings.Contains(err.Error(), "context deadline exceeded") {
				c.log.WithFields(logrus.Fields{
					"network":       alert.Network,
					"consensusNode": consensusNode,
					"executionNode": executionNode,
				}).WithError(err).Error("hive screenshot timed out")
			} else {
				c.log.WithError(err).Error("Failed to get Hive screenshot")
			}
		} else if len(content) > 0 {
			// Store the screenshot.
			now := time.Now()

			err = c.bot.GetChecksRepo().Persist(ctx, &store.CheckArtifact{
				Network:   alert.Network,
				Client:    alert.Client,
				CheckID:   checkID,
				Type:      "png",
				CreatedAt: now,
				UpdatedAt: now,
				Content:   content,
			})
			if err != nil {
				c.log.WithError(err).Error("Failed to persist Hive screenshot")
			} else {
				// Send the screenshot to the thread.
				if _, err := c.bot.GetSession().ChannelMessageSendComplex(thread.ID, builder.BuildHiveMessage(content)); err != nil {
					c.log.WithError(err).Error("Failed to send Hive screenshot")
				}
			}
		}
	}

	// Add mentions at the bottom of the thread if they're enabled.
	if mentions != nil && mentions.Enabled && len(mentions.Mentions) > 0 {
		if _, err := c.bot.GetSession().ChannelMessageSendComplex(thread.ID, builder.BuildMentionMessage(mentions.Mentions)); err != nil {
			c.log.WithError(err).Error("Failed to send mentions message")
		}
	}

	c.log.WithFields(logrus.Fields{
		"network": alert.Network,
		"client":  alert.Client,
	}).Info("Issues detected, sent notification")

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
	threadName := fmt.Sprintf("Issues - %s", time.Now().Format(threadDateFormat))
	if alert.Client != "" {
		threadName = fmt.Sprintf(
			"%s Issues - %s",
			cases.Title(language.English, cases.Compact).String(alert.Client),
			time.Now().Format(threadDateFormat),
		)
	}

	return c.bot.GetSession().MessageThreadStartComplex(alert.DiscordChannel, messageID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: threadAutoArchiveDuration,
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
