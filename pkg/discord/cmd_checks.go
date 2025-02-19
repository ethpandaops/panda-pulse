package discord

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/analyzer"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/store"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// ChecksCommand handles the /checks command
type ChecksCommand struct {
	bot *Bot
}

func NewChecksCommand(bot *Bot) *ChecksCommand {
	return &ChecksCommand{
		bot: bot,
	}
}

func (c *ChecksCommand) Name() string {
	return "checks"
}

func (c *ChecksCommand) Register(session *discordgo.Session) error {
	networkChoices := c.getNetworkChoices()
	clientChoices := c.getClientChoices()

	if _, err := session.ApplicationCommandCreate(session.State.User.ID, "", &discordgo.ApplicationCommand{
		Name:        "checks",
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
						Choices:     c.getNetworkChoices(),
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
		log.Printf("Command failed: %v", err)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("Command failed: %v", err),
			},
		})
	}
}

func (c *ChecksCommand) handleRun(s *discordgo.Session, i *discordgo.InteractionCreate, data *discordgo.ApplicationCommandInteractionDataOption) error {
	options := data.Options
	network := options[0].StringValue()
	client := options[1].StringValue()

	log.Printf("Received checks run command: network=%s client=%s from user=%s",
		network, client, i.Member.User.Username)

	// First respond that we're working on it
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("🔄 Running manual check for **%s** on **%s**...", client, network),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send initial response: %w", err)
	}

	// Create a temporary alert
	tempAlert := &store.MonitorAlert{
		Network:        network,
		Client:         client,
		DiscordChannel: i.ChannelID,
	}

	// Run the check using the service
	alertSent, err := c.runChecks(context.Background(), tempAlert)
	if err != nil {
		return fmt.Errorf("failed to run checks: %w", err)
	}

	if !alertSent {
		// If no alert was sent, everything is good.
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("✅ All checks passed for **%s** on **%s**", client, network)),
		})
		if err != nil {
			log.Printf("Failed to edit initial response: %v", err)
		}
	} else {
		// Otherwise, we have issues.
		_, err = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
			Content: stringPtr(fmt.Sprintf("🚫 Issues detected for **%s** on **%s**, see below for details", client, network)),
		})
		if err != nil {
			log.Printf("Failed to edit initial response: %v", err)
		}
	}

	return nil
}

func (c *ChecksCommand) handleRegister(s *discordgo.Session, i *discordgo.InteractionCreate, data *discordgo.ApplicationCommandInteractionDataOption) error {
	options := data.Options
	network := options[0].StringValue()
	channel := options[1].ChannelValue(s)

	var client *string
	if len(options) > 2 {
		c := options[2].StringValue()
		client = &c
	}

	log.Printf("Received checks register command: network=%s channel=%s client=%v from user=%s",
		network, channel.Name, client, i.Member.User.Username)

	if err := c.registerAlert(context.Background(), network, channel.ID, client); err != nil {
		if alreadyRegistered, ok := err.(*store.AlertAlreadyRegisteredError); ok {
			msg := fmt.Sprintf("ℹ️ Client **%s** is already registered for **%s** in <#%s>",
				alreadyRegistered.Client, network, channel.ID)
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: msg,
				},
			})
		}
		return fmt.Errorf("failed to register alert: %w", err)
	}

	var msg string
	if client != nil {
		msg = fmt.Sprintf("✅ Successfully registered **%s** for **%s** notifications in <#%s>", *client, network, channel.ID)
	} else {
		msg = fmt.Sprintf("✅ Successfully registered **all clients** for **%s** notifications in <#%s>", network, channel.ID)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	})
}

func (c *ChecksCommand) handleDeregister(s *discordgo.Session, i *discordgo.InteractionCreate, data *discordgo.ApplicationCommandInteractionDataOption) error {
	options := data.Options
	network := options[0].StringValue()
	var client *string
	if len(options) > 1 {
		c := options[1].StringValue()
		client = &c
	}

	log.Printf("Received checks deregister command: network=%s client=%v from user=%s",
		network, client, i.Member.User.Username)

	if err := c.deregisterAlert(context.Background(), network, client); err != nil {
		if notRegistered, ok := err.(*store.AlertNotRegisteredError); ok {
			var msg string
			if notRegistered.Client == "any" {
				msg = fmt.Sprintf("ℹ️ No clients are registered for **%s** checks", network)
			} else {
				msg = fmt.Sprintf("ℹ️ Client **%s** is not registered for **%s** checks", notRegistered.Client, network)
			}
			return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: msg,
				},
			})
		}
		return fmt.Errorf("failed to deregister alert: %w", err)
	}

	var msg string
	if client != nil {
		msg = fmt.Sprintf("✅ Successfully deregistered **%s** from **%s** notifications", *client, network)
	} else {
		msg = fmt.Sprintf("✅ Successfully deregistered **all clients** from **%s** notifications", network)
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	})
}

