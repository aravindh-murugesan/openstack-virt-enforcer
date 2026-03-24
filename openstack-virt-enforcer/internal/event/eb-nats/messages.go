package ebnats

import "time"

// EventType defines the category of action being performed or requested.
type EventType string

// EventStatus defines the current state or result of a processed event.
type EventStatus string

const (
	EventUpdateQos EventType = "UPDATE_QOS"
)

const (
	QoSEnforcementSuccess EventStatus = "QOS_ENFORCEMENT_SUCCESS"
	QoSEnforcementFailure EventStatus = "QOS_ENFORCEMENT_FAILURE"
	QoSEnforcementInPlace EventStatus = "QOS_ENFORCEMENT_IN_PLACE"
)

// NATSDiskSubscriptionMessage represents the core payload for disk-related events
// received via a NATS subscription. It maps directly to OpenStack-style
// identifiers (Nova/Cinder) for cross-system compatibility.
type NATSDiskSubscriptionMessage struct {
	ID        string    `json:"message_id"`
	ServerID  string    `json:"nova_server_uuid"`
	DiskID    string    `json:"cinder_disk_uuid"`
	EventType EventType `json:"event_type"`
	TimeStamp time.Time `json:"timestamp"`
}

// NATSDiskSubscriptionNotificationMessage extends the base subscription message
// to include execution results and metadata about the component that handled the event.
type NATSDiskSubscriptionNotificationMessage struct {
	NATSDiskSubscriptionMessage
	Status      EventStatus `json:"status"`
	Implementor string      `json:"enforced_by"`
}
