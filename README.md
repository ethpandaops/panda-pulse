# panda-pulse 🐼

A monitoring tool for Ethereum networks that checks node health and reports issues to Discord.

The checks are run against a Grafana instance, which is configured with a Prometheus datasource. The checks themselves are rather specific to the custom Prometheus metrics ethPandaOps has setup, so your mileage may vary as a public user.

## Usage

### Pulse Check All Clients

```bash
docker run -e GRAFANA_SERVICE_TOKEN=your_token \
          -e DISCORD_BOT_TOKEN=your_token \
          -e OPENROUTER_API_KEY=optional_key \
          ethpandaops/panda-pulse:0.0.2 \
          --discord-channel CHANNEL_ID \
          --network NETWORK_NAME
```

### Pulse Check Specific Client

You can also pass in a target client to scope the checks + notification. 

This can be done with `--ethereum-cl` or `--ethereum-el`:

```bash
docker run -e GRAFANA_SERVICE_TOKEN=your_token \
          -e DISCORD_BOT_TOKEN=your_token \
          -e OPENROUTER_API_KEY=optional_key \
          ethpandaops/panda-pulse:latest \
          --discord-channel CHANNEL_ID \
          --network NETWORK_NAME \
          --ethereum-cl CLIENT_NAME
```

### Configuration

#### Environment Variables

- `GRAFANA_SERVICE_TOKEN` (required): Grafana service account token
- `DISCORD_BOT_TOKEN` (required): Discord bot token for notifications
- `OPENROUTER_API_KEY` (optional): OpenRouter API key for AI analysis

#### Command Line Arguments

- `--network` (required): Network to monitor (e.g., `pectra-devnet-5`)
- `--discord-channel` (required): Discord channel ID for notifications
- `--ethereum-cl`: Filter for specific consensus client
- `--ethereum-el`: Filter for specific execution client
- `--grafana-base-url`: Grafana base URL
- `--prometheus-datasource-id`: Prometheus datasource ID
