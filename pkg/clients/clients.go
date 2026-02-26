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
	// TeamRoles maps clients to their respective team's Discord roles.
	// Multiple role names are supported to allow the bot to operate across different servers.
	TeamRoles = map[string][]string{
		"lighthouse": {"sigmaprime", "lighthouse"},
		"prysm":      {"prysmatic", "prysm"},
		"lodestar":   {"chainsafe", "lodestar"},
		"nimbus":     {"nimbus"},
		"teku":       {"teku"},
		"grandine":   {"grandine"},
		"nethermind": {"nethermind"},
		"nimbusel":   {"nimbus", "nimbusel"},
		"besu":       {"besu"},
		"geth":       {"geth"},
		"reth":       {"reth"},
		"erigon":     {"erigon"},
		"ethereumjs": {"ethereumjs"},
		"ethrex":     {"ethrex"},
	}
	// AdminRoles maps admin roles to their respective Discord roles.
	// Multiple role names are supported to allow the bot to operate across different servers.
	AdminRoles = map[string][]string{
		"ef":    {"ef", "eels", "steel", "pandas"},
		"admin": {"admin"},
		"mod":   {"mod"},
		"epf":   {"epf"},
	}
	// Pre-production clients.
	PreProductionClients = map[string]bool{
		"ethereumjs": true,
		"nimbusel":   true,
		"erigonTwo":  true, // Not in standard client list but tracked for pre-production.
	}
)
