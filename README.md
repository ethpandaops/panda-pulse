# Panda-Pulse 🐼

A comprehensive monitoring and automation tool for Ethereum networks that provides health checks, Docker image builds, and test coverage reporting through Discord bot integration.

## Overview

Panda-Pulse is designed to monitor Ethereum network infrastructure and provide actionable insights through Discord. It integrates with multiple data sources including Grafana/Prometheus for metrics, Hive for test coverage, and GitHub for automated Docker builds.

### Key Features

- **Network Health Monitoring** - Automated checks for consensus and execution client health
- **Discord Bot Integration** - Slash commands for managing monitoring, builds, and reports  
- **Docker Image Builds** - Trigger builds for Ethereum clients and tools via GitHub workflows
- **Test Coverage Reports** - Visual snapshots and summaries from Hive test infrastructure
- **Alert Management** - Flexible notification system with role-based permissions
- **Multi-Network Support** - Monitor multiple Ethereum networks (mainnet, testnets, devnets)

## Quick Start

### Docker
```bash
docker run -it \
  -e GRAFANA_SERVICE_TOKEN=your_grafana_token \
  -e DISCORD_BOT_TOKEN=your_discord_token \
  -e GITHUB_TOKEN=your_github_token \
  -e AWS_ACCESS_KEY_ID=your_aws_key \
  -e AWS_SECRET_ACCESS_KEY=your_aws_secret \
  -e S3_BUCKET=your_s3_bucket \
  -e CLIENTS_DATA_URL=https://example.com/clients.json \
  ethpandaops/panda-pulse:latest
```

### Local Development
```bash
# Start localstack for S3 simulation
docker-compose up s3

# Set environment variables
export GRAFANA_SERVICE_TOKEN=your_token
export DISCORD_BOT_TOKEN=your_token
export GITHUB_TOKEN=your_token
# ... other variables

# Run the application
go run cmd/main.go
```

## Discord Bot Commands

The Discord bot provides comprehensive slash commands for monitoring and automation:

### `/checks` - Network Health Monitoring
- `list [network]` - List all registered health checks
- `register <network> <channel> [client]` - Register health checks for a network
- `deregister <network> [client]` - Remove health checks for a network  
- `debug <id>` - Show detailed information about a specific check
- `run <network> <client>` - Execute a manual health check

### `/build` - Docker Image Builds
- `client-cl <client>` - Build a consensus layer client Docker image
- `client-el <client>` - Build an execution layer client Docker image
- `tool <workflow>` - Build a tool or utility Docker image

All build commands support optional parameters:
- `repository` - Override source repository
- `ref` - Specify branch, tag, or commit SHA
- `docker_tag` - Custom Docker tag for the build
- `build_args` - Additional Docker build arguments

### `/hive` - Test Coverage Reports
- `list [network]` - List available Hive test summaries
- `register <network> <channel>` - Register for automated test reports
- `deregister <network>` - Stop automated test reports
- `run <network>` - Generate manual test coverage report
- `summary <network>` - Get test coverage summary with visual snapshots

### `/mentions` - Alert Management
- `add <network> <client> <user/role>` - Add user/role to alert notifications
- `remove <network> <client> <user/role>` - Remove from alert notifications
- `list [network] [client]` - Show current mention configurations
- `enable <network> <client>` - Enable mentions for a monitoring target
- `disable <network> <client>` - Disable mentions for a monitoring target

## Architecture

### Core Components

- **Service** - Main application orchestrator managing component lifecycle
- **Analyzer** - Health check processing and alert generation
- **Cartographoor** - Client metadata and network configuration management
- **Discord Bot** - Slash command interface and user interaction
- **Store** - Data persistence layer using AWS S3
- **Scheduler** - Background job management for periodic tasks

### Health Checks

The monitoring system includes several specialized health checks:

- **CL Finalized Epoch** - Consensus layer finalization monitoring
- **CL Head Slot** - Consensus layer chain head tracking  
- **CL Sync Status** - Consensus layer synchronization health
- **EL Block Height** - Execution layer chain height monitoring
- **EL Sync Status** - Execution layer synchronization health

### Dynamic Workflow Integration

The build system dynamically discovers available Docker workflows from GitHub:
- Automatically detects new client builds when added to [eth-client-docker-image-builder](https://github.com/ethpandaops/eth-client-docker-image-builder)
- Filters clients vs tools based on Cartographoor metadata
- Handles special client name mappings (e.g., nimbus → nimbus-eth2)
- Refreshes choices every 15 minutes to stay current

## Configuration

### Required Environment Variables

| Variable | Description |
|----------|-------------|
| `GRAFANA_SERVICE_TOKEN` | Grafana service account token for metrics access |
| `DISCORD_BOT_TOKEN` | Discord bot token for API access |
| `GITHUB_TOKEN` | GitHub token for workflow triggers and API access |
| `AWS_ACCESS_KEY_ID` | AWS access key for S3 storage |
| `AWS_SECRET_ACCESS_KEY` | AWS secret key for S3 storage |
| `S3_BUCKET` | S3 bucket name for data persistence |
| `CLIENTS_DATA_URL` | URL to client metadata JSON (Cartographoor data) |

### Optional Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFANA_BASE_URL` | - | Grafana instance base URL |
| `PROMETHEUS_DATASOURCE_ID` | - | Grafana Prometheus datasource ID |
| `S3_BUCKET_PREFIX` | - | Prefix for S3 object keys |
| `AWS_REGION` | `us-east-1` | AWS region for S3 |
| `AWS_ENDPOINT_URL` | - | Custom S3 endpoint (for localstack/non-AWS) |
| `METRICS_ADDRESS` | `:9091` | Prometheus metrics endpoint |
| `HEALTH_CHECK_ADDRESS` | `:9191` | Health check endpoint |

## Permissions & Security

The Discord bot uses role-based access control:

- **Admin Roles** - Full access to all commands and configurations
- **Team Roles** - Client-specific access based on team assignments
- **Build Access** - Any team member can trigger builds for their clients

Role configuration is managed through the `DISCORD_*` environment variables and supports flexible team-to-client mappings.

## Monitoring & Observability

- **Prometheus Metrics** - Exposed on `:9091` for monitoring bot performance
- **Health Checks** - Available on `:9191` for liveness/readiness probes
- **Structured Logging** - JSON logs with contextual information
- **Command Metrics** - Track Discord command usage and performance

## Development

### Local Setup with Localstack

```bash
# Start localstack S3 simulation
docker-compose up s3

# Configure environment for local development
export AWS_ENDPOINT_URL=http://localhost:4566
export AWS_REGION=us-east-1
export S3_BUCKET=panda-pulse-dev
# ... other required variables

# Run the application
go run cmd/main.go
```

### Testing

```bash
# Run all tests
go test ./...

# Run with coverage
go test ./... -cover

# Integration tests (requires testcontainers)
go test ./pkg/store/...
```

### Adding New Health Checks

1. Implement the check interface in `pkg/checks/`
2. Register the check type in `pkg/checks/checks.go`
3. Add check-specific configuration if needed
4. Update Discord command choices if applicable

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
