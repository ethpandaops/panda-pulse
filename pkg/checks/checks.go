package checks

import (
	"context"
	"time"
)

// Result represents the outcome of a health check.
type Result struct {
	Name        string
	Category    Category
	Status      Status
	Description string
	Timestamp   time.Time
	Details     map[string]interface{}
}

// Status represents the status of a check.
type Status string

// Define the statuses.
const (
	StatusOK   Status = "OK"
	StatusFail Status = "FAIL"
)

// Check represents a single health check.
type Check interface {
	// Name returns the name of the check.
	Name() string
	// Category returns the category of the check.
	Category() Category
	// ClientType returns the client type of the check.
	ClientType() ClientType
	// Run executes the check and returns the result.
	Run(ctx context.Context, cfg Config) (*Result, error)
}

// Config contains configuration for checks.
type Config struct {
	Network       string
	ConsensusNode string
	ExecutionNode string
	GrafanaToken  string
}

// Runner executes health checks.
type Runner interface {
	// RegisterCheck adds a check to the runner.
	RegisterCheck(check Check)
	// RunChecks executes all registered checks.
	RunChecks(ctx context.Context, cfg Config) ([]*Result, error)
}

// defaultRunner is a default implementation of the Runner interface.
type defaultRunner struct {
	checks []Check
}

// NewDefaultRunner creates a new default check runner.
func NewDefaultRunner() Runner {
	return &defaultRunner{
		checks: make([]Check, 0),
	}
}

// RegisterCheck adds a check to the runner.
func (r *defaultRunner) RegisterCheck(check Check) {
	r.checks = append(r.checks, check)
}

// RunChecks executes all registered checks.
func (r *defaultRunner) RunChecks(ctx context.Context, cfg Config) ([]*Result, error) {
	results := make([]*Result, 0)

	for _, check := range r.checks {
		if cfg.ConsensusNode != ClientTypeAll.String() && check.ClientType() != ClientTypeCL {
			continue
		}

		if cfg.ExecutionNode != ClientTypeAll.String() && check.ClientType() != ClientTypeEL {
			continue
		}

		result, err := check.Run(ctx, cfg)
		if err != nil {
			result = &Result{
				Name:        check.Name(),
				Category:    check.Category(),
				Status:      StatusFail,
				Description: err.Error(),
				Timestamp:   time.Now(),
			}
		}

		results = append(results, result)
	}

	return results, nil
}
