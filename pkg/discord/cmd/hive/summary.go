package hive

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
)

const (
	// Status icons for test results.
	iconSuccess   = "✅"
	iconWarning   = "⚠️"
	iconFailure   = "❌"
	iconExcellent = "🟢"
	iconMedium    = "🟡"
	iconPoor      = "🔴"
)

// sendHiveSummary sends a Hive summary to Discord.
func (c *HiveCommand) sendHiveSummary(
	ctx context.Context,
	alert *hive.HiveSummaryAlert,
	summary *hive.SummaryResult,
	prevSummary *hive.SummaryResult,
	results []hive.TestResult,
) error {
	session := c.bot.GetSession()

	// Send the combined summary overview and test type breakdown in the main channel.
	overviewEmbed := createCombinedOverviewEmbed(summary, prevSummary, results, alert.Suite)

	// Create message send object.
	messageSend := &discordgo.MessageSend{
		Content: "",
		Embeds:  []*discordgo.MessageEmbed{overviewEmbed},
	}

	// Add button that links to the Hive dashboard only if network name is available.
	networkName := summary.Network
	if networkName != "" {
		// Use the mapped network name for the Hive URL
		hiveNetworkName := c.bot.GetHive().MapNetworkName(networkName)
		hiveURL := fmt.Sprintf("https://hive.ethpandaops.io/#/group/%s", hiveNetworkName)

		messageSend.Components = []discordgo.MessageComponent{
			discordgo.ActionsRow{
				Components: []discordgo.MessageComponent{
					discordgo.Button{
						Label: "Open Hive",
						Style: discordgo.LinkButton,
						URL:   hiveURL,
					},
				},
			},
		}
	}

	mainMessage, err := session.ChannelMessageSendComplex(alert.DiscordChannel, messageSend)
	if err != nil {
		return fmt.Errorf("failed to send main message: %w", err)
	}

	// Create a thread for the client details.
	threadName := fmt.Sprintf("Hive Summary - %s", summary.Timestamp.Format(threadDateFormat))
	if alert.Suite != "" {
		threadName = fmt.Sprintf("Hive Summary (%s) - %s", alert.Suite, summary.Timestamp.Format(threadDateFormat))
	}

	thread, err := session.MessageThreadStartComplex(alert.DiscordChannel, mainMessage.ID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: threadAutoArchiveDuration,
	})
	if err != nil {
		return fmt.Errorf("failed to create thread: %w", err)
	}

	// Send client breakdown as individual messages in the thread.
	if err := sendClientBreakdownMessages(ctx, session, thread.ID, summary, prevSummary, results, c.bot.GetHive()); err != nil {
		return fmt.Errorf("failed to send client breakdown messages: %w", err)
	}

	return nil
}

// sendClientBreakdownMessages sends each client as a separate message in the thread.
func sendClientBreakdownMessages(
	ctx context.Context,
	session *discordgo.Session,
	threadID string,
	summary *hive.SummaryResult,
	prevSummary *hive.SummaryResult,
	results []hive.TestResult,
	hiveClient hive.Hive,
) error {
	// Sort clients by failures (descending).
	clients := make([]string, 0, len(summary.ClientResults))
	for client := range summary.ClientResults {
		clients = append(clients, client)
	}

	sort.Slice(clients, func(i, j int) bool {
		return summary.ClientResults[clients[i]].FailedTests > summary.ClientResults[clients[j]].FailedTests
	})

	// If we have no clients, send a default message.
	if len(clients) == 0 {
		_, err := session.ChannelMessageSend(threadID, "No client results available.")

		return err
	}

	// Send a message for each client.
	for _, clientKey := range clients {
		embed := createClientEmbed(clientKey, summary.ClientResults[clientKey], prevSummary, results, summary.Network, hiveClient)

		_, err := session.ChannelMessageSendEmbed(threadID, embed)
		if err != nil {
			return fmt.Errorf("failed to send client embed for %s: %w", clientKey, err)
		}
	}

	return nil
}

