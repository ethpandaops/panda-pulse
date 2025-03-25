package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/clients"
	"github.com/ethpandaops/panda-pulse/pkg/grafana"
	"github.com/ethpandaops/panda-pulse/pkg/logger"
)

const queryELSync = `
	count by (instance, ingress_user, consensus_client, execution_client)(
		eth_exe_sync_is_syncing{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"} == 1
	)
`

// ELSyncCheck is a check that verifies if the EL nodes are syncing.
type ELSyncCheck struct {
	grafanaClient grafana.Client
}

// NewELSyncCheck creates a new ELSyncCheck.
func NewELSyncCheck(grafanaClient grafana.Client) *ELSyncCheck {
	return &ELSyncCheck{
		grafanaClient: grafanaClient,
	}
}

// Name returns the name of the check.
func (c *ELSyncCheck) Name() string {
	return "Node failing to sync"
}

// Category returns the category of the check.
func (c *ELSyncCheck) Category() Category {
	return CategorySync
}

// ClientType returns the client type of the check.
func (c *ELSyncCheck) ClientType() clients.ClientType {
	return clients.ClientTypeEL
}

// Run executes the check.
func (c *ELSyncCheck) Run(ctx context.Context, log *logger.CheckLogger, cfg Config) (*Result, error) {
	query := fmt.Sprintf(queryELSync, cfg.Network, cfg.ConsensusNode, cfg.ExecutionNode)

	log.Print("\n=== Running EL sync check")

	response, err := c.grafanaClient.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Pull out nodes not syncing by their labels.
	var notSyncedNodes []string

	for _, frame := range response.Results.PandaPulse.Frames {
		for _, field := range frame.Schema.Fields {
			if labels := field.Labels; labels != nil {
				if labels["instance"] != "" {
					nodeName := strings.ReplaceAll(labels["instance"], labels["ingress_user"]+"-", "")
					notSyncedNodes = append(notSyncedNodes, nodeName)
					log.Printf("  - Unsynced node: %s", nodeName)
				}
			}
		}
	}

	if len(notSyncedNodes) == 0 {
		log.Printf("  - All nodes are synced")

		return &Result{
			Name:        c.Name(),
			Category:    c.Category(),
			Status:      StatusOK,
			Description: "All EL nodes are synced",
			Timestamp:   time.Now(),
			Details: map[string]interface{}{
				"query": query,
			},
			AffectedNodes: []string{},
		}, nil
	}

	return &Result{
		Name:        c.Name(),
		Category:    c.Category(),
		Status:      StatusFail,
		Description: "The following EL nodes are not synced",
		Timestamp:   time.Now(),
		Details: map[string]interface{}{
			"query":          query,
			"notSyncedNodes": strings.Join(notSyncedNodes, "\n"),
		},
		AffectedNodes: notSyncedNodes,
	}, nil
}
