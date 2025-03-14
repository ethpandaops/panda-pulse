package store

import "fmt"

// AlertAlreadyRegisteredError represents an error when trying to register an alert that already exists.
type AlertAlreadyRegisteredError struct {
	Network string
	Channel string
	Guild   string
	Client  string
}

// Error implements error.
func (e *AlertAlreadyRegisteredError) Error() string {
	return fmt.Sprintf("client %s is already registered for network %s in this channel", e.Client, e.Network)
}

// AlertNotRegisteredError represents an error when trying to deregister an alert that doesn't exist.
type AlertNotRegisteredError struct {
	Network string
	Guild   string
	Client  string
}

// Error implements error.
func (e *AlertNotRegisteredError) Error() string {
	return fmt.Sprintf("client %s is not registered for network %s", e.Client, e.Network)
}
