package http

import (
	"fmt"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"
)

// ClientWrapper wraps an HTTP client with metrics instrumentation.
type ClientWrapper struct {
	client  *http.Client
	metrics *Metrics
	log     *logrus.Logger
}

// NewClientWrapper creates a new HTTP client wrapper with metrics.
func NewClientWrapper(client *http.Client, metrics *Metrics, log *logrus.Logger) *ClientWrapper {
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	return &ClientWrapper{
		client:  client,
		metrics: metrics,
		log:     log,
	}
}

// Do executes an HTTP request with metrics tracking.
func (c *ClientWrapper) Do(req *http.Request, service, operation string) (*http.Response, error) {
	startTime := time.Now()

	// Record the API request.
	c.metrics.RecordAPIRequest(service, operation)

	// Execute the request.
	resp, err := c.client.Do(req)

	// Record request duration.
	duration := time.Since(startTime).Seconds()
	c.metrics.ObserveAPIRequestDuration(service, operation, duration)

	// Handle errors.
	if err != nil {
		c.log.WithFields(logrus.Fields{
			"service":   service,
			"operation": operation,
			"error":     err,
			"url":       req.URL.String(),
			"method":    req.Method,
			"duration":  duration,
		}).Error("API request error")

		c.metrics.RecordAPIError(service, operation, "network_error")

		return nil, err
	}

	// Check for HTTP errors.
	if resp.StatusCode >= 400 {
		errType := fmt.Sprintf("http_%d", resp.StatusCode)

		// Reduce logging noise. We attempt to hit hive to see if it's available for the given
		// network, it'll 404 if it's not and we don't need to log each time for that.
		if resp.StatusCode == 404 && service == "hive" {
			return nil, nil //nolint:nilnil // This is a special case.
		}

		c.log.WithFields(logrus.Fields{
			"service":     service,
			"operation":   operation,
			"status_code": resp.StatusCode,
			"url":         req.URL.String(),
			"method":      req.Method,
			"duration":    duration,
		}).Error("API response error")

		c.metrics.RecordAPIError(service, operation, errType)
	}

	return resp, nil
}

// Get performs a GET request with metrics.
func (c *ClientWrapper) Get(url, service, operation string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	return c.Do(req, service, operation)
}

// Client returns the underlying HTTP client.
func (c *ClientWrapper) Client() *http.Client {
	return c.client
}

// MetricsRoundTripper is an http.RoundTripper that collects metrics.
type MetricsRoundTripper struct {
	next    http.RoundTripper
	metrics *Metrics
	log     *logrus.Logger
	service string
}

// RoundTripperOption is a function that configures a MetricsRoundTripper.
type RoundTripperOption func(*MetricsRoundTripper)

// WithService sets the service name for the MetricsRoundTripper.
func WithService(service string) RoundTripperOption {
	return func(t *MetricsRoundTripper) {
		t.service = service
	}
}

// NewMetricsRoundTripper creates a new metrics-collecting round tripper.
func NewMetricsRoundTripper(next http.RoundTripper, metrics *Metrics, log *logrus.Logger, opts ...RoundTripperOption) *MetricsRoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}

	t := &MetricsRoundTripper{
		next:    next,
		metrics: metrics,
		log:     log,
		service: "api", // Default service name
	}

	// Apply options
	for _, opt := range opts {
		opt(t)
	}

	return t
}

// RoundTrip implements the http.RoundTripper interface.
func (t *MetricsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	startTime := time.Now()
	operation := req.URL.Path

	// Record the API request.
	t.metrics.RecordAPIRequest(t.service, operation)

	// Execute the request.
	resp, err := t.next.RoundTrip(req)

	// Record request duration.
	duration := time.Since(startTime).Seconds()
	t.metrics.ObserveAPIRequestDuration(t.service, operation, duration)

	// Handle errors.
	if err != nil {
		t.log.WithFields(logrus.Fields{
			"service":   t.service,
			"operation": operation,
			"error":     err,
			"url":       req.URL.String(),
			"method":    req.Method,
			"duration":  duration,
		}).Error("API request error")

		t.metrics.RecordAPIError(t.service, operation, "network_error")

		return nil, err
	}

	// Check for HTTP errors.
	if resp.StatusCode >= 400 {
		errType := fmt.Sprintf("http_%d", resp.StatusCode)

		// Reduce logging noise. We attempt to hit hive to see if it's available for the given
		// network, it'll 404 if it's not and we don't need to log each time for that.
		if resp.StatusCode == 404 && t.service == "hive" {
			return nil, nil //nolint:nilnil // This is a special case.
		}

		t.log.WithFields(logrus.Fields{
			"service":     t.service,
			"operation":   operation,
			"status_code": resp.StatusCode,
			"url":         req.URL.String(),
			"method":      req.Method,
			"duration":    duration,
		}).Error("API response error")

		t.metrics.RecordAPIError(t.service, operation, errType)
	}

	return resp, nil
}
