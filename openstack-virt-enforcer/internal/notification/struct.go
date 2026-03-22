package notification

// Webhook defines the configuration required to send alerts to an external HTTP endpoint.
type Webhook struct {
	URL      string
	Username string
	Password string
	Verify   bool
}
