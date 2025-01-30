package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
)

const queryCLHeadSlot = `
	(increase(
		beacon_head_slot{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"}[5m]
	) == 0) + 1
`

// HeadSlotCheck is a check that verifies if the CL head slot is advancing.
type HeadSlotCheck struct {
	grafanaClient *grafana.Client
}

// NewHeadSlotCheck creates a new HeadSlotCheck.
func NewHeadSlotCheck(grafanaClient *grafana.Client) *HeadSlotCheck {
	return &HeadSlotCheck{
		grafanaClient: grafanaClient,
	}
}

// Name returns the name of the check.
func (c *HeadSlotCheck) Name() string {
	return "Head slot not advancing"
}

// Category returns the category of the check.
func (c *HeadSlotCheck) Category() Category {
	return CategorySync
}

// ClientType returns the client type of the check.
func (c *HeadSlotCheck) ClientType() ClientType {
	return ClientTypeCL
}

// Run executes the check.
func (c *HeadSlotCheck) Run(ctx context.Context, cfg Config) (*Result, error) {
	query := fmt.Sprintf(queryCLHeadSlot, cfg.Network, cfg.ConsensusNode, cfg.ExecutionNode)

	response, err := c.grafanaClient.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Pull out nodes not advancing by their labels.
	var stuckNodes []string

	for _, frame := range response.Results.PandaPulse.Frames {
		for _, field := range frame.Schema.Fields {
			if labels := field.Labels; labels != nil {
				if labels["instance"] != "" {
					stuckNodes = append(stuckNodes, strings.Replace(labels["instance"], labels["network"]+"-", "", -1))
				}
			}
		}
	}

	if len(stuckNodes) == 0 {
		return &Result{
			Name:        c.Name(),
			Category:    c.Category(),
			Status:      StatusOK,
			Description: "All CL nodes are advancing properly",
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
		Description: "The following CL nodes are not advancing their head slot",
		Timestamp:   time.Now(),
		Details: map[string]interface{}{
			"query":      query,
			"stuckNodes": strings.Join(stuckNodes, "\n"),
		},
	}, nil
}
