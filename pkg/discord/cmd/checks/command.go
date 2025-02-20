package checks

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
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

		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Command failed: %v", err),
			},
		})
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

	hiveAvailable, err := hive.IsAvailable(context.Background(), hive.Config{
		Network:       alert.Network,
		ConsensusNode: consensusNode,
		ExecutionNode: executionNode,
	})
	if err != nil {
		return false, fmt.Errorf("failed to check hive availability: %w", err)
	}

	if hiveAvailable {
		hiveContent, err := hive.SnapshotTestCoverage(context.Background(), hive.Config{
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
				return false, fmt.Errorf("failed to snapshot test coverage: %w", err)
			}
		}

		// Persist the hive output, so we can include in our discord embed.
		if hiveContent != nil && len(hiveContent) > 0 {
			if err := c.bot.GetChecksRepo().Persist(ctx, &store.CheckArtifact{
				Network:   alert.Network,
				Client:    alert.Client,
				CheckID:   runner.GetID(),
				Type:      "png",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
				Content:   hiveContent,
			}); err != nil {
				return false, fmt.Errorf("failed to persist check artifact: %w", err)
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
		hasFailures          bool
		isRootCause          bool
		hasUnexplainedIssues bool
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

	title := alert.Network
	if alert.Client != "" {
		title = cases.Title(language.English, cases.Compact).String(alert.Client)
	}

	// Create and populate the main embed.
	embed := &discordgo.MessageEmbed{
		Title:     title,
		Color:     hashToColor(alert.Network),
		Timestamp: time.Now().Format(time.RFC3339),
		Fields:    make([]*discordgo.MessageEmbedField, 0),
	}

	// Group results by category and collect all issues.
	categories := make(map[checks.Category]*categoryResults)

	// Process only failed results.
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

	// Create + send the main message.
	mainMsg := c.createMainMessage(embed, alert, results, checkID, hiveAvailable)

	msg, err := c.bot.GetSession().ChannelMessageSendComplex(alert.DiscordChannel, mainMsg)
	if err != nil {
		return true, fmt.Errorf("failed to send Discord message: %w", err)
	}

	// Create a thread for us to dump the issue breakdown into.
	threadName := fmt.Sprintf("Issues - %s", time.Now().Format("2006-01-02"))
	if alert.Client != "" {
		threadName = fmt.Sprintf(
			"%s Issues - %s",
			cases.Title(language.English, cases.Compact).String(alert.Client),
			time.Now().Format("2006-01-02"),
		)
	}

	thread, err := c.bot.GetSession().MessageThreadStartComplex(alert.DiscordChannel, msg.ID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: 60,
		Invitable:           false,
	})
	if err != nil {
		return true, fmt.Errorf("failed to create thread: %w", err)
	}

	// Process each category's issues.
	for _, category := range orderedCategories {
		cat, exists := categories[category]
		if !exists || !cat.hasFailed {
			continue
		}

		if err := c.sendCategoryIssues(alert, thread.ID, category, cat, checkID, hiveAvailable); err != nil {
			return true, err
		}
	}

	return true, nil
}

// createMainMessage creates the main message with embed and buttons.
func (c *ChecksCommand) createMainMessage(embed *discordgo.MessageEmbed, alert *store.MonitorAlert, results []*checks.Result, checkID string, hiveAvailable bool) *discordgo.MessageSend {
	// Count unique failed checks.
	uniqueFailedChecks := make(map[string]bool)

	for _, result := range results {
		if result.Status == checks.StatusFail {
			uniqueFailedChecks[result.Name] = true
		}
	}

	if logo := clients.GetClientLogo(alert.Client); logo != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: logo,
		}
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   fmt.Sprintf("%s %d Active Issues", "⚠️", len(uniqueFailedChecks)),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   fmt.Sprintf("🌐 %s", alert.Network),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Value:  "Check the thread below for a breakdown",
		Inline: false,
	})

	// Add footer with check ID.
	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: fmt.Sprintf("ID: %s", checkID),
	}

	executionClient := "All"
	consensusClient := "All"

	if clients.IsELClient(alert.Client) {
		executionClient = alert.Client
	}

	if clients.IsCLClient(alert.Client) {
		consensusClient = alert.Client
	}

	btns := []discordgo.MessageComponent{
		discordgo.Button{
			Label: "📊 Grafana",
			Style: discordgo.LinkButton,
			URL:   fmt.Sprintf("https://grafana.observability.ethpandaops.io/d/cebekx08rl9tsc/panda-pulse?orgId=1&var-consensus_client=%s&var-execution_client=%s&var-network=%s&var-filter=ingress_user%%7C%%21~%%7Csynctest.%%2A", consensusClient, executionClient, alert.Network),
		},
		discordgo.Button{
			Label: "📝 Logs",
			Style: discordgo.LinkButton,
			URL:   fmt.Sprintf("https://grafana.observability.ethpandaops.io/d/aebfg1654nqwwd/panda-pulse-client-error-logs?orgId=1&var-network=%s", alert.Network),
		},
	}

	if hiveAvailable {
		btns = append(btns, discordgo.Button{
			Label: "🐝 Hive",
			Style: discordgo.LinkButton,
			URL:   fmt.Sprintf("https://hive.ethpandaops.io/%s/index.html#summary-sort=name&group-by=client", alert.Network),
		})
	}

	return &discordgo.MessageSend{
		Embed: embed,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: btns,
			},
		},
	}
}

