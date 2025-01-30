package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/checks"
)

// Notifier is a Discord notifier.
type Notifier struct {
	session       *discordgo.Session
	openRouterKey string
	httpClient    *http.Client
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

// SendResults sends the results to Discord.
func (n *Notifier) SendResults(channelID string, network string, targetClient string, results []*checks.Result) error {
	title := fmt.Sprintf("🐼 Pulse Check (%s)", network)
	if targetClient != "" {
		title = fmt.Sprintf("🐼 Pulse Check (%s - %s)", network, targetClient)
	}

	// Create the main embed for summary.
	embed := &discordgo.MessageEmbed{
		Title:     title,
		Color:     0x555555,
		Timestamp: time.Now().Format(time.RFC3339),
		Fields:    make([]*discordgo.MessageEmbedField, 0),
	}

	// Group results by category and collect all issues.
	var (
		hasFailed         bool
		allIssues         = make([]string, 0)
		categories        = make(map[checks.Category]*categoryResults)
		orderedCategories = []checks.Category{
			checks.CategoryGeneral,
			checks.CategorySync,
		}
	)

	// First pass: group results and collect any issues.
	for _, result := range results {
		if result.Status == checks.StatusOK {
			continue
		}

		if _, exists := categories[result.Category]; !exists {
			categories[result.Category] = &categoryResults{
				failedChecks: make([]*checks.Result, 0),
			}
		}

		cat := categories[result.Category]
		cat.hasFailed = true
		cat.failedChecks = append(cat.failedChecks, result)
		hasFailed = true

		// Collect the issue so we can print an AI summary.
		allIssues = append(allIssues, fmt.Sprintf("Category: %s", result.Category.String()))
		issue := fmt.Sprintf("[FAIL] %s: %s", result.Name, result.Description)

		if details := formatDetails(result.Details); details != "" {
			issue += " " + strings.ReplaceAll(details, "```", "")
		}

		allIssues = append(allIssues, issue)
	}

	// Add summary fields for each category.
	for _, category := range orderedCategories {
		cat, exists := categories[category]
		if !exists || !cat.hasFailed {
			continue
		}

		var summary string

		if len(cat.failedChecks) > 0 {
			var plural string

			if len(cat.failedChecks) > 1 {
				plural = "s"
			}

			summary += fmt.Sprintf("%d %s issue%s detected", len(uniqueChecks(cat.failedChecks)), strings.ToLower(category.String()), plural)
		}

		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Value:  summary,
			Inline: false,
		})
	}

	// Set notification color and content based on status.
	if hasFailed {
		embed.Color = 0xff0000 // Red

		// Fetch an AI summary if there are issues and an OpenRouter key is available.
		if len(allIssues) > 0 && n.openRouterKey != "" {
			aiSummary, err := n.getAISummary(allIssues, targetClient)
			if err == nil && aiSummary != "" {
				embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
					Name:   "🤖 AI Analysis",
					Value:  aiSummary,
					Inline: false,
				})
			}
		}
	} else {
		// We're good, no issues detected.
		embed.Color = 0x00ff00 // Green

		desc := "No issues detected"
		if targetClient != "" {
			desc = fmt.Sprintf("No issues detected for %s", targetClient)
		}

		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Value:  desc,
			Inline: false,
		})
	}

	// 🐼's rule.
	embed.Footer = &discordgo.MessageEmbedFooter{
		Text: "With ❤️ from ethPandaOps",
	}

	// Build our message out, with appropriate buttons linking out to grafana and loki.
	mainMsg := discordgo.MessageSend{
		Embed: embed,
		Components: []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label: "📊 Grafana",
						Style: discordgo.LinkButton,
						URL:   fmt.Sprintf("https://grafana.observability.ethpandaops.io/d/cebekx08rl9tsc/panda-pulse?orgId=1&var-consensus_client=All&var-execution_client=All&var-network=%s&var-filter=ingress_user%%7C%%21~%%7Csynctest.%%2A", network),
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

	// Send the main message.
	msg, err := n.session.ChannelMessageSendComplex(channelID, &mainMsg)
	if err != nil {
		return fmt.Errorf("failed to send Discord message: %w", err)
	}

	// If there are no issues, we're done.
	if !hasFailed {
		return nil
	}

	// Otherwise, create a thread and punch the issues into it.
	thread, err := n.session.MessageThreadStartComplex(channelID, msg.ID, &discordgo.ThreadStart{
		Name:                fmt.Sprintf("Issues - %s", time.Now().Format("2006-01-02")),
		AutoArchiveDuration: 60, // Archive thread after an hour.
		Invitable:           false,
	})
	if err != nil {
		return fmt.Errorf("failed to create thread: %w", err)
	}

	// Send each category's issues
	for _, category := range orderedCategories {
		cat, exists := categories[category]
		if !exists || !cat.hasFailed {
			continue
		}

		// Send category header and issues.
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

		if _, err := n.session.ChannelMessageSend(thread.ID, msg); err != nil {
			return fmt.Errorf("failed to send category message: %w", err)
		}

		// Determine the instances affected by this category's checks.
		instances := make(map[string]bool)

		for _, check := range cat.failedChecks {
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

		// If there are no instances, we're done.
		if len(instances) == 0 {
			continue
		}

		// Add affected instances (if any).
		msg = "\n**Affected instances**\n```bash\n"

		for instance := range instances {
			msg += fmt.Sprintf("%s\n", instance)
		}

		msg += "```"

		if _, err := n.session.ChannelMessageSend(thread.ID, msg); err != nil {
			return fmt.Errorf("failed to send instance message: %w", err)
		}

		// Add SSH commands for easy copy/paste by users.
		msg = "\n**SSH commands**\n```bash\n"

		for instance := range instances {
			msg += fmt.Sprintf("ssh devops@%s.%s.ethpandaops.io\n\n", instance, network)
		}

		msg += "```"

		if _, err := n.session.ChannelMessageSend(thread.ID, msg); err != nil {
			return fmt.Errorf("failed to send instance message: %w", err)
		}
	}

	return nil
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

// getAISummary fetches an AI summary of the issues provided, optionally scoped to a specific client.
func (n *Notifier) getAISummary(issues []string, targetClient string) (string, error) {
	var clientContext string
	if targetClient != "" {
		clientContext = fmt.Sprintf("Note: This analysis is specifically for the %s client. ", targetClient)
	}

	prompt := fmt.Sprintf(
		`You are an impartial Ethereum network monitoring assistant. %sProvide a brief, 
	concise technical summary of these issues, avoid providing any recommendations and listing out 
	instance names. Return only the formatted summary (dont use markdown headers), do not include 
	any unnecessary verbs, text or reply prompts: \n\n%s`,
		clientContext,
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

func formatDetails(details map[string]interface{}) string {
	if len(details) == 0 {
		return ""
	}

	parts := make([]string, 0)

	for k, v := range details {
		// Skip the query field as it's internal.
		if k == "query" {
			continue
		}

		parts = append(parts, fmt.Sprintf("%v", v))
	}

	if len(parts) == 0 {
		return ""
	}

	return fmt.Sprintf("```\n%s\n```", strings.Join(parts, "\n"))
}

func uniqueChecks(results []*checks.Result) []*checks.Result {
	var (
		seen   = make(map[string]bool)
		unique = make([]*checks.Result, 0)
	)

	// Iterate over the results and add unique checks to the list.
	for _, check := range results {
		if !seen[check.Name] {
			seen[check.Name] = true

			unique = append(unique, check)
		}
	}

	return unique
}
