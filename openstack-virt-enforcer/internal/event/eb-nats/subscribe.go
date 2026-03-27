package ebnats

import (
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

func (n *NATSInstance) SubscribeDiskQOSUpdate(durable bool) (jetstream.Consumer, error) {

	diskQoSConsumer, err := n.InitConsumer("cinder-disk-qos", []string{"cinder.disk.qos.subs"}, durable)
	if err != nil {
		return nil, err
	}
	return diskQoSConsumer, nil
}

func (n *NATSInstance) SubscribeDiskQOSEnforcementNotifications(subjectSuffix string, durable bool) (jetstream.Consumer, error) {

	if subjectSuffix == "" {
		subjectSuffix = "*"
	}
	diskQoSConsumer, err := n.InitConsumer(
		"cinder-disk-qos",
		[]string{
			fmt.Sprintf("cinder.disk.qos.enforcement.notifications.%s", subjectSuffix),
		},
		durable,
	)
	if err != nil {
		return nil, err
	}
	return diskQoSConsumer, nil
}