func (c *ChecksCommand) handleList(s *discordgo.Session, i *discordgo.InteractionCreate, data *discordgo.ApplicationCommandInteractionDataOption) error {
	var network *string
	if len(data.Options) > 0 {
		n := data.Options[0].StringValue()
		network = &n
	}

	alerts, err := c.listAlerts(context.Background(), network)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	var msg strings.Builder

	// Get all unique networks
	networks := make(map[string]bool)
	for _, alert := range alerts {
		networks[alert.Network] = true
	}

	// If no alerts found
	if len(networks) == 0 {
		msg.WriteString("ℹ️ No checks are currently registered")
		if network != nil {
			msg.WriteString(fmt.Sprintf(" for the network **%s**", *network))
		} else {
			msg.WriteString(" for any network")
		}
		msg.WriteString("\n")
	}

	// For each network, show the client status table
	for networkName := range networks {
		if network != nil && networkName != *network {
			continue
		}

		// Create a map of registered clients for this network
		type clientInfo struct {
			registered bool
			channelID  string
		}
		registered := make(map[string]clientInfo)

		// Initialize all clients as unregistered
		allClients := append(clients.CLClients, clients.ELClients...)
		for _, client := range allClients {
			registered[client] = clientInfo{registered: false}
		}

		// Update with registered clients and their channels
		for _, alert := range alerts {
			if alert.Network == networkName {
				registered[alert.Client] = clientInfo{
					registered: true,
					channelID:  alert.DiscordChannel,
				}
			}
		}

		msg.WriteString(fmt.Sprintf("🌐 Clients registered for **%s** notifications\n", networkName))
		msg.WriteString("```\n")
		msg.WriteString("┌──────────────┬────────┐\n")
		msg.WriteString("│ Client       │ Status │\n")
		msg.WriteString("├──────────────┼────────┤\n")

		for _, client := range allClients {
			info := registered[client]
			status := "❌"
			if info.registered {
				status = "✅"
			}
			msg.WriteString(fmt.Sprintf("│ %-12s │   %s   │\n", client, status))
		}
		msg.WriteString("└──────────────┴────────┘\n```")

		// First, collect all unique channels
		channels := make(map[string]bool)
		for _, alert := range alerts {
			if alert.Network == networkName {
				channels[alert.DiscordChannel] = true
			}
		}

		if len(channels) > 0 {
			msg.WriteString("Alerts are sent to ")
			first := true
			for channelID := range channels {
				if !first {
					msg.WriteString(", ")
				}
				msg.WriteString(fmt.Sprintf("<#%s>", channelID))
				first = false
			}
			msg.WriteString("\n")
		}
	}

	return s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg.String(),
		},
	})
}

func (c *ChecksCommand) runChecks(ctx context.Context, alert *store.MonitorAlert) (bool, error) {
	// Determine client type if not set
	if alert.ClientType == clients.ClientTypeAll {
		for _, client := range clients.CLClients {
			if client == alert.Client {
				alert.ClientType = clients.ClientTypeCL
				break
			}
		}
		if alert.ClientType == clients.ClientTypeAll {
			for _, client := range clients.ELClients {
				if client == alert.Client {
					alert.ClientType = clients.ClientTypeEL
					break
				}
			}
		}
		if alert.ClientType == clients.ClientTypeAll {
			return false, fmt.Errorf("unknown client: %s", alert.Client)
		}
	}

	// Run the checks
	var consensusNode, executionNode string
	if alert.ClientType == clients.ClientTypeCL {
		consensusNode = alert.Client
	} else {
		executionNode = alert.Client
	}

	results, analysis, err := c.bot.checksRunner.RunChecks(ctx, checks.Config{
		Network:       alert.Network,
		ConsensusNode: consensusNode,
		ExecutionNode: executionNode,
		GrafanaToken:  c.bot.config.GrafanaToken,
	})
	if err != nil {
		return false, fmt.Errorf("failed to run checks: %w", err)
	}

	alertSent, err := c.sendResults(
		alert.DiscordChannel,
		alert.Network,
		alert.Client,
		results,
		analysis,
	)
	if err != nil {
		return alertSent, fmt.Errorf("failed to send discord notification: %w", err)
	}

	return alertSent, nil
}

