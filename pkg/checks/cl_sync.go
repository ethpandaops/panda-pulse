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

const queryCLSync = `
	count by (instance, ingress_user, consensus_client, execution_client)(
		eth_con_sync_is_syncing{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"} == 1
	)
`

// CLSyncCheck is a check that verifies if the CL nodes are syncing.
type CLSyncCheck struct {
	grafanaClient grafana.Client
}

// NewCLSyncCheck creates a new CLSyncCheck.
func NewCLSyncCheck(grafanaClient grafana.Client) *CLSyncCheck {
	return &CLSyncCheck{
		grafanaClient: grafanaClient,
	}
}

// Name returns the name of the check.
func (c *CLSyncCheck) Name() string {
	return "Node failing to sync"
}

// Category returns the category of the check.
func (c *CLSyncCheck) Category() Category {
	return CategorySync
}

// ClientType returns the client type of the check.
func (c *CLSyncCheck) ClientType() clients.ClientType {
	return clients.ClientTypeCL
}

// Run executes the check.
func (c *CLSyncCheck) Run(ctx context.Context, log *logger.CheckLogger, cfg Config) (*Result, error) {
	query := fmt.Sprintf(queryCLSync, cfg.Network, cfg.ConsensusNode, cfg.ExecutionNode)

	log.Print("\n=== Running CL sync check")

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
			Description: "All CL nodes are synced",
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
		Description: "The following CL nodes are not synced",
		Timestamp:   time.Now(),
		Details: map[string]interface{}{
			"query":          query,
			"notSyncedNodes": strings.Join(notSyncedNodes, "\n"),
		},
		AffectedNodes: notSyncedNodes,
	}, nil
}
