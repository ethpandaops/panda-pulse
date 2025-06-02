package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/discord/cmd/common"
	"github.com/sirupsen/logrus"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

// WorkflowInfo contains information about a workflow.
type WorkflowInfo struct {
	Repository   string
	Branch       string
	Name         string
	BuildArgs    string
	HasBuildArgs bool
}

// GitHubFile represents a file from GitHub API.
//
//nolint:tagliatelle // Github defined structure.
type GitHubFile struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	DownloadURL string `json:"download_url"`
	Type        string `json:"type"`
}

// WorkflowInputs represents the inputs section of a GitHub workflow.
//
//nolint:tagliatelle // Github defined structure.
type WorkflowInputs struct {
	Repository struct {
		Default string `yaml:"default"`
	} `yaml:"repository"`
	Ref struct {
		Default string `yaml:"default"`
	} `yaml:"ref"`
	BuildArgs *struct {
		Default string `yaml:"default"`
	} `yaml:"build_args,omitempty"`
}

// Workflow represents a GitHub workflow file.
//
//nolint:tagliatelle // Github defined structure.
type Workflow struct {
	On struct {
		WorkflowDispatch struct {
			Inputs WorkflowInputs `yaml:"inputs"`
		} `yaml:"workflow_dispatch"`
	} `yaml:"on"`
}

// WorkflowFetcher handles fetching and caching workflow information.
type WorkflowFetcher struct {
	httpClient      *http.Client
	githubToken     string
	log             *logrus.Logger
	cache           map[string]WorkflowInfo
	cacheMutex      sync.RWMutex
	lastUpdated     time.Time
	cacheExpiration time.Duration
	botContext      common.BotContext // Add bot context to access Cartographoor
}

// NewWorkflowFetcher creates a new workflow fetcher.
func NewWorkflowFetcher(httpClient *http.Client, githubToken string, log *logrus.Logger, botContext common.BotContext) *WorkflowFetcher {
	return &WorkflowFetcher{
		httpClient:      httpClient,
		githubToken:     githubToken,
		log:             log,
		cache:           make(map[string]WorkflowInfo),
		cacheExpiration: 1 * time.Hour, // Cache for 1 hour
		botContext:      botContext,
	}
}

// RefreshCache forces a refresh of the workflow cache.
func (wf *WorkflowFetcher) RefreshCache() error {
	workflows, err := wf.fetchWorkflows()
	if err != nil {
		return fmt.Errorf("failed to refresh workflow cache: %w", err)
	}

	wf.cacheMutex.Lock()
	wf.cache = workflows
	wf.lastUpdated = time.Now()
	wf.cacheMutex.Unlock()

	wf.log.WithField("count", len(workflows)).Info("Workflow cache refreshed")

	return nil
}

// GetAllWorkflows returns all workflows from the GitHub repository.
func (wf *WorkflowFetcher) GetAllWorkflows() (map[string]WorkflowInfo, error) {
	wf.cacheMutex.RLock()
	if time.Since(wf.lastUpdated) < wf.cacheExpiration && len(wf.cache) > 0 {
		// Return cached data
		result := make(map[string]WorkflowInfo)

		for k, v := range wf.cache {
			result[k] = v
		}

		wf.cacheMutex.RUnlock()

		return result, nil
	}
	wf.cacheMutex.RUnlock()

	// Need to fetch fresh data
	workflows, err := wf.fetchWorkflows()
	if err != nil {
		// If we have stale cache data, use it rather than failing completely
		wf.cacheMutex.RLock()
		if len(wf.cache) > 0 {
			wf.log.WithError(err).Warn("Failed to fetch fresh workflows, using stale cache")

			result := make(map[string]WorkflowInfo)

			for k, v := range wf.cache {
				result[k] = v
			}

			wf.cacheMutex.RUnlock()

			return result, nil
		}

		wf.cacheMutex.RUnlock()

		return nil, fmt.Errorf("failed to fetch workflows and no cache available: %w", err)
	}

	// Update cache
	wf.cacheMutex.Lock()
	wf.cache = workflows
	wf.lastUpdated = time.Now()
	wf.cacheMutex.Unlock()

	return workflows, nil
}

