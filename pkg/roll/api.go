package roll

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// APIActuator rolls by calling a node's watchtower HTTP API directly. This
// requires the watchtower API to be reachable from where this runs (i.e.
// publicly/VPN-exposed) and a shared token. Prefer SSHActuator unless you
// specifically want to avoid SSH access.
type APIActuator struct {
	token      string
	scheme     string
	port       int
	hostPrefix string
	httpClient *http.Client
}

// NewAPIActuator returns an APIActuator targeting each node's watchtower vhost
// (hostPrefix + the node host, e.g. "watchtower-<host>"). scheme defaults to
// "https"; hostPrefix defaults to "watchtower-"; port 0 omits the port (so 443
// for https). The watchtower vhost is bearer-auth only — no basic auth.
func NewAPIActuator(token, scheme string, port int, hostPrefix string) *APIActuator {
	if scheme == "" {
		scheme = "https"
	}

	if hostPrefix == "" {
		hostPrefix = "watchtower-"
	}

	return &APIActuator{
		token:      token,
		scheme:     scheme,
		port:       port,
		hostPrefix: hostPrefix,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// Name implements Actuator.
func (a *APIActuator) Name() string { return "api" }

// Roll implements Actuator: POST /v1/update to the target's watchtower API.
func (a *APIActuator) Roll(ctx context.Context, target Target, image string) error {
	host := sshHost(target.SSH)
	if host == "" {
		return fmt.Errorf("invalid target %q", target.SSH)
	}

	host = a.hostPrefix + host

	endpoint := fmt.Sprintf("%s://%s/v1/update", a.scheme, host)
	if a.port != 0 {
		endpoint = fmt.Sprintf("%s://%s:%d/v1/update", a.scheme, host, a.port)
	}

	if image != "" {
		endpoint += "?image=" + url.QueryEscape(image)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, http.NoBody)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+a.token)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("trigger update: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("watchtower returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

// sshHost extracts the host (FQDN) from an "user@host[:port]" SSH value.
func sshHost(target string) string {
	host := target
	if at := strings.Index(host, "@"); at >= 0 {
		host = host[at+1:]
	}

	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}

	return host
}
