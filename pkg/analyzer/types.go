package analyzer

import (
	"fmt"
	"strings"
)

// ClientType represents the type of client.
type ClientType string

const (
	ClientTypeEL ClientType = "EL"
	ClientTypeCL ClientType = "CL"
)

// NodeStatus represents the status of a node.
type NodeStatus struct {
	Name      string
	IsHealthy bool
}

// AnalysisResult is the result of the analysis.
type AnalysisResult struct {
	RootCause         []string            // List of clients determined to be root cause.
	UnexplainedIssues []string            // List of issues that can't be explained by root cause.
	AffectedNodes     map[string][]string // Map of issue type to affected nodes.
	RootCauseEvidence map[string]string   // Evidence for why each root cause was determined.
}

// ClientPair represents a CL-EL client combination.
type ClientPair struct {
	CLClient string
	ELClient string
}

// String returns the string representation of a ClientPair.
func (cp ClientPair) String() string {
	return fmt.Sprintf("%s-%s", cp.CLClient, cp.ELClient)
}

// ParseClientPair parses a node name into CL and EL clients.
func ParseClientPair(nodeName string) ClientPair {
	// Remove any network prefix if it exists
	parts := strings.Split(nodeName, "-")
	if len(parts) < 2 {
		return ClientPair{}
	}

	// Find the CL and EL parts
	// Format is typically: [network]-[cl_client]-[el_client]-[number]
	// or: [cl_client]-[el_client]-[number]
	var clClient, elClient string

	if len(parts) >= 4 && strings.HasPrefix(nodeName, "pectra-devnet-6-") {
		// Format: pectra-devnet-6-cl-el-number.
		clClient = parts[len(parts)-3]
		elClient = parts[len(parts)-2]
	} else if len(parts) >= 3 {
		// Format: cl-el-number.
		clClient = parts[0]
		elClient = parts[1]
	}

	return ClientPair{
		CLClient: clClient,
		ELClient: elClient,
	}
}

// NodeStatusMap tracks the status of nodes by client pair.
type NodeStatusMap map[ClientPair][]NodeStatus
