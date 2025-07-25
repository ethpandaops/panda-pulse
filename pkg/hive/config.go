package hive

import (
	"fmt"
)

// Config contains configuration for Hive.
type Config struct {
	BaseURL string
}

// SnapshotConfig contains configuration for taking a screenshot of the test coverage.
type SnapshotConfig struct {
	Network       string
	ConsensusNode string
	ExecutionNode string
}

// Validate validates the snapshot configuration.
func (c *SnapshotConfig) Validate() error {
	if c.Network == "" {
		return fmt.Errorf("network cannot be empty")
	}

	if c.ConsensusNode == "" && c.ExecutionNode == "" {
		return fmt.Errorf("either consensus or execution node must be specified")
	}

	return nil
}
