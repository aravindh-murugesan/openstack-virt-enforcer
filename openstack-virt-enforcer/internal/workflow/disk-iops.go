package workflow

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud/openstack"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
)

func EnforceIoTuneForDomain(conns Connections, opts EnforceIoTuneOpts, logger Logger) EnforceIoTuneResult {

	// Response
	response := EnforceIoTuneResult{
		Request: opts,
	}

	// Setup Logger
	logger.SetupLogger()
	log := logger.Instance.With("global_run_id", logger.GlobalRunID, "run_id", logger.RunID, "domain", opts.DomainID)

	log.Info("Workflow Started", "name", "Enforce IOTune for a Libvirt Domain")

	// Setup Context for tasks
	ctx := context.Background()
	if conns.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			ctx, time.Duration(conns.Timeout)*time.Second,
		)
		defer cancel()
		log.Info("Workflow Timeout Configured", "workflow_name", "Enforce IOTune for a Libvirt Domain")
	}

	// Check if the requested domain exist
	v, err := virt.GetVirtualMachine(conns.Libvirt, opts.DomainID)
	if err != nil {
		response.Result = IoTuneEnforcementFailed
		response.Message += fmt.Sprintf("Failed to query libvirt domain information: %v", err)
		log.Error("Failed to query libvirt domain information", "err", err)
		return response
	}

	log = log.With("openstack_vm_name", v.OpenstackName)
	log.Debug("Successfully fetched libvirt domain information", "disks", len(v.Devices.Disks))

	overallStatus := IoTuneEnforcementSuccess
	hasChanges := false
	for idx, disk := range v.Devices.Disks {
		dLogger := log.With(
			"domain_disk", disk.Serial,
			"attachment_index", fmt.Sprintf("%d/%d", idx+1, len(v.Devices.Disks)),
		)

		// Enforcement should only be done when DiskID is empty or if DiskID matches current disk
		if opts.DiskID != "" && opts.DiskID != disk.Serial {
			dLogger.Debug("IO Tune Enforcement for the disk has been skipped", "reason", "serial does not match requested id")
			response.Message += fmt.Sprintf("Disk %d (%s) - Skipped, ", idx, disk.Serial)
			continue
		}

		diskInfoCinder, err := conns.Openstack.GetVolume(ctx, disk.Serial)
		if err != nil {
			overallStatus = IoTuneEnforcementFailed
			response.Message += fmt.Sprintf("Disk %d (%s) - Failed (Openstack API), ", idx, disk.Serial)
			dLogger.Error("Failed to gather disk information from cinder", "error", err)
			continue
		}

		// Target IOTUNE Policy for the disk
		diskIOTunePolicy := virt.IOTune{}
		isIOTuneOK := false

		// Check for Manual Metadata Overrides (CSV Format: total,write,read).
		volumeQOSPolicyOverride, ok := diskInfoCinder.Metadata[IOPOLICYKEYOVERRIDE]
		if !ok {
			dLogger.Warn("Failed to identify IO Policy override in cinder metadata. Attempting base policy")
		} else {
			policyOverride := strings.Split(volumeQOSPolicyOverride, ",")
			if len(policyOverride) == 3 {
				totalIOPSValue, terr := strconv.Atoi(policyOverride[0])
				writeIOPSValue, werr := strconv.Atoi(policyOverride[1])
				readIOPSValue, rerr := strconv.Atoi(policyOverride[2])

				if terr != nil || werr != nil || rerr != nil {
					dLogger.Error(
						"Unable to formulate the iops limit values from cinder metadata",
						"totalIOPSError", terr,
						"writeIOPSError", werr,
						"readIOPSError", rerr,
					)
				} else {
					dLogger.Info("Successfully obtained QOS values from volume metadata", "values", policyOverride)
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
		if !isIOTuneOK {
			if len(opts.OpenstackQoS) == 0 {
				fetchedQOSes, err := conns.Openstack.ListQosSpec(ctx)
				if err != nil {
					dLogger.Error("Failed to query QOS spec list from openstack", "err", err)
					overallStatus = IoTuneEnforcementFailed
					response.Message += fmt.Sprintf("Disk %d (%s) - Failed (QoS API Failure), ", idx, disk.Serial)
					continue
				}
				opts.OpenstackQoS = fetchedQOSes
				dLogger.Debug("Fetched the QOS Spec list from openstack", "qos_policies", opts.OpenstackQoS)
			} else {
				dLogger.Debug("Provided QOS list is used instead of querying openstack")
			}

			var qosSpecMap map[string]string

			// Construct the QOS spec values for the mentioned policy name
			for _, policy := range opts.OpenstackQoS {
				if policy.Name == opts.BaseQoSPolicy {
					qosSpecMap = policy.Specs
					dLogger.Debug("Successfully fetched the base QOS policy", "base_policy", opts.BaseQoSPolicy)
					break
				}
			}

			// Check for policy match
			if len(qosSpecMap) == 0 {
				dLogger.Error("Failed to identify base qos policy. Cannot be enforced", "base_policy", opts.BaseQoSPolicy)
				overallStatus = IoTuneEnforcementFailed
				response.Message += fmt.Sprintf("Disk %d (%s) - Failed (BasePolicy not found), ", idx, disk.Serial)
				continue
			}

			convertedQOSPolicy, err := openstack.ParseOpenstackMetadataToStruct[virt.IOTune](qosSpecMap, "xml")
			if err != nil {
				dLogger.Error("Failed to convert openstack metadata to IOTune Object", "err", err)
				overallStatus = IoTuneEnforcementFailed
				response.Message += fmt.Sprintf("Disk %d (%s) - Failed (Invalid QOS parameters), ", idx, disk.Serial)
				continue
			}
			diskIOTunePolicy = *convertedQOSPolicy
			dLogger.Info("Successfully obtained QOS values from base policy", "base_policy", opts.BaseQoSPolicy)
		}

		// State Reconciliation
		if disk.IOTune == diskIOTunePolicy {
			dLogger.Info("IO limits are already in place as per openstack metadata.")
			response.Message += fmt.Sprintf("Disk %d (%s) - OKAY, ", idx, disk.Serial)
			continue
		}

		// Audit Mode Escape Hatch
		if !opts.Enforce {
			dLogger.Info("IOTune does not match intended spec, but skipping enforcement (Audit Mode)")
			if overallStatus != IoTuneEnforcementFailed {
				overallStatus = IoTuneEnforcementNotRequested
			}
			response.Message += fmt.Sprintf("Disk %d (%s) - AUDIT ONLY, ", idx, disk.Serial)
			continue
		}

		// Execute Enforcement
		if err := virt.SetIOLimits(conns.Libvirt, v, diskIOTunePolicy, disk.Target.Dev); err != nil {
			dLogger.Error("Failed to modify IO limits on libvirt", "err", err)
			overallStatus = IoTuneEnforcementFailed
			response.Message += fmt.Sprintf("Disk %d (%s) - Failed (Libvirt API), ", idx, disk.Serial)
			continue
		}

		dLogger.Info("Successfully enforced IO Limits on the libvirt layer", "policy", diskIOTunePolicy)
		hasChanges = true
		response.Message += fmt.Sprintf("Disk %d (%s) - SUCCESS, ", idx, disk.Serial)
	}

	// 6. Finalize Response
	if overallStatus == IoTuneEnforcementSuccess && !hasChanges {
		overallStatus = IoTuneEnforcementInPlace
	}

	response.Result = overallStatus
	return response
}

func EnforceIoTuneForAllDomain(conns Connections, opts EnforceIoTuneOpts, logger Logger) {
	// Intentionally not returning any return, as this is an internal flow. And should not be used called from external clients.
	// Setup Logger
	logger.SetupLogger()
	log := logger.Instance.With("global_run_id", logger.GlobalRunID, "run_id", logger.RunID)

	log.Info("Workflow Started", "name", "Enforce IOTune for all Libvirt Domains")

	// Setup Context for tasks
	ctx := context.Background()
	if conns.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(
			ctx, time.Duration(conns.Timeout)*time.Second,
		)
		defer cancel()
		log.Info("Workflow Timeout Configured", "workflow_name", "Enforce IOTune for all Libvirt Domains")
	}

	// Prefetch QoS - more efficient to prefetch here since we are processing all the domains.
	if len(opts.OpenstackQoS) == 0 {
		fetchedQOSes, err := conns.Openstack.ListQosSpec(ctx)
		if err != nil {
			log.Error("Failed to query QOS spec list from openstack", "err", err)
			return
		}
		opts.OpenstackQoS = fetchedQOSes
		log.Debug("Fetched the QOS Spec list from openstack", "qos_policies", opts.OpenstackQoS)
	} else {
		log.Debug("Provided QOS list is used instead of querying openstack")
	}

	// Get the list of all libvirt domains
	log.Debug("Attempting to fetch domain list from libvirt")
	domains, err := virt.GetAllVirtualMachines(conns.Libvirt, true)
	if err != nil {
		log.Error("Failed to fetch virtual machine list from libvirt", "err", err)
		return
	}
	log.Info("Fetched domains from libvirt", "count", len(domains))

	// Initialize the worker pool orchestration.
	var VMDiskIOEnforcementWG sync.WaitGroup
	jobs := make(chan virt.VirtualMachine, len(domains))

	// Spawn workers to process the job queue.
	for w := 1; w <= 5; w++ {
		VMDiskIOEnforcementWG.Add(1)
		go func() {
			defer VMDiskIOEnforcementWG.Done()
			for dom := range jobs {
				jobOpts := opts
				jobOpts.DomainID = dom.Name
				jobLogger := logger
				jobLogger.RunID = ""
				EnforceIoTuneForDomain(conns, jobOpts, jobLogger)
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

	log.Info(
		"Workflow Completed", "workflow_name", "All VM Disk IOTune Enforcement",
		"description", "Applies the QOS Spec from cinder volume metadata for all the available domains in the host",
	)
}
