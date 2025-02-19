package checks

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"math"
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

// categoryResults is a struct that holds the results of a category.
type categoryResults struct {
	failedChecks []*checks.Result
	hasFailed    bool
}

// Order categories as we want them to be displayed.
var orderedCategories = []checks.Category{
	checks.CategoryGeneral,
	checks.CategorySync,
}

// Helper to create string pointer.
func stringPtr(s string) *string {
	return &s
}

// hashToColor generates a visually distinct, deterministic color int from a string.
func hashToColor(s string) int {
	hash := sha256.Sum256([]byte(s))

	// Map hue to avoid green (90°-180°)
	hue := remapHue(float64(hash[0]) / 255.0)
	saturation := 0.75                                     // Fixed for vibrancy
	lightness := 0.55 + (float64(hash[10]) / 255.0 * 0.15) // Ensures spread-out lightness

	// Convert HSL to RGB
	r, g, b := hslToRGB(hue, lightness, saturation)

	// Convert to int in 0xRRGGBB format.
	return (r << 16) | (g << 8) | b
}

// hslToRGB converts HSL to RGB (0-255 range for each color).
func hslToRGB(h, l, s float64) (int, int, int) {
	var r, g, b float64

	if s == 0 {
		r, g, b = l, l, l // Achromatic.
	} else {
		q := l * (1 + s)
		if l >= 0.5 {
			q = l + s - (l * s)
		}

		p := 2*l - q

		r = hueToRGB(p, q, h+1.0/3.0)
		g = hueToRGB(p, q, h)
		b = hueToRGB(p, q, h-1.0/3.0)
	}

	return int(math.Round(r * 255)), int(math.Round(g * 255)), int(math.Round(b * 255))
}

// hueToRGB is a helper function for HSL to RGB conversion.
func hueToRGB(p, q, t float64) float64 {
	if t < 0 {
		t += 1
	}

	if t > 1 {
		t -= 1
	}

	if t < 1.0/6.0 {
		return p + (q-p)*6*t
	}

	if t < 1.0/2.0 {
		return q
	}

	if t < 2.0/3.0 {
		return p + (q-p)*(2.0/3.0-t)*6
	}

	return p
}

// remapHue ensures the hue avoids green (90°-180°).
func remapHue(h float64) float64 {
	hueDegrees := h * 360.0

	// If in green range (90-180°), shift to a non-green area.
	if hueDegrees >= 90.0 && hueDegrees <= 180.0 {
		hueDegrees = 180.0 + (hueDegrees - 90.0) // Shift it to the blue/purple spectrum.
	}

	return hueDegrees / 360.0 // Normalize back to 0-1 range.
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

// getClientChoices returns the choices for the client dropdown.
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

// getNetworkChoices returns the choices for the network dropdown.
func (c *ChecksCommand) getNetworkChoices() []*discordgo.ApplicationCommandOptionChoice {
	networks, err := c.bot.GetGrafana().GetNetworks(context.Background())
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

// RunChecks runs the health checks for a given alert.
func (c *ChecksCommand) RunChecks(ctx context.Context, alert *store.MonitorAlert) (bool, error) {
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

	// Run the checks.
	var consensusNode, executionNode string
	if alert.ClientType == clients.ClientTypeCL {
		consensusNode = alert.Client
	} else {
		executionNode = alert.Client
	}

	results, analysis, err := c.bot.GetChecksRunner().RunChecks(ctx, checks.Config{
		Network:       alert.Network,
		ConsensusNode: consensusNode,
		ExecutionNode: executionNode,
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

// sendResults sends the analysis results to Discord.
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

	msg, err := c.bot.GetSession().ChannelMessageSendComplex(channelID, mainMsg)
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

	thread, err := c.bot.GetSession().MessageThreadStartComplex(channelID, msg.ID, &discordgo.ThreadStart{
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

	if _, err := c.bot.GetSession().ChannelMessageSend(threadID, msg); err != nil {
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
