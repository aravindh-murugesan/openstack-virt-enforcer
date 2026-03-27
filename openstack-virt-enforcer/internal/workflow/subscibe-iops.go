package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	ebnats "github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/event/eb-nats"
	"github.com/nats-io/nats.go/jetstream"
)

func SubscribeVolumeIoTune(
	conns Connections,
	logger Logger,
	volumeId string,
	totalIops int,
	writeIops int,
	readIops int,
) {

	logger.SetupLogger()
	log := logger.Instance.With(
		"global_run_id", logger.GlobalRunID,
		"run_id", logger.RunID,
	)

	log.Info("Workflow Started", "name", "Volume IOTune Subscription")

	ctx := context.Background()

	metadata := map[string]string{
		"x-virt-enforcer-io-policy-override": fmt.Sprintf("%d,%d,%d", totalIops, writeIops, readIops),
	}

	vol, reqID, err := conns.Openstack.CreateVolumeSubscription(ctx, volumeId, metadata)
	if err != nil {
		log.Error("Failed to create IOPS subscription on openstack volume", "volume_id", volumeId, "request_id", reqID)
		return
	}

	log.Info(
		"Successfully applied QOS values on the volume",
		"volume_id", volumeId,
		"volume_metadata", vol.Metadata,
		"volume_status", vol.Status,
		"server_id", vol.Attachments[0].ServerID,
	)

	if vol.Attachments[0].ServerID == "" || conns.Nats.URL == "" {
		return
	}

	msgID := fmt.Sprintf("%s-%s", volumeId, vol.Attachments[0].ServerID)

	if alive := conns.Nats.Connection.IsConnected(); !alive {
		log.Error("NATS Connection is dead. Please try again")
		time.Sleep(5 * time.Second)
		return
	}

	if err := conns.Nats.InitJetStream(); err != nil {
		log.Error("Failed to initialize Jetstream context. Please try again", "err", err)
		time.Sleep(5 * time.Second)
		return
	}

	// Initialize the Consumer
	consumer, err := conns.Nats.SubscribeDiskQOSEnforcementNotifications(msgID, false)
	if err != nil {
		log.Error("Failed to initialize NATS consumer", "err", err)
		return
	}

	var wg sync.WaitGroup
	successChan := make(chan struct{})

	handler := func(msg jetstream.Msg) {
		// We add to WaitGroup here so a shutdown waits for a message currently being processed
		wg.Add(1)
		defer wg.Done()

		var subReply ebnats.NATSDiskSubscriptionNotificationMessage
		if err := json.Unmarshal(msg.Data(), &subReply); err != nil {
			log.Error("Failed to parse NATS message", "err", err)
			msg.Term()
			return
		}

		log.Info("Message Received on NATS", "status", subReply.Status, "responder_agent", subReply.Implementor)
		if err := msg.Ack(); err != nil {
			log.Warn("Message ack sent successfully")
		}
		close(successChan)
		return
	}

	notificationConsumes, err := consumer.Consume(handler)
	if err != nil {
		log.Error("Failed to start NATS consumer consumption", "err", err)
		return
	}
	defer notificationConsumes.Stop()

	log.Info("Attempting to send subscription message", "message_id", msgID)

	pubCtx, pubCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer pubCancel()
	subMessage := ebnats.NATSDiskSubscriptionMessage{
		ID:        msgID,
		ServerID:  vol.Attachments[0].ServerID,
		DiskID:    vol.ID,
		EventType: ebnats.EventUpdateQos,
	}
	pub, err := conns.Nats.PublishDiskQOSUpdate(
		pubCtx,
		subMessage,
	)
	if err != nil {
		log.Error("Request to QOS Subscription channel has failed ", "err", err)
	} else {
		log.Info("Request to QOS Subscription channel has been sent successfully", "stream", pub.Stream)
	}

	sigQuit := make(chan os.Signal, 1)
	signal.Notify(sigQuit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-successChan:
		return
	case <-sigQuit:
		log.Warn("Program exited due to termination signal received from OS")
		return
	case <-ctx.Done():
		log.Info("Stopping NATS consumer (Graceful Shutdown)...")
		notificationConsumes.Stop()

	}

}
