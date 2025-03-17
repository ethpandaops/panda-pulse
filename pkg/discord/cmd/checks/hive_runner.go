package checks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/ethpandaops/panda-pulse/pkg/hive"
	"github.com/sirupsen/logrus"
)

// RunHiveSummary runs a Hive summary check for a given alert.
func (c *ChecksCommand) RunHiveSummary(ctx context.Context, alert *hive.HiveSummaryAlert) error {
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
	}).Info("Fetched Hive test results")

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
		}).Info("Comparing current summary with previous summary")
	}

	// Ensure we're not comparing a summary with itself
	if prevSummary != nil && summary.Timestamp.Equal(prevSummary.Timestamp) {
		c.log.Warn("Current and previous summaries have the same timestamp, skipping comparison")
		prevSummary = nil
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

// Helper function to get client names for logging
func getClientNames(summary *hive.SummaryResult) []string {
	names := make([]string, 0, len(summary.ClientResults))
	for clientName := range summary.ClientResults {
		names = append(names, clientName)
	}
	return names
}

// sendHiveSummary sends a Hive summary to Discord.
func (c *ChecksCommand) sendHiveSummary(
	ctx context.Context,
	alert *hive.HiveSummaryAlert,
	summary *hive.SummaryResult,
	prevSummary *hive.SummaryResult,
	results []hive.TestResult,
) error {
	session := c.bot.GetSession()

	// Send the combined summary overview and test type breakdown in the main channel
	overviewEmbed := createCombinedOverviewEmbed(summary, prevSummary, results)

	// Create message send object
	messageSend := &discordgo.MessageSend{
		Content: "",
		Embeds:  []*discordgo.MessageEmbed{overviewEmbed},
	}

	// Add button that links to the Hive dashboard only if network name is available
	networkName := summary.Network
	if networkName != "" {
		hiveURL := fmt.Sprintf("https://hive.ethpandaops.io/%s/index.html", networkName)

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

	// Create a thread for the client details
	threadName := fmt.Sprintf("Hive Test Results - %s", summary.Timestamp.Format(threadDateFormat))
	thread, err := session.MessageThreadStartComplex(alert.DiscordChannel, mainMessage.ID, &discordgo.ThreadStart{
		Name:                threadName,
		AutoArchiveDuration: threadAutoArchiveDuration,
	})
	if err != nil {
		return fmt.Errorf("failed to create thread: %w", err)
	}

	// Send client breakdown in the thread
	clientEmbed := createClientBreakdownEmbed(summary, prevSummary, results)
	_, err = session.ChannelMessageSendEmbed(thread.ID, clientEmbed)
	if err != nil {
		return fmt.Errorf("failed to send client breakdown embed: %w", err)
	}

	return nil
}

// createCombinedOverviewEmbed creates an embed with the summary overview and test type breakdown.
func createCombinedOverviewEmbed(summary *hive.SummaryResult, prevSummary *hive.SummaryResult, results []hive.TestResult) *discordgo.MessageEmbed {
	// Format the timestamp in a user-friendly way
	lastUpdated := summary.Timestamp.Format("Mon, 2 Jan 2006 15:04:05 MST")

	// Create the overview fields
	fields := []*discordgo.MessageEmbedField{
		{
			Name:   "Total Tests Run",
			Value:  fmt.Sprintf("%d", summary.TotalTests),
			Inline: true,
		},
		{
			Name:   "Overall Pass Rate",
			Value:  formatPassRate(summary.OverallPassRate, summary.TotalFails),
			Inline: true,
		},
		{
			Name:   "Total Failures",
			Value:  fmt.Sprintf("%d", summary.TotalFails),
			Inline: true,
		},
		{
			Name:   "Test Date",
			Value:  lastUpdated,
			Inline: false,
		},
	}

	// Add regression information if we have previous data
	if prevSummary != nil {
		regressions := detectRegressions(summary, prevSummary, results)
		if len(regressions) > 0 {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   "⚠️ Regressions Detected",
				Value:  formatRegressions(regressions),
				Inline: false,
			})
		}
	}

	// Add test type breakdown
	// Group results by test type
	testTypeResults := make(map[string]struct {
		Total  int
		Passes int
		Fails  int
	})

	// Process each test result and aggregate by test type
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

		// Add this result's counts to the test type totals
		stats.Total += result.NTests
		stats.Passes += result.Passes
		stats.Fails += result.Fails
		testTypeResults[testType] = stats
	}

	// Sort test types alphabetically
	testTypes := make([]string, 0, len(testTypeResults))
	for testType := range testTypeResults {
		testTypes = append(testTypes, testType)
	}
	sort.Strings(testTypes)

	// Add a separator field
	fields = append(fields, &discordgo.MessageEmbedField{
		Value:  "📊 **Test Type Breakdown**",
		Inline: false,
	})

	// Add test type fields
	for _, testType := range testTypes {
		stats := testTypeResults[testType]
		passRate := 0.0
		if stats.Total > 0 {
			passRate = float64(stats.Passes) / float64(stats.Total) * 100
		}

		// Format the pass rate with appropriate precision
		var passRateStr string
		if stats.Fails > 0 && passRate >= 99.95 {
			// Use higher precision for near-100% pass rates with failures
			passRateStr = fmt.Sprintf("%.2f%%", passRate)
		} else {
			passRateStr = fmt.Sprintf("%.1f%%", passRate)
		}

		fields = append(fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("`%s`", testType),
			Value:  fmt.Sprintf("%s pass (%d/%d)", passRateStr, stats.Passes, stats.Total),
			Inline: true,
		})
	}

	return &discordgo.MessageEmbed{
		Title:  "🐝 **Hive Test Results**",
		Color:  0x3498DB, // Blue
		Fields: fields,
	}
}

