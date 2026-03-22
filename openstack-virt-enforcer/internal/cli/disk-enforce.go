package cli

import (
	"fmt"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/notification"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/workflow"
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
	Short: "Enforce IOPS limits for a specific domain",
	RunE: func(cmd *cobra.Command, args []string) error {
		webhookProvider := notification.Webhook{
			URL:      webhookURL,
			Username: webhookUsername,
			Password: webhookPassword,
		}

		libvirtConnection, err := workflow.ConnectToLibvirt(libvirtURL)
		if err != nil {
			return fmt.Errorf("Unable to connect to libvirt: %w", err)
		}

		workflow.AllVMDiskIOEnforcement(
			libvirtConnection,
			cloudName,
			baseQOSPolicy,
			timeout,
			logLevel,
			webhookProvider,
		)
		return nil
	},
}

func init() {
	// Register commands into the Cobra hierarchy.
	rootCommand.AddCommand(iops)
	iops.AddCommand(diskEnforceDomains)

	// Define specific flags for the bulk enforcement command.
	diskEnforceDomains.Flags().StringVar(&baseQOSPolicy, "base-qos-policy", "", "Base QOS Spec to fallback when there is no iops metadata in openstack volumes. This has to exist on openstack (Required)")
	diskEnforceDomains.MarkFlagRequired("base-qos-policy")
}
