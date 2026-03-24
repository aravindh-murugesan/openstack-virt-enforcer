package ebnats

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

// PublishDiskQOSUpdate serializes a NATSDiskSubscriptionMessage to JSON and publishes it
// to the "cinder.disk.qos.subs" subject. This is typically used to broadcast a
// request for a QoS change.
func (n *NATSInstance) PublishDiskQOSUpdate(ctx context.Context, message NATSDiskSubscriptionMessage) (*jetstream.PubAck, error) {

	// Parse the message to a JSON
	payload, err := json.Marshal(&message)
	if err != nil {
		return nil, err
	}
	pub, err := n.JetStreamConnection.Publish(ctx, "cinder.disk.qos.subs", payload)
	if err != nil {
		return nil, err
	}

	return pub, nil
}

// PublishDiskQOSEnforcementNotification publishes the result of a QoS enforcement action.
// The subject is dynamically constructed as "cinder.disk.qos.enforcement.notifications.<subjectSuffix>".
func (n *NATSInstance) PublishDiskQOSEnforcementNotification(ctx context.Context, subjectSuffix string, message NATSDiskSubscriptionNotificationMessage) (*jetstream.PubAck, error) {

	// Parse the message to a JSON
	payload, err := json.Marshal(&message)
	if err != nil {
		return nil, err
	}
	pub, err := n.JetStreamConnection.Publish(ctx, fmt.Sprintf("cinder.disk.qos.enforcement.notifications.%s", subjectSuffix), payload)
	if err != nil {
		return nil, err
	}

	return pub, nil
}
