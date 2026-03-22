package cli

import (
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/notification"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/workflow"
	"github.com/spf13/cobra"
)

// daemonCommand initializes the "Listen and React" workflow.
//
// This command runs as a foreground daemon that subscribes to Libvirt
// lifecycle and device events. It automatically reconciles I/O settings
// in real-time as VMs start or new disks are attached.
var daemonCommand = &cobra.Command{
	Use:     "listen-n-react",
	GroupID: "ve",
	Short:   "A Daemon process that hooks to libvirt's broadcast events and reacts to it in real-time.",
	Run: func(cmd *cobra.Command, args []string) {
		webhookProvider := notification.Webhook{
			URL:      webhookURL,
			Username: webhookUsername,
			Password: webhookPassword,
		}

		workflow.LibvirtListenReact(
			libvirtURL,
			cloudName,
			baseQOSPolicy,
			timeout,
			logLevel,
			webhookProvider,
		)
	},
}

func init() {
	// Register the daemon command to the root CLI.
	rootCommand.AddCommand(daemonCommand)

	// Define mandatory fallback policy flag.
	daemonCommand.Flags().StringVar(&baseQOSPolicy, "base-qos-policy", "", "Base QOS Spec to fallback when there is no iops metadata in openstack volumes. This has to exist on openstack (Required)")
	daemonCommand.MarkFlagRequired("base-qos-policy")
}
