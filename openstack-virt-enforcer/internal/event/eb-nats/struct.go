// Package ebnats provides a structured wrapper around NATS and JetStream connections
// to simplify messaging patterns and client identification.
package ebnats

import (
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSInstance holds the state for a NATS connection and its associated JetStream context.
// It serves as a centralized container for managing connectivity and metadata
// for a specific NATS client.
type NATSInstance struct {
	// Connection is the underlying primary NATS client connection.
	Connection *nats.Conn
	// JetStreamConnection provides the context for interacting with NATS JetStream capabilities.
	JetStreamConnection jetstream.JetStream
	// URL is the server address (e.g., "nats://localhost:4222") the instance is connected to.
	URL string
	// Credentials (More method will be added in the future)
	Username string
	Password string
	// ClientIdentifier is a unique string used to identify this specific client instance
	// within the NATS cluster.
	ClientIdentifier string
}
