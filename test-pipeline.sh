#!/bin/bash
set -euo pipefail

echo "🔍 Testing go-discord-chatgpt deployment pipeline..."
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Helper functions
success() {
    echo -e "${GREEN}✅ $1${NC}"
}

warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

error() {
    echo -e "${RED}❌ $1${NC}"
}

info() {
    echo -e "${YELLOW}ℹ️  $1${NC}"
}

# Test 1: Go version
echo "1. Checking Go version..."
if command -v go &> /dev/null; then
    GO_VERSION=$(go version | cut -d' ' -f3)
    if [[ "$GO_VERSION" == "go1.24.3" ]]; then
        success "Go version is correct: $GO_VERSION"
    else
        warning "Go version is $GO_VERSION, expected go1.24.3"
    fi
else
    error "Go is not installed"
    exit 1
fi

# Test 2: Dependencies
echo "2. Checking Go dependencies..."
if go mod verify; then
    success "Go dependencies are verified"
else
    error "Go dependencies verification failed"
    exit 1
fi

# Test 3: Build test
echo "3. Testing local build..."
if go build -o test-binary ./main.go; then
    success "Local build successful"
    rm -f test-binary
else
    error "Local build failed"
    exit 1
fi

# Test 4: Tests
echo "4. Running tests..."
if go test -v ./...; then
    success "All tests passed"
else
    error "Tests failed"
    exit 1
fi

# Test 5: GoReleaser configuration
echo "5. Checking GoReleaser configuration..."
if go tool goreleaser check; then
    success "GoReleaser configuration is valid"
else
    error "GoReleaser configuration is invalid"
    exit 1
fi

# Test 6: GoReleaser snapshot build
echo "6. Testing GoReleaser snapshot build..."
if go tool goreleaser build --snapshot --clean; then
    success "GoReleaser snapshot build successful"
    rm -rf dist/
else
    error "GoReleaser snapshot build failed"
    exit 1
fi

# Test 7: Docker build (if Docker is available)
echo "7. Testing Docker build..."
if command -v docker &> /dev/null; then
    if docker build -f Dockerfile -t go-discord-chatgpt:test . &> /dev/null; then
        success "Docker build successful"
        docker rmi go-discord-chatgpt:test &> /dev/null || true
    else
        warning "Docker build failed (this is optional for CI)"
    fi
else
    warning "Docker is not available (this is optional for CI)"
fi

# Test 8: Check required files
echo "8. Checking required files..."
REQUIRED_FILES=(
    ".goreleaser.yml"
    "Dockerfile"
    "Dockerfile.goreleaser"
    ".dockerignore"
    ".github/workflows/ci.yml"
    ".github/workflows/cd.yml"
    ".github/scripts/deploy.sh"
    "README.md"
    "DEPLOYMENT.md"
)

for file in "${REQUIRED_FILES[@]}"; do
    if [[ -f "$file" ]]; then
        success "Found required file: $file"
    else
        error "Missing required file: $file"
        exit 1
    fi
done

# Test 9: Check GitHub workflows syntax
echo "9. Checking GitHub workflows syntax..."
for workflow in .github/workflows/*.yml; do
    if [[ -f "$workflow" ]]; then
        # Basic YAML syntax check
        if python3 -c "import yaml; yaml.safe_load(open('$workflow'))" 2>/dev/null; then
            success "Valid YAML syntax: $(basename "$workflow")"
        elif which yamllint &> /dev/null && yamllint "$workflow" &> /dev/null; then
            success "Valid YAML syntax: $(basename "$workflow")"
        else
            warning "Could not validate YAML syntax for: $(basename "$workflow") (install python3-yaml or yamllint for validation)"
        fi
    fi
done

# Test 10: Check deployment script permissions
echo "10. Checking deployment script..."
if [[ -x ".github/scripts/deploy.sh" ]]; then
    success "Deployment script is executable"
else
    error "Deployment script is not executable"
    exit 1
fi

echo
echo "🎉 All tests passed! The deployment pipeline is ready."
echo
info "Next steps:"
echo "  1. Configure GitHub Secrets (see DEPLOYMENT.md)"
echo "  2. Set up DigitalOcean droplet"
echo "  3. Push code to trigger CI pipeline"
echo "  4. Create a release tag (e.g., git tag v0.1.0 && git push origin v0.1.0)"
echo
info "For detailed setup instructions, see: DEPLOYMENT.md"
