# Deployment Guide

This document provides step-by-step instructions for setting up the complete CI/CD pipeline and deploying the go-discord-chatgpt bot.

## Overview

The deployment pipeline uses:
- **GoReleaser v2** for professional release management
- **GitHub Actions** for CI/CD automation
- **GitHub Container Registry** for Docker image hosting
- **DigitalOcean** for production hosting
- **Multi-architecture builds** (Linux amd64/arm64)

## Quick Start Checklist

### Repository Setup
- [ ] Fork/clone the repository
- [ ] Configure GitHub Actions permissions
- [ ] Set up GitHub Container Registry
- [ ] Add required GitHub Secrets

### Server Setup
- [ ] Create DigitalOcean droplet
- [ ] Install Docker
- [ ] Set up SSH key authentication
- [ ] Configure application directory

### Configuration
- [ ] Create production configuration
- [ ] Test local build process
- [ ] Verify CI/CD pipeline
- [ ] Deploy first release

## Detailed Setup Instructions

### 1. Repository Configuration

#### Enable GitHub Actions
1. Go to repository **Settings** → **Actions** → **General**
2. Enable "Allow all actions and reusable workflows"
3. Set "Workflow permissions" to "Read and write permissions"
4. Enable "Allow GitHub Actions to create and approve pull requests"

#### GitHub Container Registry
1. Ensure repository visibility allows package publishing
2. The `GITHUB_TOKEN` automatically has `packages:write` permission

### 2. GitHub Secrets Configuration

Navigate to **Settings** → **Secrets and variables** → **Actions** and add:

| Secret Name | Description | Example |
|-------------|-------------|---------|
| `DO_HOST` | DigitalOcean droplet IP | `192.168.1.100` |
| `DO_USERNAME` | SSH username | `root` |
| `DO_SSH_PRIVATE_KEY` | SSH private key content | `-----BEGIN OPENSSH PRIVATE KEY-----...` |
| `BOT_CONFIG` | Complete config.yaml content | See configuration section below |

### 3. DigitalOcean Server Setup

#### Create Droplet
1. Create Ubuntu 22.04 LTS droplet (minimum 1GB RAM)
2. Note the IP address for `DO_HOST` secret

#### Server Configuration
```bash
# Connect to your droplet
ssh root@your-droplet-ip

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

# Verify Docker installation
docker --version
```

#### SSH Key Setup
```bash
# On your local machine, generate deployment key
ssh-keygen -t ed25519 -C "github-actions-deploy" -f ~/.ssh/github_actions_deploy

# Copy public key to droplet
ssh-copy-id -i ~/.ssh/github_actions_deploy.pub root@your-droplet-ip

# Display private key content for GitHub secret
cat ~/.ssh/github_actions_deploy
```

### 4. Bot Configuration

Create your production configuration for the `BOT_CONFIG` secret:

```yaml
discord:
  bot_token: "YOUR_DISCORD_BOT_TOKEN"
  app_id: "YOUR_DISCORD_APP_ID"
  guild_ids:
    - "YOUR_GUILD_ID_1"
    - "YOUR_GUILD_ID_2"
  interaction_timeout_seconds: 15

openai:
  api_key: "YOUR_OPENAI_API_KEY"
  preferred_models:
    - "gpt-4"
    - "gpt-3.5-turbo"
  message_cache_size: 1000
  negative_thread_cache_size: 100
  max_concurrent_requests: 10
```

### 5. Branch Protection (Optional but Recommended)

1. Go to **Settings** → **Branches**
2. Add rule for `main` branch
3. Enable "Require status checks to pass before merging"
4. Select "CI" workflow as required check

## Pipeline Workflows

### CI Pipeline (Pull Requests)

Triggered on: Pull requests to `main` branch

**Steps:**
1. Run comprehensive tests and linting
2. Validate GoReleaser configuration
3. Test snapshot build process
4. Report status back to PR

### CD Pipeline (Main Branch)

Triggered on: Push to `main` branch (non-tag)

**Steps:**
1. Run full test suite
2. Create snapshot build with commit SHA
3. Build multi-architecture Docker images
4. Push to GitHub Container Registry
5. Deploy snapshot to production server

### Release Pipeline (Version Tags)

Triggered on: Push of version tags (`v*`)

**Steps:**
1. Run full test suite
2. Create official release with GoReleaser
3. Build optimized Linux binaries
4. Generate automated changelog
5. Create GitHub release with assets
6. Build and push versioned Docker images
7. Deploy specific version to production

## Release Management

### Creating Releases

```bash
# Create and push version tag
git tag v1.0.0
git push origin v1.0.0

# Or use GitHub CLI
gh release create v1.0.0 --title "Release v1.0.0" --notes "Initial release"
```