// createClientEmbed creates an embed for a single client.
//
//nolint:gocyclo // splitting apart would add complexity.
func createClientEmbed(
	clientKey string,
	result *hive.ClientSummary,
	prevSummary *hive.SummaryResult,
	results []hive.TestResult,
	network string,
	hiveClient hive.Hive,
) *discordgo.MessageEmbed {
	// Use a default name if ClientName is empty.
	clientName := result.ClientName
	if clientName == "" {
		clientName = clientKey
	}

	// Create fields for the embed.
	fields := []*discordgo.MessageEmbedField{}

	// Add version info if available.
	cleanVersion := cleanVersionString(result.ClientVersion)
	if cleanVersion != "" && cleanVersion != "unknown" {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Version",
			Value:  fmt.Sprintf("📦 %s", cleanVersion),
			Inline: true,
		})
	}

	// Add pass rate info.
	var passRateValue string

	// Calculate more precise pass rate for near-100% with failures.
	if result.PassRate >= 99.95 && result.FailedTests > 0 {
		exactPassRate := float64(result.PassedTests) / float64(result.TotalTests) * 100
		passRateValue = fmt.Sprintf("✅ %.2f%% (%d/%d)", exactPassRate, result.PassedTests, result.TotalTests)
	} else {
		passRateValue = fmt.Sprintf("✅ %.1f%% (%d/%d)", result.PassRate, result.PassedTests, result.TotalTests)
	}

	fields = append(fields, &discordgo.MessageEmbedField{
		Name:   "Pass Rate",
		Value:  passRateValue,
		Inline: true,
	})

	// Add failures info if there are any.
	if result.FailedTests > 0 {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Failures",
			Value:  fmt.Sprintf("❌ %d", result.FailedTests),
			Inline: true,
		})
	}

	// Calculate change from previous day if available.
	var changeValue string

	if prevSummary != nil {
		if prevClient, ok := prevSummary.ClientResults[clientKey]; ok && prevClient.TotalTests > 0 {
			prevPassRate := float64(prevClient.PassedTests) / float64(prevClient.TotalTests) * 100
			change := result.PassRate - prevPassRate

			// Check if there are failure changes.
			hasFailureChanges := result.FailedTests != prevClient.FailedTests

			// Show pass rate change if it's significant or if there are failure changes.
			if change > 0.05 {
				changeValue = fmt.Sprintf("📈 Pass rate improved by %.1f%%", change)
			} else if change < -0.05 {
				changeValue = fmt.Sprintf("📉 Pass rate decreased by %.1f%%", -change)
			} else if hasFailureChanges {
				// For small pass rate changes with failure changes, still show the direction.
				if change > 0 {
					changeValue = fmt.Sprintf("📈 Pass rate improved slightly (%.2f%%)", change)
				} else if change < 0 {
					changeValue = fmt.Sprintf("📉 Pass rate decreased slightly (%.2f%%)", -change)
				} else {
					changeValue = "Pass rate unchanged despite failure changes"
				}
			} else {
				// No significant pass rate change and no failure changes.
				changeValue = "No change since last check"
			}

			// Add failure change information if there are any.
			if result.FailedTests > prevClient.FailedTests {
				failureIncrease := result.FailedTests - prevClient.FailedTests
				changeValue = fmt.Sprintf("%s\n⚠️ %d new failures since last check", changeValue, failureIncrease)
			} else if result.FailedTests < prevClient.FailedTests {
				failureDecrease := prevClient.FailedTests - result.FailedTests
				changeValue = fmt.Sprintf("%s\n✅ %d fewer failures since last check", changeValue, failureDecrease)
			}
		}
	}

	// Add anomaly detection.
	if result.FailedTests > 0 {
		anomalies := detectAnomalies(clientKey, result, prevSummary, results)
		if len(anomalies) > 0 {
			// Limit to 2 anomalies to avoid cluttering.
			if len(anomalies) > 2 {
				anomalies = anomalies[:2]
			}

			anomalyText := strings.Join(anomalies, "\n")
			if changeValue != "" {
				changeValue = fmt.Sprintf("%s\n\n%s", changeValue, anomalyText)
			} else {
				changeValue = anomalyText
			}
		}
	}

	// Add links to specific test suites if available.
	testSuiteLinks := buildTestSuiteLinks(clientKey, results, network, hiveClient)
	if testSuiteLinks != "" {
		if changeValue != "" {
			changeValue = fmt.Sprintf("%s\n%s", changeValue, testSuiteLinks)
		} else {
			changeValue = testSuiteLinks
		}
	}

	if changeValue != "" {
		// Discord has a 1024 character limit for embed field values
		const maxFieldLength = 1024
		if len(changeValue) > maxFieldLength {
			// Truncate and add ellipsis
			changeValue = changeValue[:maxFieldLength-3] + "..."
		}

		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   "Details",
			Value:  changeValue,
			Inline: false,
		})
	}

	// Determine embed color based on pass rate
	var color int

	statusIcon := iconSuccess

	if result.FailedTests == 0 {
		color = 0x51CF66 // Green for perfect score
	} else if result.PassRate >= 99 {
		color = 0xF5A623 // Yellow/gold for very high pass rate
		statusIcon = iconWarning
	} else {
		color = 0xFF6B6B // Red for concerning pass rate
		statusIcon = iconFailure
	}

	// Format client name with proper casing
	displayName := clientName
	// Common client name mappings for better display
	switch strings.ToLower(clientName) {
	case "geth", "go-ethereum":
		displayName = "Geth"
	case "besu":
		displayName = "Besu"
	case "nethermind":
		displayName = "Nethermind"
	case "erigon":
		displayName = "Erigon"
	case "nimbus-el", "nimbusel":
		displayName = "NimbusEL"
	case "reth":
		displayName = "Reth"
	}

	embed := &discordgo.MessageEmbed{
		Title:  fmt.Sprintf("%s %s", statusIcon, displayName),
		Color:  color,
		Fields: fields,
	}

	return embed
}

