package notification

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Notify sends a JSON-marshaled notification to the configured [Webhook] URL.
//
// It performs a POST request with a default 30-second timeout. If credentials
// are provided in the Webhook struct, it automatically applies Basic Authentication
// headers. It returns an error if the marshaling fails, the network request
// drops, or the server returns a non-2xx status code.
func (w *Webhook) Notify(notification any) error {

	payload, err := json.Marshal(notification)
	if err != nil {
		return err
	}

	// Configure transport to respect the Verify toggle.
	// This allows the use of self-signed certificates when Verify is false.
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: !w.Verify,
		},
	}

	// Use a defined timeout to prevent hanging the calling process.
	client := http.Client{
		Timeout:   30 * time.Second,
		Transport: tr,
	}

	req, err := http.NewRequest("POST", w.URL, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	// Apply Basic Auth only if credentials are non-empty strings.
	if w.Username != "" || w.Password != "" {
		req.SetBasicAuth(w.Username, w.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to send notification via Webhook: %w", err)
	}

	defer resp.Body.Close()

	// Treat any status outside the 2xx range as a failure.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Failed to send notification via Webhook: %d", resp.StatusCode)
	}

	return nil
}
