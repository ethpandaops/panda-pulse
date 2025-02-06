/*
Package analyzer provides functionality for analyzing the health relationships between Ethereum consensus layer (CL)
and execution layer (EL) clients. It helps identify root causes of client failures by detecting patterns where:

1. For CL clients: If an EL client is failing with multiple CL clients, it's likely the root cause
2. For EL clients: If the target EL client is failing with multiple CL clients, it's likely the root cause

The analyzer tracks client relationships and their health status, distinguishing between explained issues
(traced back to a root cause) and unexplained issues that may need further investigation.

Example usage:

	analyzer := NewAnalyzer("geth", ClientTypeEL)
	analyzer.AddNodeStatus("lighthouse-geth-1", false)
	analyzer.AddNodeStatus("prysm-geth-1", false)
	result := analyzer.Analyze()
*/
package analyzer

import (
	"fmt"
	"log"
	"strings"
)

const (
	// MinFailuresForRootCause is the minimum number of failures needed to consider something a root cause.
	MinFailuresForRootCause = 2
)

// Analyzer is a struct that analyzes the status of a client.
type Analyzer struct {
	// nodeStatusMap tracks the health status of all client pairs (CL-EL relationships).
	// The map key is a ClientPair (e.g., lighthouse-geth), and the value is a list of NodeStatus
	// for all instances of that pair.
	nodeStatusMap NodeStatusMap
	// targetClient is the name of the client we're analyzing (e.g., "geth", "lighthouse").
	targetClient string
	// clientType indicates whether we're analyzing a CL or EL client.
	clientType ClientType
}

// clientStatusTracker tracks the health status and relationships of a client.
type clientStatusTracker struct {
	// totalPeers is the total number of peer relationships this client has.
	// e.g., for an EL client, this would be the number of CL clients it pairs with.
	totalPeers int
	// failingPeers is the count of failing peer relationships.
	// Used to track how many peers are having issues with this client.
	failingPeers int
	// failingList contains the names of peers that are failing with this client.
	// e.g., for an EL client, this would be the list of CL client names that are failing.
	// Used for logging and evidence gathering when determining root causes.
	failingList []string
}

// NewAnalyzer creates a new Analyzer.
func NewAnalyzer(targetClient string, clientType ClientType) *Analyzer {
	return &Analyzer{
		nodeStatusMap: make(NodeStatusMap),
		targetClient:  targetClient,
		clientType:    clientType,
	}
}

// Analyze analyzes the status of a client.
func (a *Analyzer) Analyze() *AnalysisResult {
	log.Printf("\n=== Analyzing %s (%s)", a.targetClient, a.clientType)

	result := &AnalysisResult{
		RootCause:         make([]string, 0),
		UnexplainedIssues: make([]string, 0),
		AffectedNodes:     make(map[string][]string),
		RootCauseEvidence: make(map[string]string),
	}

	var (
		rootCauses []string
		evidence   map[string]string
	)

	switch a.clientType {
	case ClientTypeCL:
		status := a.analyzeClientRelationships(ClientTypeCL)
		rootCauses, evidence = a.findRootCausesForCL(status)
	case ClientTypeEL:
		rootCauses, evidence = a.findRootCausesForEL()
	default:
		// If we made it here, we have bigger problems.
		log.Printf("  - Unknown client type: %s", a.clientType)
	}

	result.RootCause = rootCauses
	result.RootCauseEvidence = evidence
	result.UnexplainedIssues = a.findUnexplainedIssues(rootCauses)

	a.logAnalysisResults(result)

	return result
}

// AddNodeStatus adds a node status to the analyzer.
func (a *Analyzer) AddNodeStatus(nodeName string, isHealthy bool) {
	pair := parseClientPair(nodeName)

	if _, exists := a.nodeStatusMap[pair]; !exists {
		a.nodeStatusMap[pair] = make([]NodeStatus, 0)
	}

	a.nodeStatusMap[pair] = append(a.nodeStatusMap[pair], NodeStatus{
		Name:      nodeName,
		IsHealthy: isHealthy,
	})
}

// getClientAndPeerNames gets the client and peer names.
func (a *Analyzer) getClientAndPeerNames(pair ClientPair, targetType ClientType) (client, peer string) {
	if targetType == ClientTypeCL {
		return pair.ELClient, pair.CLClient
	}

	return pair.CLClient, pair.ELClient
}

// isTargetClientIssue checks if the issue is related to the target client.
func (a *Analyzer) isTargetClientIssue(pair ClientPair) bool {
	switch a.clientType {
	case ClientTypeCL:
		return pair.CLClient == a.targetClient
	case ClientTypeEL:
		return pair.ELClient == a.targetClient
	default:
		return false
	}
}

// hasHealthIssue checks if the node has a health issue.
func (a *Analyzer) hasHealthIssue(statuses []NodeStatus) bool {
	for _, status := range statuses {
		if !status.IsHealthy {
			return true
		}
	}

	return false
}

