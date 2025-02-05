package analyzer

import (
	"fmt"
	"log"
	"strings"
)

// Analyzer is a struct that analyzes the status of a client.
type Analyzer struct {
	nodeStatusMap NodeStatusMap
	targetClient  string
	clientType    ClientType
}

// NewAnalyzer creates a new Analyzer.
func NewAnalyzer(targetClient string, clientType ClientType) *Analyzer {
	return &Analyzer{
		nodeStatusMap: make(NodeStatusMap),
		targetClient:  targetClient,
		clientType:    clientType,
	}
}

// AddNodeStatus adds a node status to the analyzer.
func (a *Analyzer) AddNodeStatus(nodeName string, isHealthy bool) {
	pair := ParseClientPair(nodeName)
	if _, exists := a.nodeStatusMap[pair]; !exists {
		a.nodeStatusMap[pair] = make([]NodeStatus, 0)
	}

	a.nodeStatusMap[pair] = append(a.nodeStatusMap[pair], NodeStatus{
		Name:      nodeName,
		IsHealthy: isHealthy,
	})
}

// Analyze analyzes the status of the client.
func (a *Analyzer) Analyze() *AnalysisResult {
	result := &AnalysisResult{
		RootCause:         make([]string, 0),
		UnexplainedIssues: make([]string, 0),
		AffectedNodes:     make(map[string][]string),
		RootCauseEvidence: make(map[string]string),
	}

	log.Printf("\n=== Analyzing %s (%s)", a.targetClient, a.clientType)

	// For CL clients, check if any EL clients are having widespread issues. For example, if
	// we consistently see the same EL clients failing across multiple CL clients, we can
	// reasonably conclude that the EL clients are the root cause in this scenario.
	elStatus := make(map[string]struct {
		totalCLs    int
		failingCLs  int
		failingList []string
	})

	// First identify root causes.
	if a.clientType == ClientTypeCL {
		// Count total CL clients for each EL.
		for pair := range a.nodeStatusMap {
			if _, exists := elStatus[pair.ELClient]; !exists {
				elStatus[pair.ELClient] = struct {
					totalCLs    int
					failingCLs  int
					failingList []string
				}{0, 0, make([]string, 0)}
			}

			status := elStatus[pair.ELClient]
			status.totalCLs++
			elStatus[pair.ELClient] = status
		}

		// Then count failing relationships.
		for pair, statuses := range a.nodeStatusMap {
			hasIssue := false

			for _, status := range statuses {
				if !status.IsHealthy {
					hasIssue = true

					break
				}
			}

			if hasIssue {
				status := elStatus[pair.ELClient]
				status.failingCLs++

				if !contains(status.failingList, pair.CLClient) {
					status.failingList = append(status.failingList, pair.CLClient)
				}

				elStatus[pair.ELClient] = status
			}
		}

		// Identify EL clients that are failing with multiple CL clients.
		for el, status := range elStatus {
			if el == "" {
				continue // Skip empty client names.
			}

			log.Printf(
				"  - %s is failing with CL clients: %s",
				el,
				strings.Join(status.failingList, ", "),
			)

			if len(status.failingList) > 2 {
				result.RootCause = append(result.RootCause, el)
				result.RootCauseEvidence[el] = fmt.Sprintf(
					"Failing with %d CL clients: %s",
					len(status.failingList),
					strings.Join(status.failingList, ", "),
				)
			}
		}
	}

	// Now identify unexplained issues.
	for pair, statuses := range a.nodeStatusMap {
		for _, status := range statuses {
			if !status.IsHealthy {
				// Only consider issues with our target client. We don't want to be including
				// noise about other clients in the individual client notifications.
				if (a.clientType == ClientTypeCL && pair.CLClient == a.targetClient) ||
					(a.clientType == ClientTypeEL && pair.ELClient == a.targetClient) {
					isExplained := false

					for _, rootCause := range result.RootCause {
						if (a.clientType == ClientTypeCL && pair.ELClient == rootCause) ||
							(a.clientType == ClientTypeEL && pair.CLClient == rootCause) {
							isExplained = true

							break
						}
					}

					if !isExplained {
						result.UnexplainedIssues = append(result.UnexplainedIssues, status.Name)
					}
				}
			}
		}
	}

	result.UnexplainedIssues = unique(result.UnexplainedIssues)
	for _, issue := range result.UnexplainedIssues {
		log.Printf("  - %s (unexplained issue)", issue)
	}

	if len(elStatus) == 0 && len(result.UnexplainedIssues) == 0 {
		log.Printf("  - No issues to analyze")
	}

	return result
}

// Helper function to check if a string slice contains a value.
func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}

	return false
}

// Helper function to deduplicate a string slice.
func unique(slice []string) []string {
	var (
		seen   = make(map[string]bool)
		result = make([]string, 0)
	)

	for _, str := range slice {
		if !seen[str] {
			seen[str] = true

			result = append(result, str)
		}
	}

	return result
}
