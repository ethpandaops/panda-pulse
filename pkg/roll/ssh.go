package roll

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

// DefaultWatchtowerContainer is the watchtower container name deployed by the
// ethpandaops ethereum_node Ansible role.
const DefaultWatchtowerContainer = "ethereum-node-docker-watchtower"

// DefaultWatchtowerPort is watchtower's default HTTP API port.
const DefaultWatchtowerPort = 8080

const sshDialTimeout = 15 * time.Second

// safeRef guards values interpolated into the remote shell script. Single-quoted
// in the script, this charset (no quotes/spaces/metachars) prevents injection.
var safeRef = regexp.MustCompile(`^[A-Za-z0-9._/:@-]*$`)

// SSHActuator rolls an image by SSHing to the host and triggering the
// node-local watchtower HTTP API. It discovers watchtower's container IP and
// API token from `docker inspect` on the node, so it needs no published port
// and no token stored centrally — only SSH access (which devops already has).
type SSHActuator struct {
	signer        ssh.Signer
	hostKeyCB     ssh.HostKeyCallback
	containerName string
	port          int
	log           logrus.FieldLogger
}

// SSHConfig configures an SSHActuator.
type SSHConfig struct {
	// PrivateKeyPath is the path to the SSH private key used to authenticate.
	PrivateKeyPath string
	// KnownHostsCallback verifies host keys. If nil, host keys are not verified
	// (acceptable for ephemeral devnets; a warning is logged).
	KnownHostsCallback ssh.HostKeyCallback
	// ContainerName overrides the watchtower container name.
	ContainerName string
	// Port overrides the watchtower HTTP API port.
	Port int
	// Log is the logger.
	Log logrus.FieldLogger
}

// NewSSHActuator builds an SSHActuator from the given config.
func NewSSHActuator(cfg SSHConfig) (*SSHActuator, error) {
	if cfg.Log == nil {
		cfg.Log = logrus.New()
	}

	if cfg.PrivateKeyPath == "" {
		return nil, fmt.Errorf("ssh private key path is required")
	}

	keyBytes, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key: %w", err)
	}

	hostKeyCB := cfg.KnownHostsCallback
	if hostKeyCB == nil {
		cfg.Log.Warn("roll: SSH host key verification disabled (no known_hosts configured)")

		hostKeyCB = ssh.InsecureIgnoreHostKey() //nolint:gosec // devnet hosts; opt-in known_hosts available
	}

	container := cfg.ContainerName
	if container == "" {
		container = DefaultWatchtowerContainer
	}

	port := cfg.Port
	if port == 0 {
		port = DefaultWatchtowerPort
	}

	return &SSHActuator{
		signer:        signer,
		hostKeyCB:     hostKeyCB,
		containerName: container,
		port:          port,
		log:           cfg.Log,
	}, nil
}

// Name implements Actuator.
func (a *SSHActuator) Name() string { return "ssh" }

// Roll implements Actuator: SSH to the host and trigger the local watchtower.
func (a *SSHActuator) Roll(ctx context.Context, target Target, image string) error {
	if !safeRef.MatchString(image) {
		return fmt.Errorf("invalid image reference %q", image)
	}

	if !safeRef.MatchString(a.containerName) {
		return fmt.Errorf("invalid container name %q", a.containerName)
	}

	user, host := splitSSH(target.SSH)
	if host == "" {
		return fmt.Errorf("invalid ssh target %q", target.SSH)
	}

	out, err := a.run(ctx, user, host, a.script(image))
	if err != nil {
		return fmt.Errorf("ssh roll %s: %w: %s", target.Name, err, strings.TrimSpace(out))
	}

	return nil
}

// script builds the remote shell that discovers watchtower's IP + token and
// triggers the update. image is single-quoted and charset-validated by Roll.
func (a *SSHActuator) script(image string) string {
	return fmt.Sprintf(`set -e
cn='%s'
img='%s'
ip=$(docker inspect "$cn" -f '{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}' | awk '{print $1}')
[ -n "$ip" ] || { echo "watchtower container $cn not found" >&2; exit 3; }
tok=$(docker inspect "$cn" -f '{{range .Config.Env}}{{println .}}{{end}}' | sed -n 's/^WATCHTOWER_HTTP_API_TOKEN=//p')
[ -n "$tok" ] || { echo "WATCHTOWER_HTTP_API_TOKEN not set on $cn" >&2; exit 4; }
url="http://$ip:%d/v1/update"
[ -n "$img" ] && url="$url?image=$img"
curl -fsS -X POST -H "Authorization: Bearer $tok" "$url"
`, a.containerName, image, a.port)
}

func (a *SSHActuator) run(ctx context.Context, user, host, script string) (string, error) {
	clientCfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(a.signer)},
		HostKeyCallback: a.hostKeyCB,
		Timeout:         sshDialTimeout,
	}

	dialer := &net.Dialer{Timeout: sshDialTimeout}

	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return "", fmt.Errorf("dial: %w", err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, host, clientCfg)
	if err != nil {
		_ = conn.Close()

		return "", fmt.Errorf("ssh handshake: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	done := make(chan error, 1)
	go func() { done <- session.Run(script) }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGKILL)

		return buf.String(), ctx.Err()
	case err := <-done:
		return buf.String(), err
	}
}

// splitSSH parses "user@host[:port]" into user and host:port (defaulting :22).
func splitSSH(target string) (user, hostport string) {
	user = "root"
	host := target

	if at := strings.Index(target, "@"); at >= 0 {
		user = target[:at]
		host = target[at+1:]
	}

	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, strconv.Itoa(22))
	}

	return user, host
}
