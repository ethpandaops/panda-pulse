package analyzer

import (
	"context"
	"testing"

	"github.com/ethpandaops/panda-pulse/pkg/cartographoor"
	"github.com/ethpandaops/panda-pulse/pkg/logger"
	"github.com/stretchr/testify/assert"
)

func TestAnalyzer_RootCauseDetection(t *testing.T) {
	cs, _ := cartographoor.NewService(context.Background(), cartographoor.ServiceConfig{})

	tests := []struct {
		name            string
		targetClient    string
		clientType      ClientType
		cartographoor   *cartographoor.Service
		nodes           map[string]bool // map[nodeName]isHealthy
		wantRootCause   []string
		wantUnexplained []string
	}{
		{
			name:          "all healthy nodes",
			targetClient:  "lighthouse",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				"lighthouse-geth-1":       true,
				"lighthouse-besu-1":       true,
				"lighthouse-nethermind-1": true,
			},
			wantRootCause:   []string{},
			wantUnexplained: []string{},
		},
		{
			name:          "unexplained issue - single failure pair",
			targetClient:  "lighthouse",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				"lighthouse-erigon-1": false, // Only this lighthouse+erigon pair is failing.
				"lighthouse-geth-1":   true,  // Other lighthouse pairs are healthy.
				"lighthouse-besu-1":   true,
				"prysm-erigon-1":      true, // Other clients with erigon are healthy.
				"teku-erigon-1":       true,
			},
			wantRootCause:   []string{},
			wantUnexplained: []string{"lighthouse-erigon-1"},
		},
		{
			name:          "clear root cause - EL client failing with many CL clients",
			targetClient:  "ethereumjs",
			clientType:    ClientTypeEL,
			cartographoor: cs,
			nodes: map[string]bool{
				"lighthouse-ethereumjs-1": false,
				"teku-ethereumjs-1":       false,
				"lodestar-ethereumjs-1":   false,
				"grandine-ethereumjs-1":   false,
				"nimbus-ethereumjs-1":     false,
				// Some healthy nodes
				"lighthouse-geth-1": true,
				"prysm-geth-1":      true,
			},
			wantRootCause:   []string{"ethereumjs"},
			wantUnexplained: []string{},
		},
		// This tests when we have multiple instances of the same client pair (prysm-geth-N).
		// Some failing, some healthy, each failing instance should be listed as unexplained.
		{
			name:          "multiple node instances - same client pair",
			targetClient:  "prysm",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				"prysm-geth-1": false,
				"prysm-geth-2": false,
				"prysm-geth-3": true,
				"prysm-geth-4": false,
				"prysm-geth-5": true,
				"prysm-geth-6": false,
			},
			wantRootCause: []string{},
			wantUnexplained: []string{
				"prysm-geth-1",
				"prysm-geth-2",
				"prysm-geth-4",
				"prysm-geth-6",
			},
		},
		{
			name:          "clear root cause - CL client failing with many EL clients",
			targetClient:  "prysm",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				"prysm-erigon-1":     false,
				"prysm-geth-1":       false,
				"prysm-ethereumjs-1": false,
				"prysm-reth-1":       false,
				"prysm-besu-1":       false,
				// Some healthy nodes
				"lighthouse-geth-1": true,
				"teku-geth-1":       true,
			},
			wantRootCause:   []string{"prysm"},
			wantUnexplained: []string{},
		},
		{
			name:          "false positive - client only failing with known root causes",
			targetClient:  "lighthouse",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				// ethereumjs + nethermind are root causes (failing with many CL clients).
				"lighthouse-ethereumjs-1": false,
				"teku-ethereumjs-1":       false,
				"lodestar-ethereumjs-1":   false,
				"grandine-ethereumjs-1":   false,
				"nimbus-ethereumjs-1":     false,
				"lighthouse-nethermind-1": false,
				"teku-nethermind-1":       false,
				"grandine-nethermind-1":   false,
				// lighthouse's other pairs are healthy
				"lighthouse-geth-1": true,
				"lighthouse-besu-1": true,
			},
			wantRootCause:   []string{"ethereumjs", "nethermind"},
			wantUnexplained: []string{},
		},
		{
			name:          "mixed health status - some nodes healthy, some failing",
			targetClient:  "ethereumjs",
			clientType:    ClientTypeEL,
			cartographoor: cs,
			nodes: map[string]bool{
				"lighthouse-ethereumjs-1": false,
				"teku-ethereumjs-1":       false,
				"lodestar-ethereumjs-1":   false,
				"grandine-ethereumjs-1":   false,
				"nimbus-ethereumjs-1":     false,
				// Some healthy nodes with same client
				"prysm-ethereumjs-1":      true,
				"lighthouse-ethereumjs-2": true,
			},
			wantRootCause:   []string{"ethereumjs"},
			wantUnexplained: []string{},
		},
		{
			name:          "borderline case - client failing with exactly MinFailuresForRootCause peers",
			targetClient:  "reth",
			clientType:    ClientTypeEL,
			cartographoor: cs,
			nodes: map[string]bool{
				// Exactly MinFailuresForRootCause (2) failures.
				"lighthouse-reth-1": false,
				"teku-reth-1":       false,
				// Some healthy nodes
				"prysm-reth-1":  true,
				"nimbus-reth-1": true,
			},
			wantRootCause:   []string{"reth"},
			wantUnexplained: []string{},
		},
		{
			name:          "below threshold - client failing with less than MinFailuresForRootCause peers",
			targetClient:  "reth",
			clientType:    ClientTypeEL,
			cartographoor: cs,
			nodes: map[string]bool{
				// Only one failure, below MinFailuresForRootCause (2).
				"lighthouse-reth-1": false,
				// Some healthy nodes
				"prysm-reth-1":  true,
				"teku-reth-1":   true,
				"nimbus-reth-1": true,
			},
			wantRootCause:   []string{},
			wantUnexplained: []string{"lighthouse-reth-1"},
		},
		{
			name:          "secondary root cause - client failing with non-root-cause peers",
			targetClient:  "lighthouse",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				// lighthouse failing with multiple non-root-cause EL clients.
				"lighthouse-geth-1":       false,
				"lighthouse-besu-1":       false,
				"lighthouse-nethermind-1": false,
				// Other CL clients healthy with these EL clients.
				"prysm-geth-1":        true,
				"teku-besu-1":         true,
				"nimbus-nethermind-1": true,
				// Some other failures that aren't root causes.
				"lighthouse-erigon-1":     false,
				"lighthouse-ethereumjs-1": false,
			},
			wantRootCause:   []string{"lighthouse"},
			wantUnexplained: []string{},
		},
		{
			name:          "major root cause overrides - client failing with many peers including root causes",
			targetClient:  "lighthouse",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				// lighthouse failing with many peers (>4).
				"lighthouse-geth-1":       false,
				"lighthouse-besu-1":       false,
				"lighthouse-nethermind-1": false,
				"lighthouse-erigon-1":     false,
				"lighthouse-ethereumjs-1": false,
				// Some of these peers are root causes themselves.
				"teku-ethereumjs-1":   false,
				"prysm-ethereumjs-1":  false,
				"nimbus-ethereumjs-1": false,
				// But lighthouse should still be a root cause due to failing with >4 peers.
			},
			wantRootCause:   []string{"lighthouse", "ethereumjs"},
			wantUnexplained: []string{},
		},
		{
			name:          "secondary root cause - EL client failing with non-root-cause peers",
			targetClient:  "besu",
			clientType:    ClientTypeEL,
			cartographoor: cs,
			nodes: map[string]bool{
				// besu failing with multiple non-root-cause CL clients
				"lighthouse-besu-1": false,
				"teku-besu-1":       false,
				"prysm-besu-1":      false,
				// Other EL clients healthy with these CL clients
				"lighthouse-geth-1": true,
				"teku-nethermind-1": true,
				"prysm-erigon-1":    true,
				// Single failures that should be unexplained when viewed from CL perspective.
				"grandine-besu-1": false,
				"lodestar-besu-1": false,
			},
			wantRootCause:   []string{"besu"},
			wantUnexplained: []string{}, // These won't show up as unexplained from besu's perspective.
		},
		{
			name:          "secondary root cause - CL client failing with non-root-cause peers",
			targetClient:  "teku",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				// teku failing with multiple non-root-cause EL clients.
				"teku-geth-1":       false,
				"teku-besu-1":       false,
				"teku-nethermind-1": false,
				// Other CL clients healthy with these EL clients.
				"lighthouse-geth-1":   true,
				"prysm-besu-1":        true,
				"nimbus-nethermind-1": true,
				// Additional failures that don't affect root cause status.
				"teku-ethereumjs-1": false,
				"teku-reth-1":       false,
			},
			wantRootCause:   []string{"teku"}, // Only teku is a root cause.
			wantUnexplained: []string{},
		},
		{
			name:          "unexplained issues - CL clients with single failures",
			targetClient:  "grandine",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				// Single failure with besu
				"grandine-besu-1": false,
				// Other pairs healthy
				"grandine-geth-1": true,
				"grandine-reth-1": true,
			},
			wantRootCause:   []string{},
			wantUnexplained: []string{"grandine-besu-1"},
		},
		{
			name:          "pre-production client exception - not removed as false positive",
			targetClient:  "lighthouse",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				// ethereumjs is already in PreProductionClients map
				// Failing with exactly MinFailuresForRootCause peers (which happen to be major root causes)
				"lighthouse-ethereumjs-1": false,
				"prysm-ethereumjs-1":      false,
				// These are major root causes (failing with many peers)
				"lighthouse-geth-1":       false,
				"lighthouse-besu-1":       false,
				"lighthouse-nethermind-1": false,
				"lighthouse-erigon-1":     false,
				"prysm-geth-1":            false,
				"prysm-besu-1":            false,
				"prysm-nethermind-1":      false,
				"prysm-erigon-1":          false,
				// Some healthy pairs
				"teku-geth-1":   true,
				"nimbus-besu-1": true,
			},
			// ethereumjs would normally be removed as a false positive (only failing with major root causes),
			// but it should be kept due to being a pre-production client with ≥ MinFailuresForRootCause failures
			wantRootCause:   []string{"ethereumjs", "lighthouse", "prysm"},
			wantUnexplained: []string{},
		},
		{
			name:          "pre-production client skipped in unexplained issues",
			targetClient:  "lighthouse",
			clientType:    ClientTypeCL,
			cartographoor: cs,
			nodes: map[string]bool{
				// A failing node with pre-production client (ethereumjs)
				"lighthouse-ethereumjs-1": false,
				// Some other healthy nodes
				"lighthouse-geth-1":  true,
				"lighthouse-besu-1":  true,
				"prysm-ethereumjs-1": true,
			},
			// Even though lighthouse-ethereumjs is failing, it shouldn't be included in unexplained issues
			// because it's a pre-production client pair
			wantRootCause:   []string{},
			wantUnexplained: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := logger.NewCheckLogger("id")
			a := NewAnalyzer(log, tt.targetClient, tt.clientType, tt.cartographoor)

			for nodeName, isHealthy := range tt.nodes {
				a.AddNodeStatus(nodeName, isHealthy)
			}

			result := a.Analyze()

			assert.ElementsMatch(t, tt.wantRootCause, result.RootCause, "root causes don't match")
			assert.ElementsMatch(t, tt.wantUnexplained, result.UnexplainedIssues, "unexplained issues don't match")
		})
	}
}
