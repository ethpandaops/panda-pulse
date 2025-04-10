package clients

import "strings"

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
	ELNimbusel   = "nimbusel"
	ELBesu       = "besu"
	ELGeth       = "geth"
	ELReth       = "reth"
	ELErigon     = "erigon"
	ELEthereumJS = "ethereumjs"
)

var (
	// Buckets of known clients.
	CLClients = []string{CLLighthouse, CLPrysm, CLLodestar, CLNimbus, CLTeku, CLGrandine}
	ELClients = []string{ELNethermind, ELNimbusel, ELBesu, ELGeth, ELReth, ELErigon, ELEthereumJS}
	// TeamRoles maps clients to their respective team's Discord role.
	TeamRoles = map[string]string{
		CLLighthouse: "sigmaprime",
		CLPrysm:      "prysmatic",
		CLLodestar:   "chainsafe",
		CLNimbus:     "nethermind",
		CLTeku:       "teku",
		CLGrandine:   "grandine",
		ELNethermind: "nethermind",
		ELNimbusel:   "nethermind",
		ELBesu:       "besu",
		ELGeth:       "geth",
		ELReth:       "reth",
		ELErigon:     "erigon",
		ELEthereumJS: "ethereumjs",
	}
	// AdminRoles maps admin roles to their respective Discord role.
	AdminRoles = map[string]string{
		"ef":    "ef",
		"admin": "admin",
		"mod":   "mod",
	}
	// Pre-production clients.
	PreProductionClients = map[string]bool{
		ELEthereumJS: true,
		ELNimbusel:   true,
		"erigonTwo":  true, // Not in standard client list but tracked for pre-production.
	}
	// DefaultRepositories maps clients to their default source repositories.
	DefaultRepositories = map[string]string{
		CLLighthouse: "sigp/lighthouse",
		CLPrysm:      "OffchainLabs/prysm",
		CLLodestar:   "chainsafe/lodestar",
		CLNimbus:     "status-im/nimbus-eth1",
		CLTeku:       "ConsenSys/teku",
		CLGrandine:   "grandinetech/grandine",
		ELNethermind: "NethermindEth/nethermind",
		ELNimbusel:   "status-im/nimbus-eth2",
		ELBesu:       "hyperledger/besu",
		ELGeth:       "ethereum/go-ethereum",
		ELReth:       "paradigmxyz/reth",
		ELErigon:     "erigontech/erigon",
		ELEthereumJS: "ethereumjs/ethereumjs-monorepo",
	}
	// DefaultBranches maps clients to their default branches.
	DefaultBranches = map[string]string{
		CLLighthouse: "stable",
		CLPrysm:      "develop",
		CLLodestar:   "unstable",
		CLNimbus:     "master",
		CLTeku:       "master",
		CLGrandine:   "develop",
		ELNethermind: "master",
		ELNimbusel:   "stable",
		ELBesu:       "main",
		ELGeth:       "master",
		ELReth:       "main",
		ELErigon:     "main",
		ELEthereumJS: "master",
	}
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

// GetClientLogo returns the Twitter profile image URL for a given client.
func GetClientLogo(client string) string {
	switch strings.ToLower(client) {
	// Consensus Layer Clients
	case CLLighthouse:
		return "https://pbs.twimg.com/profile_images/1106229297151303681/EuqfU4v4_400x400.png"
	case CLPrysm:
		return "https://pbs.twimg.com/profile_images/1805984255861755905/PaPSwIzL_400x400.jpg"
	case CLLodestar:
		return "https://pbs.twimg.com/profile_images/1533709115712753665/O5PDykiV_400x400.jpg"
	case CLNimbus:
		return "https://pbs.twimg.com/profile_images/1721659785739960320/3XPpm8Or_400x400.jpg"
	case CLTeku:
		return "https://pbs.twimg.com/profile_images/1673661934036500480/Ee6NYB_K_400x400.jpg"
	case CLGrandine:
		return "https://pbs.twimg.com/profile_images/1409832041739345923/-ldZic7y_400x400.jpg"
	// Execution Layer Clients
	case ELNethermind:
		return "https://pbs.twimg.com/profile_images/1806762018747072512/8XWySkUI_400x400.png"
	case ELNimbusel:
		return "https://pbs.twimg.com/profile_images/1721659785739960320/3XPpm8Or_400x400.jpg"
	case ELBesu:
		return "https://pbs.twimg.com/profile_images/1418537229123735552/XoFWio0T_400x400.jpg"
	case ELGeth:
		return "https://pbs.twimg.com/profile_images/1605238709451898881/Kl-bliNn_400x400.jpg"
	case ELReth:
		return "https://raw.githubusercontent.com/paradigmxyz/reth/refs/heads/main/assets/reth-docs.png"
	case ELErigon:
		return "https://pbs.twimg.com/profile_images/1420080204148576274/-4OFIs2x_400x400.jpg"
	case ELEthereumJS:
		return "https://pbs.twimg.com/profile_images/1330796558330322946/Y2YReI9m_400x400.png"
	default:
		return ""
	}
}

// IsPreProductionClient returns true if the client is considered pre-production.
func IsPreProductionClient(client string) bool {
	return PreProductionClients[client]
}
