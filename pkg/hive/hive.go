package hive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

const (
	unknown               = "unknown"
	BaseURL               = "https://hive.ethpandaops.io"
	defaultViewportWidth  = 500
	defaultViewportHeight = 800
	httpTimeout           = 30 * time.Second
)

var httpClient = &http.Client{
	Timeout: httpTimeout,
}

// Hive is the interface for Hive operations.
type Hive interface {
	// Snapshot takes a screenshot of the test coverage for a specific client.
	Snapshot(ctx context.Context, cfg SnapshotConfig) ([]byte, error)
	// IsAvailable checks if Hive is available for a given network.
	IsAvailable(ctx context.Context, network string) (bool, error)
	// GetBaseURL returns the base URL of the Hive instance.
	GetBaseURL() string
	// FetchTestResults fetches the latest test results for a network.
	FetchTestResults(ctx context.Context, network string) ([]TestResult, error)
	// ProcessSummary processes test results into a summary.
	ProcessSummary(results []TestResult) *SummaryResult
	// MapNetworkName maps the network name to the corresponding Hive network name.
	MapNetworkName(network string) string
}

// hive is a Hive client implementation of Hive.
type hive struct {
	baseURL string
}

// clientNameMap maps our internal client names to Hive's client names, some of them differ slightly.
var clientNameMap = map[string]string{
	"geth":     "go-ethereum",
	"nimbusel": "nimbus-el",
}

// networkNameMap maps fully qualified network names to Hive's simpler network names.
var networkNameMap = map[string]string{
	"pectra-devnet-6": "pectra",
	// Add other mappings as needed
}

// NewHive creates a new Hive client.
func NewHive(cfg *Config) Hive {
	return &hive{
		baseURL: cfg.BaseURL,
	}
}

// MapNetworkName maps our fully qualified network name to Hive's simpler network name.
func (h *hive) MapNetworkName(network string) string {
	return mapNetworkName(network)
}

// GetBaseURL returns the base URL of the Hive instance.
func (h *hive) GetBaseURL() string {
	return h.baseURL
}

// Snapshot takes a screenshot of the test coverage for a specific client.
func (h *hive) Snapshot(ctx context.Context, cfg SnapshotConfig) ([]byte, error) {
	// Ensure the configuration is valid.
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// Create browser context with mobile viewport.
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), getDefaultChromeOptions()...)
	defer cancel()

	browserCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Set timeout.
	timeoutCtx, cancel := context.WithTimeout(browserCtx, httpTimeout)
	defer cancel()

	// Determine which client to screenshot and map the name.
	var clientName string
	if cfg.ConsensusNode != "" {
		clientName = mapClientName(cfg.ConsensusNode)
	} else {
		clientName = mapClientName(cfg.ExecutionNode)
	}

	// Map network name for Hive
	hiveNetwork := mapNetworkName(cfg.Network)

	// Build the URL + build a selector for both boxes (consume-engine and consume-rlp).
	var (
		pageURL  = fmt.Sprintf("%s/%s/index.html#summary-sort=name&group-by=client", h.baseURL, hiveNetwork)
		selector = fmt.Sprintf(`div[data-client="%s_default"][class*="client-box"]`, clientName)
		buf      []byte
		exists   bool
	)

	// First check if the element exists.
	if err := chromedp.Run(
		timeoutCtx,
		chromedp.Navigate(pageURL),
		chromedp.WaitVisible(`div[class*="client-box"]`),
		chromedp.WaitReady("body"),
		chromedp.Evaluate(fmt.Sprintf(`document.querySelector('%s') !== null`, selector), &exists),
	); err != nil {
		return nil, fmt.Errorf("failed to check element existence: %w", err)
	}

	// Not all clients have hive tests, we're done.
	if !exists {
		return nil, nil
	}

	// Get the parent div that contains both boxes.
	parentSelector := fmt.Sprintf(
		`//div[contains(@class, "client-box") and @data-client="%s_default"]/ancestor::div[contains(@class, "suite-box")]`,
		clientName,
	)

	if err := chromedp.Run(
		timeoutCtx,
		chromedp.WaitVisible(selector),
		chromedp.Screenshot(parentSelector, &buf, chromedp.NodeVisible, chromedp.BySearch),
	); err != nil {
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}

	return buf, nil
}

