package workflow

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud/openstack"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/notification"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
	"github.com/digitalocean/go-libvirt"
	"github.com/google/uuid"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/qos"
)

// VMDiskIOEnforcement orchestrates the synchronization of Storage QoS settings
// between OpenStack Cinder volumes and Libvirt domain disks.
//
// The function follows a hierarchical policy resolution:
//  1. Manual Override: Checks for the [IOPOLICYKEYOVERRIDE] key in volume metadata.
//  2. Base Policy: Falls back to a named OpenStack QoS Spec if no override is found.
//
// Logic Flow:
//   - Generates a unique "run_id" for tracing the workflow across logs.
//   - Fetches the current Libvirt domain state.
//   - Iterates through attached disks and retrieves corresponding Cinder volume data.
//   - Compares desired I/O limits with actual Libvirt settings.
//   - Updates Libvirt limits only if a mismatch is detected (State Reconciliation).
func VMDiskIOEnforcement(
	libvirtConnection *libvirt.Libvirt,
	openstackConnection openstack.Client,
	domainID string,
	qoses []qos.QoS,
	baseQOSPolicy string,
	timeout int,
	logLevel string,
	notifyProvider notification.Webhook,
) {

	// Initialize a structured logger with a unique request ID for distributed tracing.
	logger := SetupLogger(logLevel, openstackConnection.ProfileName)
	runID := fmt.Sprintf("req-virt-%s", uuid.New().String())
	logger = logger.With("run_id", runID)

	logger.Info(
		"Workflow Started", "workflow_name", "VM Disk IOTune Enforcement",
		"description", "Applies the QOS Spec from cinder volume metadata",
		"domain_name", domainID,
	)

	// Configure context with the user-defined timeout.
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			ctx, time.Duration(timeout)*time.Second,
		)
		defer cancel()
		logger.Debug("Workflow Timeout Configured", "workflow_name", "VM Disk IOTune Enforcement")
	}

	// Gather Domain details from Libvirt.
	logger.Debug("Attempting to fetch domain details from libvirt")
	domain, err := virt.GetVirtualMachine(libvirtConnection, domainID)
	if err != nil {
		logger.Error("Failed to query libvirt domain information", "err", err)
		return
	}
	logger = logger.With("openstack_name", domain.OpenstackName)
	logger.Info("Successfully fetched domain details", "disks", len(domain.Devices.Disks))

	// Iterate and process each attached disk.
	for idx, disk := range domain.Devices.Disks {
		diskLogger := logger.With(
			"cinder_id", disk.Serial,
			"attachment_index", fmt.Sprintf("%d / %d", idx+1, len(domain.Devices.Disks)),
		)
		diskLogger.Debug("Attempting to process libvirt disk / cinder volume")

		// Retrieve the source-of-truth volume data from Cinder.
		cinderInfo, err := openstackConnection.GetVolume(ctx, disk.Serial)
		if err != nil {
			diskLogger.Error("Failed to gather disk information from cinder", "error", err)
			continue
		}

		// Target IOTUNE Policy for the disk
		diskIOTunePolicy := virt.IOTune{}
		isIOTuneOK := false

		// Check for Manual Metadata Overrides (CSV Format: total,write,read).
		volumeQOSPolicyOverride, ok := cinderInfo.Metadata[IOPOLICYKEYOVERRIDE]
		if !ok {
			diskLogger.Warn("Failed to identify IO Policy override in the cinder volume metadata. Attempting to enforce base policy")
		} else {
			policyOverride := strings.Split(volumeQOSPolicyOverride, ",")
			if len(policyOverride) == 3 {

				totalIOPSValue, terr := strconv.Atoi(policyOverride[0])
				writeIOPSValue, werr := strconv.Atoi(policyOverride[1])
				readIOPSValue, rerr := strconv.Atoi(policyOverride[2])
				if terr != nil || werr != nil || rerr != nil {
					diskLogger.Error(
						"Unable to formulate the iops limit values from cinder metadata",
						"totalIOPSError", terr,
						"writeIOPSError", werr,
						"readIOPSError", rerr,
					)
				} else {
					logger.Info("Successfully obtained QOS values from volume metadata", "values", policyOverride)
					// Prioritize Total IOPS if provided; otherwise, apply specific Read/Write limits.
					if totalIOPSValue != 0 {
						diskIOTunePolicy = virt.IOTune{
							TotalIopsSec: uint64(totalIOPSValue),
							SizeIopsSec:  16384,
						}
						isIOTuneOK = true
					} else {
						diskIOTunePolicy = virt.IOTune{
							WriteIopsSec: uint64(writeIOPSValue),
							ReadIopsSec:  uint64(readIOPSValue),
							SizeIopsSec:  16384,
						}
						isIOTuneOK = true
					}
				}
			}
		}

		// Fallback to Base QoS Spec if no override exists or parsing failed.
		// This named QoS spec is expected to exist in openstack and will not be created by virt-enforcer.
		if !isIOTuneOK {
			// Get QOS Policies
			if len(qoses) == 0 {
				fetchedQOSes, err := openstackConnection.ListQosSpec(ctx)
				if err != nil {
					logger.Error("Failed to query QOS spec list from openstack", "err", err)
					return
				}
				qoses = fetchedQOSes
				logger.Info("Fetched the QOS Spec list from openstack", "qos_policies", qoses)
			} else {
				logger.Info("Provided QOS list is used instead of querying openstack", "qos_policies", qoses)
			}

			var qosSpecMap map[string]string

			// Contruct the QOS spec values for the mentioned policy name in volume.
			for _, policy := range qoses {
				if policy.Name == baseQOSPolicy {
					qosSpecMap = policy.Specs
					logger.Debug("Successfully fetched the base QOS policy from openstack", "base_policy", baseQOSPolicy)
					break
				}
			}

			// Check for policy match
			if len(qosSpecMap) == 0 {
				diskLogger.Error(
					"Failed to identify the base qos policy from openstack. QOS Cannot be enforced for this volume",
					"base_policy", baseQOSPolicy,
				)
				continue
			}

			convertedQOSPolicy, err := openstack.ParseOpenstackMetadataToStruct[virt.IOTune](qosSpecMap, "xml")
			if err != nil {
				diskLogger.Error(
					"Failed to convert openstack metadata to IOTune Object. Please validate if the QOS is valid. QOS cannot be enforced for this volume",
					"err", err,
				)
				continue
			}
			diskIOTunePolicy = *convertedQOSPolicy

			diskLogger.Info("Successfully obtained QOS values from base policy", "base_policy", baseQOSPolicy)
		}

		// State Reconciliation.
		// Only trigger the Libvirt RPC call if the desired policy differs from current settings.
		if disk.IOTune == diskIOTunePolicy {
			diskLogger.Info("IO limits are already in place as per openstack metadata. No modification is are required.")
			continue
		}

		if err := virt.SetIOLimits(libvirtConnection, domain, diskIOTunePolicy, disk.Target.Dev); err != nil {
			diskLogger.Error("Failed to modify IO limits on libvirt to match openstack metadata", "err", err)
			continue
		}

		diskLogger.Info("Successfully enforced IO Limits on the libvirt layer to match openstack metadata", "policy", diskIOTunePolicy)
	}
}

