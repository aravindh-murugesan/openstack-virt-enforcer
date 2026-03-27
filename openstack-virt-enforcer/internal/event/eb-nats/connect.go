package ebnats

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Connect establishes the primary NATS connection. If URL is empty, it defaults to localhost.
// If ClientIdentifier is missing, it generates one based on the system hostname
// with a "virtenf-" prefix and sanitized periods.
func (n *NATSInstance) Connect() error {

	// Validate essential inputs.
	if n.URL == "" {
		n.URL = "localhost:4222"
	}

	// Durable names for consumers/publishers
	if n.ClientIdentifier == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return fmt.Errorf(
				"Client Identifier is not provided and hostname command returned an error when fall back is attempted: %w", err,
			)
		}
		// Sanitize hostname to be NATS-compatible by replacing dots with dashes
		n.ClientIdentifier = strings.ReplaceAll(fmt.Sprintf("virtenf-%s", hostname), ".", "-")
	}

	opts := []nats.Option{
		nats.Name(n.ClientIdentifier),
	}

	if n.Username != "" && n.Password != "" {
		opts = append(opts, nats.UserInfo(n.Username, n.Password))
	}

	ns, err := nats.Connect(n.URL, opts...)
	if err != nil {
		return err
	}

	n.Connection = ns
	return nil
}

// InitJetStream initializes the JetStream context using the existing NATS connection.
// It returns an error if the base NATS connection has not been established.
func (n *NATSInstance) InitJetStream() error {

	if n.Connection == nil {
		return fmt.Errorf("NATS connections is not initialized properly to start jetstream connection.")
	}

	js, err := jetstream.New(n.Connection)
	if err != nil {
		return err
	}
	n.JetStreamConnection = js
	return nil
}

// InitStreams creates or updates the core JetStream stream for cinder disk QoS
// It enforces a maximum of 5 replicas and sets up specific subjects for subscriptions and notifications.
func (n *NATSInstance) InitStreams(replicas int) error {

	if n.Connection == nil || n.JetStreamConnection == nil {
		return fmt.Errorf("Either NATS or Jetstream connections are not initialized properly to start consumers.")
	}

	if replicas > 5 {
		replicas = 5
	}

	pctx := context.Background()

	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()

	_, err := n.JetStreamConnection.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:        "cinder-disk-qos",
		Description: "",
		Retention:   jetstream.RetentionPolicy(1),
		Replicas:    replicas,
		Subjects:    []string{"cinder.disk.qos.subs", "cinder.disk.qos.enforcement.notifications.*"},
		Compression: jetstream.S2Compression,
	})
	if err != nil {
		return err
	}
	return nil
}

// InitConsumer sets up a JetStream consumer on a specified stream.
// If durable is true, the consumer will persist using the ClientIdentifier as the durable name,
// allowing it to resume after a disconnect.
func (n *NATSInstance) InitConsumer(streamName string, subjects []string, durable bool) (jetstream.Consumer, error) {

	if n.Connection == nil || n.JetStreamConnection == nil {
		return nil, fmt.Errorf("Either NATS or Jetstream connections are not initialized properly to start consumers.")
	}

	pctx := context.Background()

	ctx, cancel := context.WithTimeout(pctx, 10*time.Second)
	defer cancel()

	stream, err := n.JetStreamConnection.Stream(ctx, streamName)
	if err != nil {
		return nil, err
	}

	consumerConfig := jetstream.ConsumerConfig{
		Name:           n.ClientIdentifier,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: subjects,
	}

	if durable {
		consumerConfig.Durable = n.ClientIdentifier
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, consumerConfig)
	if err != nil {
		return nil, err
	}

	return consumer, err
}
