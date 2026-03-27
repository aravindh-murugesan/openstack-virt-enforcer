package cli

import (
	"fmt"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud/openstack"
	ebnats "github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/event/eb-nats"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/workflow"
	"github.com/spf13/cobra"
)

var (
	subWaitForEnforcement bool
	subTotalIOPS          int
	subWriteIOPS          int
	subReadIOPS           int
	subVolume             string
)

var diskIOPSsub = &cobra.Command{
	Use:     "subscribe",
	Short:   "Creates a new IOPS subscription on a disk",
	Aliases: []string{"sub"},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if subWaitForEnforcement == true && natsURL == "" {
			return fmt.Errorf("--nats-url cannot be empty when --wait flag is enabled")
		}

		if subTotalIOPS > 0 && (subReadIOPS != 0 || subWriteIOPS != 0) {
			return fmt.Errorf(" --read-iops and --write-iops cannot be used when --total-iops is provided")
		}

		if subTotalIOPS == 0 && (subReadIOPS == 0 || subWriteIOPS == 0) {
			return fmt.Errorf(" Both --read-iops and --write-iops have to be provided when --total-iops is not set")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {

		nats := ebnats.NATSInstance{
			URL:      natsURL,
			Username: natsUser,
			Password: natsPassword,
		}
		if err := nats.Connect(); err != nil {
			return err
		}

		ostk, err := openstack.ConnectToOpenstack(cloudName)
		if err != nil {
			return err
		}
		conns := workflow.Connections{
			Nats:      &nats,
			Openstack: &ostk,
		}

		workflow.SubscribeVolumeIoTune(
			conns,
			workflow.Logger{
				Level: logLevel,
			},
			subVolume,
			subTotalIOPS,
			subWriteIOPS,
			subReadIOPS,
		)

		return nil
	},
}

func init() {
	iops.AddCommand(diskIOPSsub)
	diskIOPSsub.Flags().BoolVar(&subWaitForEnforcement, "wait", false, "Waits for the volume subscription to be enforced. This requires access to NATS.")
	diskIOPSsub.Flags().IntVar(&subTotalIOPS, "total-iops", 0, "Total IOPS limit to be applied.")
	diskIOPSsub.Flags().IntVar(&subWriteIOPS, "write-iops", 0, "Write IOPS limit to be applied. (Cannot be used with --total-iops)")
	diskIOPSsub.Flags().IntVar(&subReadIOPS, "read-iops", 0, "Read IOPS limit to be applied. (Cannot be used with --total-iops)")
	diskIOPSsub.Flags().StringVar(&subVolume, "volume-id", "", "Target Volume ID that requires IOPS subscription")

	rootCommand.PersistentFlags().StringVar(&subVolume, "volume-id", "", "Target Volume ID that requires IOPS subscription")

}