// AllVMDiskIOEnforcement performs a bulk reconciliation of I/O limits for all
// active virtual machines on the host.
//
// It utilizes a concurrent worker pool (defaulting to 5 workers) to process
// multiple domains in parallel. The workflow fetches global QoS specifications
// from OpenStack once and shares them across workers to minimize API load.
//
// Concurrency Model:
//   - A 'jobs' channel distributes [virt.VirtualMachine] tasks to the pool.
//   - A [sync.WaitGroup] ensures the workflow only completes after all workers
//     have finished their assigned domains.
//   - Context-aware timeouts are passed down to individual enforcer calls to
//     prevent hung goroutines.
func AllVMDiskIOEnforcement(
	libvirtConnection *libvirt.Libvirt,
	cloudName string,
	baseQOSPolicy string,
	timeout int,
	logLevel string,
	notifyProvider notification.Webhook,
) {

	// Initialize logger and signal start of bulk workflow.
	logger := SetupLogger(logLevel, cloudName)
	logger.Info(
		"Workflow Started", "workflow_name", "All VM Disk IOTune Enforcement",
		"description", "Applies the QOS Spec from cinder volume metadata for all the available domains in the host",
	)

	// Formulate a context as per the timeout value provided.
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			ctx, time.Duration(timeout)*time.Second,
		)
		defer cancel()
		logger.Debug("Workflow Timeout Configured", "workflow_name", "All VM Disk IOTune Enforcement")
	}

	// Establish OpenStack connection for metadata and QoS lookup.
	logger.Debug("Attempting to connect to openstack")
	ostk, err := openstack.ConnectToOpenstack(cloudName)
	if err != nil {
		logger.Error("Failed to connect to Openstack", "err", err)
		return
	}
	logger.Info("Connected to Openstack successfully")

	// Pre-fetch QoS specifications to avoid redundant API calls within workers.
	logger.Debug("Attempting to fetch QOS policy from openstack")
	qoses, err := ostk.ListQosSpec(ctx)
	if err != nil {
		logger.Error("Failed to query QOS spec list from openstack", "err", err)
		return
	}

	// Retrieve all active domains from Libvirt.
	logger.Debug("Attempting to fetch domain list from libvirt")
	domains, err := virt.GetAllVirtualMachines(libvirtConnection, true)
	if err != nil {
		logger.Error("Failed to fetch virtual machine list from libvirt", "err", err)
		return
	}
	logger.Info("Fetched domains from libvirt", "count", len(domains))

	// Initialize the worker pool orchestration.
	var VMDiskIOEnforcementWG sync.WaitGroup
	jobs := make(chan virt.VirtualMachine, len(domains))

	// Spawn workers to process the job queue.
	for w := 1; w <= 5; w++ {
		VMDiskIOEnforcementWG.Add(1)
		go func() {
			defer VMDiskIOEnforcementWG.Done()
			for dom := range jobs {
				VMDiskIOEnforcement(
					libvirtConnection,
					ostk,
					dom.Name,
					qoses,
					baseQOSPolicy,
					timeout,
					logLevel,
					notifyProvider,
				)
			}
		}()

	}

	// Feed all discovered domains into the job channel.
	for _, dom := range domains {
		jobs <- dom
	}

	// Close the channel to signal workers that no more jobs are coming.
	close(jobs)

	// Block until all workers have completed their tasks.
	VMDiskIOEnforcementWG.Wait()

	logger.Info(
		"Workflow Completed", "workflow_name", "All VM Disk IOTune Enforcement",
		"description", "Applies the QOS Spec from cinder volume metadata for all the available domains in the host",
	)

}
