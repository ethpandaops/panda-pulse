package checks

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

// Define a list of known clients.
const (
	CLLighthouse = "lighthouse"
	CLPrysm      = "prysm"
	CLLodestar   = "lodestar"
	CLNimbus     = "nimbus"
	CLTeku       = "teku"
	CLGrandine   = "grandine"
	ELNethermind = "nethermind"
	ELBesu       = "besu"
	ELGeth       = "geth"
	ELReth       = "reth"
	ELErigon     = "erigon"
	ELEthereumJS = "ethereumjs"
)

// Buckets of known clients.
var (
	CLClients = []string{CLLighthouse, CLPrysm, CLLodestar, CLNimbus, CLTeku, CLGrandine}
	ELClients = []string{ELNethermind, ELBesu, ELGeth, ELReth, ELErigon, ELEthereumJS}
)

// IsCLClient returns true if the client is a consensus client.
func IsCLClient(client string) bool {
	for _, c := range CLClients {
		if c == client {
			return true
		}
	}

	return false
}

// IsELClient returns true if the client is an execution client.
func IsELClient(client string) bool {
	for _, c := range ELClients {
		if c == client {
			return true
		}
	}

	return false
}