// detectRegressions identifies tests that were previously passing but are now failing
func detectRegressions(current *hive.SummaryResult, previous *hive.SummaryResult, results []hive.TestResult) map[string][]string {
	// Map of client -> list of regression descriptions
	regressions := make(map[string][]string)

	// First, group current results by client and test type
	currentTestResults := groupResultsByClientAndTestType(results)

	// Compare client results
	for clientName, currentResult := range current.ClientResults {
		// Skip if client has no failures
		if currentResult.FailedTests == 0 {
			continue
		}

		// Check if client existed in previous results
		prevResult, exists := previous.ClientResults[clientName]
		if !exists {
			// New client, can't determine regressions
			continue
		}

		// Check if failures increased
		if currentResult.FailedTests > prevResult.FailedTests {
			// This client has more failures than before
			increase := currentResult.FailedTests - prevResult.FailedTests

			// Find which test types have increased failures
			testTypeRegressions := findTestTypeRegressions(clientName, currentTestResults, previous, results)

			// Add overall regression info
			regressionMsg := fmt.Sprintf("%d new failures (from %d to %d)",
				increase, prevResult.FailedTests, currentResult.FailedTests)

			// Add test type details if available
			if len(testTypeRegressions) > 0 {
				regressionMsg += fmt.Sprintf("\n  Affected tests: %s", strings.Join(testTypeRegressions, ", "))
			}

			regressions[clientName] = append(regressions[clientName], regressionMsg)
		}
	}

	return regressions
}

// groupResultsByClientAndTestType groups test results by client and test type
func groupResultsByClientAndTestType(results []hive.TestResult) map[string]map[string]hive.TestResult {
	// Map of client -> test type -> result
	grouped := make(map[string]map[string]hive.TestResult)

	for _, result := range results {
		// Skip results with no tests
		if result.NTests == 0 {
			continue
		}

		// Initialize client map if needed
		if _, exists := grouped[result.Client]; !exists {
			grouped[result.Client] = make(map[string]hive.TestResult)
		}

		// Store the result by test type
		// If we already have a result for this test type, use the one with the most recent timestamp
		existingResult, exists := grouped[result.Client][result.Name]
		if !exists || result.Timestamp.After(existingResult.Timestamp) {
			grouped[result.Client][result.Name] = result
		}
	}

	return grouped
}