// IsAvailable checks if Hive is available for a given network.
func (h *hive) IsAvailable(ctx context.Context, network string) (bool, error) {
	if network == "" {
		return false, fmt.Errorf("network cannot be empty")
	}

	// Map network name for Hive
	hiveNetwork := mapNetworkName(network)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodHead,
		fmt.Sprintf("%s/%s/index.html", h.baseURL, hiveNetwork),
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to check hive availability: %w", err)
	}

	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// FetchTestResults fetches the latest test results for a network.
func (h *hive) FetchTestResults(ctx context.Context, network string) ([]TestResult, error) {
	if network == "" {
		return nil, fmt.Errorf("network cannot be empty")
	}

	// Map network name for Hive
	hiveNetwork := mapNetworkName(network)

	// Fetch the listing.jsonl file which contains all test results
	listingURL := fmt.Sprintf("%s/%s/listing.jsonl", h.baseURL, hiveNetwork)
	fmt.Println("Fetching test results from:", listingURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listingURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch test results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch test results: status code %d", resp.StatusCode)
	}

	// Read and parse the JSONL file
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Split by newlines and parse each line as JSON
	lines := bytes.Split(body, []byte("\n"))
	allResults := make([]TestResult, 0, len(lines))

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		var result TestResult
		if err := json.Unmarshal(line, &result); err != nil {
			continue // Skip invalid lines
		}

		// If timestamp is zero, try to extract it from the filename
		// Filenames are often in the format: 1741786498-23e4ac7883f531a28a16a05cb3f4dc08.json
		// where the first part is a Unix timestamp
		if result.Timestamp.IsZero() && result.FileName != "" {
			parts := strings.Split(result.FileName, "-")
			if len(parts) > 0 {
				if ts, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
					result.Timestamp = time.Unix(ts, 0).UTC()
				}
			}
		}

		// Extract client name from the Clients array
		if len(result.Clients) > 0 {
			// Use the first client in the array
			clientFull := result.Clients[0]

			// Client names are typically in the format "client_default"
			// Extract just the client part
			if idx := strings.Index(clientFull, "_"); idx > 0 {
				result.Client = clientFull[:idx]
			} else {
				result.Client = clientFull
			}

			// Extract version from the Versions map
			if result.Versions != nil {
				if version, ok := result.Versions[clientFull]; ok {
					result.Version = version
				}
			}
		}

		// If client is still empty, use a default value
		if result.Client == "" {
			result.Client = unknown
		}

		// If version is empty, use a default value
		if result.Version == "" {
			result.Version = unknown
		}

		// If testSuiteID is empty, use the network name
		if result.TestSuiteID == "" {
			result.TestSuiteID = network // Use original network name, not the mapped one
		}

		allResults = append(allResults, result)
	}

	// Filter to only keep the most recent results for each client and test type
	// This prevents counting the same tests multiple times
	latestResults := filterLatestResults(allResults)

	return latestResults, nil
}

