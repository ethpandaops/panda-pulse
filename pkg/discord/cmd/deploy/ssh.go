package deploy

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SSHConfig holds SSH connection configuration.
type SSHConfig struct {
	User    string
	Host    string
	Timeout time.Duration
}

// SSHResult holds the result of an SSH command execution.
type SSHResult struct {
	NodeName string
	Success  bool
	Output   string
	Error    error
}

// executeSSHCommand executes a command over SSH.
func executeSSHCommand(config SSHConfig, command string) (string, error) {
	// Create SSH command with timeout
	sshCmd := exec.Command("ssh",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=no", // Consider security implications in production
		fmt.Sprintf("%s@%s", config.User, config.Host),
		command)

	var stdout, stderr bytes.Buffer
	sshCmd.Stdout = &stdout
	sshCmd.Stderr = &stderr

	// Execute the command
	err := sshCmd.Run()
	if err != nil {
		return "", fmt.Errorf("ssh execution failed: %w\nStderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// deployToNode attempts to deploy a docker tag to a node via SSH.
func (c *DeployCommand) deployToNode(nodeName, network, dockerTag string) SSHResult {
	sshHost := fmt.Sprintf("%s.%s.ethpandaops.io", nodeName, network)
	sshUser := "devops"

	config := SSHConfig{
		User:    sshUser,
		Host:    sshHost,
		Timeout: 30 * time.Second,
	}

	if nodeName == "bootnode-1" {
		return SSHResult{
			NodeName: nodeName,
			Success:  true,
			Output:   "bootnode ignored",
			Error:    nil,
		}
	}

	// The actual deployment command that would be executed on the remote node
	//deployCmd := fmt.Sprintf("deploy-docker-image %s", dockerTag)

	runlikeCmd := fmt.Sprintf("docker run --rm -v /var/run/docker.sock:/var/run/docker.sock docker.ethquokkaops.io/dh/assaflavie/runlike %s", "execution")

	output, err := executeSSHCommand(config, runlikeCmd)

	// The output will look something like this:
	/*
		docker run -d --name=execution --hostname=8350029fa2b7 --user=1003 --volume /data/geth:/data --volume /data/execution-auth.secret:/execution-auth.jwt:ro --env=VIRTUAL_HOST=rpc.prysm-geth-1.eof-devnet-0.ethpandaops.io --env=VIRTUAL_PORT=8545 --env=LETSENCRYPT_HOST=rpc.prysm-geth-1.eof-devnet-0.ethpandaops.io --network=shared --workdir=/ -p 30303:30303 -p 30303:30303/udp -p 127.0.0.1:8545:8545 --expose=8546 -p 127.0.0.1:8551:8551 --restart=always --log-opt max-file=8 --log-opt max-size=500m --runtime=runc docker.ethquokkaops.io/dh/ethpandaops/geth:osaka-mega-eof-82db28a --datadir=/data --port=30303 --http --http.addr=0.0.0.0 --http.port=8545 --authrpc.addr=0.0.0.0 --authrpc.port=8551 '--authrpc.vhosts=*' --authrpc.jwtsecret=/execution-auth.jwt --nat=extip:165.227.193.76 --metrics --metrics.port=6060 --metrics.addr=0.0.0.0 --discovery.v5 --http.api=eth,net,web3,debug,admin '--http.vhosts=*' --networkid=7023642286 --syncmode=full '--bootnodes=enode://ffb8686c32f4c1e1e5031c811fde31a0fce62c2318f63ef6fb54d5ca4dbb40a65f8ccbd2545b9d375030eda316017b89c98185e588290125e2747cde98a81627@68.183.122.110:30303?discport=30303,enode://9073a769baaa8782471e69e524377d5c58b44ea28af02882a924e8d81b6a77ad3e76a634a7101849e028a3328d98c2758f792b1e93b9e9ebf8b744edf425acff@165.227.193.76:30303?discport=30303,enode://40fb586432cb0fa4e90405ac190438e4ab632ee7c8a8ece6833438eb2465b714e8b16cfa881eddaee355ee48f9504b5589fb4cb18bc57722ff3878e7976e3afd@142.93.14.227:30303?discport=30303' --ethstats=prysm-geth-1:SuperSecret@ethstats.eof-devnet-0.ethpandaops.io
	*/
	// We want to replace the 'osaka-mega-eof-82db28a' part of 'docker.ethquokkaops.io/dh/ethpandaops/geth:osaka-mega-eof-82db28a' with the new docker tag
	newImage := strings.Replace(output, "osaka-mega-eof-82db28a", dockerTag, 1)

	// We want to add a `-d` after the `docker run` part
	newImage = strings.Replace(newImage, "docker run", "docker run -d", 1)

	fmt.Println(output)
	fmt.Println(newImage)

	// Stop existing container.
	output, err = executeSSHCommand(config, "docker stop execution")
	fmt.Println(output)

	// Remove existing container.
	output, err = executeSSHCommand(config, "docker rm execution")
	fmt.Println(output)

	// Then we want to execute the newImage output
	output, err = executeSSHCommand(config, newImage)

	fmt.Println(output)

	return SSHResult{
		NodeName: nodeName,
		Success:  err == nil,
		Output:   output,
		Error:    err,
	}
}

// formatSSHResults formats SSH results into a readable format.
func formatSSHResults(results []SSHResult) string {
	var lines []string

	for _, result := range results {
		if result.Success {
			lines = append(lines, fmt.Sprintf("✅ **%s**: Successfully deployed", result.NodeName))
		} else {
			lines = append(lines, fmt.Sprintf("❌ **%s**: Failed - %v", result.NodeName, result.Error))
		}
	}

	return strings.Join(lines, "\n")
}
