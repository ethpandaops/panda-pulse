package checks

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/analyzer"
)

// Result represents the outcome of a health check.
type Result struct {
	Name          string
	Category      Category
	Status        Status
	Description   string
	Timestamp     time.Time
	Details       map[string]interface{}
	AffectedNodes []string
}

// Status represents the status of a check.
type Status string

// Define the statuses.
const (
	StatusOK   Status = "OK"
	StatusFail Status = "FAIL"
)

// Check represents a single health check.
type Check interface {
	// Name returns the name of the check.
	Name() string
	// Category returns the category of the check.
	Category() Category
	// ClientType returns the client type of the check.
	ClientType() ClientType
	// Run executes the check and returns the result.
	Run(ctx context.Context, cfg Config) (*Result, error)
}

// Config contains configuration for checks.
type Config struct {
	Network       string
	ConsensusNode string
	ExecutionNode string
	GrafanaToken  string
}

// Runner executes health checks.
type Runner interface {
	// RegisterCheck adds a check to the runner.
	RegisterCheck(check Check)
	// RunChecks executes all registered checks.
	RunChecks(ctx context.Context, cfg Config) ([]*Result, *analyzer.AnalysisResult, error)
}

// defaultRunner is a default implementation of the Runner interface.
type defaultRunner struct {
	checks []Check
}

// NewDefaultRunner creates a new default check runner.
func NewDefaultRunner() Runner {
	return &defaultRunner{
		checks: make([]Check, 0),
	}
}

// RegisterCheck adds a check to the runner.
func (r *defaultRunner) RegisterCheck(check Check) {
	r.checks = append(r.checks, check)
}

// RunChecks executes all registered checks.
func (r *defaultRunner) RunChecks(ctx context.Context, cfg Config) ([]*Result, *analyzer.AnalysisResult, error) {
	results := make([]*Result, 0)

	// Create analyzer based on which client type we're targeting.
	var (
		a      *analyzer.Analyzer
		client string
	)

	if cfg.ConsensusNode != ClientTypeAll.String() {
		a = analyzer.NewAnalyzer(cfg.ConsensusNode, analyzer.ClientTypeCL)
		client = cfg.ConsensusNode
	} else if cfg.ExecutionNode != ClientTypeAll.String() {
		a = analyzer.NewAnalyzer(cfg.ExecutionNode, analyzer.ClientTypeEL)
		client = cfg.ExecutionNode
	}

	log.Printf("=== Running checks:\n  - %s\n  - %s", client, cfg.Network)

	// Run all checks against ALL clients to gather complete data for analysis. This is important to
	// allow us to identify root causes behind some of the client issues.
	origConsensusNode := cfg.ConsensusNode
	origExecutionNode := cfg.ExecutionNode
	cfg.ConsensusNode = ClientTypeAll.String()
	cfg.ExecutionNode = ClientTypeAll.String()

	// First pass: gather all data for analysis.
	allResults := make([]*Result, 0)

	for _, check := range r.checks {
		result, err := check.Run(ctx, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to run check %s: %w", check.Name(), err)
		}

		// Add all affected nodes to analyzer for complete analysis.
		if result.Status == StatusFail {
			for _, node := range result.AffectedNodes {
				a.AddNodeStatus(node, false)
			}
		}

		allResults = append(allResults, result)
	}

	// Run analysis with complete data.
	analysisResult := a.Analyze()

	// Second pass: filter results to only include target client data.
	for _, result := range allResults {
		if result.Status == StatusFail {
			// Create a filtered copy of the result.
			filteredResult := &Result{
				Name:          result.Name,
				Category:      result.Category,
				Status:        result.Status,
				Description:   result.Description,
				Timestamp:     result.Timestamp,
				Details:       make(map[string]interface{}),
				AffectedNodes: make([]string, 0),
			}

			// Filter affected nodes..
			for _, node := range result.AffectedNodes {
				if strings.Contains(node, client) {
					filteredResult.AffectedNodes = append(filteredResult.AffectedNodes, node)
				}
			}

			// Only include result if it has affected nodes for our target client. We don't want
			// to be including noisy about other clients in the notification.
			if len(filteredResult.AffectedNodes) > 0 {
				// Copy and filter details.
				for k, v := range result.Details {
					if k == "query" {
						filteredResult.Details[k] = v

						continue
					}

					// Filter node lists in details.
					if str, ok := v.(string); ok {
						filtered := make([]string, 0)

						for _, line := range strings.Split(str, "\n") {
							if strings.Contains(line, client) {
								filtered = append(filtered, line)
							}
						}

						if len(filtered) > 0 {
							filteredResult.Details[k] = strings.Join(filtered, "\n")
						}
					}
				}

				results = append(results, filteredResult)
			}
		}
	}

	// Log analysis summary.
	log.Printf("\n=== Analysis summary")

	if len(analysisResult.RootCause) > 0 {
		for _, rc := range analysisResult.RootCause {
			log.Printf("  - %s identified as root cause", rc)
		}
	}

	if len(analysisResult.UnexplainedIssues) > 0 {
		for _, issue := range analysisResult.UnexplainedIssues {
			log.Printf("  - %s (unexplained issue)", issue)
		}
	}

	if len(analysisResult.RootCause) == 0 && len(analysisResult.UnexplainedIssues) == 0 {
		log.Printf("  - No issues detected")
	}

	// Log our notification decision.
	var (
		hasUnexplainedIssues bool
		isRootCause          bool
	)

	for _, rc := range analysisResult.RootCause {
		if rc == client {
			isRootCause = true

			break
		}
	}

	for _, issue := range analysisResult.UnexplainedIssues {
		if strings.Contains(issue, client) {
			hasUnexplainedIssues = true

			break
		}
	}

	log.Print("\n=== Notification decision")

	if isRootCause {
		log.Printf("  - NOTIFY: Client identified as root cause")
	} else if hasUnexplainedIssues {
		log.Printf("  - NOTIFY: Client has unexplained issues")
	} else {
		log.Printf("  - NO NOTIFICATION: No root cause or unexplained issues")
	}

	// Restore original config.
	cfg.ConsensusNode = origConsensusNode
	cfg.ExecutionNode = origExecutionNode

	return results, analysisResult, nil
}
