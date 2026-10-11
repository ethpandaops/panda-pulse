package roll

import "context"

// Actuator triggers an image roll on a single target host. Implementations
// differ only in how they reach the node's container runtime/watchtower.
type Actuator interface {
	// Name identifies the actuator (for logging).
	Name() string
	// Roll triggers a pull + recreate on the target. If image is non-empty it
	// scopes the roll to that image (matched regardless of tag when untagged);
	// empty means all of the host's watched containers.
	Roll(ctx context.Context, target Target, image string) error
}
