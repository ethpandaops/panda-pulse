package hive

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
