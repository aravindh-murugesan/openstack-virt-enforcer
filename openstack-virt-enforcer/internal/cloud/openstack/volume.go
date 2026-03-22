package openstack

import (
	"context"
	"fmt"

	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/qos"
	"github.com/gophercloud/gophercloud/v2/openstack/blockstorage/v3/volumes"
)

// GetVolume retrieves a specific Cinder volume by its UUID.
//
// This operation is wrapped in the client's retry logic. If the request fails,
// the returned error will include the 'X-Openstack-Request-Id' to assist
// with server-side log correlation.
func (c *Client) GetVolume(ctx context.Context, uuid string) (volumes.Volume, error) {

	vol := volumes.Volume{}
	reqID := ""

	getOP := func(innerCtx context.Context) error {
		resp := volumes.Get(innerCtx, c.BlockStorageClient, uuid)
		// Capture Request ID for tracing even on failure.
		reqID = resp.Header.Get("X-Openstack-Request-Id")
		v, err := resp.Extract()

		if err != nil {
			return fmt.Errorf("RequestID: %s | %w", reqID, err)
		}
		vol = *v
		return nil
	}

	if err := ExecuteAction(ctx, c.RetryConfig, "GetVolume", getOP); err != nil {
		return vol, err
	}

	return vol, nil
}

// ListQosSpec retrieves all available Quality of Service (QoS) specifications
// from the Block Storage (Cinder) service.
//
// It performs a paginated list request and aggregates all results into a single
// slice. This operation is resilient to transient API timeouts via [ExecuteAction].
func (c *Client) ListQosSpec(ctx context.Context) ([]qos.QoS, error) {

	qoses := make([]qos.QoS, 0)
	reqID := ""

	listOP := func(innerCtx context.Context) error {
		resp, err := qos.List(c.BlockStorageClient, qos.ListOpts{}).AllPages(innerCtx)
		if err != nil {
			return err
		}
		q, err := qos.ExtractQoS(resp)
		if err != nil {
			return fmt.Errorf("Request ID: %s | %w", reqID, err)
		}
		qoses = q
		return nil
	}

	if err := ExecuteAction(ctx, c.RetryConfig, "ListQOSSpecs", listOP); err != nil {
		return qoses, err
	}
	return qoses, nil
}
