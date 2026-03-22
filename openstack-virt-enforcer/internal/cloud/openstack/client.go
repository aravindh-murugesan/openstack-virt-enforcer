package openstack

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/aravindh-murugesan/openstack-virt-enforcer/openstack-virt-enforcer/internal/cloud"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/utils/v2/openstack/clientconfig"
)

// Client manages the connection and service clients for OpenStack interactions.
//
// It serves as a high-level wrapper around standard gophercloud clients,
// integrating [cloud.RetryConfig] for transient error handling and
// standardizing profile management via clouds.yaml.
type Client struct {
	// ProfileName corresponds to a named entry in the clouds.yaml configuration file.
	ProfileName string
	// RetryConfig defines the behavior for transient error handling during
	// authentication and service calls.
	RetryConfig cloud.RetryConfig

	// Internal service clients for specific OpenStack APIs.
	ComputeClient      *gophercloud.ServiceClient
	BlockStorageClient *gophercloud.ServiceClient
	IdentityClient     *gophercloud.ServiceClient

	// Region is the target OpenStack region (e.g., "RegionOne").
	Region string
	// Interface defines the endpoint visibility (e.g., "public", "internal", "admin").
	Interface string
}

// executeWithRetry is a helper to run any operation using the client's retry configuration.
func (c *Client) executeWithRetry(ctx context.Context, opName string, operation func(ctx context.Context) error) error {
	return ExecuteAction(ctx, c.RetryConfig, opName, operation)
}

// GetCloudProviderName returns the identifier for this provider.
func (c *Client) GetCloudProviderName() string {
	return "openstack"
}

// NewClient initializes the OpenStack provider and its associated service
// clients (Cinder, Nova, Keystone).
//
// The initialization process:
//  1. Parses the local cloud configuration YAML.
//  2. Authenticates against the Identity (Keystone) service using retry logic.
//  3. Configures an HTTP client that respects the SSL/TLS "Verify" setting.
//  4. Resolves endpoints and initializes versioned service clients.
//
// It returns a wrapped error if authentication fails or if any required
// service client cannot be initialized.
func (c *Client) NewClient() error {
	slog.Debug("Initializing OpenStack client", "profile", c.ProfileName)

	var provider *gophercloud.ProviderClient
	opts := &clientconfig.ClientOpts{
		Cloud: c.ProfileName,
	}

	// Parse the cloud config yaml file.
	cloudConfig, readErr := clientconfig.GetCloudFromYAML(opts)
	if readErr != nil {
		return fmt.Errorf("failed to parse cloud config: %w", readErr)
	}

	// authenticateOperation encapsulates the authentication logic to allow
	// the retry helper to re-run it in case of transient network issues.
	authenticateOperation := func(ctx context.Context) error {

		p, err := openstack.NewClient(cloudConfig.AuthInfo.AuthURL)
		if err != nil {
			return err
		}

		// Handle insecure TLS verification if configured in the YAML.
		if *cloudConfig.Verify == false {
			tlsconfig := &tls.Config{}
			tlsconfig.InsecureSkipVerify = true
			transport := &http.Transport{TLSClientConfig: tlsconfig}
			p.HTTPClient = http.Client{
				Transport: transport,
			}
		}

		ao, err := clientconfig.AuthOptions(opts)
		if err != nil {
			return err
		}

		err = openstack.Authenticate(ctx, p, *ao)
		if err != nil {
			return err
		}

		provider = p
		return nil
	}

	// 1. Establish Connection & Authentication
	err := c.executeWithRetry(context.Background(), "OpenStack Authentication", authenticateOperation)
	if err != nil {
		return fmt.Errorf("authentication failed for profile '%s': %w", c.ProfileName, err)
	}

	// Resolve the appropriate endpoint availability (internal vs public vs admin).
	var availability gophercloud.Availability
	switch cloudConfig.EndpointType {
	case "internal":
		availability = gophercloud.AvailabilityInternal
	case "admin":
		availability = gophercloud.AvailabilityAdmin
	default:
		availability = gophercloud.AvailabilityPublic
	}

	endpointOpts := gophercloud.EndpointOpts{
		Availability: availability,
		Region:       cloudConfig.RegionName,
	}

	// Initialize specific versioned service clients.
	blockStorage, err := openstack.NewBlockStorageV3(provider, endpointOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize Block Storage v3 client: %w", err)
	}

	compute, err := openstack.NewComputeV2(provider, endpointOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize Compute v2 client: %w", err)
	}

	identity, err := openstack.NewIdentityV3(provider, endpointOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize Identity V3 client: %w", err)
	}

	// Finalize client assignment.
	c.BlockStorageClient = blockStorage
	c.ComputeClient = compute
	c.IdentityClient = identity
	c.Region = cloudConfig.RegionName
	c.Interface = cloudConfig.EndpointType

	return nil
}
