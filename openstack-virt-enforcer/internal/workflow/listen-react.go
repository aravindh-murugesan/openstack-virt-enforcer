package workflow

import (
	"context"
	"encoding/json"
	"os"
	"os/signal"
	"slices"
	"sync"
	"syscall"
	"time"

	ebnats "github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/event/eb-nats"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
	"github.com/digitalocean/go-libvirt"
	"github.com/nats-io/nats.go/jetstream"
)

// --- ORCHESTRATOR ---
func LibvirtListenAndReact(
	conns Connections,
	opts DaemonOpts,
	logger Logger,
) {
	logger.SetupLogger()
	log := logger.Instance.With("global_run_id", logger.GlobalRunID, "run_id", logger.RunID)

	log.Info("Workflow Started", "name", "Openstack Virt Enforcer - Listen and React for Libvirt")

	// 1. The Clean Context: Automatically cancels on Ctrl+C / SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var wg sync.WaitGroup

	// Spawn the main Libvirt watcher
	libvirtLogger := logger
	libvirtLogger.RunID = ""

	wg.Add(1)
	go LibvirtEventReactor(conns, opts.LibvirtControllers.IoTuneEnforcement, libvirtLogger, &wg, ctx)

	// Spawn the nats watcher
	if conns.Nats.Connection != nil {
		natsLogger := logger
		natsLogger.RunID = ""
		go NatsEventReactor(conns, opts.LibvirtControllers.IoTuneEnforcement, natsLogger, &wg, ctx)
	} else {
		log.Info(
			"Skipping NATS Listener",
			"reason", "NATS connection is not established. NATS connection .connect() method has to be called before init",
		)
	}

	// Block here until the user stops the daemon
	<-ctx.Done()
	log.Info("System interrupt received. Waiting for active workflows to finish...")

	// Graceful Shutdown with a 30-second timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Info("All active workflows completed. Libvirt listener shut down gracefully.")
	case <-time.After(30 * time.Second):
		log.Warn("Shutdown timeout exceeded! Forcing exit. Background tasks may be interrupted.")
	}
}

func NatsEventReactor(
	conns Connections,
	opts EnforceIoTuneOpts,
	logger Logger,
	wg *sync.WaitGroup,
	ctx context.Context,
) {
	defer wg.Done()

	logger.SetupLogger()
	log := logger.Instance.With("global_run_id", logger.GlobalRunID, "run_id", logger.RunID, "reactor", "nats-iotune-event-reactor")

	log.Info("Reactor Started", "name", "NATS Message Listener for IOTune Subscription")

	libvirtURL, err := conns.Libvirt.ConnectGetUri()
	if err != nil {
		log.Error("Unable to get the Libvirt URI from connected instance. Please check if libvirt deamon is responding or not")
		return
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("NATS Event Listener stopped gracefully.")
			return
		default:
			// Health Pings for libvirt and reconnection
			if libvirtAlive := conns.Libvirt.IsConnected(); !libvirtAlive {
				log.Error("Libvirt Connection is dead. Attempting to reconnect...", "alive", libvirtAlive)
				lc, err := virt.ConnectToLibvirt(libvirtURL)
				if err != nil {
					log.Error("Libvirt Connection cannot be established. Will attempt to reconnect in 2s...", "err", err)
					time.Sleep(2 * time.Second)
					continue
				}
				conns.Libvirt = lc
				continue
			}
			log.Info("Libvirt Connection validated successfully", "alive", "true")

			if alive := conns.Nats.Connection.IsConnected(); !alive {
				log.Error("NATS Connection is dead. Waiting 5s before retrying...")
				time.Sleep(5 * time.Second)
				continue
			}

			if err := conns.Nats.InitJetStream(); err != nil {
				log.Error("Failed to initialize Jetstream context. Retrying in 5s...", "err", err)
				time.Sleep(5 * time.Second)
				continue
			}

			iotuneLogger := logger
			iotuneLogger.RunID = ""
			iotuneLogger.GlobalRunID = logger.RunID
			ProcessIoTuneSubscriptionsNats(conns, opts, iotuneLogger, wg, ctx)
		}
	}

}

