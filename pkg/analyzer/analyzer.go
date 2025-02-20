package analyzer

import (
	"fmt"
	"strings"

	"github.com/ethpandaops/panda-pulse/pkg/logger"
)

const (
	MinFailuresForRootCause = 2
)

type ClientFailure struct {
	Client     string
	Type       ClientType
	FailedWith []string
}

type ClientPairWithNodes struct {
	Pair  ClientPair
	Nodes []string
}

type AnalysisState struct {
	CLFailures       map[string]*ClientFailure
	ELFailures       map[string]*ClientFailure
	RootCauses       map[string]string // key: client name, value: evidence
	UnexplainedPairs []ClientPairWithNodes
}

type Analyzer struct {
	nodeStatusMap NodeStatusMap
	targetClient  string
	clientType    ClientType
	log           *logger.CheckLogger
}

type Config struct {
	Network          string
	ConsensusNode    string
	ExecutionNode    string
	DiscordChannel   string
	GrafanaToken     string
	DiscordToken     string
	GrafanaBaseURL   string
	PromDatasourceID string
}

func NewAnalyzer(log *logger.CheckLogger, targetClient string, clientType ClientType) *Analyzer {
	return &Analyzer{
		nodeStatusMap: make(NodeStatusMap),
		targetClient:  targetClient,
		clientType:    clientType,
		log:           log,
	}
}

func (a *Analyzer) Analyze() *AnalysisResult {
	a.log.Print("\n=== Analyzing check results")

	state := &AnalysisState{
		CLFailures: make(map[string]*ClientFailure),
		ELFailures: make(map[string]*ClientFailure),
		RootCauses: make(map[string]string),
	}

	// Step 1: Collect all failures.
	a.collectFailures(state)

	// Step 2: Find primary root causes (clients failing with many peers).
	a.findPrimaryRootCauses(state)

	// Step 3: Find secondary root causes (clients failing with non-root-cause peers).
	a.findSecondaryRootCauses(state)

	// Step 4: Remove false positives (clients only failing with root causes).
	a.removeFalsePositives(state)

	// Step 5: Identify unexplained issues.
	a.findUnexplainedIssues(state)

	// Convert state to result.
	result := &AnalysisResult{
		RootCause:         make([]string, 0),
		UnexplainedIssues: make([]string, 0),
		AffectedNodes:     make(map[string][]string),
		RootCauseEvidence: state.RootCauses,
	}

	// Add root causes to result.
	for client := range state.RootCauses {
		result.RootCause = append(result.RootCause, client)
	}

	// Add unexplained issues to result.
	for _, pairWithNodes := range state.UnexplainedPairs {
		result.UnexplainedIssues = append(result.UnexplainedIssues, pairWithNodes.Nodes...)
	}

	a.logAnalysisResults(result)

	return result
}

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

func (a *Analyzer) collectFailures(state *AnalysisState) {
	// For each client pair and their statuses.
	for pair, statuses := range a.nodeStatusMap {
		// Skip if no failures.
		hasFailure := false

		for _, s := range statuses {
			if !s.IsHealthy {
				hasFailure = true

				break
			}
		}

		if !hasFailure {
			continue
		}

		// Add to CL failures.
		if _, exists := state.CLFailures[pair.CLClient]; !exists {
			state.CLFailures[pair.CLClient] = &ClientFailure{
				Client:     pair.CLClient,
				Type:       ClientTypeCL,
				FailedWith: make([]string, 0),
			}
		}

		if !contains(state.CLFailures[pair.CLClient].FailedWith, pair.ELClient) {
			state.CLFailures[pair.CLClient].FailedWith = append(
				state.CLFailures[pair.CLClient].FailedWith,
				pair.ELClient,
			)
		}

		// Add to EL failures.
		if _, exists := state.ELFailures[pair.ELClient]; !exists {
			state.ELFailures[pair.ELClient] = &ClientFailure{
				Client:     pair.ELClient,
				Type:       ClientTypeEL,
				FailedWith: make([]string, 0),
			}
		}

		if !contains(state.ELFailures[pair.ELClient].FailedWith, pair.CLClient) {
			state.ELFailures[pair.ELClient].FailedWith = append(
				state.ELFailures[pair.ELClient].FailedWith,
				pair.CLClient,
			)
		}

		a.log.Printf("  - %s is failing with %s", pair.CLClient, pair.ELClient)
	}
}

func (a *Analyzer) findPrimaryRootCauses(state *AnalysisState) {
	// Find CL clients failing with many EL clients.
	for client, failure := range state.CLFailures {
		if len(failure.FailedWith) >= MinFailuresForRootCause {
			state.RootCauses[client] = fmt.Sprintf(
				"CL client failing with %d EL clients: %s",
				len(failure.FailedWith),
				strings.Join(failure.FailedWith, ", "),
			)

			a.log.Printf("  - Primary root cause: %s (%s)", client, state.RootCauses[client])
		}
	}

	// Find EL clients failing with many CL clients.
	for client, failure := range state.ELFailures {
		if len(failure.FailedWith) >= MinFailuresForRootCause {
			state.RootCauses[client] = fmt.Sprintf(
				"EL client failing with %d CL clients: %s",
				len(failure.FailedWith),
				strings.Join(failure.FailedWith, ", "),
			)

			a.log.Printf("  - Primary root cause: %s (%s)", client, state.RootCauses[client])
		}
	}
}

