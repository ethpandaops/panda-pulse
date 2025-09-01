package checks

import (
	"github.com/ethpandaops/panda-pulse/pkg/checks"
)

// categoryResults is a struct that holds the results of a category.
type categoryResults struct {
	failedChecks []*checks.Result
	hasFailed    bool
}

// Order categories as we want them to be displayed.
var orderedCategories = []checks.Category{
	checks.CategoryGeneral,
	checks.CategorySync,
}

// Helper to create string pointer.
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
