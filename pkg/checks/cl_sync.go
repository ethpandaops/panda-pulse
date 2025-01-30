package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
)

const queryCLSync = `
	count by (instance, ingress_user, consensus_client, execution_client)(
		eth_con_sync_is_syncing{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"} == 1
	)
`

// CLSyncCheck is a check that verifies if the CL nodes are syncing.
type CLSyncCheck struct {
	grafanaClient grafana.GrafanaClient
}

// NewCLSyncCheck creates a new CLSyncCheck.
func NewCLSyncCheck(grafanaClient grafana.GrafanaClient) *CLSyncCheck {
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
func (c *CLSyncCheck) ClientType() ClientType {
	return ClientTypeCL
}

// Run executes the check.
func (c *CLSyncCheck) Run(ctx context.Context, cfg Config) (*Result, error) {
	query := fmt.Sprintf(queryCLSync, cfg.Network, cfg.ConsensusNode, cfg.ExecutionNode)

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
					notSyncedNodes = append(notSyncedNodes, strings.Replace(labels["instance"], labels["ingress_user"]+"-", "", -1))
				}
			}
		}
	}

	if len(notSyncedNodes) == 0 {
		return &Result{
			Name:        c.Name(),
			Category:    c.Category(),
			Status:      StatusOK,
			Description: "All CL nodes are synced",
			Timestamp:   time.Now(),
			Details: map[string]interface{}{
				"query": query,
			},
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
	}, nil
}