// createCombinedOverviewEmbed creates an embed with the summary overview and test type breakdown.
func createCombinedOverviewEmbed(summary *hive.SummaryResult, prevSummary *hive.SummaryResult, results []hive.TestResult, suite string) *discordgo.MessageEmbed {
	// Format the timestamp in a user-friendly way using UTC.
	lastUpdated := summary.Timestamp.UTC().Format("Mon, 2 Jan 2006")

	// Create the overview fields with improved formatting
	passRateIcon := iconExcellent
	if summary.OverallPassRate < 95 {
		passRateIcon = iconPoor
	} else if summary.OverallPassRate < 99.5 {
		passRateIcon = iconMedium
	}

	failureIcon := iconSuccess
	if summary.TotalFails > 0 {
		failureIcon = iconWarning
	}

	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "📊 Total Tests Run",
			Value:  fmt.Sprintf("**%s**", formatNumber(summary.TotalTests)),
			Inline: true,
		},
		{
			Name:   fmt.Sprintf("%s Overall Pass Rate", passRateIcon),
			Value:  fmt.Sprintf("**%s**", formatPassRate(summary.OverallPassRate, summary.TotalFails)),
			Inline: true,
		},
		{
			Name:   fmt.Sprintf("%s Total Failures", failureIcon),
			Value:  fmt.Sprintf("**%d**", summary.TotalFails),
			Inline: true,
		},
		{
			Name:   "📅 Test Date",
			Value:  fmt.Sprintf("**%s**", lastUpdated),
			Inline: true,
		},
	}

	// Add test type breakdown.
	testTypeResults := make(map[string]struct {
		Total  int
		Passes int
		Fails  int
	})

	// Process each test result and aggregate by test type.
	for _, result := range results {
		testType := result.Name

		stats, exists := testTypeResults[testType]
		if !exists {
			stats = struct {
				Total  int
				Passes int
				Fails  int
			}{0, 0, 0}
		}

		// Add this result's counts to the test type totals.
		stats.Total += result.NTests
		stats.Passes += result.Passes
		stats.Fails += result.Fails
		testTypeResults[testType] = stats
	}

	// Sort test types alphabetically.
	testTypes := make([]string, 0, len(testTypeResults))
	for testType := range testTypeResults {
		testTypes = append(testTypes, testType)
	}

	sort.Strings(testTypes)

	// Add test type fields with improved formatting
	for _, testType := range testTypes {
		var (
			stats    = testTypeResults[testType]
			passRate = 0.0
		)

		if stats.Total > 0 {
			passRate = float64(stats.Passes) / float64(stats.Total) * 100
		}

		// Add status indicator
		statusIcon := iconSuccess
		if passRate < 95 {
			statusIcon = iconFailure
		} else if passRate < 100 {
			statusIcon = iconWarning
		}

		// Format the pass rate with appropriate precision
		var passRateStr string
		if stats.Fails > 0 && passRate >= 99.95 {
			// Use higher precision for near-100% pass rates with failures
			passRateStr = fmt.Sprintf("%.2f%%", passRate)
		} else {
			passRateStr = fmt.Sprintf("%.1f%%", passRate)
		}

		// Format test name with proper styling
		displayName := fmt.Sprintf("**%s**", testType)

		//value := fmt.Sprintf("%s %s pass (%s/%s)", statusIcon, passRateStr, formatNumber(stats.Passes), formatNumber(stats.Total))
		value := fmt.Sprintf("%s %s", statusIcon, passRateStr)

		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   displayName,
			Value:  value,
			Inline: true,
		})
	}

	// Create title with optional suite information
	title := fmt.Sprintf("Ethereum Hive • %s", summary.Network)
	if suite != "" {
		title = fmt.Sprintf("Ethereum Hive • %s • %s", summary.Network, suite)
	}

	// Determine color based on overall pass rate
	embedColor := 0xF5A623 // Hive brand yellow/gold
	if summary.OverallPassRate < 95 {
		embedColor = 0xFF6B6B // Red for concerning pass rates
	} else if summary.OverallPassRate >= 99.5 {
		embedColor = 0x51CF66 // Green for excellent pass rates
	}

	return &discordgo.MessageEmbed{
		//Title:  title,
		Color:  embedColor,
		Fields: fields,
		//Thumbnail: &discordgo.MessageEmbedThumbnail{
		//	URL: "https://ethpandaops.io/img/hive-logo.png",
		//},
		Author: &discordgo.MessageEmbedAuthor{
			Name:    title,
			IconURL: "https://ethpandaops.io/img/hive-logo.png",
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Generated on %s", summary.Timestamp.UTC().Format("Jan 2, 2006 at 15:04 UTC")),
		},
	}
}