func ProcessIoTuneSubscriptionsNats(
	conns Connections,
	opts EnforceIoTuneOpts,
	logger Logger,
	wg *sync.WaitGroup,
	ctx context.Context,
) (shouldRetry bool) {

	logger.SetupLogger()
	log := logger.Instance.With(
		"global_run_id", logger.GlobalRunID,
		"run_id", logger.RunID,
		"reactor", "nats-event-reactor",
		"processor", "nats_iotune_events",
	)

	// Initialize the Consumer
	consumer, err := conns.Nats.SubscribeDiskQOSUpdate(true)
	if err != nil {
		log.Error("Failed to initialize NATS consumer", "err", err)
		return true
	}

	reconnectNeeded := make(chan bool, 1)

	handler := func(msg jetstream.Msg) {
		// We add to WaitGroup here so a shutdown waits for a message currently being processed
		wg.Add(1)
		defer wg.Done()

		var incomingSubMessage ebnats.NATSDiskSubscriptionMessage
		if err := json.Unmarshal(msg.Data(), &incomingSubMessage); err != nil {
			log.Error("Failed to parse NATS message", "err", err)
			msg.Term()
			return
		}

		log.Info("Message Received on NATS", "type", incomingSubMessage.EventType)

		// Enforcement process when the message is received.
		//
		// Check if the server is present of the hypervisor
		domains, err := virt.GetAllVirtualMachines(conns.Libvirt, true)
		if err != nil {
			log.Error("Unable to fetch the list of VMs from libvirt")
			msg.NakWithDelay(5 * time.Second)
			select {
			case reconnectNeeded <- true:
			default:
			}
			return
		}
		matchedDomainIndex := slices.IndexFunc(domains, func(v virt.VirtualMachine) bool {
			return v.Name == incomingSubMessage.ServerID
		}) // Returns -1 for not found

		if matchedDomainIndex == -1 {
			log.Info("Requested VM does not exist on the hypervisor. Nothing to be done.")
			msg.Ack()
			return
		}

		if incomingSubMessage.EventType == ebnats.EventUpdateQos {
			jobOpts := opts
			jobOpts.Enforce = true
			jobOpts.DiskID = incomingSubMessage.DiskID
			jobOpts.DomainID = incomingSubMessage.ServerID
			jobLogger := logger
			jobLogger.RunID = ""
			result := EnforceIoTuneForDomain(conns, jobOpts, jobLogger)
			log.Info("Enforcement Workflow completed", "result", result)
			msg.Ack()

			pubCtx, pubCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer pubCancel()

			log.Info("Attempting to respond back to the request", "message_id", incomingSubMessage.ID)
			notificationMessage := ebnats.NATSDiskSubscriptionNotificationMessage{
				NATSDiskSubscriptionMessage: incomingSubMessage,
				Status:                      ebnats.EventStatus(result.Result),
				Implementor:                 conns.Nats.ClientIdentifier,
			}

			pub, err := conns.Nats.PublishDiskQOSEnforcementNotification(
				pubCtx,
				incomingSubMessage.ID,
				notificationMessage,
			)
			if err != nil {
				log.Error("Response to QOS Notification channel has failed", "err", err)
			} else {
				log.Info("Response to QOS Update sent to notification channel", "stream", pub.Stream)
			}
		} else {
			log.Warn("Message rejected intentionally", "type", incomingSubMessage.EventType, "reason", "invalid message type received.")
			msg.Term()
		}
	}

	qosConsume, err := consumer.Consume(handler)
	if err != nil {
		log.Error("Failed to start NATS consumer consumption", "err", err)
		return true
	}

	select {
	case <-ctx.Done():
		log.Info("Stopping NATS consumer (Graceful Shutdown)...")
		qosConsume.Stop()
		return false
	case <-reconnectNeeded:
		log.Warn("Stopping NATS consumer (Health Failure detected)")
		qosConsume.Stop()
		return true
	}
}