// SendResults sends the analysis results to Discord.
func (c *ChecksCommand) sendResults(channelID string, network string, targetClient string, results []*checks.Result, analysis *analyzer.AnalysisResult) (bool, error) {
	var (
		hasFailures          bool
		isRootCause          bool
		hasUnexplainedIssues bool
	)

	// Check if this client is a root cause.
	for _, rootCause := range analysis.RootCause {
		if rootCause == targetClient {
			isRootCause = true
			break
		}
	}

	// Check for unexplained issues specific to this client.
	for _, issue := range analysis.UnexplainedIssues {
		if strings.Contains(issue, targetClient) {
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

	title := network
	if targetClient != "" {
		title = cases.Title(language.English, cases.Compact).String(targetClient)
	}

	// Create and populate the main embed.
	embed := &discordgo.MessageEmbed{
		Title:     title,
		Color:     hashToColor(network),
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
	mainMsg := c.createMainMessage(embed, network, results, targetClient)

	msg, err := c.bot.session.ChannelMessageSendComplex(channelID, mainMsg)
	if err != nil {
		return true, fmt.Errorf("failed to send Discord message: %w", err)
	}

	// Create a thread for us to dump the issue breakdown into.
	threadName := fmt.Sprintf("Issues - %s", time.Now().Format("2006-01-02"))
	if targetClient != "" {
		threadName = fmt.Sprintf(
			"%s Issues - %s",
			cases.Title(language.English, cases.Compact).String(targetClient),
			time.Now().Format("2006-01-02"),
		)
	}

	thread, err := c.bot.session.MessageThreadStartComplex(channelID, msg.ID, &discordgo.ThreadStart{
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

		if err := c.sendCategoryIssues(network, thread.ID, category, cat, targetClient); err != nil {
			return true, err
		}
	}

	return true, nil
}

// createMainMessage creates the main message with embed and buttons.
func (c *ChecksCommand) createMainMessage(embed *discordgo.MessageEmbed, network string, results []*checks.Result, targetClient string) *discordgo.MessageSend {
	// Count unique failed checks.
	uniqueFailedChecks := make(map[string]bool)

	for _, result := range results {
		if result.Status == checks.StatusFail {
			uniqueFailedChecks[result.Name] = true
		}
	}

	if logo := clients.GetClientLogo(targetClient); logo != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{
			URL: logo,
		}
	}

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   fmt.Sprintf("%s %d Active Issues", "⚠️", len(uniqueFailedChecks)),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   fmt.Sprintf("🌐 %s", network),
		Inline: true,
	})

	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Value:  "Check the thread below for a breakdown",
		Inline: false,
	})

	executionClient := "All"
	consensusClient := "All"

	if clients.IsELClient(targetClient) {
		executionClient = targetClient
	}

	if clients.IsCLClient(targetClient) {
		consensusClient = targetClient
	}

	return &discordgo.MessageSend{
		Embed: embed,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label: "📊 Grafana",
						Style: discordgo.LinkButton,
						URL:   fmt.Sprintf("https://grafana.observability.ethpandaops.io/d/cebekx08rl9tsc/panda-pulse?orgId=1&var-consensus_client=%s&var-execution_client=%s&var-network=%s&var-filter=ingress_user%%7C%%21~%%7Csynctest.%%2A", consensusClient, executionClient, network),
					},
					discordgo.Button{
						Label: "📝 Logs",
						Style: discordgo.LinkButton,
						URL:   fmt.Sprintf("https://grafana.observability.ethpandaops.io/d/aebfg1654nqwwd/panda-pulse-client-error-logs?orgId=1&var-network=%s", network),
					},
				},
			},
		},
	}
}