// findTestTypeRegressions identifies which test types have increased failures
func findTestTypeRegressions(clientName string, currentResults map[string]map[string]hive.TestResult,
	prevSummary *hive.SummaryResult, prevResults []hive.TestResult) []string {

	// Group previous results by client and test type
	prevTestResults := groupResultsByClientAndTestType(prevResults)

	// Check if we have current results for this client
	clientResults, exists := currentResults[clientName]
	if !exists {
		return nil
	}

	// Check if we have previous results for this client
	prevClientResults, exists := prevTestResults[clientName]
	if !exists {
		return nil
	}

	// Find test types with increased failures
	var regressions []string

	for testType, currentResult := range clientResults {
		// Skip if no failures in current result
		if currentResult.Fails == 0 {
			continue
		}

		// Check if we have previous results for this test type
		prevResult, exists := prevClientResults[testType]
		if !exists {
			// New test type, can't determine regression
			continue
		}

		// Check if failures increased
		if currentResult.Fails > prevResult.Fails {
			increase := currentResult.Fails - prevResult.Fails
			failRate := 0.0
			if currentResult.NTests > 0 {
				failRate = float64(currentResult.Fails) / float64(currentResult.NTests) * 100
			}

			regressions = append(regressions, fmt.Sprintf("`%s` (+%d, %.1f%% fail rate)",
				testType, increase, failRate))
		}
	}

	// Sort regressions for consistent output
	sort.Strings(regressions)

	return regressions
}

// formatRegressions formats the regression information for display
func formatRegressions(regressions map[string][]string) string {
	if len(regressions) == 0 {
		return "No regressions detected"
	}

	// Sort clients by name for consistent output
	clients := make([]string, 0, len(regressions))
	for client := range regressions {
		clients = append(clients, client)
	}
	sort.Strings(clients)

	// Build the output
	var lines []string
	for _, client := range clients {
		clientRegressions := regressions[client]
		for _, regression := range clientRegressions {
			lines = append(lines, fmt.Sprintf("• **%s**: %s", client, regression))
		}
	}

	return strings.Join(lines, "\n")
}

