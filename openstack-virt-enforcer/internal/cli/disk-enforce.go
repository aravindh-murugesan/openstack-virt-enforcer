package cli

import (
	"fmt"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud/openstack"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/workflow"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// iops represents the base command for all I/O related operations.
var iops = &cobra.Command{
	Use:     "iops",
	Aliases: []string{"volume"},
	GroupID: "disk",
}

// diskEnforceDomains triggers a one-shot reconciliation of I/O limits for all
// active virtual machines on the current host.
var diskEnforceDomains = &cobra.Command{
	Use:   "enforce-all-domains",
	Short: "Enforce IOPS limits for a all domain",
	RunE: func(cmd *cobra.Command, args []string) error {

		libvirtConnection, err := virt.ConnectToLibvirt(libvirtURL)
		if err != nil {
			return fmt.Errorf("Unable to connect to libvirt: %w", err)
		}

		openstackConnection, err := openstack.ConnectToOpenstack(cloudName)
		if err != nil {
			return fmt.Errorf("Unable to connect to openstack: %w", err)
		}

		conns := workflow.Connections{
			Libvirt:   libvirtConnection,
			Openstack: &openstackConnection,
		}

		workflow.EnforceIoTuneForAllDomain(
			conns,
			workflow.EnforceIoTuneOpts{
				Enforce:       true,
				BaseQoSPolicy: baseQOSPolicy,
			},
			workflow.Logger{
				Level:       logLevel,
				GlobalRunID: fmt.Sprintf("ve-%s", uuid.NewString()),
			},
		)

		return nil
	},
}

// diskEnforceDomains triggers a one-shot reconciliation of I/O limits for all
// active virtual machines on the current host.
var diskEnforceDomain = &cobra.Command{
	Use:   "enforce-domain",
	Short: "Enforce IOPS limits for a specific domain",
	RunE: func(cmd *cobra.Command, args []string) error {

		libvirtConnection, err := virt.ConnectToLibvirt(libvirtURL)
		if err != nil {
			return fmt.Errorf("Unable to connect to libvirt: %w", err)
		}

		openstackConnection, err := openstack.ConnectToOpenstack(cloudName)
		if err != nil {
			return fmt.Errorf("Unable to connect to openstack: %w", err)
		}

		conns := workflow.Connections{
			Libvirt:   libvirtConnection,
			Openstack: &openstackConnection,
		}

		workflow.EnforceIoTuneForDomain(
			conns,
			workflow.EnforceIoTuneOpts{
				DomainID:      iopsDomainName,
				Enforce:       true,
				BaseQoSPolicy: baseQOSPolicy,
			},
			workflow.Logger{
				Level: logLevel,
			},
		)

		return nil
	},
}

func init() {
	// Register commands into the Cobra hierarchy.
	rootCommand.AddCommand(iops)
	iops.AddCommand(diskEnforceDomains)
	iops.AddCommand(diskEnforceDomain)

	// Define specific flags for the bulk enforcement command.
	diskEnforceDomains.Flags().StringVar(&baseQOSPolicy, "base-qos-policy", "", "Base QOS Spec to fallback when there is no iops metadata in openstack volumes. This has to exist on openstack (Required)")
	diskEnforceDomains.MarkFlagRequired("base-qos-policy")

	// Define specific flags for the per-domain enforcement command.
	diskEnforceDomain.Flags().StringVar(&baseQOSPolicy, "base-qos-policy", "", "Base QOS Spec to fallback when there is no iops metadata in openstack volumes. This has to exist on openstack (Required)")
	diskEnforceDomain.MarkFlagRequired("base-qos-policy")
	diskEnforceDomain.Flags().StringVar(&iopsDomainName, "domain-name", "", "Name of the domain to enforce IOTune Values")
	diskEnforceDomain.MarkFlagRequired("domain-name")

}
