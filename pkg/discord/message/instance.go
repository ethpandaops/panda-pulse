package message

import "fmt"

// instance represents a node/instance of a client pair in the network.
type instance struct {
	name    string
	network string
	client  string
}

// String returns the string representation of the instance.
func (i instance) String() string {
	return i.name
}

// sshCommand returns the SSH command to connect to the instance.
func (i instance) sshCommand() string {
	return fmt.Sprintf("ssh devops@%s.%s.ethpandaops.io", i.name, i.network)
}

// newInstance creates a new instance with the given parameters.
func newInstance(name, network, client string) instance {
	return instance{
		name:    name,
		network: network,
		client:  client,
	}
}
