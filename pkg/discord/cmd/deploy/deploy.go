package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// InventoryResponse represents the structure of the inventory API response.
type InventoryResponse struct {
	EthereumPairs map[string]NodePair `json:"ethereum_pairs"`
}

// NodePair represents a pair of execution and consensus client nodes.
type NodePair struct {
	Consensus ConsensusNode `json:"consensus"`
	Execution ExecutionNode `json:"execution"`
}

// ConsensusNode represents a consensus client node.
type ConsensusNode struct {
	Client    string `json:"client"`
	Image     string `json:"image"`
	EnrString string `json:"enr"`
	PeerId    string `json:"peer_id"`
	BeaconURI string `json:"beacon_uri"`
}

// ExecutionNode represents an execution client node.
type ExecutionNode struct {
	Client   string `json:"client"`
	Image    string `json:"image"`
	EnodeStr string `json:"enode"`
	RpcURI   string `json:"rpc_uri"`
}

// filterNodesByClient filters nodes based on either consensus or execution client
func filterNodesByClient(pairs map[string]NodePair, clientFilter string) map[string]NodePair {
	filteredPairs := make(map[string]NodePair)

	for name, pair := range pairs {
		// Check if node name starts with the client filter (consensus client case)
		if strings.HasPrefix(name, clientFilter+"-") {
			filteredPairs[name] = pair
			continue
		}

		// Check if it's an execution client by examining the second part of the name
		// Format: consensusClient-executionClient-x
		parts := strings.Split(name, "-")
		if len(parts) >= 2 && parts[1] == clientFilter {
			filteredPairs[name] = pair
			continue
		}

		// As a fallback, also check the actual client names in the pair data
		if strings.EqualFold(pair.Consensus.Client, clientFilter) ||
			strings.EqualFold(pair.Execution.Client, clientFilter) {
			filteredPairs[name] = pair
		}
	}

	return filteredPairs
}

// prepareDryRun prepares a dry run message showing what would be deployed.
func (c *DeployCommand) prepareDryRun(network, clientFilter, dockerTag string) (string, error) {
	// Fetch the inventory
	inventory, err := c.fetchInventory(network)
	if err != nil {
		return "", fmt.Errorf("failed to fetch inventory: %w", err)
	}

	// Filter for the specified client (handling both consensus and execution clients)
	filteredPairs := filterNodesByClient(inventory.EthereumPairs, clientFilter)

	if len(filteredPairs) == 0 {
		return "", fmt.Errorf("no nodes found for client '%s' in network '%s'", clientFilter, network)
	}

	// Generate SSH commands as a dry run
	var results []string
	for name := range filteredPairs {
		sshHost := fmt.Sprintf("%s.%s.ethpandaops.io", name, network)
		sshUser := "devops"

		// Generate SSH command
		sshCommand := fmt.Sprintf("ssh %s@%s 'deploy-docker-image %s'", sshUser, sshHost, dockerTag)

		results = append(results, fmt.Sprintf("• **%s**: `%s`", name, sshCommand))
	}

	return fmt.Sprintf("Would deploy to %d nodes for client '%s':\n\n%s",
		len(filteredPairs),
		clientFilter,
		strings.Join(results, "\n")), nil
}

