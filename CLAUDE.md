# Panda-Pulse 🐼

A monitoring tool for Ethereum networks that checks node health and reports issues to Discord. The system integrates with Grafana/Prometheus for metrics collection, uses AWS S3 for data storage, and provides Discord bot commands for network monitoring management.

## Project Structure
Claude MUST read the `.cursor/rules/project_architecture.mdc` file before making any structural changes to the project.

## Code Standards  
Claude MUST read the `.cursor/rules/code_standards.mdc` file before writing any code in this project.

## Development Workflow
Claude MUST read the `.cursor/rules/development_workflow.mdc` file before making changes to build, test, or deployment configurations.

## Component Documentation
Individual components have their own CLAUDE.md files with component-specific rules. Always check for and read component-level documentation when working on specific parts of the codebase.

## Quick Reference

### Core Components
- **Service**: Main application orchestration (`pkg/service/`)
- **Analyzer**: Health check processing (`pkg/analyzer/`)
- **Cartographoor**: Client management (`pkg/cartographoor/`)
- **Checks**: Health check implementations (`pkg/checks/`)
- **Discord Bot**: Discord integration (`pkg/discord/`)
- **Store**: Data persistence with S3 (`pkg/store/`)

### Key Technologies
- Go 1.24 with standard library patterns
- Discord Bot API for user interaction
- Grafana/Prometheus for metrics collection
- AWS S3 for data storage
- Docker for containerization

### Environment Variables
Essential configuration via environment variables:
- `GRAFANA_SERVICE_TOKEN`: Grafana API access
- `DISCORD_BOT_TOKEN`: Discord bot authentication
- `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`: S3 access
- `S3_BUCKET`: Storage bucket name

### Testing
- Run tests: `go test ./...`
- Integration tests use testcontainers
- Mocks generated with uber-go/mock
- Local development with localstack for S3