package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
)

const queryCLFinalizedEpoch = `
	beacon_finalized_epoch{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"}
	- on (network) 
	group_right(instance, consensus_client, execution_client, ingress_user)
	max(beacon_finalized_epoch{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"}) by (network) < -4
`

// CLFinalizedEpochCheck is a check that verifies if the CL finalized epoch is advancing.
type CLFinalizedEpochCheck struct {
	grafanaClient grafana.GrafanaClient
}

// NewCLFinalizedEpochCheck creates a new CLFinalizedEpochCheck.
func NewCLFinalizedEpochCheck(grafanaClient grafana.GrafanaClient) *CLFinalizedEpochCheck {
	return &CLFinalizedEpochCheck{
		grafanaClient: grafanaClient,
	}
}

// Name returns the name of the check.
func (c *CLFinalizedEpochCheck) Name() string {
	return "Finalized epoch not advancing"
}

// Category returns the category of the check.
func (c *CLFinalizedEpochCheck) Category() Category {
	return CategorySync
}

// ClientType returns the client type of the check.
func (c *CLFinalizedEpochCheck) ClientType() ClientType {
	return ClientTypeCL
}

// Run executes the check.
func (c *CLFinalizedEpochCheck) Run(ctx context.Context, cfg Config) (*Result, error) {
	query := fmt.Sprintf(
		queryCLFinalizedEpoch,
		cfg.Network,
		cfg.ConsensusNode,
		cfg.ExecutionNode,
		cfg.Network,
		cfg.ConsensusNode,
		cfg.ExecutionNode,
	)

	response, err := c.grafanaClient.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Pull out nodes not finalising by their labels.
	var stuckNodes []string

	for _, frame := range response.Results.PandaPulse.Frames {
		for _, field := range frame.Schema.Fields {
			if labels := field.Labels; labels != nil {
				if labels["instance"] != "" {
					stuckNodes = append(stuckNodes, strings.Replace(labels["instance"], labels["ingress_user"]+"-", "", -1))
				}
			}
		}
	}

	if len(stuckNodes) == 0 {
		return &Result{
			Name:        c.Name(),
			Category:    c.Category(),
			Status:      StatusOK,
			Description: "All CL nodes are finalizing properly",
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
		Description: "The following CL nodes are not finalizing",
		Timestamp:   time.Now(),
		Details: map[string]interface{}{
			"query":      query,
			"stuckNodes": strings.Join(stuckNodes, "\n"),
		},
	}, nil
}
