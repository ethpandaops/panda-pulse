package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAnalyzer_RootCauseDetection(t *testing.T) {
	tests := []struct {
		name             string
		targetClient     string
		clientType       ClientType
		nodes            map[string]bool // map[nodeName]isHealthy
		wantRootCause    []string
		wantUnexplained  []string
		wantNotification bool
	}{
		{
			name:         "EL client failing with multiple CL clients should be classed as root cause",
			targetClient: "nimbusel",
			clientType:   ClientTypeEL,
			nodes: map[string]bool{
				"grandine-nimbusel-1":   false,
				"lighthouse-nimbusel-1": false,
				"lodestar-nimbusel-1":   false,
				"nimbus-nimbusel-1":     false,
				"prysm-nimbusel-1":      false,
				"teku-nimbusel-1":       false,
				// Add some healthy nodes with other EL clients to spice things up.
				"lighthouse-geth-1": true,
				"prysm-geth-1":      true,
				"teku-nethermind-1": true,
			},
			wantRootCause:    []string{"nimbusel"},
			wantUnexplained:  []string{}, // These should be explained by nimbusel being root cause.
			wantNotification: true,       // Should notify as the targetClient itself is the root cause.
		},
		{
			name:         "CL client with single EL issue should not be classed as root cause",
			targetClient: "grandine",
			clientType:   ClientTypeCL,
			nodes: map[string]bool{
				"grandine-erigon-1":     false,
				"grandine-geth-1":       true,
				"grandine-nethermind-1": true,
				"lighthouse-erigon-1":   true,
				"prysm-erigon-1":        true,
			},
			wantRootCause:    []string{},
			wantUnexplained:  []string{"grandine-erigon-1"},
			wantNotification: true, // Should notify as it has unexplained issue (grandine-erigon-1).
		},
		{
			name:         "Multiple CL clients failing with same EL should classify EL as root cause",
			targetClient: "lighthouse",
			clientType:   ClientTypeCL,
			nodes: map[string]bool{
				"grandine-ethereumjs-1":   false,
				"lighthouse-ethereumjs-1": false,
				"lodestar-ethereumjs-1":   false,
				"nimbus-ethereumjs-1":     false,
				"prysm-ethereumjs-1":      false,
				// Add some healthy nodes with other CL clients to ensure we filter them out nicely.
				"lighthouse-geth-1":       true,
				"lighthouse-nethermind-1": true,
			},
			wantRootCause:    []string{"ethereumjs"},
			wantUnexplained:  []string{},
			wantNotification: false, // Should not notify as issues are explained (root cause is ethereumjs).
		},
		{
			name:         "No notification needed when all nodes are healthy",
			targetClient: "lighthouse",
			clientType:   ClientTypeCL,
			nodes: map[string]bool{
				"lighthouse-geth-1":       true,
				"lighthouse-nethermind-1": true,
				"lighthouse-besu-1":       true,
			},
			wantRootCause:    []string{},
			wantUnexplained:  []string{},
			wantNotification: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAnalyzer(tt.targetClient, tt.clientType)

			// Add all node statuses.
			for nodeName, isHealthy := range tt.nodes {
				a.AddNodeStatus(nodeName, isHealthy)
			}

			// Run analysis.
			result := a.Analyze()

			// Check root causes.
			assert.ElementsMatch(t, tt.wantRootCause, result.RootCause, "root causes don't match")

			// Check unexplained issues.
			assert.ElementsMatch(t, tt.wantUnexplained, result.UnexplainedIssues, "unexplained issues don't match")

			// Check if notification would be sent.
			shouldNotify := len(result.UnexplainedIssues) > 0
			for _, rc := range result.RootCause {
				if rc == tt.targetClient {
					shouldNotify = true

					break
				}
			}
			assert.Equal(t, tt.wantNotification, shouldNotify, "notification decision incorrect")

			// If root causes were found, verify we have evidence.
			for _, rc := range result.RootCause {
				assert.NotEmpty(t, result.RootCauseEvidence[rc], "missing evidence for root cause %s", rc)
			}
		})
	}
}
