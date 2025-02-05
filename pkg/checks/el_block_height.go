package checks

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
)

const queryELBlockHeight = `
	eth_exe_block_most_recent_number{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"}
	- on (network) 
	group_right(instance, consensus_client, execution_client, ingress_user)
	max(eth_exe_block_most_recent_number{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"}) by (network) < -5
`

// ELBlockHeightCheck is a check that verifies if the EL nodes are advancing.
type ELBlockHeightCheck struct {
	grafanaClient grafana.GrafanaClient
}

// NewELBlockHeightCheck creates a new ELBlockHeightCheck.
func NewELBlockHeightCheck(grafanaClient grafana.GrafanaClient) *ELBlockHeightCheck {
	return &ELBlockHeightCheck{
		grafanaClient: grafanaClient,
	}
}

// Name returns the name of the check.
func (c *ELBlockHeightCheck) Name() string {
	return "Block height not advancing"
}

// Category returns the category of the check.
func (c *ELBlockHeightCheck) Category() Category {
	return CategorySync
}

// ClientType returns the client type of the check.
func (c *ELBlockHeightCheck) ClientType() ClientType {
	return ClientTypeEL
}

// Run executes the check.
func (c *ELBlockHeightCheck) Run(ctx context.Context, cfg Config) (*Result, error) {
	query := fmt.Sprintf(
		queryELBlockHeight,
		cfg.Network,
		cfg.ConsensusNode,
		cfg.ExecutionNode,
		cfg.Network,
		cfg.ConsensusNode,
		cfg.ExecutionNode,
	)

	log.Print("\n=== Running EL block height check")

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
					nodeName := strings.Replace(labels["instance"], labels["ingress_user"]+"-", "", -1)
					stuckNodes = append(stuckNodes, nodeName)
					log.Printf("  - Not advancing block height: %s", nodeName)
				}
			}
		}
	}

	if len(stuckNodes) == 0 {
		log.Printf("  - All nodes are advancing properly")

		return &Result{
			Name:        c.Name(),
			Category:    c.Category(),
			Status:      StatusOK,
			Description: "All EL nodes are advancing properly",
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
		Description: "The following EL nodes are not advancing",
		Timestamp:   time.Now(),
		Details: map[string]interface{}{
			"query":      query,
			"stuckNodes": strings.Join(stuckNodes, "\n"),
		},
		AffectedNodes: stuckNodes,
	}, nil
}