// analyzeClientRelationships analyzes the relationships between CL and EL clients counting total peers
// and any failing relationships we may have.
func (a *Analyzer) analyzeClientRelationships(targetType ClientType) map[string]*clientStatusTracker {
	status := make(map[string]*clientStatusTracker)

	// Count total peers.
	for pair := range a.nodeStatusMap {
		clientName, _ := a.getClientAndPeerNames(pair, targetType)
		if _, exists := status[clientName]; !exists {
			status[clientName] = &clientStatusTracker{}
		}

		status[clientName].totalPeers++
	}

	// Count failing relationships.
	for pair, statuses := range a.nodeStatusMap {
		if !a.hasHealthIssue(statuses) {
			continue
		}

		clientName, peerName := a.getClientAndPeerNames(pair, targetType)
		if status[clientName] != nil {
			status[clientName].addFailure(peerName)
		}
	}

	return status
}

// findTargetFailures finds the nodes that are failing for our target client.
func (a *Analyzer) findTargetFailures() []string {
	failures := make([]string, 0)

	for pair, statuses := range a.nodeStatusMap {
		if !a.isTargetClientIssue(pair) {
			continue
		}

		for _, status := range statuses {
			if !status.IsHealthy {
				failures = append(failures, status.Name)
			}
		}
	}

	return failures
}

// findUnexplainedIssues finds the nodes that are failing for our target client but are not explained by the root causes.
func (a *Analyzer) findUnexplainedIssues(rootCauses []string) []string {
	unexplained := make([]string, 0)

	for pair, statuses := range a.nodeStatusMap {
		if !a.isTargetClientIssue(pair) {
			continue
		}

		for _, status := range statuses {
			if !status.IsHealthy && !a.isIssueExplained(pair, rootCauses) {
				unexplained = append(unexplained, status.Name)
			}
		}
	}

	return unique(unexplained)
}

// isIssueExplained checks if the issue is explained by being classified as a root cause.
func (a *Analyzer) isIssueExplained(pair ClientPair, rootCauses []string) bool {
	for _, rootCause := range rootCauses {
		// If we're analyzing an EL client and it's the root cause, all its "issues" are explained.
		if a.clientType == ClientTypeEL && rootCause == a.targetClient {
			return true
		}

		// Now check if the client is failing due to it being a root cause.
		switch a.clientType {
		case ClientTypeCL:
			if pair.ELClient == rootCause {
				return true
			}
		case ClientTypeEL:
			if pair.CLClient == rootCause {
				return true
			}
		}
	}

	return false
}

// findRootCausesForCL finds the root causes for the CL client.
func (a *Analyzer) findRootCausesForCL(status map[string]*clientStatusTracker) ([]string, map[string]string) {
	var (
		rootCauses = make([]string, 0)
		evidence   = make(map[string]string)
	)

	for el, stat := range status {
		if el == "" {
			continue
		}

		log.Printf(
			"  - %s is failing with CL clients: %s",
			el,
			strings.Join(stat.failingList, ", "),
		)

		if len(stat.failingList) > MinFailuresForRootCause {
			rootCauses = append(rootCauses, el)
			evidence[el] = fmt.Sprintf(
				"Failing with %d CL clients: %s",
				len(stat.failingList),
				strings.Join(stat.failingList, ", "),
			)
		}
	}

	return rootCauses, evidence
}

// findRootCausesForEL finds any root causes for the EL client.
func (a *Analyzer) findRootCausesForEL() ([]string, map[string]string) {
	var (
		rootCauses = make([]string, 0)
		evidence   = make(map[string]string)
	)

	targetFailures := a.findTargetFailures()

	if len(targetFailures) > MinFailuresForRootCause {
		rootCauses = append(rootCauses, a.targetClient)
		evidence[a.targetClient] = fmt.Sprintf(
			"Failing with %d nodes: %s",
			len(targetFailures),
			strings.Join(targetFailures, ", "),
		)
	}

	return rootCauses, evidence
}

// logAnalysisResults logs the analysis results.
func (a *Analyzer) logAnalysisResults(result *AnalysisResult) {
	if len(result.UnexplainedIssues) == 0 && len(result.RootCause) == 0 {
		log.Printf("  - No issues to analyze")

		return
	}

	for _, cause := range result.RootCause {
		log.Printf("  - Root cause identified: %s (%s)", cause, result.RootCauseEvidence[cause])
	}

	for _, issue := range result.UnexplainedIssues {
		log.Printf("  - %s (unexplained issue)", issue)
	}
}

// addFailure adds a failure to the client status tracker.
func (c *clientStatusTracker) addFailure(peerName string) {
	c.failingPeers++

	if !contains(c.failingList, peerName) {
		c.failingList = append(c.failingList, peerName)
	}
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

// unique deduplicates a string slice.
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
