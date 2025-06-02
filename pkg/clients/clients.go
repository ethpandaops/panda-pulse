package clients

// ClientType represents the type of client.
type ClientType string

// Define the client types.
const (
	ClientTypeAll ClientType = ".*"
	ClientTypeCL  ClientType = "consensus"
	ClientTypeEL  ClientType = "execution"
)

// String returns the string representation of a client type.
func (c ClientType) String() string {
	switch c {
	case ClientTypeCL:
		return "Consensus"
	case ClientTypeEL:
		return "Execution"
	default:
		return ".*"
	}
}

var (
	// TeamRoles maps clients to their respective team's Discord role.
	TeamRoles = map[string]string{
		"lighthouse": "sigmaprime",
		"prysm":      "prysmatic",
		"lodestar":   "chainsafe",
		"nimbus":     "nimbus",
		"teku":       "teku",
		"grandine":   "grandine",
		"nethermind": "nethermind",
		"nimbusel":   "nimbus",
		"besu":       "besu",
		"geth":       "geth",
		"reth":       "reth",
		"erigon":     "erigon",
		"ethereumjs": "ethereumjs",
	}
	// AdminRoles maps admin roles to their respective Discord role.
	AdminRoles = map[string]string{
		"ef":    "ef",
		"admin": "admin",
		"mod":   "mod",
	}
	// Pre-production clients.
	PreProductionClients = map[string]bool{
		"ethereumjs": true,
		"nimbusel":   true,
		"erigonTwo":  true, // Not in standard client list but tracked for pre-production.
	}
)