// formatPassRate formats a pass rate with appropriate precision.
func formatPassRate(passRate float64, failures int) string {
	if failures > 0 && passRate >= 99.95 {
		// Use higher precision for near-100% pass rates with failures.
		return fmt.Sprintf("%.2f%%", passRate)
	}

	return fmt.Sprintf("%.1f%%", passRate)
}

// formatNumber formats a number with thousand separators.
func formatNumber(n int) string {
	str := fmt.Sprintf("%d", n)
	if n < 1000 {
		return str
	}

	// Add comma separators
	// Pre-allocate with estimated size (original length + commas)
	result := make([]byte, 0, len(str)+(len(str)-1)/3)

	for i, digit := range str {
		if i > 0 && (len(str)-i)%3 == 0 {
			result = append(result, ',')
		}

		result = append(result, byte(digit))
	}

	return string(result)
}

// buildTestSuiteLinks creates a link to the Hive dashboard for viewing detailed results.
func buildTestSuiteLinks(clientName string, results []hive.TestResult, network string, hiveClient hive.Hive) string {
	if network == "" {
		return ""
	}

	// Map network name for Hive URL
	mappedNetworkName := hiveClient.MapNetworkName(network)

	// Create a single link to the Hive dashboard for this network
	hiveURL := fmt.Sprintf("https://hive.ethpandaops.io/#/group/%s", mappedNetworkName)

	return fmt.Sprintf("📊 [View detailed results in Hive](%s)", hiveURL)
}

