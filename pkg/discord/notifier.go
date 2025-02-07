package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/analyzer"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
)

// Notifier is a Discord notifier.
type Notifier struct {
	session       *discordgo.Session
	openRouterKey string
	httpClient    *http.Client
}

// openRouterResponse is the response from the OpenRouter API.
type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

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

// NewNotifier creates a new Notifier.
func NewNotifier(token string, openRouterKey string) (*Notifier, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	return &Notifier{
		session:       session,
		openRouterKey: openRouterKey,
		httpClient:    &http.Client{},
	}, nil
}

// SendResults sends the analysis results to Discord.
func (n *Notifier) SendResults(channelID string, network string, targetClient string, results []*checks.Result, analysis *analyzer.AnalysisResult) error {
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

	// If they are neither, we're done.
	if !hasUnexplainedIssues && !isRootCause {
		return nil
	}

	for _, result := range results {
		if result.Status == checks.StatusFail {
			hasFailures = true

			break
		}
	}

	// Sanity check they're failures.
	if !hasFailures {
		return nil
	}

	title := fmt.Sprintf("🐼 Pulse Check (%s)", network)
	if targetClient != "" {
		title = fmt.Sprintf("🐼 Pulse Check (%s - %s)", network, targetClient)
	}

	// Create and populate the main embed.
	embed := &discordgo.MessageEmbed{
		Title:     title,
		Color:     0xff0000,
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
	mainMsg := n.createMainMessage(embed, network, results, targetClient)

	msg, err := n.session.ChannelMessageSendComplex(channelID, mainMsg)
	if err != nil {
		return fmt.Errorf("failed to send Discord message: %w", err)
	}

	// Create a thread for us to dump the issue breakdown into.
	threadName := fmt.Sprintf("Issues - %s", time.Now().Format("2006-01-02"))
	if targetClient != "" {
		threadName = fmt.Sprintf("%s Issues - %s", targetClient, time.Now().Format("2006-01-02"))
	}

	thread, err := n.session.MessageThreadStartComplex(channelID, msg.ID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: 60,
		Invitable:           false,
	})
	if err != nil {
		return fmt.Errorf("failed to create thread: %w", err)
	}

	// Process each category's issues.
	for _, category := range orderedCategories {
		cat, exists := categories[category]
		if !exists || !cat.hasFailed {
			continue
		}

		if err := n.sendCategoryIssues(network, thread.ID, category, cat, targetClient); err != nil {
			return err
		}
	}

	return nil
}

// createMainMessage creates the main message with embed and buttons.
func (n *Notifier) createMainMessage(embed *discordgo.MessageEmbed, network string, results []*checks.Result, targetClient string) *discordgo.MessageSend {
	// Count unique failed checks.
	uniqueFailedChecks := make(map[string]bool)

	for _, result := range results {
		if result.Status == checks.StatusFail {
			uniqueFailedChecks[result.Name] = true
		}
	}

	// Add issue count field.
	embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
		Name:   fmt.Sprintf("%d issues found", len(uniqueFailedChecks)),
		Inline: false,
	})

	// Add AI summary if we have an OpenRouter key.
	if n.openRouterKey != "" {
		// Collect all issues for the summary.
		var issues []string

		for _, result := range results {
			if result.Status == checks.StatusFail {
				issues = append(issues, fmt.Sprintf("%s: %s", result.Name, result.Description))
				if len(result.AffectedNodes) > 0 {
					issues = append(issues, fmt.Sprintf("Affected nodes: %s", strings.Join(result.AffectedNodes, ", ")))
				}
			}
		}

		if len(issues) > 0 {
			if summary, err := n.getAISummary(issues, targetClient); err == nil && summary != "" {
				embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
					Name:   "🤖 AI Summary",
					Value:  summary,
					Inline: false,
				})
			}
		}
	}

	executionClient := "All"
	consensusClient := "All"

	if checks.IsELClient(targetClient) {
		executionClient = targetClient
	}

	if checks.IsCLClient(targetClient) {
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
func (n *Notifier) sendCategoryIssues(
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

	if _, err := n.session.ChannelMessageSend(threadID, msg); err != nil {
		return fmt.Errorf("failed to send category message: %w", err)
	}

	// Extract instances from this category's checks.
	instances := n.extractInstances(cat.failedChecks)
	if len(instances) == 0 {
		return nil
	}

	// Send affected instances.
	if err := n.sendInstanceList(threadID, instances); err != nil {
		return err
	}

	// Only send SSH commands if a specific client is targeted.
	if targetClient != "" {
		if err := n.sendSSHCommands(threadID, instances, network); err != nil {
			return err
		}
	}

	return nil
}

// extractInstances extracts instance names from check results.
func (n *Notifier) extractInstances(checks []*checks.Result) map[string]bool {
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
								instances[instance] = true
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
func (n *Notifier) sendInstanceList(threadID string, instances map[string]bool) error {
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

	_, err := n.session.ChannelMessageSend(threadID, msg)

	return err
}

// sendSSHCommands sends SSH commands for the affected instances.
func (n *Notifier) sendSSHCommands(threadID string, instances map[string]bool, network string) error {
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

	_, err := n.session.ChannelMessageSend(threadID, msg)

	return err
}

// getAISummary fetches an AI summary of the issues provided, optionally scoped to a specific client.
func (n *Notifier) getAISummary(issues []string, targetClient string) (string, error) {
	var clientContext string
	if targetClient != "" {
		clientContext = fmt.Sprintf("Note: This analysis is specifically for the %s client. ", targetClient)
	}

	prompt := fmt.Sprintf(
		`You are an impartial Ethereum network monitoring assistant. %s. Provide a brief, 
	concise technical summary of these issues, avoid providing any recommendations and listing out 
	instance names. Please don't just regugutate the issues, provide a summary of the issues targeting 
	the %s client. Return only the formatted summary (dont use markdown headers), do not include 
	any unnecessary verbs, text or reply prompts: \n\n%s`,
		clientContext,
		targetClient,
		strings.Join(issues, "\n"),
	)

	payload := map[string]interface{}{
		"model": "meta-llama/llama-3.1-70b-instruct:free",
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal OpenRouter payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", fmt.Errorf("failed to create OpenRouter request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+n.openRouterKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute OpenRouter request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read OpenRouter response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code from OpenRouter %d: %s", resp.StatusCode, string(body))
	}

	var aiResp openRouterResponse
	if err := json.Unmarshal(body, &aiResp); err != nil {
		return "", fmt.Errorf("failed to decode OpenRouter response: %w", err)
	}

	if len(aiResp.Choices) == 0 {
		return "", fmt.Errorf("no summary generated by OpenRouter")
	}

	return aiResp.Choices[0].Message.Content, nil
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
