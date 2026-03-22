package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	libvirtURL      string
	cloudName       string
	logLevel        string
	timeout         int
	webhookURL      string
	webhookUsername string
	webhookPassword string
	baseQOSPolicy   string
)

// rootCommand defines the base entry point for the OpenStack Virt Enforcer CLI.
//
// It handles global configuration flags and ensures that required parameters
// like the OpenStack cloud profile are validated before any sub-command executes.
var rootCommand = &cobra.Command{
	Use:     "openstack-virt-enforcer",
	Aliases: []string{"virt-enforcer"},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Allow 'version' (and 'help') to run without the flag
		if cmd.Name() == "version" || cmd.Name() == "help" {
			return nil
		}

		if cloudName == "" {
			return fmt.Errorf("required flag(s) \"--cloud\" not set")
		}

		return nil
	},
	Long: `OpenStack Virt Enforcer is a high-performance sidecar that synchronizes openStack metadata with native Libvirt domain configurations.
While OpenStack provides a broad abstraction layer, it's also very rigid in some areas. Virt Enforcer aims to bridge the functionality gaps for features not exposed via openstack api.

Current Capabilities:
  - Storage: Cinder Volume I/O Throttling (total_iops_sec, write_iops_sec, read_iops_sec) on the volume metadata rather than on volume types for flexibility
  `,
}

func Execute() error {
	return rootCommand.Execute()
}

func init() {
	// Grouping commands improves the readability of the generated help text.
	rootCommand.AddGroup(&cobra.Group{ID: "ve", Title: "Virt Enforcer - Global"})
	rootCommand.AddGroup(&cobra.Group{ID: "disk", Title: "Virt Enforcer - Disk"})

	// Define global flags available to all sub-commands.
	rootCommand.PersistentFlags().StringVar(&libvirtURL, "libvirt-url", "", "Socket or TLS address to connect to libvirt")
	rootCommand.PersistentFlags().StringVar(&cloudName, "cloud", "", "Name of the cloud profile as in clouds.yaml (required)")
	rootCommand.PersistentFlags().IntVar(&timeout, "timeout", 0, "Global execution timeout in seconds (0 = run indefinitely)")
	rootCommand.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Logging level (debug, info, warn, error)")

	// Webhook configuration for external alerting.
	rootCommand.PersistentFlags().StringVar(&webhookURL, "webhook-url", "", "Webhook URL for alerting")
	rootCommand.PersistentFlags().StringVar(&webhookUsername, "webhook-username", "", "Webhook username for alerting")
	rootCommand.PersistentFlags().StringVar(&webhookPassword, "webhook-password", "", "Webhook password for alerting")

}
