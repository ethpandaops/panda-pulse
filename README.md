# panda-pulse 🐼

A monitoring tool for Ethereum networks that checks node health and reports issues to Discord.

## Usage

### Docker (Recommended)

```bash
docker run -e GRAFANA_SERVICE_TOKEN=your_token \
          -e DISCORD_BOT_TOKEN=your_token \
          -e OPENROUTER_API_KEY=optional_key \
          ethpandaops/panda-pulse:0.0.1 \
          --discord-channel CHANNEL_ID \
          --network NETWORK_NAME
```

### Configuration

#### Environment Variables

- `GRAFANA_SERVICE_TOKEN` (required): Grafana service account token
- `DISCORD_BOT_TOKEN` (required): Discord bot token for notifications
- `OPENROUTER_API_KEY` (optional): OpenRouter API key for AI analysis

#### Command Line Arguments

- `--network` (required): Network to monitor (e.g., `pectra-devnet-5`)
- `--discord-channel` (required): Discord channel ID for notifications
- `--ethereum-cl`: Filter for specific consensus client (default: all)
- `--ethereum-el`: Filter for specific execution client (default: all)
