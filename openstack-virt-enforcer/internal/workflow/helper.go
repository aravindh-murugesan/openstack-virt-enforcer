package workflow

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud"
	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud/openstack"
	"github.com/digitalocean/go-libvirt"
	"github.com/lmittmann/tint"
)

// SetupLogger configures a structured logger template for uniformity across workflows.
//
// It uses the [tint] handler to provide colorized output for better terminal
// readability. The logger is automatically contextualized with the
// "cloud_profile" attribute to assist in multi-cloud log filtering.
func SetupLogger(level string, cloudName string) *slog.Logger {
	var logLevel slog.Level
	switch level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := tint.NewHandler(os.Stderr, &tint.Options{
		Level: logLevel,
	})

	return slog.New(handler).With("cloud_profile", cloudName)
}

// ConnectToLibvirt establishes a connection to a Libvirt daemon via a URI.
//
// If the provided connUrl is empty, it defaults to the local QEMU system
// socket (qemu:///system). It returns an error if the URI is malformed or
// if the connection cannot be established.
func ConnectToLibvirt(connUrl string) (*libvirt.Libvirt, error) {

	if connUrl == "" {
		connUrl = string(libvirt.QEMUSystem)
	}

	validatedURL, err := url.Parse(connUrl)
	if err != nil {
		return nil, err
	}

	libirtConnection, err := libvirt.ConnectToURI(validatedURL)
	if err != nil {
		return nil, err
	}

	return libirtConnection, nil
}

// ConnectToOpenstack initializes an OpenStack client based on a local profile name.
//
// It configures the client with an exponential backoff retry strategy:
//   - MaxRetries: 5
//   - BaseDelay: 2 seconds
//   - MaxDelay: 10 seconds
//   - OperationTimeout: 30 seconds
//
// It returns an initialized [openstack.Client] or an error if the profile
// cannot be authenticated or the service endpoints cannot be resolved.
func ConnectToOpenstack(cloudName string) (openstack.Client, error) {

	if cloudName == "" {
		return openstack.Client{}, fmt.Errorf("Cloud profile cannot be empty")
	}

	openstackClient := openstack.Client{
		ProfileName: cloudName,
		RetryConfig: cloud.RetryConfig{
			MaxRetries:       5,
			BaseDelay:        2 * time.Second,
			MaxDelay:         10 * time.Second,
			OperationTimeout: 30 * time.Second,
		},
	}

	if err := openstackClient.NewClient(); err != nil {
		return openstack.Client{}, err
	}

	return openstackClient, nil
}
