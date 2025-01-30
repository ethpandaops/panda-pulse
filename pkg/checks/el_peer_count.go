package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
)

const queryELPeerCount = `
	eth_exe_net_peer_count{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"} < 5
`

// ELPeerCountCheck is a check that verifies if the EL nodes have sufficient peers.
type ELPeerCountCheck struct {
	grafanaClient *grafana.Client
}

// NewELPeerCountCheck creates a new ELPeerCountCheck.
func NewELPeerCountCheck(grafanaClient *grafana.Client) *ELPeerCountCheck {
	return &ELPeerCountCheck{
		grafanaClient: grafanaClient,
	}
}

// Name returns the name of the check.
func (c *ELPeerCountCheck) Name() string {
	return "Low peer count"
}

// Category returns the category of the check.
func (c *ELPeerCountCheck) Category() Category {
	return CategorySync
}

// ClientType returns the client type of the check.
func (c *ELPeerCountCheck) ClientType() ClientType {
	return ClientTypeEL
}

// Run executes the check.
func (c *ELPeerCountCheck) Run(ctx context.Context, cfg Config) (*Result, error) {
	query := fmt.Sprintf(queryELPeerCount, cfg.Network, cfg.ConsensusNode, cfg.ExecutionNode)

	response, err := c.grafanaClient.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Pull out nodes with low peer count by their labels.
	var lowPeerNodes []string

	for _, frame := range response.Results.PandaPulse.Frames {
		for _, field := range frame.Schema.Fields {
			if labels := field.Labels; labels != nil {
				if labels["instance"] != "" {
					lowPeerNodes = append(lowPeerNodes, strings.Replace(labels["instance"], labels["network"]+"-", "", -1))
				}
			}
		}
	}

	if len(lowPeerNodes) == 0 {
		return &Result{
			Name:        c.Name(),
			Category:    c.Category(),
			Status:      StatusOK,
			Description: "All EL nodes have sufficient peers",
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
		Description: "The following EL nodes have low peer count",
		Timestamp:   time.Now(),
		Details: map[string]interface{}{
			"query":        query,
			"lowPeerNodes": strings.Join(lowPeerNodes, "\n"),
		},
	}, nil
}