// deployWithProgress processes the deployment command with progress updates.
func (c *DeployCommand) deployWithProgress(network, clientFilter, dockerTag string, progressChan chan<- string) (string, error) {
	// Fetch the inventory
	inventory, err := c.fetchInventory(network)
	if err != nil {
		return "", fmt.Errorf("failed to fetch inventory: %w", err)
	}

	// Update progress
	progressChan <- fmt.Sprintf("🔄 Fetched inventory for network `%s`. Filtering for client `%s`...", network, clientFilter)

	// Filter for the specified client (handling both consensus and execution clients)
	filteredPairs := filterNodesByClient(inventory.EthereumPairs, clientFilter)

	if len(filteredPairs) == 0 {
		return "", fmt.Errorf("no nodes found for client '%s' in network '%s'", clientFilter, network)
	}

	// Update progress
	progressChan <- fmt.Sprintf("🔄 Found %d nodes for client `%s`. Starting deployment...", len(filteredPairs), clientFilter)

	// Create a wait group to wait for all deployments to complete
	var wg sync.WaitGroup

	// Create a mutex to protect the results slice
	var mu sync.Mutex

	// Collect results from all deployments
	results := make([]SSHResult, 0, len(filteredPairs))

	// Deploy to each node concurrently
	nodeNames := make([]string, 0, len(filteredPairs))
	for name := range filteredPairs {
		nodeNames = append(nodeNames, name)
	}

	// Sort node names for consistent order
	// sort.Strings(nodeNames) - Omitted for brevity

	totalNodes := len(nodeNames)
	completedNodes := 0

	for _, nodeName := range nodeNames {
		wg.Add(1)

		// Launch deployment in a goroutine
		go func(name string) {
			defer wg.Done()

			// Update progress for this node
			nodeProgressMsg := fmt.Sprintf("🔄 Deploying to node `%s` (%d/%d)...", name, completedNodes+1, totalNodes)
			progressChan <- nodeProgressMsg

			// Perform the deployment
			result := c.deployToNode(name, network, dockerTag)

			// Store the result
			mu.Lock()
			results = append(results, result)
			completedNodes++

			// Update progress with completion status
			var statusIcon string
			if result.Success {
				statusIcon = "✅"
			} else {
				statusIcon = "❌"
			}

			progressChan <- fmt.Sprintf("🔄 Progress: %d/%d nodes processed\n\nLast completed: %s `%s`",
				completedNodes, totalNodes, statusIcon, name)

			mu.Unlock()
		}(nodeName)

		// Add a small delay between starting deployments to avoid overwhelming systems
		time.Sleep(500 * time.Millisecond)
	}

	// Wait for all deployments to complete
	wg.Wait()

	// Format the results
	resultMsg := formatSSHResults(results)

	// Count successes and failures
	successes := 0
	for _, r := range results {
		if r.Success {
			successes++
		}
	}

	summary := fmt.Sprintf("Deployment complete: %d/%d successful", successes, len(results))

	// Send final progress update
	progressChan <- fmt.Sprintf("✅ Deployment finished. Processing results...")

	return fmt.Sprintf("## Deployment Results\n\n**Summary:** %s\n\n%s", summary, resultMsg), nil
}

// deploy is a simpler version without progress reporting - used for testing.
func (c *DeployCommand) deploy(network, clientFilter, dockerTag string) (string, error) {
	// Fetch the inventory
	inventory, err := c.fetchInventory(network)
	if err != nil {
		return "", fmt.Errorf("failed to fetch inventory: %w", err)
	}

	// Filter for the specified client (handling both consensus and execution clients)
	filteredPairs := filterNodesByClient(inventory.EthereumPairs, clientFilter)

	if len(filteredPairs) == 0 {
		return "", fmt.Errorf("no nodes found for client '%s' in network '%s'", clientFilter, network)
	}

	// In a real-world scenario with many nodes, we should consider implementing:
	// 1. Concurrency limits
	// 2. Progress reporting
	// 3. Error handling and retries

	// Create a wait group to wait for all deployments to complete
	var wg sync.WaitGroup

	// Create a mutex to protect the results slice
	var mu sync.Mutex

	// Collect results from all deployments
	results := make([]SSHResult, 0, len(filteredPairs))

	// For progress updates
	progressMsg := fmt.Sprintf("Found %d nodes for client '%s'. Deploying...", len(filteredPairs), clientFilter)

	// Deploy to each node concurrently
	for nodeName := range filteredPairs {
		wg.Add(1)

		// Launch deployment in a goroutine
		go func(name string) {
			defer wg.Done()

			// Perform the deployment
			result := c.deployToNode(name, network, dockerTag)

			// Store the result
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(nodeName)
	}

	// Wait for all deployments to complete
	wg.Wait()

	// Format the results
	resultMsg := formatSSHResults(results)

	// Count successes and failures
	successes := 0
	for _, r := range results {
		if r.Success {
			successes++
		}
	}

	summary := fmt.Sprintf("Deployment complete: %d/%d successful", successes, len(results))

	return fmt.Sprintf("%s\n\n%s\n\n%s", progressMsg, summary, resultMsg), nil
}

// fetchInventory fetches the inventory for the specified network.
func (c *DeployCommand) fetchInventory(network string) (*InventoryResponse, error) {
	url := fmt.Sprintf("https://config.%s.ethpandaops.io/api/v1/nodes/inventory", network)

	// Set a reasonable timeout for the HTTP request
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned non-OK status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var inventory InventoryResponse
	if err := json.Unmarshal(body, &inventory); err != nil {
		return nil, fmt.Errorf("failed to unmarshal inventory: %w", err)
	}

	return &inventory, nil
}