### Version Numbering

Follow semantic versioning:
- `v1.0.1` - Patch (bug fixes)
- `v1.1.0` - Minor (new features)
- `v2.0.0` - Major (breaking changes)

### Conventional Commits

For automated changelog generation:

```bash
feat: add new slash command for server management
fix: resolve memory leak in message cache
docs: update API documentation
ci: add GoReleaser configuration
chore: update dependencies
```

## Local Development Testing

### Test GoReleaser Configuration
```bash
# Check configuration
go tool goreleaser check

# Test snapshot build
go tool goreleaser build --snapshot --clean

# Test full release process (dry run)
go tool goreleaser release --snapshot --clean
```

### Test Docker Build
```bash
# Build development image
docker build -f Dockerfile -t go-discord-chatgpt:dev .

# Test run (requires config.yaml)
docker run --rm -v $(pwd)/config.yaml:/app/config.yaml:ro go-discord-chatgpt:dev
```

## Deployment Verification

### Check Pipeline Status
1. Go to **Actions** tab in GitHub
2. Verify workflow completion
3. Check for any error messages

### Verify Docker Images
```bash
# List available images
docker images | grep go-discord-chatgpt

# Check image labels
docker inspect ghcr.io/raikerian/go-discord-chatgpt:latest
```

### Monitor Production Deployment
```bash
# SSH to server
ssh root@your-droplet-ip

# Check container status
docker ps | grep go-discord-chatgpt

# View logs
docker logs -f go-discord-chatgpt

# Check deployed version
docker inspect --format='{{.Config.Labels.version}}' go-discord-chatgpt
```

## Troubleshooting

### Common Issues

#### 1. Docker Login Fails
```bash
# Manually test GitHub Container Registry login
echo $GITHUB_TOKEN | docker login ghcr.io -u $GITHUB_USERNAME --password-stdin
```

#### 2. SSH Connection Fails
```bash
# Test SSH connection
ssh -i ~/.ssh/github_actions_deploy root@your-droplet-ip

# Check SSH key permissions
chmod 600 ~/.ssh/github_actions_deploy
```

#### 3. Container Won't Start
```bash
# Check container logs
docker logs go-discord-chatgpt

# Verify config file
cat /opt/go-discord-chatgpt/config.yaml

# Test config syntax
docker run --rm -v /opt/go-discord-chatgpt/config.yaml:/app/config.yaml:ro \
  ghcr.io/raikerian/go-discord-chatgpt:latest --validate-config
```

#### 4. GoReleaser Errors
```bash
# Update GoReleaser
go get -u github.com/goreleaser/goreleaser/v2

# Clean and retry
go tool goreleaser --clean
```

### Health Checks

The deployment includes automated health checks:
- Process monitoring every 30 seconds
- Automatic container restart on failure
- Health status visible in `docker ps`

### Manual Recovery

#### Rollback to Previous Version
```bash
# SSH to server
ssh root@your-droplet-ip

# List available images
docker images | grep go-discord-chatgpt

# Deploy previous version
cd /opt/go-discord-chatgpt
./deploy.sh v1.0.0  # Replace with desired version
```

#### Force Redeploy
```bash
# Stop and remove container
docker stop go-discord-chatgpt
docker rm go-discord-chatgpt

# Pull latest image
docker pull ghcr.io/raikerian/go-discord-chatgpt:latest

# Restart using deployment script
./deploy.sh latest
```

## Security Considerations

- ✅ All sensitive data stored in GitHub Secrets
- ✅ SSH key-based authentication only
- ✅ Non-root container user
- ✅ Read-only configuration mounting
- ✅ Minimal Alpine base image
- ✅ Regular security updates via pipeline

## Monitoring and Maintenance

### Regular Tasks
- Monitor GitHub Actions for failures
- Review deployment logs weekly
- Update dependencies monthly
- Rotate SSH keys quarterly

### Performance Monitoring
```bash
# Check container resource usage
docker stats go-discord-chatgpt

# Monitor application logs
docker logs -f go-discord-chatgpt

# Check system resources
htop
df -h
```

### Backup Strategy
- Configuration stored in GitHub Secrets
- Docker images stored in GitHub Container Registry
- Application state is stateless (no data persistence required)

## Support

- Check GitHub Issues for common problems
- Review GitHub Actions logs for deployment issues
- Monitor Discord bot status in your server
- Test locally before pushing changes

## Next Steps

After successful deployment:
1. Test all slash commands in Discord
2. Monitor logs for any errors
3. Set up monitoring/alerting if needed
4. Plan regular maintenance schedule
5. Consider setting up staging environment