// GetToolWorkflows returns tool workflows, excluding known EL/CL clients.
func (wf *WorkflowFetcher) GetToolWorkflows() (map[string]WorkflowInfo, error) {
	allWorkflows, err := wf.GetAllWorkflows()
	if err != nil {
		return nil, err
	}

	// Get all known clients from Cartographoor
	var (
		cartographoor  = wf.botContext.GetCartographoor()
		knownWorkflows = make(map[string]bool)
	)

	// Add all EL clients (map to their workflow names)
	for _, client := range cartographoor.GetELClients() {
		workflowName := wf.getClientToWorkflowName(client)
		knownWorkflows[workflowName] = true
	}

	// Add all CL clients (map to their workflow names)
	for _, client := range cartographoor.GetCLClients() {
		workflowName := wf.getClientToWorkflowName(client)
		knownWorkflows[workflowName] = true
	}

	// Filter out known client workflows
	toolWorkflows := make(map[string]WorkflowInfo)

	for name, workflow := range allWorkflows {
		if !knownWorkflows[name] {
			toolWorkflows[name] = workflow
		}
	}

	return toolWorkflows, nil
}

// getClientToWorkflowName maps client names to their corresponding workflow names.
func (wf *WorkflowFetcher) getClientToWorkflowName(clientName string) string {
	// Special case mapping for clients with different repo/workflow names
	switch clientName {
	case "nimbus":
		return "nimbus-eth2"
	case "nimbusel":
		return "nimbus-eth1"
	default:
		return clientName
	}
}

// fetchWorkflows fetches workflow information from GitHub.
func (wf *WorkflowFetcher) fetchWorkflows() (map[string]WorkflowInfo, error) {
	if wf.githubToken == "" {
		return nil, fmt.Errorf("GitHub token is required for workflow fetching")
	}

	// Fetch workflow files from GitHub
	files, err := wf.getWorkflowFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow files: %w", err)
	}

	workflows := make(map[string]WorkflowInfo)

	for _, file := range files {
		// Only process build-push-*.yml files
		if !strings.HasPrefix(file.Name, "build-push-") || !strings.HasSuffix(file.Name, ".yml") {
			continue
		}

		// Extract workflow name
		workflowName := strings.TrimPrefix(file.Name, "build-push-")
		workflowName = strings.TrimSuffix(workflowName, ".yml")

		// Fetch and parse workflow content
		workflowInfo, err := wf.parseWorkflow(file.DownloadURL, workflowName)
		if err != nil {
			wf.log.WithError(err).WithField("workflow", workflowName).Warn("Failed to parse workflow, skipping")

			continue
		}

		workflows[workflowName] = workflowInfo
	}

	return workflows, nil
}

// getWorkflowFiles fetches the list of workflow files from GitHub.
func (wf *WorkflowFetcher) getWorkflowFiles() ([]GitHubFile, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/.github/workflows", DefaultRepository)

	req, err := http.NewRequest("GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Authorization", "Bearer "+wf.githubToken)

	resp, err := wf.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch workflow files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var files []GitHubFile
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return files, nil
}

// parseWorkflow fetches and parses a workflow file to extract metadata.
func (wf *WorkflowFetcher) parseWorkflow(downloadURL, workflowName string) (WorkflowInfo, error) {
	req, err := http.NewRequest("GET", downloadURL, http.NoBody)
	if err != nil {
		return WorkflowInfo{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+wf.githubToken)

	resp, err := wf.httpClient.Do(req)
	if err != nil {
		return WorkflowInfo{}, fmt.Errorf("failed to fetch workflow content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return WorkflowInfo{}, fmt.Errorf("failed to fetch workflow, status %d", resp.StatusCode)
	}

	var workflow Workflow
	if err := yaml.NewDecoder(resp.Body).Decode(&workflow); err != nil {
		return WorkflowInfo{}, fmt.Errorf("failed to parse YAML: %w", err)
	}

	inputs := workflow.On.WorkflowDispatch.Inputs

	info := WorkflowInfo{
		Repository:   inputs.Repository.Default,
		Branch:       inputs.Ref.Default,
		Name:         workflowName,
		HasBuildArgs: inputs.BuildArgs != nil,
	}

	// Extract default build args if present
	if inputs.BuildArgs != nil {
		info.BuildArgs = inputs.BuildArgs.Default
	}

	// Set default branch if empty
	if info.Branch == "" {
		info.Branch = "main"
	}

	// Generate display name (capitalize and replace hyphens)
	displayName := strings.ReplaceAll(workflowName, "-", " ")
	titleCaser := cases.Title(language.English)
	displayName = titleCaser.String(displayName)
	info.Name = displayName

	return info, nil
}
