# Go Discord ChatGPT Bot

A Discord bot built with Go, [Arikawa v3](https://github.com/diamondburned/arikawa), and [Uber Fx](https://github.com/uber-go/fx). It integrates with OpenAI's GPT models to provide intelligent chat functionalities via slash commands.

## Features

- **Slash Commands**: Modern Discord slash command interface
- **GPT Integration**: Direct integration with OpenAI's GPT models
- **Thread Support**: Maintains conversation context in Discord threads
- **Dependency Injection**: Clean architecture using Uber Fx
- **Structured Logging**: Comprehensive logging with Zap
- **Modular Design**: Extensible command and service architecture

## Commands

- `/chat <message>` - Chat with GPT and create a conversation thread
- `/ping` - Simple health check command
- `/version` - Display the current bot version

## Development

### Prerequisites

- Go 1.24.3 or later
- Docker (for containerized deployment)
- Valid Discord Bot Token and OpenAI API Key

### Local Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/Raikerian/go-discord-chatgpt.git
   cd go-discord-chatgpt
   ```

2. Copy the example configuration:
   ```bash
   cp config.example.yaml config.yaml
   ```

3. Configure your `config.yaml` with:
   - Discord Bot Token
   - Discord Application ID
   - OpenAI API Key
   - Guild IDs (for testing)

4. Install dependencies:
   ```bash
   go mod download
   ```

5. Run the bot:
   ```bash
   go run main.go
   ```

### Testing

Run tests with:
```bash
go test -v ./...
```

Run linting:
```bash
golangci-lint run
```

### Building

#### Local Build
```bash
go build -o go-discord-chatgpt ./main.go
```

#### Using GoReleaser
```bash
# Test build (snapshot)
go tool goreleaser build --snapshot --clean

# Check configuration
go tool goreleaser check
```

## Deployment

This project uses a complete CI/CD pipeline with **GoReleaser v2** for professional release management and automated deployments.

### CI/CD Pipeline

#### On Pull Requests:
- ✅ Runs comprehensive tests and linting
- ✅ Validates GoReleaser configuration
- ✅ Tests snapshot builds
- ✅ Reports status back to PR

#### On Push to Main:
- ✅ Builds snapshot version with commit SHA
- ✅ Creates multi-architecture Docker images (amd64/arm64)
- ✅ Pushes to GitHub Container Registry
- ✅ Deploys snapshot to production server

#### On Version Tags (v*):
- ✅ Creates official release with GoReleaser
- ✅ Builds optimized Linux binaries
- ✅ Generates automated changelogs
- ✅ Creates GitHub release with downloadable assets
- ✅ Builds and pushes versioned Docker images
- ✅ Deploys specific version to production server

### Release Management

#### Creating Releases

```bash
# Patch release (bug fixes)
git tag v1.0.1
git push origin v1.0.1

# Minor release (new features)
git tag v1.1.0
git push origin v1.1.0

# Major release (breaking changes)
git tag v2.0.0
git push origin v2.0.0
```

#### Conventional Commits for Automated Changelogs

```bash
feat: add new slash command for server management
fix: resolve memory leak in message cache
docs: update API documentation
ci: add GoReleaser configuration
chore: update dependencies
```

### Docker Deployment

#### Using Published Images

```bash
# Pull and run latest version
docker pull ghcr.io/raikerian/go-discord-chatgpt:latest
docker run -d \
  --name go-discord-chatgpt \
  --restart unless-stopped \
  -v /path/to/config.yaml:/app/config.yaml:ro \
  ghcr.io/raikerian/go-discord-chatgpt:latest

# Run specific version
docker pull ghcr.io/raikerian/go-discord-chatgpt:v1.0.0
docker run -d \
  --name go-discord-chatgpt \
  --restart unless-stopped \
  -v /path/to/config.yaml:/app/config.yaml:ro \
  ghcr.io/raikerian/go-discord-chatgpt:v1.0.0
```

#### Building Local Images

```bash
# Development build
docker build -f Dockerfile -t go-discord-chatgpt:dev .

# Production build (using GoReleaser)
go tool goreleaser build --snapshot --clean
docker build -f Dockerfile.goreleaser -t go-discord-chatgpt:local .
```

### Production Deployment

The repository includes automated deployment to DigitalOcean droplets. To set up:

#### 1. GitHub Secrets Configuration

Add these secrets in repository Settings → Secrets and variables → Actions:

| Secret | Description |
|--------|-------------|
| `DO_HOST` | DigitalOcean droplet IP address |
| `DO_USERNAME` | SSH username (usually `root`) |
| `DO_SSH_PRIVATE_KEY` | SSH private key for authentication |
| `BOT_CONFIG` | Complete `config.yaml` content |

#### 2. Server Preparation

```bash
# Update system
apt update && apt upgrade -y

# Install Docker
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh

# Start and enable Docker
systemctl start docker
systemctl enable docker

# Create application directory
mkdir -p /opt/go-discord-chatgpt
```

#### 3. SSH Key Setup

```bash
# Generate deployment key
ssh-keygen -t ed25519 -C "github-actions-deploy" -f ~/.ssh/github_actions_deploy

# Copy public key to server
ssh-copy-id -i ~/.ssh/github_actions_deploy.pub root@your-droplet-ip

# Copy private key content to GitHub secrets
cat ~/.ssh/github_actions_deploy
```

### Manual Deployment

For manual deployments or rollbacks:

```bash
# SSH to server
ssh root@your-droplet-ip

# Deploy specific version
cd /opt/go-discord-chatgpt
./deploy.sh v1.2.0

# Deploy latest
./deploy.sh latest

# Rollback to previous version
./deploy.sh v1.1.0
```

### Monitoring

#### View Logs
```bash
# View container logs
docker logs go-discord-chatgpt

# Follow logs in real-time
docker logs -f go-discord-chatgpt
```

#### Check Deployment Status
```bash
# Check container status
docker ps | grep go-discord-chatgpt

# Check deployed version
docker inspect --format='{{.Config.Labels.version}}' go-discord-chatgpt

# Check deployment time
docker inspect --format='{{.Config.Labels.deployed}}' go-discord-chatgpt
```

### Health Checks

The Docker containers include built-in health checks:
- ✅ Process monitoring
- ✅ 30-second intervals
- ✅ Automatic restart on failure

### Security

- 🔒 Non-root container user
- 🔒 Minimal Alpine base image
- 🔒 Read-only configuration mounting
- 🔒 Key-based SSH authentication
- 🔒 GitHub Secrets for sensitive data

## Configuration

See `config.example.yaml` for a complete configuration template with all available options.

## Architecture

The bot uses a modular architecture with dependency injection:

- **Bot Service**: Core Discord event handling
- **Chat Service**: Orchestrates GPT interactions
- **Commands**: Slash command implementations
- **Conversation Store**: Message history management
- **AI Provider**: OpenAI API integration
- **Cache**: LRU caching for performance

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Add tests for new functionality
5. Ensure all tests pass (`go test -v ./...`)
6. Commit using conventional commit format (`git commit -m 'feat: add amazing feature'`)
7. Push to the branch (`git push origin feature/amazing-feature`)
8. Open a Pull Request

The CI pipeline will automatically test your changes and provide feedback.
