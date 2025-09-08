# Discord Command Cleanup Utility

A utility tool for managing and cleaning up Discord slash commands registered by the panda-pulse bot. This tool helps resolve issues with duplicate command registrations and manages the transition between global and guild-specific commands.

## Purpose

Discord slash commands can become duplicated or orphaned due to:
- Multiple bot deployments
- Switching between global and guild-specific registration
- Testing and development iterations
- Failed cleanup during bot shutdown

This utility provides a safe way to audit and clean up these commands.

## Building

```bash
go build -o cleanup-commands ./cmd/cleanup-commands/main.go
```

## Usage

### Environment Variables

The tool uses the same environment variables as the main bot:
- `DISCORD_BOT_TOKEN` - Required: Discord bot authentication token
- `DISCORD_GUILD_ID` - Optional: Guild ID for guild-specific command management

### Command Line Flags

```bash
./cleanup-commands [flags]
```

| Flag | Default | Description |
|------|---------|-------------|
| `-token` | `$DISCORD_BOT_TOKEN` | Discord bot token |
| `-guild` | `$DISCORD_GUILD_ID` | Guild ID to manage commands for |
| `-dry-run` | `true` | If true, only list commands without deleting |
| `-keep-guild` | `true` | Keep guild-specific commands |
| `-keep-global` | `false` | Keep global commands |

## Examples

### 1. Audit Commands (Dry Run)

See all registered commands without making changes:

```bash
# Set your bot token
export DISCORD_BOT_TOKEN="your-bot-token"
export DISCORD_GUILD_ID="595666850260713488"

# Run in dry-run mode (default)
./cleanup-commands
```

Output will show:
- All global commands with their IDs
- All guild-specific commands with their IDs  
- Any duplicate registrations detected
- Recommended cleanup actions

### 2. Remove All Global Commands

Clean up global commands while keeping guild-specific ones:

```bash
./cleanup-commands -dry-run=false -keep-global=false -keep-guild=true
```

This is the recommended approach when transitioning to guild-specific commands.

### 3. Remove Duplicate Guild Commands

Keep guild commands but remove duplicates (keeps the most recent):

```bash
./cleanup-commands -dry-run=false -keep-guild=true -guild="595666850260713488"
```

### 4. Complete Cleanup

Remove ALL commands (both global and guild):

```bash
./cleanup-commands -dry-run=false -keep-global=false -keep-guild=false
```

⚠️ **Warning**: This removes all commands. The bot will need to re-register them on next startup.

## Understanding Command Types

### Global Commands
- Registered without a guild ID
- Available in all servers where the bot is present
- Take up to 1 hour to propagate changes
- Visible in DMs with the bot

### Guild-Specific Commands
- Registered to a specific guild/server
- Only available in that guild
- Propagate changes within ~1 second
- Recommended for development and single-server bots

## Troubleshooting

### "Unknown interaction" Errors
If users see this error, it usually means there are stale command registrations. Run the cleanup utility to remove duplicates.

### Commands Not Updating
Global commands can take up to an hour to update. Consider switching to guild-specific commands for faster updates.

### Missing Commands After Cleanup
After running cleanup, restart your bot to re-register commands:

```bash
# For guild-specific registration
export DISCORD_GUILD_ID="your-guild-id"
# Then restart your panda-pulse service
```

## Safety Features

1. **Dry Run by Default**: The tool runs in dry-run mode by default to prevent accidental deletions
2. **Duplicate Detection**: Automatically identifies and reports duplicate command registrations
3. **Selective Deletion**: Can target specific command types (global vs guild)
4. **Detailed Logging**: Shows exactly what will be or was deleted

## Best Practices

1. Always run with `-dry-run=true` first to see what will be affected
2. Keep guild-specific commands when possible (faster updates)
3. After cleanup, restart the bot to ensure proper command registration
4. Use guild-specific registration for development/testing environments

## Integration with Panda-Pulse

This utility is designed to work alongside the main panda-pulse bot. It uses the same Discord API client and respects the same environment variables. Running this utility does not affect:
- Scheduled health checks
- Stored configurations in S3
- Active monitoring alerts
- Discord channel configurations

It only manages the Discord slash command registrations themselves.