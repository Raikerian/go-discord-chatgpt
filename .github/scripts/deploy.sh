#!/bin/bash
set -euo pipefail

VERSION="${1:-latest}"
IMAGE_NAME="ghcr.io/$(echo "$GITHUB_REPOSITORY" | tr '[:upper:]' '[:lower:]'):$VERSION"
CONTAINER_NAME="go-discord-chatgpt"
CONFIG_DIR="/opt/go-discord-chatgpt"

log() {
    echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*"
}

log "Starting deployment of $IMAGE_NAME..."

# Verify Docker is running
if ! docker info >/dev/null 2>&1; then
    log "ERROR: Docker is not running"
    exit 1
fi

# Create backup of current container if it exists
if docker ps -a --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    log "Creating backup of current container..."
    docker commit $CONTAINER_NAME "${CONTAINER_NAME}-backup-$(date +%s)" || true
fi

# Stop and remove existing container
log "Stopping existing container..."
docker stop $CONTAINER_NAME 2>/dev/null || true
docker rm $CONTAINER_NAME 2>/dev/null || true

# Pull specified image version
log "Pulling image $IMAGE_NAME..."
if ! docker pull $IMAGE_NAME; then
    log "ERROR: Failed to pull image $IMAGE_NAME"
    exit 1
fi

# Ensure config directory exists
mkdir -p $CONFIG_DIR

# Validate config file exists
if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
    log "ERROR: Config file not found at $CONFIG_DIR/config.yaml"
    exit 1
fi

# Run new container
log "Starting new container..."
docker run -d \
  --name $CONTAINER_NAME \
  --restart unless-stopped \
  --label "version=$VERSION" \
  --label "deployed=$(date -Iseconds)" \
  --health-cmd="pgrep go-discord-chatgpt || exit 1" \
  --health-interval=30s \
  --health-timeout=10s \
  --health-retries=3 \
  --health-start-period=30s \
  -v $CONFIG_DIR/config.yaml:/app/config.yaml:ro \
  $IMAGE_NAME

# Wait for container to be healthy
log "Waiting for container to be healthy..."
timeout 120 bash -c 'until docker inspect --format="{{.State.Health.Status}}" $0 | grep -q healthy; do sleep 2; done' $CONTAINER_NAME

log "Deployment of $VERSION completed successfully!"

# Show deployment info
log "Container info:"
docker inspect --format='Version: {{.Config.Labels.version}}, Deployed: {{.Config.Labels.deployed}}' $CONTAINER_NAME

# Clean up old images (keep last 3)
log "Cleaning up old images..."
docker images "ghcr.io/$(echo "$GITHUB_REPOSITORY" | tr '[:upper:]' '[:lower:]')" --format "table {{.Repository}}:{{.Tag}}\t{{.CreatedAt}}" | \
    tail -n +4 | awk '{print $1}' | head -n -3 | xargs -r docker rmi || true

log "Deployment complete!"