// createClientBreakdownEmbed creates an embed with the client breakdown.
func createClientBreakdownEmbed(summary *hive.SummaryResult, prevSummary *hive.SummaryResult, results []hive.TestResult) *discordgo.MessageEmbed {
	// Sort clients by failures (descending)
	clients := make([]string, 0, len(summary.ClientResults))
	for client := range summary.ClientResults {
		clients = append(clients, client)
	}

	sort.Slice(clients, func(i, j int) bool {
		return summary.ClientResults[clients[i]].FailedTests > summary.ClientResults[clients[j]].FailedTests
	})

	// If we have no clients, add a default entry
	if len(clients) == 0 {
		// Create a default field for the overall stats
		var value string
		if summary.TotalFails > 0 {
			value = fmt.Sprintf(
				"✅ %s Pass (%d/%d)\n❌ Failures: %d",
				formatPassRate(summary.OverallPassRate, summary.TotalFails),
				summary.TotalPasses,
				summary.TotalTests,
				summary.TotalFails,
			)
		} else {
			value = fmt.Sprintf(
				"✅ 100.0%% Pass (%d/%d)",
				summary.TotalPasses,
				summary.TotalTests,
			)
		}

		fields := []*discordgo.MessageEmbedField{
			{
				Name:  "**Overall Results**",
				Value: value,
			},
		}

		return &discordgo.MessageEmbed{
			Title:  "🔍 Client Performance",
			Color:  0x3498DB, // Blue
			Fields: fields,
		}
	}

	// Collect all test types to display at the top
	allTestTypes := make(map[string]struct{})
	for _, clientKey := range clients {
		result := summary.ClientResults[clientKey]
		for _, testType := range result.TestTypes {
			allTestTypes[testType] = struct{}{}
		}
	}

	// Convert to sorted slice
	testTypesList := make([]string, 0, len(allTestTypes))
	for testType := range allTestTypes {
		testTypesList = append(testTypesList, testType)
	}
	sort.Strings(testTypesList)

	// Create fields array
	fields := make([]*discordgo.MessageEmbedField, 0, len(clients)*2) // *2 for clients and separators

	// Limit the number of clients to display to avoid Discord embed size limit
	// Discord has a limit of 6000 characters per embed
	maxClients := 10
	if len(clients) > maxClients {
		clients = clients[:maxClients]
	}

	for i, clientKey := range clients {
		// Add a separator before each client except the first one
		if i > 0 {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   "\u200b", // Zero-width space
				Value:  "┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄",
				Inline: false,
			})
		}

		result := summary.ClientResults[clientKey]

		// Calculate change from previous day if available
		var passRateChangeStr string
		var failureChangeStr string
		if prevSummary != nil {
			if prevClient, ok := prevSummary.ClientResults[clientKey]; ok && prevClient.TotalTests > 0 {
				prevPassRate := float64(prevClient.PassedTests) / float64(prevClient.TotalTests) * 100
				change := result.PassRate - prevPassRate

				// Check if there are failure changes
				hasFailureChanges := result.FailedTests != prevClient.FailedTests

				// Show pass rate change if it's significant or if there are failure changes
				if change > 0.05 {
					passRateChangeStr = fmt.Sprintf("📈 Pass rate improved by %.1f%% since last run", change)
				} else if change < -0.05 {
					passRateChangeStr = fmt.Sprintf("📉 Pass rate decreased by %.1f%% since last run", -change)
				} else if hasFailureChanges {
					// For small pass rate changes with failure changes, still show the direction
					if change > 0 {
						passRateChangeStr = fmt.Sprintf("📈 Pass rate improved slightly (%.2f%%)", change)
					} else if change < 0 {
						passRateChangeStr = fmt.Sprintf("📉 Pass rate decreased slightly (%.2f%%)", -change)
					} else {
						passRateChangeStr = "Pass rate unchanged despite failure changes"
					}
				} else {
					// No significant pass rate change and no failure changes
					passRateChangeStr = "No change since last run"
				}

				// Add failure change information on a separate line
				if result.FailedTests > prevClient.FailedTests {
					failureIncrease := result.FailedTests - prevClient.FailedTests
					failureChangeStr = fmt.Sprintf("⚠️ %d new failures since last run", failureIncrease)
				} else if result.FailedTests < prevClient.FailedTests {
					failureDecrease := prevClient.FailedTests - result.FailedTests
					failureChangeStr = fmt.Sprintf("✅ %d fewer failures since last run", failureDecrease)
				}
			}
		}

		// Clean up the version string
		cleanVersion := cleanVersionString(result.ClientVersion)

		// Create field value
		value := ""

		// Only show failures if there are any
		if result.FailedTests > 0 {
			// If we have failures but the rounded pass rate is 100%, adjust the display
			if result.PassRate >= 99.95 {
				// Calculate more precise pass rate
				exactPassRate := float64(result.PassedTests) / float64(result.TotalTests) * 100
				value = fmt.Sprintf(
					"✅ %.2f%% Pass (%d/%d)",
					exactPassRate,
					result.PassedTests,
					result.TotalTests,
				)
			} else {
				value = fmt.Sprintf(
					"✅ %.1f%% Pass (%d/%d)",
					result.PassRate,
					result.PassedTests,
					result.TotalTests,
				)
			}

			// Add failure count
			value += fmt.Sprintf("\n❌ Failures: %d", result.FailedTests)
		} else {
			// No failures, just show the pass rate
			value = fmt.Sprintf(
				"✅ 100.0%% Pass (%d/%d)",
				result.PassedTests,
				result.TotalTests,
			)
		}

		// Add pass rate change information if available
		if passRateChangeStr != "" {
			value += fmt.Sprintf("\n%s", passRateChangeStr)
		}

		// Add failure change information if available
		if failureChangeStr != "" {
			value += fmt.Sprintf("\n%s", failureChangeStr)
		}

		// Add version info if available
		if cleanVersion != "" && cleanVersion != "unknown" {
			value = fmt.Sprintf("📦 %s\n%s", cleanVersion, value)
		}

		// Use a default name if ClientName is empty
		clientName := result.ClientName
		if clientName == "" {
			clientName = clientKey
		}

		// Add links to specific test suites if available
		testSuiteLinks := buildTestSuiteLinks(clientKey, results, summary.Network)

		// Add the links to the value
		if testSuiteLinks != "" {
			value = fmt.Sprintf("%s\n%s", value, testSuiteLinks)
		}

		fields = append(fields, &discordgo.MessageEmbedField{
			Name:  fmt.Sprintf("**%s**", clientName),
			Value: value,
		})
	}

	// If we limited the clients, add a note
	if len(summary.ClientResults) > maxClients {
		fields = append(fields, &discordgo.MessageEmbedField{
			Name: "Note",
			Value: fmt.Sprintf(
				"Showing top %d clients out of %d total. All clients are included in the overall statistics.",
				maxClients,
				len(summary.ClientResults),
			),
		})
	}

	return &discordgo.MessageEmbed{
		Title:  "🔍 Client Performance",
		Color:  0x3498DB, // Blue instead of green
		Fields: fields,
	}
}

