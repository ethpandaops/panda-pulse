# panda-pulse 🐼

A monitoring tool for Ethereum networks that checks node health and reports issues to Discord.

The checks are run against a Grafana instance, which is configured with a Prometheus datasource. The checks themselves are rather specific to the custom Prometheus metrics ethPandaOps has setup, so your mileage may vary as a public user.

## Quick Start

```bash
docker run -it \
  -e GRAFANA_SERVICE_TOKEN=your_token \
  -e DISCORD_BOT_TOKEN=your_token \
  -e AWS_ACCESS_KEY_ID=your_key \
  -e AWS_SECRET_ACCESS_KEY=your_secret \
  -e S3_BUCKET=your_bucket \
  ethpandaops/panda-pulse:latest
```

## Bot Commands

The Discord bot responds to the following slash commands:

- `/checks list {optional:network}` - List out all checks registered
- `/checks register {network} {channel} {optional:client}` - Register checks for a network in a given channel
- `/checks deregister {network} {optional:client}` - Deregister checks for a network
- `/checks debug {id}` - Shows debug information for a check
- `/checks run {network} {client}` - Run a manual check for a network and client

### Local Development with Localstack

A docker-compose setup is provided for local development using Localstack:

```bash
docker-compose up s3  # Starts localstack
```

## Configuration

### Required Environment Variables

- `GRAFANA_SERVICE_TOKEN`: Grafana service account token
- `DISCORD_BOT_TOKEN`: Discord bot token
- `AWS_ACCESS_KEY_ID`: S3 access key
- `AWS_SECRET_ACCESS_KEY`: S3 secret key
- `S3_BUCKET`: S3 bucket name

### Optional Environment Variables

- `GRAFANA_BASE_URL`: Grafana instance URL
- `PROMETHEUS_DATASOURCE_ID`: Grafana Prometheus datasource ID
- `S3_BUCKET_PREFIX`: Prefix for S3 objects
- `AWS_REGION`: S3 region (default: `us-east-1`)
- `AWS_ENDPOINT_URL`: Custom S3 endpoint (eg: for `localstack` or non-AWS buckets)
- `METRICS_ADDRESS`: Prometheus metrics endpoint (default: `:9091`)
- `HEALTH_CHECK_ADDRESS`: Health check endpoint (default: `:9191`)