package checks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/analyzer"
	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/logger"
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
	ClientType() clients.ClientType
	// Run executes the check and returns the result.
	Run(ctx context.Context, log *logger.CheckLogger, cfg Config) (*Result, error)
}

// Config contains configuration for checks.
type Config struct {
	Network       string
	ConsensusNode string
	ExecutionNode string
}

// Runner executes health checks.
type Runner interface {
	// RegisterCheck adds a check to the runner.
	RegisterCheck(check Check)
	// RunChecks executes all registered checks.
	RunChecks(ctx context.Context) error
	// GetID returns the ID of the runner.
	GetID() string
	// GetLog returns the log of the runner.
	GetLog() *logger.CheckLogger
	// GetResults returns the results of the runner.
	GetResults() []*Result
	// GetAnalysis returns the analysis of the runner.
	GetAnalysis() *analyzer.AnalysisResult
}

// defaultRunner is a default implementation of the Runner interface.
type defaultRunner struct {
	id       string
	log      *logger.CheckLogger
	cfg      Config
	checks   []Check
	results  []*Result
	analysis *analyzer.AnalysisResult
}

// NewDefaultRunner creates a new default check runner.
func NewDefaultRunner(cfg Config) Runner {
	// Give the runner a unique ID, so we can identify things easily.
	id := generateCheckID()

	// Initialize check logger. We use this to dump a detailed log of the check run,
	// which is then persisted to S3 alongside other check artifacts. It helps us identify
	// how panda-pulse got to the conclusion it did as to whether we should notify or not.
	log := logger.NewCheckLogger(id)

	return &defaultRunner{
		id:     id,
		log:    log,
		cfg:    cfg,
		checks: make([]Check, 0),
	}
}

// GetID returns the ID of the runner.
func (r *defaultRunner) GetID() string {
	return r.id
}

// GetLog returns the log of the runner.
func (r *defaultRunner) GetLog() *logger.CheckLogger {
	return r.log
}

// GetResults returns the results of the runner.
func (r *defaultRunner) GetResults() []*Result {
	return r.results
}

// GetAnalysis returns the analysis of the runner.
func (r *defaultRunner) GetAnalysis() *analyzer.AnalysisResult {
	return r.analysis
}

// RegisterCheck adds a check to the runner.
func (r *defaultRunner) RegisterCheck(check Check) {
	r.checks = append(r.checks, check)
}

// RunChecks executes all registered checks.
func (r *defaultRunner) RunChecks(ctx context.Context) error {
	// Create analyzer based on which client type we're targeting.
	var (
		results = make([]*Result, 0)
		a       *analyzer.Analyzer
		client  string
	)

	if r.cfg.ConsensusNode != "" {
		a = analyzer.NewAnalyzer(r.log, r.cfg.ConsensusNode, analyzer.ClientTypeCL)
		client = r.cfg.ConsensusNode
	}

	if r.cfg.ExecutionNode != "" {
		a = analyzer.NewAnalyzer(r.log, r.cfg.ExecutionNode, analyzer.ClientTypeEL)
		client = r.cfg.ExecutionNode
	}

	r.log.Printf("=== Running checks:\n  - %s\n  - %s", client, r.cfg.Network)

	// Run all checks against ALL clients to gather complete data for analysis. This is important to
	// allow us to identify root causes behind some of the client issues.
	origConsensusNode := r.cfg.ConsensusNode
	origExecutionNode := r.cfg.ExecutionNode
	r.cfg.ConsensusNode = clients.ClientTypeAll.String()
	r.cfg.ExecutionNode = clients.ClientTypeAll.String()

	// As a first pass, gather all data for analysis.
	allResults := make([]*Result, 0)

	for _, check := range r.checks {
		result, err := check.Run(ctx, r.log, r.cfg)
		if err != nil {
			return fmt.Errorf("failed to run check %s: %w", check.Name(), err)
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

	// As a second pass, filter results to only include target client data.
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
			// to be including noise about other clients in the notification.
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

	// Dump out some info so we know what's going on.
	logAnalysisSummary(r.log, analysisResult)
	logNotificationDecision(r.log, client, analysisResult)

	// Restore original config.
	r.cfg.ConsensusNode = origConsensusNode
	r.cfg.ExecutionNode = origExecutionNode

	r.results = results
	r.analysis = analysisResult

	return nil
}

// logAnalysisSummary logs a summary of the analysis results.
func logAnalysisSummary(log *logger.CheckLogger, analysisResult *analyzer.AnalysisResult) {
	log.Printf("\n=== Analysis summary")

	switch {
	case len(analysisResult.RootCause) > 0 || len(analysisResult.UnexplainedIssues) > 0:
		for _, rc := range analysisResult.RootCause {
			log.Printf("  - %s identified as root cause", rc)
		}

		for _, issue := range analysisResult.UnexplainedIssues {
			log.Printf("  - %s (unexplained issue)", issue)
		}
	default:
		log.Printf("  - No issues detected")
	}
}

// logNotificationDecision logs whether we should notify about the client's issues and why.
func logNotificationDecision(log *logger.CheckLogger, client string, analysisResult *analyzer.AnalysisResult) {
	log.Print("\n=== Notification decision")

	switch {
	case contains(analysisResult.RootCause, client):
		log.Printf("  - NOTIFY: Client identified as root cause")
	case hasClientIssue(client, analysisResult.UnexplainedIssues):
		log.Printf("  - NOTIFY: Client has unexplained issues")
	default:
		log.Printf("  - NO NOTIFICATION: No root cause or unexplained issues")
	}
}

// hasClientIssue checks if any of the issues are related to the given client.
func hasClientIssue(client string, issues []string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, client) {
			return true
		}
	}

	return false
}

// contains checks if a string slice contains a value.
func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}

	return false
}

// generateCheckID generates a unique ID for a check run.
func generateCheckID() string {
	// Generate 8 random bytes.
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// If random fails, use timestamp only.
		return time.Now().UTC().Format("20060102-150405")
	}

	// Format as timestamp-random.
	return fmt.Sprintf("%s-%s",
		time.Now().UTC().Format("20060102-150405"),
		hex.EncodeToString(b),
	)
}
