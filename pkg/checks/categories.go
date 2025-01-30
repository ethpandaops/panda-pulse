package checks

// Category represents the type of check.
type Category string

// Define the categories.
const (
	CategoryGeneral Category = "general"
	CategorySync    Category = "sync"
)

// String returns the string representation of a category.
func (c Category) String() string {
	switch c {
	case CategoryGeneral:
		return "General"
	case CategorySync:
		return "Sync"
	default:
		return "Unknown"
	}
}