// sendCategoryIssues sends category-specific issues to the thread.
func (c *ChecksCommand) sendCategoryIssues(
	network string,
	threadID string,
	category checks.Category,
	cat *categoryResults,
	targetClient string,
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

	if _, err := c.bot.session.ChannelMessageSend(threadID, msg); err != nil {
		return fmt.Errorf("failed to send category message: %w", err)
	}

	// Extract instances from this category's checks.
	instances := c.extractInstances(cat.failedChecks, targetClient)
	if len(instances) == 0 {
		return nil
	}

	// Send affected instances.
	if err := c.sendInstanceList(threadID, instances); err != nil {
		return err
	}

	// Only send SSH commands if a specific client is targeted.
	if targetClient != "" {
		if err := c.sendSSHCommands(threadID, instances, network); err != nil {
			return err
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

	_, err := c.bot.session.ChannelMessageSend(threadID, msg)

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

	_, err := c.bot.session.ChannelMessageSend(threadID, msg)

	return err
}

func (c *ChecksCommand) registerAlert(ctx context.Context, network, channelID string, specificClient *string) error {
	if specificClient == nil {
		// For registering all clients, just proceed with registration
		return c.registerAllClients(ctx, network, channelID)
	}

	// Check if this specific client is already registered
	alerts, err := c.bot.monitorRepo.ListMonitorAlerts(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	for _, alert := range alerts {
		if alert.Network == network && alert.Client == *specificClient && alert.DiscordChannel == channelID {
			return &store.AlertAlreadyRegisteredError{
				Network: network,
				Channel: channelID,
				Client:  *specificClient,
			}
		}
	}

	// Check if client exists in our known clients
	clientType := clients.ClientTypeAll
	for _, c := range clients.CLClients {
		if c == *specificClient {
			clientType = clients.ClientTypeCL
			break
		}
	}

	if clientType == clients.ClientTypeAll {
		for _, c := range clients.ELClients {
			if c == *specificClient {
				clientType = clients.ClientTypeEL
				break
			}
		}
	}
	if clientType == clients.ClientTypeAll {
		return fmt.Errorf("unknown client: %s", *specificClient)
	}

	alert := &store.MonitorAlert{
		Network:        network,
		Client:         *specificClient,
		ClientType:     clientType,
		DiscordChannel: channelID,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := c.bot.monitorRepo.RegisterMonitorAlert(ctx, alert); err != nil {
		return fmt.Errorf("failed to store alert: %w", err)
	}

	if err := c.scheduleAlert(alert); err != nil {
		return fmt.Errorf("failed to schedule alert: %w", err)
	}

	return nil
}

func (c *ChecksCommand) registerAllClients(ctx context.Context, network, channelID string) error {
	// Register CL clients
	for _, client := range clients.CLClients {
		alert := &store.MonitorAlert{
			Network:        network,
			Client:         client,
			ClientType:     clients.ClientTypeCL,
			DiscordChannel: channelID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := c.bot.monitorRepo.RegisterMonitorAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to store CL alert: %w", err)
		}
		if err := c.scheduleAlert(alert); err != nil {
			return fmt.Errorf("failed to schedule CL alert: %w", err)
		}
	}

	// Register EL clients
	for _, client := range clients.ELClients {
		alert := &store.MonitorAlert{
			Network:        network,
			Client:         client,
			ClientType:     clients.ClientTypeEL,
			DiscordChannel: channelID,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := c.bot.monitorRepo.RegisterMonitorAlert(ctx, alert); err != nil {
			return fmt.Errorf("failed to store EL alert: %w", err)
		}
		if err := c.scheduleAlert(alert); err != nil {
			return fmt.Errorf("failed to schedule EL alert: %w", err)
		}
	}
	return nil
}

func (c *ChecksCommand) scheduleAlert(alert *store.MonitorAlert) error {
	schedule := "*/1 * * * *"
	jobName := fmt.Sprintf("network-health-%s-%s-%s", alert.Network, alert.ClientType, alert.Client)

	log.Printf("Scheduling alert: network=%s client=%s type=%s job=%s schedule=%s",
		alert.Network, alert.Client, alert.ClientType, jobName, schedule)

	return c.bot.scheduler.AddJob(jobName, schedule, func(ctx context.Context) error {
		log.Printf("Running checks for network=%s client=%s", alert.Network, alert.Client)
		_, err := c.runChecks(ctx, alert)
		return err
	})
}

func (c *ChecksCommand) ScheduleAlert(alert *store.MonitorAlert) error {
	schedule := "*/1 * * * *"
	jobName := fmt.Sprintf("network-health-%s-%s-%s", alert.Network, alert.ClientType, alert.Client)

	log.Printf("Scheduling alert: network=%s client=%s type=%s job=%s schedule=%s",
		alert.Network, alert.Client, alert.ClientType, jobName, schedule)

	return c.bot.scheduler.AddJob(jobName, schedule, func(ctx context.Context) error {
		log.Printf("Running checks for network=%s client=%s", alert.Network, alert.Client)
		_, err := c.runChecks(ctx, alert)
		return err
	})
}

func (c *ChecksCommand) deregisterAlert(ctx context.Context, network string, client *string) error {
	log.Printf("Deregistering alert for network=%s client=%v", network, client)

	// If client is specified, only remove that client's alert
	if client != nil {
		// First try to find the alert to get its type
		alerts, err := c.bot.monitorRepo.ListMonitorAlerts(ctx)
		if err != nil {
			return fmt.Errorf("failed to list alerts: %w", err)
		}

		// Find the alert to get its type
		var clientType clients.ClientType
		found := false
		for _, a := range alerts {
			if a.Network == network && a.Client == *client {
				clientType = a.ClientType
				found = true
				break
			}
		}

		if !found {
			return &store.AlertNotRegisteredError{
				Network: network,
				Client:  *client,
			}
		}

		jobName := fmt.Sprintf("network-health-%s-%s-%s", network, clientType, *client)
		c.bot.scheduler.RemoveJob(jobName)

		// Remove from S3
		if err := c.bot.monitorRepo.DeleteMonitorAlert(ctx, network, *client); err != nil {
			return fmt.Errorf("failed to delete alert: %w", err)
		}
		return nil
	}

	// Otherwise, remove all clients for this network
	alerts, err := c.bot.monitorRepo.ListMonitorAlerts(ctx)
	if err != nil {
		return fmt.Errorf("failed to list alerts: %w", err)
	}

	found := false
	for _, alert := range alerts {
		if alert.Network == network {
			found = true
			jobName := fmt.Sprintf("network-health-%s-%s-%s", network, alert.ClientType, alert.Client)
			c.bot.scheduler.RemoveJob(jobName)

			if err := c.bot.monitorRepo.DeleteMonitorAlert(ctx, network, alert.Client); err != nil {
				return fmt.Errorf("failed to delete alert: %w", err)
			}
		}
	}

	if !found {
		return &store.AlertNotRegisteredError{
			Network: network,
			Client:  "any",
		}
	}

	return nil
}

func (c *ChecksCommand) listAlerts(ctx context.Context, network *string) ([]*store.MonitorAlert, error) {
	alerts, err := c.bot.monitorRepo.ListMonitorAlerts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list alerts: %w", err)
	}

	if network == nil {
		return alerts, nil
	}

	// Filter alerts for specific network
	filtered := make([]*store.MonitorAlert, 0)
	for _, alert := range alerts {
		if alert.Network == *network {
			filtered = append(filtered, alert)
		}
	}

	return filtered, nil
}

func (c *ChecksCommand) getClientChoices() []*discordgo.ApplicationCommandOptionChoice {
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(clients.CLClients)+len(clients.ELClients))
	for _, client := range clients.CLClients {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  client,
			Value: client,
		})
	}

	for _, client := range clients.ELClients {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  client,
			Value: client,
		})
	}
	return choices
}

func (c *ChecksCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
	networks, err := c.bot.grafana.GetNetworks(context.Background())
	if err != nil {
		log.Printf("Failed to get networks from Grafana: %v", err)

		return nil
	}

	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(networks))
	for _, network := range networks {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  network,
			Value: network,
		})
	}

	return choices
}

// getCategoryEmoji returns the emoji for a given category.
func getCategoryEmoji(category checks.Category) string {
	switch category {
	case checks.CategorySync:
		return "🔄"
	case checks.CategoryGeneral:
		return "⚡"
	default:
		return "📋"
	}
}
