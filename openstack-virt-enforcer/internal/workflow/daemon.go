package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/notification"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
	"github.com/digitalocean/go-libvirt"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/qos"
)

// handleDiskEvent processes Libvirt device attachment messages.
//
// It specifically filters for Cinder volumes by looking for the "ua-" alias
// prefix. When a valid disk attachment is detected, it triggers a background goroutine
// to reconcile the I/O policy with OpenStack.
func handleDiskEvent(
	event any,
	l *libvirt.Libvirt,
	cloudName string,
	baseQOSPolicy string,
	timeout int,
	logLevel string,
	notifyProvider notification.Webhook,
	logger *slog.Logger,
) {
	if msg, ok := event.(*libvirt.DomainEventCallbackDeviceAddedMsg); ok {
		domName := msg.Dom.Name
		// Parse the disk alias to extract the Cinder UUID.
		parts := strings.Split(msg.DevAlias, "ua-")
		var diskCinderID string
		if len(parts) > 1 {
			diskCinderID = parts[1]
		} else {
			// If it's not a Cinder volume (e.g., config-drive),
			// we should skip enforcement for this specific device.
			logger.Debug("Skipping non-cinder device", "alias", msg.DevAlias)
			return
		}
		logger.Info("Event Detected", "type", "Disk Attachment", "domain", domName, "disk_uuid", diskCinderID)

		go func() {
			openstackConn, err := ConnectToOpenstack(cloudName)
			if err != nil {
				logger.Error("Failed to connect to openstack", "err", err)
				return
			}
			// Trigger Enforcement
			VMDiskIOEnforcement(
				l,
				openstackConn,
				domName,
				[]qos.QoS{},
				baseQOSPolicy,
				timeout,
				logLevel,
				notifyProvider,
			)
		}()
	}
}

// handleLifecycleEvent processes VM state changes (Start, Stop, Resume, etc.).
//
// It triggers a full I/O enforcement check when a VM starts or completes
// a migration (VM_RESUMED). It ignores "Incoming Migration" events to
// avoid race conditions during the transfer process.
func handleLifecycleEvent(
	event any,
	l *libvirt.Libvirt,
	cloudName string,
	baseQOSPolicy string,
	timeout int,
	logLevel string,
	notifyProvider notification.Webhook,
	logger *slog.Logger,
) {
	if msg, ok := event.(*libvirt.DomainEventCallbackLifecycleMsg); ok {
		domName := msg.Msg.Dom.Name
		eventName, detail := virt.ParseLifecycle(msg.Msg.Event, msg.Msg.Detail)
		logger.Info(
			"Event Detected",
			"type", fmt.Sprintf("VM Lifecycle - %s", eventName),
			"domain", domName,
			"detail", detail,
		)
		if eventName == "VM_STARTED" || (eventName == "VM_RESUMED" && strings.Contains(detail, "Migration Finished")) {
			// Ignore livemirgration starts, enforce on completion during VM_RESUMED.
			if strings.Contains(detail, "Incoming Migration") {
				return
			}

			// Trigger Enforcement
			go func() {
				openstackConn, err := ConnectToOpenstack(cloudName)
				if err != nil {
					logger.Error("Failed to connect to openstack", "err", err)
					return
				}
				VMDiskIOEnforcement(
					l,
					openstackConn,
					domName,
					[]qos.QoS{},
					baseQOSPolicy,
					timeout,
					logLevel,
					notifyProvider,
				)
			}()

		}
	}
}

// subscribeProcess manages the active event streams from Libvirt.
//
// It monitors for disk attachments and lifecycle changes while maintaining
// a 10-second heartbeat to ensure the connection to libvirtd is healthy.
// It returns true if a connection loss is detected, signaling a need for retry.
func subscribeProcess(
	ctx context.Context,
	l *libvirt.Libvirt,
	cloudName string,
	baseQOSPolicy string,
	timeout int,
	logLevel string,
	notifyProvider notification.Webhook,
	logger *slog.Logger,
) bool {

	diskEvents, err := l.SubscribeEvents(ctx, libvirt.DomainEventIDDeviceAdded, libvirt.OptDomain{})
	if err != nil {
		logger.Error("Error subscribing to disk events", "err", err)
		return true
	}

	lifecycleEvent, err := l.SubscribeEvents(ctx, libvirt.DomainEventIDLifecycle, libvirt.OptDomain{})
	if err != nil {
		logger.Error("Error subscribing to lifecycle events", "err", err)
		return true
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {

		case <-ctx.Done():
			return false

		case <-ticker.C:
			if _, err := l.ConnectGetLibVersion(); err != nil {
				logger.Error("Libvirt heartbeat failed", "err", err)
				return true
			}

		case event, ok := <-diskEvents:
			if !ok {
				logger.Error("Connection to disk event has closed", "err", err)
				return true
			}
			handleDiskEvent(event, l, cloudName, baseQOSPolicy, timeout, logLevel, notifyProvider, logger)

		case event, ok := <-lifecycleEvent:
			if !ok {
				logger.Error("Connection to lifecycle event has closed", "err", err)
				return true
			}
			handleLifecycleEvent(event, l, cloudName, baseQOSPolicy, timeout, logLevel, notifyProvider, logger)
		}
	}
}

// LibvirtListenReact runs a continuous loop that monitors Libvirt for events
// and reacts by enforcing I/O policies.
//
// Features:
//   - Initial Sync: Performs a full host reconciliation on startup.
//   - Event Driven: Subscribes to disk and lifecycle events for immediate reaction.
//   - Self-Healing: Automatically attempts to reconnect if the libvirtd socket is lost.
//   - Graceful Shutdown: Listens for SIGINT/SIGTERM to close connections cleanly.
func LibvirtListenReact(
	libvirtURL string,
	cloudName string,
	baseQOSPolicy string,
	timeout int,
	logLevel string,
	notifyProvider notification.Webhook,
) {

	// Initialize a structured logger from helper.go
	logger := SetupLogger(logLevel, cloudName)
	logger.Info(
		"Workflow Started", "workflow_name", "Openstack Virt Enforcer - Listen and React for Libvirt",
		"description", "Applies the QOS Spec from cinder volume metadata for all the available domains in the host",
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Capture system interrupts for graceful cleanup.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Info("Stopping event listener...", "reason", "Received a system interrupt call")
		cancel()
	}()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Event listener stopped successfully")
			return
		default:
			libvirtConn, err := ConnectToLibvirt(libvirtURL)
			if err != nil {
				logger.Error("Failed to connect to libvirt. Will retry in 5 seconds", "err", err)
				time.Sleep(5 * time.Second)
				continue
			}

			logger.Info("Connected to libvirt successfully")

			// Bootstrap: Enforce for all existing domains before listening for new ones.
			AllVMDiskIOEnforcement(
				libvirtConn,
				cloudName,
				baseQOSPolicy,
				timeout,
				logLevel,
				notifyProvider,
			)

			// Subscribe returns true when there is a lost connection requires retry.
			lostConnection := subscribeProcess(
				ctx,
				libvirtConn,
				cloudName,
				baseQOSPolicy,
				timeout,
				logLevel,
				notifyProvider,
				logger,
			)

			if !lostConnection {
				libvirtConn.Disconnect()
				logger.Info("Event listener shut down normally")
				return
			}

			// If we lost connection, cleanup and loop back to the top to reconnect
			libvirtConn.Disconnect()
			logger.Warn("Libvirtd connection lost. Re-establishing in 2s...")
			time.Sleep(2 * time.Second)
		}
	}
}
