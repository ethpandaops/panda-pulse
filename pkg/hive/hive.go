package hive

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/chromedp/chromedp"
)

// BaseURL is the base URL of the Hive instance.
const BaseURL = "https://hive.ethpandaops.io"

// Hive is the interface for Hive operations.
type Hive interface {
	// Snapshot takes a screenshot of the test coverage for a specific client.
	Snapshot(ctx context.Context, cfg SnapshotConfig) ([]byte, error)
	// IsAvailable checks if Hive is available for a given network.
	IsAvailable(ctx context.Context, network string) (bool, error)
	// GetBaseURL returns the base URL of the Hive instance.
	GetBaseURL() string
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

// NewHive creates a new Hive client.
func NewHive(cfg *Config) Hive {
	return &hive{
		baseURL: cfg.BaseURL,
	}
}

// GetBaseURL returns the base URL of the Hive instance.
func (h *hive) GetBaseURL() string {
	return h.baseURL
}

// Snapshot takes a screenshot of the test coverage for a specific client.
func (h *hive) Snapshot(ctx context.Context, cfg SnapshotConfig) ([]byte, error) {
	// Create browser context with mobile viewport, use mobile emulation to get a better screenshot
	// and less dead space.
	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("headless", true),
		chromedp.WindowSize(500, 800),
		chromedp.Flag("enable-mobile-emulation", true),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	browserCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	// Set timeout
	timeoutCtx, cancel := context.WithTimeout(browserCtx, 10*time.Second)
	defer cancel()

	// Determine which client to screenshot and map the name.
	var clientName string
	if cfg.ConsensusNode != "" {
		clientName = mapClientName(cfg.ConsensusNode)
	} else if cfg.ExecutionNode != "" {
		clientName = mapClientName(cfg.ExecutionNode)
	} else {
		return nil, fmt.Errorf("no client specified")
	}

	// Build the URL + build a selector for both boxes (consume-engine and consume-rlp).
	var (
		pageURL  = fmt.Sprintf("%s/%s/index.html#summary-sort=name&group-by=client", h.baseURL, cfg.Network)
		selector = fmt.Sprintf(`div[data-client="%s_default"][class*="client-box"]`, clientName)
		buf      []byte
		exists   bool
	)

	// First check if the element exists
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
	parentSelector := fmt.Sprintf(`//div[contains(@class, "client-box") and @data-client="%s_default"]/ancestor::div[contains(@class, "suite-box")]`, clientName)

	if err := chromedp.Run(
		timeoutCtx,
		// Wait for any boxes for this client to be visible.
		chromedp.WaitVisible(selector),
		// Take screenshot of the parent div containing both boxes.
		chromedp.Screenshot(parentSelector, &buf, chromedp.NodeVisible, chromedp.BySearch),
	); err != nil {
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}

	return buf, nil
}

// IsAvailable checks if Hive is available for a given network.
func (h *hive) IsAvailable(ctx context.Context, network string) (bool, error) {
	url := fmt.Sprintf("%s/%s/index.html", h.baseURL, network)

	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("failed to check hive availability: %w", err)
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK, nil
}

// mapClientName maps our internal client name to Hive's client name.
func mapClientName(client string) string {
	if mapped, ok := clientNameMap[client]; ok {
		return mapped
	}

	return client
}