// detectAnomalies in test results.
func detectAnomalies(clientKey string, result *hive.ClientSummary, prevSummary *hive.SummaryResult, results []hive.TestResult) []string {
	// If no previous summary, we can't detect anomalies.
	if prevSummary == nil {
		return nil
	}

	var anomalies []string

	// Check for significant pass rate drops.
	if result.FailedTests > 0 {
		prevClient, ok := prevSummary.ClientResults[clientKey]
		if ok && prevClient.TotalTests > 0 {
			prevPassRate := float64(prevClient.PassedTests) / float64(prevClient.TotalTests) * 100
			passRateDrop := prevPassRate - result.PassRate

			// If pass rate dropped by more than 5 percentage points, flag it
			// But only if it's not already obvious from the failure count.
			if passRateDrop > 5 && result.FailedTests <= prevClient.FailedTests {
				anomalies = append(anomalies, fmt.Sprintf("⚠️ Unusual: Pass rate dropped by %.1f%% since last check", passRateDrop))
			}

			// If failures increased by more than 50%, flag it.
			// But only if the absolute increase is significant (more than 10).
			// This avoids cases like "increased by 300%" when going from 1 to 4 failures.
			if prevClient.FailedTests > 0 && result.FailedTests > prevClient.FailedTests {
				failureIncrease := result.FailedTests - prevClient.FailedTests
				failureIncreasePercent := float64(failureIncrease) / float64(prevClient.FailedTests) * 100

				if failureIncreasePercent > 100 && failureIncrease > 10 {
					anomalies = append(anomalies, fmt.Sprintf("⚠️ Unusual: Failures increased by %.0f%% since last check", failureIncreasePercent))
				}
			}

			// If client previously had zero failures but now has failures, flag it.
			// But only if it's a significant number of failures (more than 5).
			if prevClient.FailedTests == 0 && result.FailedTests > 5 {
				anomalies = append(anomalies, "⚠️ Unusual: Previously passing all tests, now failing multiple tests")
			}
		}
	}

	// Group results by test type for this client.
	testTypeResults := make(map[string]hive.TestResult)

	for _, r := range results {
		if r.Client == clientKey {
			// If we have multiple results for the same test type, use the most recent one.
			existing, exists := testTypeResults[r.Name]
			if !exists || r.Timestamp.After(existing.Timestamp) {
				testTypeResults[r.Name] = r
			}
		}
	}

	// Check for test types that suddenly started failing.
	for testType, currentResult := range testTypeResults {
		// Skip if the test is passing now.
		if currentResult.Fails == 0 {
			continue
		}

		// Check if this test type was previously passing for a long time.
		var (
			consecutivelyPassing bool
			oldestPassingResult  time.Time
		)

		for _, prevResult := range results {
			if prevResult.Client == clientKey && prevResult.Name == testType &&
				prevResult.Timestamp.Before(currentResult.Timestamp) &&
				prevResult.Fails == 0 && prevResult.NTests > 0 {
				if oldestPassingResult.IsZero() || prevResult.Timestamp.Before(oldestPassingResult) {
					oldestPassingResult = prevResult.Timestamp
				}

				consecutivelyPassing = true
			}
		}

		// Only report if the test has been passing for a while (more than 7 days).
		if consecutivelyPassing && !oldestPassingResult.IsZero() {
			daysSincePassing := int(currentResult.Timestamp.Sub(oldestPassingResult).Hours() / 24)
			if daysSincePassing > 7 {
				anomalies = append(
					anomalies,
					fmt.Sprintf(
						"⚠️ Unusual: `%s` tests failing after passing for %d+ days",
						testType,
						daysSincePassing,
					),
				)
			}
		}
	}

	return anomalies
}

// cleanVersionString cleans up version strings to make them more readable.
func cleanVersionString(version string) string {
	if version == "" || version == "unknown" {
		return ""
	}

	// Generic pattern: client/version/platform
	// Examples:
	// - Geth/v1.15.0-unstable-7f0dd394-20250204/linux-amd64/...
	// - besu/v25.3-develop-083b1d3/linux-x86_64/openjdk-java...
	// - nimbus-eth1/v0.1.0-45767278/linux-amd64/Nim-2.0.14...
	if strings.Contains(version, "/") {
		parts := strings.Split(version, "/")
		if len(parts) >= 2 {
			// Check if the second part looks like a version (starts with v or has digits)
			if strings.HasPrefix(parts[1], "v") || containsDigit(parts[1]) {
				return parts[1] // Return the version part
			}
		}
	}

	// Handle colon-separated formats
	// Examples:
	// - reth Version: 1.2.2
	// - geth Version: 1.22
	// - version: 1.09
	// - Platform: Linux x64
	if strings.Contains(version, ":") {
		parts := strings.Split(version, ":")
		if len(parts) >= 2 {
			// Check if the second part contains digits (likely a version number)
			secondPart := strings.TrimSpace(parts[1])
			if containsDigit(secondPart) {
				return secondPart
			}

			return secondPart // Return whatever is after the colon
		}
	}

	// Limit length
	maxLen := 30
	if len(version) > maxLen {
		version = version[:maxLen] + "..."
	}

	return strings.TrimSpace(version)
}

// containsDigit checks if a string contains at least one digit.
func containsDigit(s string) bool {
	for _, c := range s {
		if c >= '0' && c <= '9' {
			return true
		}
	}

	return false
}