func LibvirtEventReactor(
	conns Connections,
	opts EnforceIoTuneOpts,
	logger Logger,
	wg *sync.WaitGroup,
	ctx context.Context,
) {
	defer wg.Done()

	logger.SetupLogger()
	log := logger.Instance.With("global_run_id", logger.GlobalRunID, "run_id", logger.RunID, "reactor", "libvirt-event-reactor")

	log.Info("Reactor Started", "name", "Libvirt Live Event Monitor")

	libvirtURL, err := conns.Libvirt.ConnectGetUri()
	if err != nil {
		log.Error("Unable to get the Libvirt URI from connected instance. Please check if libvirt deamon is responding or not")
		return
	}

	for {
		select {
		case <-ctx.Done():
			log.Info("Libvirt Event listener stopped gracefully")
			return
		default:
			// Health Pings for libvirt and reconnection
			if alive := conns.Libvirt.IsConnected(); !alive {
				log.Error("Libvirt Connection is dead. Attempting to reconnect...", "alive", alive)
				lc, err := virt.ConnectToLibvirt(libvirtURL)
				if err != nil {
					log.Error("Libvirt Connection cannot be established. Will attempt to reconnect in 2s...", "err", err)
					time.Sleep(2 * time.Second)
					continue
				}
				conns.Libvirt = lc
				continue
			}
			log.Info("Libvirt Connection validated successfully", "alive", "true")

			// Start up Audit. Periodic audit are taken care of in ProcessEventsLibvirt
			auditLogger := logger
			auditLogger.RunID = ""
			auditLogger.GlobalRunID = logger.RunID
			EnforceIoTuneForAllDomain(conns, opts, auditLogger)

			eventInstanceLogger := logger
			eventInstanceLogger.RunID = ""
			shouldRetryLibvirtEvents := ProcessEventsLibvirt(ctx, wg, conns, opts, eventInstanceLogger)

			if !shouldRetryLibvirtEvents {
				conns.Libvirt.Disconnect()
				log.Info("Event Listener has shut down cleanly...")
				return
			}

			conns.Libvirt.Disconnect()
			log.Debug("Attempting to reconnect to libvirt in 2s...")
			time.Sleep(2 * time.Second)
		}
	}
}

func ProcessEventsLibvirt(
	ctx context.Context,
	wg *sync.WaitGroup,
	conns Connections,
	opts EnforceIoTuneOpts,
	logger Logger,
) (shouldRetry bool) {

	logger.SetupLogger()
	log := logger.Instance.With(
		"global_run_id", logger.GlobalRunID,
		"run_id", logger.RunID,
		"reactor", "libvirt-event-reactor",
		"processor", "libvirt_events",
	)

	diskEvents, err := conns.Libvirt.SubscribeEvents(ctx, libvirt.DomainEventIDDeviceAdded, libvirt.OptDomain{})
	if err != nil {
		log.Error("Error subscribing to disk events", "err", err)
		return true // FIXED: Added missing return
	}

	lifecycleEvent, err := conns.Libvirt.SubscribeEvents(ctx, libvirt.DomainEventIDLifecycle, libvirt.OptDomain{})
	if err != nil {
		log.Error("Error subscribing to lifecycle events", "err", err)
		return true
	}

	healthTicker := time.NewTicker(10 * time.Second)
	defer healthTicker.Stop()

	iotuneAuditTicket := time.NewTicker(time.Duration(opts.AuditInterval) * time.Minute)
	defer iotuneAuditTicket.Stop()

	for {
		select {
		case <-ctx.Done():
			return false

		case <-healthTicker.C:
			if _, err := conns.Libvirt.ConnectGetLibVersion(); err != nil {
				log.Error("Libvirt heartbeat failed", "err", err)
				return true
			}

		case <-iotuneAuditTicket.C:
			jobLogger := logger
			jobLogger.RunID = ""
			jobLogger.GlobalRunID = logger.RunID
			go EnforceIoTuneForAllDomain(conns, opts, jobLogger)

		case event, ok := <-diskEvents:
			if !ok {
				log.Error("Connection to disk event has closed unexpectedly")
				return true
			}

			jobLogger := logger
			jobLogger.RunID = ""
			jobLogger.GlobalRunID = logger.RunID

			// Track the goroutine with wg, and fire it off
			wg.Add(1)
			go handlerLibvirtDiskEvents(event, wg, conns, opts, jobLogger)

		case event, ok := <-lifecycleEvent:
			if !ok {
				log.Error("Connection to lifecycle event has closed unexpectedly")
				return true
			}

			jobLogger := logger
			jobLogger.RunID = ""
			jobLogger.GlobalRunID = logger.RunID

			// Track the goroutine with wg, and fire it off
			wg.Add(1)
			go handlerLibvirtLifecycleEvents(event, wg, conns, opts, jobLogger)
		}
	}
}

// import (
// 	"context"
// 	"os"
// 	"os/signal"
// 	"sync"
// 	"syscall"
// 	"time"

// 	"github.com/digitalocean/go-libvirt"
// )

// func LibvirtListenAndReact(
// 	conns Connections,
// 	opts DaemonOpts,
// 	logger Logger,
// ) {

// 	// Setup Logger
// 	logger.SetupLogger()
// 	log := logger.Instance.With("global_run_id", logger.GlobalRunID, "run_id", logger.RunID)

// 	log.Info("Workflow Started", "name", "Openstack Virt Enforcer - Listen and React for Libvirt")

// 	ctx, cancel := context.WithCancel(context.Background())
// 	defer cancel()