// sendCategoryIssues sends category-specific issues to the thread.
func (c *ChecksCommand) sendCategoryIssues(
	alert *store.MonitorAlert,
	threadID string,
	category checks.Category,
	cat *categoryResults,
	checkID string,
	hiveAvailable bool,
) error {
	msg := fmt.Sprintf("\n\n**%s %s Issues**\n------------------------------------------\n", getCategoryEmoji(category), category.String())
	msg += "**Issues detected**\n"

	names := make(map[string]bool)
	for _, check := range cat.failedChecks {
		if _, ok := names[check.Name]; !ok {
			names[check.Name] = true
		}
	}

	for name := range names {
		msg += fmt.Sprintf("- %s\n", name)
	}

	if _, err := c.bot.GetSession().ChannelMessageSend(threadID, msg); err != nil {
		return fmt.Errorf("failed to send category message: %w", err)
	}

	// Extract instances from this category's checks.
	instances := c.extractInstances(cat.failedChecks, alert.Client)
	if len(instances) == 0 {
		return nil
	}

	// Send affected instances.
	if err := c.sendInstanceList(threadID, instances); err != nil {
		return err
	}

	// Only send SSH commands if a specific client is targeted.
	if err := c.sendSSHCommands(threadID, instances, alert.Network); err != nil {
		return err
	}

	if !hiveAvailable {
		return nil
	}

	// Get and send the Hive screenshot
	screenshot, err := c.bot.GetChecksRepo().GetArtifact(context.Background(), alert.Network, alert.Client, checkID, "png")
	if err == nil && screenshot != nil && len(screenshot.Content) > 0 {
		_, err = c.bot.GetSession().ChannelMessageSendComplex(threadID, &discordgo.MessageSend{
			Content: "\n**Hive Summary**",
			Files: []*discordgo.File{
				{
					Name:        fmt.Sprintf("hive-%s-%s.png", alert.Client, checkID),
					ContentType: "image/png",
					Reader:      bytes.NewReader(screenshot.Content),
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to send hive screenshot: %w", err)
		}
	}

	return nil
}

// extractInstances extracts instance names from check results.
func (c *ChecksCommand) extractInstances(checks []*checks.Result, targetClient string) map[string]bool {
	instances := make(map[string]bool)

	for _, check := range checks {
		if details := check.Details; details != nil {
			for k, v := range details {
				if k == "lowPeerNodes" || k == "notSyncedNodes" || k == "stuckNodes" || k == "behindNodes" {
					if str, ok := v.(string); ok {
						for _, line := range strings.Split(str, "\n") {
							parts := strings.Fields(line)
							if len(parts) > 0 {
								instance := parts[0]
								if strings.HasPrefix(instance, "(") && len(parts) > 1 {
									instance = parts[1]
								}

								instance = strings.Split(instance, " (")[0]

								// Split the instance name into parts
								nodeParts := strings.Split(instance, "-")
								if len(nodeParts) < 2 {
									continue
								}

								// Match exactly the CL or EL client name
								if nodeParts[0] == targetClient || // CL client
									(len(nodeParts) > 1 && nodeParts[1] == targetClient) { // EL client
									instances[instance] = true
								}
							}
						}
					}
				}
			}
		}
	}

	return instances
}

// sendInstanceList sends the list of affected instances.
func (c *ChecksCommand) sendInstanceList(threadID string, instances map[string]bool) error {
	msg := "\n**Affected instances**\n```bash\n"

	// Convert map keys to slice for sorting
	sortedInstances := make([]string, 0, len(instances))
	for instance := range instances {
		sortedInstances = append(sortedInstances, instance)
	}

	sort.Strings(sortedInstances)

	// Build message with sorted instances
	for _, instance := range sortedInstances {
		msg += fmt.Sprintf("%s\n", instance)
	}

	msg += "```"

	_, err := c.bot.GetSession().ChannelMessageSend(threadID, msg)

	return err
}

// sendSSHCommands sends SSH commands for the affected instances.
func (c *ChecksCommand) sendSSHCommands(threadID string, instances map[string]bool, network string) error {
	msg := "\n**SSH commands**\n```bash\n"

	// Convert map keys to slice for sorting
	sortedInstances := make([]string, 0, len(instances))
	for instance := range instances {
		sortedInstances = append(sortedInstances, instance)
	}

	sort.Strings(sortedInstances)

	// Build message with sorted instances
	for _, instance := range sortedInstances {
		msg += fmt.Sprintf("ssh devops@%s.%s.ethpandaops.io\n\n", instance, network)
	}

	msg += "```"

	_, err := c.bot.GetSession().ChannelMessageSend(threadID, msg)

	return err
}

func determineClientType(client string) (clients.ClientType, error) {
	if clients.IsCLClient(client) {
		return clients.ClientTypeCL, nil
	}

	if clients.IsELClient(client) {
		return clients.ClientTypeEL, nil
	}

	return clients.ClientTypeAll, fmt.Errorf("unknown client: %s", client)
}
