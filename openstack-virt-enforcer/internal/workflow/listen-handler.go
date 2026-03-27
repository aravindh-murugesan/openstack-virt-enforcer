package workflow

import (
	"fmt"
	"strings"
	"sync"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
	"github.com/digitalocean/go-libvirt"
)

func handlerLibvirtLifecycleEvents(
	event any,
	wg *sync.WaitGroup,
	conns Connections,
	opts EnforceIoTuneOpts,
	logger Logger,
) {
	// Satisfy the wg.Add(1) from ProcessEventsLibvirt
	defer wg.Done()

	logger.SetupLogger()
	log := logger.Instance.With(
		"global_run_id", logger.GlobalRunID,
		"run_id", logger.RunID,
		"reactor", "libvirt-event-reactor",
		"processor", "libvirt_events_lifecycle_handler",
	)

	if msg, ok := event.(*libvirt.DomainEventCallbackLifecycleMsg); ok {
		domName := msg.Msg.Dom.Name
		eventName, detail := virt.ParseLifecycle(msg.Msg.Event, msg.Msg.Detail)

		log.Info(
			"Event Detected",
			"type", fmt.Sprintf("VM Lifecycle - %s", eventName),
			"domain", domName,
			"detail", detail,
		)

		if eventName == "VM_STARTED" || (eventName == "VM_RESUMED" && strings.Contains(detail, "Migration Finished")) {
			if strings.Contains(detail, "Incoming Migration") {
				return
			}

			// Safe to modify `opts` here because it's passed by value and isolated to this goroutine
			opts.DomainID = domName
			opts.Enforce = true

			// Run synchronously because the handler itself is already running in a background goroutine
			EnforceIoTuneForDomain(conns, opts, logger)
		}
	}
}

func handlerLibvirtDiskEvents(
	event any,
	wg *sync.WaitGroup,
	conns Connections,
	opts EnforceIoTuneOpts,
	logger Logger,
) {
	// Satisfy the wg.Add(1) from ProcessEventsLibvirt
	defer wg.Done()

	logger.SetupLogger()
	log := logger.Instance.With(
		"global_run_id", logger.GlobalRunID,
		"run_id", logger.RunID,
		"reactor", "libvirt-event-reactor",
		"processor", "libvirt_events_disk_handler",
	)

	if msg, ok := event.(*libvirt.DomainEventCallbackDeviceAddedMsg); ok {
		domName := msg.Dom.Name
		parts := strings.Split(msg.DevAlias, "ua-")
		var diskCinderID string

		if len(parts) > 1 {
			diskCinderID = parts[1]
		} else {
			log.Debug("Skipping non-cinder device", "alias", msg.DevAlias)
			return
		}

		log.Info("Event Detected", "type", "Disk Attachment", "domain", domName, "disk_uuid", diskCinderID)

		// Safe to modify `opts` here because it's passed by value and isolated to this goroutine
		opts.DiskID = diskCinderID
		opts.DomainID = domName

		// Run synchronously because the handler itself is already running in a background goroutine
		EnforceIoTuneForDomain(conns, opts, logger)
	}
}

// import (
// 	"fmt"
// 	"strings"
// 	"sync"

// 	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
// 	"github.com/digitalocean/go-libvirt"
// )

// func handlerLibvirtLifecycleEvents(
// 	event any,
// 	wg *sync.WaitGroup,
// 	conns Connections,
// 	opts EnforceIoTuneOpts,
// 	logger Logger,
// ) {
// 	defer wg.Done()
// 	// Setup Logger
// 	logger.SetupLogger()
// 	log := logger.Instance.With(
// 		"global_run_id", logger.GlobalRunID,
// 		"run_id", logger.RunID,
// 		"reactor", "libvirt-event-reactor",
// 		"processor", "libvirt_events_lifecycle_handler",
// 	)

// 	if msg, ok := event.(*libvirt.DomainEventCallbackLifecycleMsg); ok {
// 		domName := msg.Msg.Dom.Name
// 		eventName, detail := virt.ParseLifecycle(msg.Msg.Event, msg.Msg.Detail)
// 		log.Info(
// 			"Event Detected",
// 			"type", fmt.Sprintf("VM Lifecycle - %s", eventName),
// 			"domain", domName,
// 			"detail", detail,
// 		)
// 		if eventName == "VM_STARTED" || (eventName == "VM_RESUMED" && strings.Contains(detail, "Migration Finished")) {
// 			// Ignore livemirgration starts, enforce on completion during VM_RESUMED.
// 			if strings.Contains(detail, "Incoming Migration") {
// 				return
// 			}

// 			// Trigger Enforcement
// 			wg.Add(1)
// 			jobLogger := logger
// 			jobLogger.RunID = ""
// 			opts.DomainID = domName
// 			go func() {
// 				defer wg.Done()
// 				EnforceIoTuneForDomain(
// 					conns,
// 					opts,
// 					jobLogger,
// 				)
// 			}()

// 		}
// 	}
// }

// func handlerLibvirtDiskEvents(
// 	event any,
// 	wg *sync.WaitGroup,
// 	conns Connections,
// 	opts EnforceIoTuneOpts,
// 	logger Logger,
// ) {

// 	defer wg.Done()
// 	// Setup Logger
// 	logger.SetupLogger()
// 	log := logger.Instance.With(
// 		"global_run_id", logger.GlobalRunID,
// 		"run_id", logger.RunID,
// 		"reactor", "libvirt-event-reactor",
// 		"processor", "libvirt_events_disk_handler",
// 	)

// 	if msg, ok := event.(*libvirt.DomainEventCallbackDeviceAddedMsg); ok {
// 		domName := msg.Dom.Name
// 		// Parse the disk alias to extract the Cinder UUID.
// 		parts := strings.Split(msg.DevAlias, "ua-")
// 		var diskCinderID string
// 		if len(parts) > 1 {
// 			diskCinderID = parts[1]
// 		} else {
// 			// If it's not a Cinder volume (e.g., config-drive),
// 			// we should skip enforcement for this specific device.
// 			log.Debug("Skipping non-cinder device", "alias", msg.DevAlias)
// 			return
// 		}
// 		log.Info("Event Detected", "type", "Disk Attachment", "domain", domName, "disk_uuid", diskCinderID)
// 		opts.DiskID = diskCinderID
// 		opts.DomainID = domName

// 		wg.Add(1)
// 		jobLogger := logger
// 		jobLogger.RunID = ""
// 		go func() {
// 			defer wg.Done()
// 			EnforceIoTuneForDomain(
// 				conns,
// 				opts,
// 				jobLogger,
// 			)
// 		}()
// 	}
// }