// ProcessSummary processes test results into a summary.
func (h *hive) ProcessSummary(results []TestResult) *SummaryResult {
	if len(results) == 0 {
		return nil
	}

	// Find the most recent timestamp from the results
	var latestTimestamp time.Time
	for _, result := range results {
		if result.Timestamp.After(latestTimestamp) {
			latestTimestamp = result.Timestamp
		}
	}

	// If we couldn't find a valid timestamp, use the current time.
	if latestTimestamp.IsZero() {
		latestTimestamp = time.Now().UTC()
	}

	// Use the original network name from the TestSuiteID for display purposes
	// This will be the fully qualified name that our system expects
	originalNetwork := results[0].TestSuiteID

	summary := &SummaryResult{
		Network:       originalNetwork,
		Timestamp:     latestTimestamp, // Use the most recent timestamp from the results.
		ClientResults: make(map[string]*ClientSummary),
		TestTypes:     make(map[string]struct{}),
	}

	// First, collect all unique test types.
	for _, result := range results {
		summary.TestTypes[result.Name] = struct{}{}
	}

	// Group results by client.
	clientResults := make(map[string][]TestResult)

	for _, result := range results {
		clientName := result.Client
		clientResults[clientName] = append(clientResults[clientName], result)
	}

	// Process each client's results.
	for clientName, clientTestResults := range clientResults {
		// Find the latest result for each test type.
		latestByTestType := make(map[string]TestResult)
		testTypes := make(map[string]struct{})

		for _, result := range clientTestResults {
			testType := result.Name
			testTypes[testType] = struct{}{}

			// If we haven't seen this test type yet, or this result is newer.
			if existing, exists := latestByTestType[testType]; !exists || result.Timestamp.After(existing.Timestamp) {
				latestByTestType[testType] = result
			}
		}

		// Create client summary using only the latest results.
		clientSummary := &ClientSummary{
			ClientName:    clientName,
			ClientVersion: "unknown",
			TestTypes:     make([]string, 0, len(testTypes)),
			TotalTests:    0,
			PassedTests:   0,
			FailedTests:   0,
		}

		// Add all test types this client was tested with.
		for testType := range testTypes {
			clientSummary.TestTypes = append(clientSummary.TestTypes, testType)
		}

		// Process the latest result for each test type.
		for _, result := range latestByTestType {
			// Use the version from the first result we process.
			if clientSummary.ClientVersion == "unknown" && result.Version != "" {
				clientSummary.ClientVersion = result.Version
			}

			// Add the test counts from this test type.
			clientSummary.TotalTests += result.NTests
			clientSummary.PassedTests += result.Passes
			clientSummary.FailedTests += result.Fails

			// Update overall counts.
			summary.TotalTests += result.NTests
			summary.TotalPasses += result.Passes
			summary.TotalFails += result.Fails
		}

		// Calculate pass rate.
		if clientSummary.TotalTests > 0 {
			clientSummary.PassRate = float64(clientSummary.PassedTests) / float64(clientSummary.TotalTests) * 100
		}

		summary.ClientResults[clientName] = clientSummary
	}

	// Calculate overall pass rate.
	if summary.TotalTests > 0 {
		summary.OverallPassRate = float64(summary.TotalPasses) / float64(summary.TotalTests) * 100
	}

	return summary
}

// filterLatestResults filters the results to only keep the most recent ones for each client and test type.
func filterLatestResults(results []TestResult) []TestResult {
	// Group results by client and test type.
	latestByClientAndType := make(map[string]map[string]TestResult)

	for _, result := range results {
		clientName := result.Client
		testType := result.Name

		// Initialize the map for this client if it doesn't exist.
		if _, exists := latestByClientAndType[clientName]; !exists {
			latestByClientAndType[clientName] = make(map[string]TestResult)
		}

		// Check if we already have a result for this client and test type.
		if existing, exists := latestByClientAndType[clientName][testType]; !exists || result.Timestamp.After(existing.Timestamp) {
			latestByClientAndType[clientName][testType] = result
		}
	}

	// Flatten the map back to a slice.
	filtered := make([]TestResult, 0)

	for _, testTypes := range latestByClientAndType {
		for _, result := range testTypes {
			filtered = append(filtered, result)
		}
	}

	return filtered
}

// mapClientName maps our internal client name to Hive's client name.
func mapClientName(client string) string {
	if mapped, ok := clientNameMap[client]; ok {
		return mapped
	}

	return client
}

// mapNetworkName maps our fully qualified network name to Hive's simpler network name.
func mapNetworkName(network string) string {
	if mapped, ok := networkNameMap[network]; ok {
		return mapped
	}

	return network
}

func getDefaultChromeOptions() []chromedp.ExecAllocatorOption {
	return append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("headless", true),
		chromedp.WindowSize(defaultViewportWidth, defaultViewportHeight),
		chromedp.Flag("enable-mobile-emulation", true),
	)
}
