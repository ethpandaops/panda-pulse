package checks

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ethpandaops/panda-pulse/pkg/grafana"
)

const queryCLPeerCount = `
	sum by (instance, ingress_user)(libp2p_peers{network=~"%s", consensus_client=~"%s", execution_client=~"%s", ingress_user!~"synctest.*"} ) < 5
`

// CLPeerCountCheck is a check that verifies if the CL peer count is sufficient.
type CLPeerCountCheck struct {
	grafanaClient grafana.GrafanaClient
}

// NewCLPeerCountCheck creates a new CLPeerCountCheck.
func NewCLPeerCountCheck(grafanaClient grafana.GrafanaClient) *CLPeerCountCheck {
	return &CLPeerCountCheck{
		grafanaClient: grafanaClient,
	}
}

// Name returns the name of the check.
func (c *CLPeerCountCheck) Name() string {
	return "Low peer count"
}

// Category returns the category of the check.
func (c *CLPeerCountCheck) Category() Category {
	return CategorySync
}

// ClientType returns the client type of the check.
func (c *CLPeerCountCheck) ClientType() ClientType {
	return ClientTypeCL
}

// Run executes the check.
func (c *CLPeerCountCheck) Run(ctx context.Context, cfg Config) (*Result, error) {
	query := fmt.Sprintf(queryCLPeerCount, cfg.Network, cfg.ConsensusNode, cfg.ExecutionNode)

	log.Print("\n=== Running CL peer count check")

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
					nodeName := strings.Replace(labels["instance"], labels["ingress_user"]+"-", "", -1)
					lowPeerNodes = append(lowPeerNodes, nodeName)
					log.Printf("  - Low peer count: %s", nodeName)
				}
			}
		}
	}

	if len(lowPeerNodes) == 0 {
		log.Printf("  - All nodes have sufficient peers")

		return &Result{
			Name:        c.Name(),
			Category:    c.Category(),
			Status:      StatusOK,
			Description: "All CL nodes have sufficient peers",
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
		Description: "The following CL nodes have low peer count",
		Timestamp:   time.Now(),
		Details: map[string]interface{}{
			"query":        query,
			"lowPeerNodes": strings.Join(lowPeerNodes, "\n"),
		},
		AffectedNodes: lowPeerNodes,
	}, nil
}
