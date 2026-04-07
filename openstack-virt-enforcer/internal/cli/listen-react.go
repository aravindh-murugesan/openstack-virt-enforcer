package cli

import (
	"fmt"
	"log/slog"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud/openstack"
	ebnats "github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/event/eb-nats"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/virt"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/workflow"
	"github.com/google/uuid"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		// webhookProvider := notification.Webhook{
		// 	URL:      webhookURL,
		// 	Username: webhookUsername,
		// 	Password: webhookPassword,
		// }

		// nats := ebnats.NATSInstance{
		// 	URL:      natsURL,
		// 	Password: natsPassword,
		// 	Username: natsUser,
		// }

		// workflow.LibvirtListenReact(
		// 	libvirtURL,
		// 	cloudName,
		// 	baseQOSPolicy,
		// 	timeout,
		// 	logLevel,
		// 	webhookProvider,
		// 	nats,
		// )
		libvirtConnection, err := virt.ConnectToLibvirt(libvirtURL)
		if err != nil {
			return fmt.Errorf("Unable to connect to libvirt: %w", err)
		}

		openstackConnection, err := openstack.ConnectToOpenstack(cloudName)
		if err != nil {
			return fmt.Errorf("Unable to connect to openstack: %w", err)
		}

		nats := ebnats.NATSInstance{
			URL:      natsURL,
			Password: natsPassword,
			Username: natsUser,
		}
		if err := nats.Connect(); err != nil {
			slog.Error("Unable to connect to nats", "err", err)
		}

		conns := workflow.Connections{
			Libvirt:   libvirtConnection,
			Openstack: &openstackConnection,
			Nats:      &nats,
		}

		opts := workflow.DaemonOpts{
			LibvirtControllers: workflow.LibvirtDaemonOpts{
				IoTuneEnforcement: workflow.EnforceIoTuneOpts{
					BaseQoSPolicy:          baseQOSPolicy,
					Enforce:                true,
					AuditInterval:          daemonIoTuneAuditInterval,
					VolumeTypeMaxIopsLimit: volTypeIOPSLimit,
				},
			},
		}

		workflow.LibvirtListenAndReact(
			conns,
			opts,
			workflow.Logger{
				Level:       logLevel,
				GlobalRunID: fmt.Sprintf("ve-%s", uuid.NewString()),
			},
		)
		return nil
	},
}

func init() {
	// Register the daemon command to the root CLI.
	rootCommand.AddCommand(daemonCommand)

	// Define mandatory fallback policy flag.
	daemonCommand.Flags().StringToStringVar(&volTypeIOPSLimit, "max-volume-type-iops-limit", map[string]string{}, "Maximum IOPS limit that can be set for a specific volume type.")
	daemonCommand.Flags().StringVar(&baseQOSPolicy, "base-qos-policy", "", "Base QOS Spec to fallback when there is no iops metadata in openstack volumes. This has to exist on openstack (Required)")
	daemonCommand.Flags().IntVar(&daemonIoTuneAuditInterval, "iotune-audit-interval", 30, "Run disk audit against openstack per x mins")
	daemonCommand.MarkFlagRequired("base-qos-policy")
}