// 	// Capture system interrupts for graceful cleanup.
// 	sigChan := make(chan os.Signal, 1)
// 	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
// 	go func() {
// 		exitCall := <-sigChan
// 		log.Info("Stopping event listener...", "reason", "Received a system interrupt call", "signal", exitCall.String())
// 		cancel()
// 	}()

// 	var wg sync.WaitGroup

// 	// Libvirt Event Reactor Loop
// 	libvirtLogger := logger
// 	libvirtLogger.RunID = ""
// 	wg.Add(1)
// 	go LibvirtEventReactor(conns, opts.IoTuneOpts, logger, &wg, ctx)

// }

// func LibvirtEventReactor(
// 	conns Connections,
// 	opts EnforceIoTuneOpts,
// 	logger Logger,
// 	wg *sync.WaitGroup,
// 	ctx context.Context,
// ) {

// 	defer wg.Done()

// 	// All the reactions to libvirt event broadcast queues will be part of this function.
// 	// Setup Logger
// 	logger.SetupLogger()
// 	log := logger.Instance.With("global_run_id", logger.GlobalRunID, "run_id", logger.RunID, "reactor", "libvirt-event-reactor")

// 	log.Info("Workflow Started", "name", "Enforce IOTune for a Libvirt Domain")

// 	for {
// 		select {
// 		case <-ctx.Done():
// 			log.Info("Event listener stopped successfully")
// 			return
// 		default:
// 			if alive := conns.Libvirt.IsConnected(); !alive {
// 				log.Error("Libvirt Connection is dead. Will attempt to reconnect in 5s...", "alive", alive)
// 				time.Sleep(5 * time.Second)
// 				continue
// 			}

// 			eventInstanceLogger := logger
// 			eventInstanceLogger.RunID = ""
// 			shouldRetryLibvirtEvents := ProcessEventsLibvirt(ctx, wg, conns, opts, eventInstanceLogger)

// 			if !shouldRetryLibvirtEvents {
// 				conns.Libvirt.Disconnect()
// 				log.Info("Event Listener has shut down cleanly...")
// 				return
// 			}

// 			conns.Libvirt.Disconnect()
// 			log.Info("Attempting to reconnect to libvirt in 5s...")
// 			time.Sleep(5 * time.Second)

// 		}
// 	}
// }

// func ProcessEventsLibvirt(
// 	ctx context.Context,
// 	wg *sync.WaitGroup,
// 	conns Connections,
// 	opts EnforceIoTuneOpts,
// 	logger Logger,
// ) (shouldRetry bool) {

// 	// Setup Logger
// 	logger.SetupLogger()
// 	log := logger.Instance.With(
// 		"global_run_id", logger.GlobalRunID,
// 		"run_id", logger.RunID,
// 		"reactor", "libvirt-event-reactor",
// 		"processor", "libvirt_events",
// 	)

// 	diskEvents, err := conns.Libvirt.SubscribeEvents(ctx, libvirt.DomainEventIDDeviceAdded, libvirt.OptDomain{})
// 	if err != nil {
// 		log.Error("Error subscribing to disk events", "err", err)
// 	}

// 	lifecycleEvent, err := conns.Libvirt.SubscribeEvents(ctx, libvirt.DomainEventIDLifecycle, libvirt.OptDomain{})
// 	if err != nil {
// 		log.Error("Error subscribing to lifecycle events", "err", err)
// 		return true
// 	}

// 	ticker := time.NewTicker(10 * time.Second)
// 	defer ticker.Stop()

// 	for {
// 		select {
// 		case <-ctx.Done():
// 			return false

// 		case <-ticker.C:
// 			if _, err := conns.Libvirt.ConnectGetLibVersion(); err != nil {
// 				log.Error("Libvirt heartbeat failed", "err", err)
// 				return true
// 			}
// 		case event, ok := <-diskEvents:
// 			if !ok {
// 				log.Error("Connection to disk event has closed unexpectedly")
// 				return true
// 			}
// 			jobLogger := logger
// 			jobLogger.RunID = ""
// 			jobLogger.GlobalRunID = logger.RunID
// 			wg.Add(1)
// 			go handlerLibvirtDiskEvents(event, wg, conns, opts, jobLogger)

// 		case event, ok := <-lifecycleEvent:
// 			if !ok {
// 				log.Error("Connection to lifecycle event has closed", "err", err)
// 				return true
// 			}
// 			jobLogger := logger
// 			jobLogger.RunID = ""
// 			jobLogger.GlobalRunID = logger.RunID
// 			wg.Add(1)
// 			go handlerLibvirtLifecycleEvents(event, wg, conns, opts, jobLogger)
// 		}

// 	}

// }