// cleanVersionString cleans up version strings to make them more readable
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

// containsDigit checks if a string contains at least one digit
func containsDigit(s string) bool {
	for _, c := range s {
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

// formatTestTypesList formats a list of test types with code formatting
func formatTestTypesList(testTypes []string) string {
	formattedTypes := make([]string, len(testTypes))
	for i, testType := range testTypes {
		formattedTypes[i] = fmt.Sprintf("`%s`", testType)
	}
	return strings.Join(formattedTypes, ", ")
}

// formatPassRate formats a pass rate with appropriate precision
func formatPassRate(passRate float64, failures int) string {
	if failures > 0 && passRate >= 99.95 {
		// Use higher precision for near-100% pass rates with failures
		return fmt.Sprintf("%.2f%%", passRate)
	}
	return fmt.Sprintf("%.1f%%", passRate)
}

// buildTestSuiteLinks creates links to specific test suites for a client
func buildTestSuiteLinks(clientName string, results []hive.TestResult, network string) string {
	// Map to store the latest test suite ID and file name for each test type
	latestSuites := make(map[string]struct {
		suiteID  string
		fileName string
	})
	latestTimestamps := make(map[string]time.Time)

	// Find the latest test suite ID for each test type for this client
	for _, result := range results {
		if result.Client != clientName || result.TestSuiteID == "" {
			continue
		}

		// Check if we already have a timestamp for this test type
		currentTimestamp, exists := latestTimestamps[result.Name]
		if !exists || result.Timestamp.After(currentTimestamp) {
			latestSuites[result.Name] = struct {
				suiteID  string
				fileName string
			}{
				suiteID:  result.TestSuiteID,
				fileName: result.FileName,
			}
			latestTimestamps[result.Name] = result.Timestamp
		}
	}

	// If we don't have any test suites, return empty string
	if len(latestSuites) == 0 {
		return ""
	}

	// Use the provided network name, default to "pectra" if empty
	networkName := network
	if networkName == "" {
		networkName = "pectra"
	}

	// Build links for each test type
	var links []string
	for testType, suiteInfo := range latestSuites {
		// Use fileName if available, otherwise fallback to suiteID.json
		suitePath := suiteInfo.suiteID + ".json"
		if suiteInfo.fileName != "" {
			suitePath = suiteInfo.fileName
		}

		links = append(links, fmt.Sprintf("[%s](https://hive.ethpandaops.io/%s/suite.html?suiteid=%s)",
			testType, networkName, suitePath))
	}

	// Sort links alphabetically
	sort.Strings(links)

	// Limit to 3 links to avoid cluttering
	if len(links) > 3 {
		links = links[:3]
	}

	return strings.Join(links, " | ")
}