func (a *Analyzer) findSecondaryRootCauses(state *AnalysisState) {
	// Find clients failing with multiple non-root-cause peers.
	for client, failure := range state.CLFailures {
		if _, exists := state.RootCauses[client]; exists {
			continue // Skip existing root causes.
		}

		var (
			nonRootCauseFailures = 0
			nonRootCauseList     = make([]string, 0)
		)

		for _, peer := range failure.FailedWith {
			if _, isRootCause := state.RootCauses[peer]; !isRootCause {
				nonRootCauseFailures++

				nonRootCauseList = append(nonRootCauseList, peer)
			}
		}

		if nonRootCauseFailures >= MinFailuresForRootCause {
			state.RootCauses[client] = fmt.Sprintf(
				"CL client failing with %d non-root-cause EL clients: %s",
				nonRootCauseFailures,
				strings.Join(nonRootCauseList, ", "),
			)

			a.log.Printf("  - Secondary root cause: %s (%s)", client, state.RootCauses[client])
		}
	}

	// Same for EL clients
	for client, failure := range state.ELFailures {
		if _, exists := state.RootCauses[client]; exists {
			continue
		}

		var (
			nonRootCauseFailures = 0
			nonRootCauseList     = make([]string, 0)
		)

		for _, peer := range failure.FailedWith {
			if _, isRootCause := state.RootCauses[peer]; !isRootCause {
				nonRootCauseFailures++

				nonRootCauseList = append(nonRootCauseList, peer)
			}
		}

		if nonRootCauseFailures >= MinFailuresForRootCause {
			state.RootCauses[client] = fmt.Sprintf(
				"EL client failing with %d non-root-cause CL clients: %s",
				nonRootCauseFailures,
				strings.Join(nonRootCauseList, ", "),
			)

			a.log.Printf("  - Secondary root cause: %s (%s)", client, state.RootCauses[client])
		}
	}
}

func (a *Analyzer) removeFalsePositives(state *AnalysisState) {
	toRemove := make([]string, 0)

	for client := range state.RootCauses {
		var failure *ClientFailure

		if f, exists := state.CLFailures[client]; exists {
			failure = f
		} else if f, exists := state.ELFailures[client]; exists {
			failure = f
		}

		if failure == nil {
			continue
		}

		// Keep clients failing with many peers (more than 4).
		if len(failure.FailedWith) > 4 {
			continue
		}

		// For clients with 2-4 failures, check if they're only failing with major root causes
		// or if they're not failing with enough non-major-root-cause peers.
		majorRootCauses := make(map[string]bool)

		for c, f := range state.CLFailures {
			if len(f.FailedWith) > 4 {
				majorRootCauses[c] = true
			}
		}

		for c, f := range state.ELFailures {
			if len(f.FailedWith) > 4 {
				majorRootCauses[c] = true
			}
		}

		// Count failures with non-major-root-cause peers.
		nonMajorRootCauseFailures := 0

		for _, peer := range failure.FailedWith {
			if !majorRootCauses[peer] {
				nonMajorRootCauseFailures++
			}
		}

		// Remove if:
		// 1. Only failing with major root causes, OR
		// 2. Not failing with enough non-major-root-cause peers.
		if nonMajorRootCauseFailures < MinFailuresForRootCause {
			toRemove = append(toRemove, client)

			if nonMajorRootCauseFailures == 0 {
				a.log.Printf(
					"  - Removing false positive: %s (only failing with major root causes)",
					client,
				)
			} else {
				a.log.Printf(
					"  - Removing false positive: %s (only failing with %d non-major-root-cause peers)",
					client,
					nonMajorRootCauseFailures,
				)
			}
		}
	}

	for _, client := range toRemove {
		delete(state.RootCauses, client)
	}
}

func (a *Analyzer) findUnexplainedIssues(state *AnalysisState) {
	// For each client pair in nodeStatusMap.
	for pair, statuses := range a.nodeStatusMap {
		// Skip if no failures or not related to target client.
		if !a.isTargetClientIssue(pair) {
			continue
		}

		// Find failing nodes.
		failingNodes := make([]string, 0)

		for _, s := range statuses {
			if !s.IsHealthy {
				failingNodes = append(failingNodes, s.Name)
			}
		}

		if len(failingNodes) == 0 {
			continue
		}

		// If neither client is a root cause, this is unexplained.
		if _, clIsRoot := state.RootCauses[pair.CLClient]; !clIsRoot {
			if _, elIsRoot := state.RootCauses[pair.ELClient]; !elIsRoot {
				state.UnexplainedPairs = append(state.UnexplainedPairs, ClientPairWithNodes{
					Pair:  pair,
					Nodes: failingNodes,
				})

				a.log.Printf("  - Unexplained issue: %s-%s", pair.CLClient, pair.ELClient)
			}
		}
	}
}

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

func (a *Analyzer) logAnalysisResults(result *AnalysisResult) {
	if len(result.UnexplainedIssues) == 0 && len(result.RootCause) == 0 {
		a.log.Printf("  - No issues to analyze")

		return
	}

	for _, cause := range result.RootCause {
		a.log.Printf("  - Root cause identified: %s (%s)", cause, result.RootCauseEvidence[cause])
	}

	for _, issue := range result.UnexplainedIssues {
		a.log.Printf("  - %s (unexplained issue)", issue)
	}
}

func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}

	return false
}
