# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

### Building

```bash
# Local build
go build -o go-discord-chatgpt ./main.go

# Test build with GoReleaser
go tool goreleaser build --snapshot --clean
```

### Testing

```bash
# Run all tests
go test -v ./...

# Run tests for a specific package
go test -v ./internal/chat/...

# Run a single test
go test -v -run TestUsageFormatter ./internal/chat
```

### Linting

```bash
# Run golangci-lint
golangci-lint run

# Check GoReleaser configuration
go tool goreleaser check
```

### Running the Bot

```bash
# Run directly
go run main.go

# Run built binary
./go-discord-chatgpt
```

## High-Level Architecture

This Discord bot uses **Uber Fx** for dependency injection and modular architecture. The application follows a clean layered architecture:

### Core Components

1. **Application Layer** (`internal/app/`)
   - `app.Application` manages the entire application lifecycle
   - Fx hooks coordinate startup/shutdown of all components

2. **Modular Architecture**
   - Each major component has its own Fx module for clean separation
   - Modules are composed in `main.go` to build the complete application

3. **Key Services**

   **Bot Service** (`internal/bot/`)
   - Routes Discord interaction events
   - Manages bot lifecycle and event handling

   **Chat Service** (`internal/chat/`)
   - Orchestrates AI chat interactions through modular components:
     - `AIProvider` - Interface for AI completions (OpenAI implementation)
     - `ConversationStore` - Manages conversation history with LRU caching
     - `DiscordInteractionManager` - Handles Discord API interactions
     - `ModelSelector` - Selects appropriate AI models
     - `UsageFormatter` - Formats usage with cost calculations
     - `MessageEmbedService` - Creates Discord embeds

   **Commands** (`internal/commands/`)
   - All commands implement the `Command` interface
   - `CommandManager` handles registration/unregistration with Discord
   - Commands are collected via Fx groups

   **Configuration** (`internal/config/`)
   - Loads from `config.yaml`
   - Provides typed configuration to all services

### Dependency Flow

```text
main.go → app.Application → Fx Modules → Services → Components
```

Services are wired together automatically by Fx based on their constructor signatures. The application uses interfaces extensively to maintain loose coupling between components.

### Key Patterns

- **Interface-Based Design**: Components depend on interfaces, not concrete types
- **Constructor Injection**: All dependencies are injected via constructors
- **Lifecycle Management**: Fx manages component startup/shutdown order
- **Modular Composition**: Features are organized into self-contained modules

## Important Go Conventions

This project follows Go best practices as documented in `.cursor/rules/golang.mdc`:

- **Naming**: Avoid repetition, use clear names, CamelCase for constants
- **Error Handling**: Wrap errors with context using `fmt.Errorf` with `%w`
- **Interfaces**: Keep small and focused, define at point of use
- **Testing**: Use table-driven tests, test behavior not implementation
- **Concurrency**: Use channels to communicate, context for cancellation

## Development Workflow

1. **Feature Implementation**: Understand requirements, explore codebase, plan changes
2. **Testing**: Write unit tests following existing patterns
3. **Linting**: Ensure code passes `golangci-lint`
4. **Documentation**: Update relevant docs if introducing new patterns

## CI/CD

- **Pull Requests**: Automated testing, linting, and GoReleaser validation
- **Main Branch**: Snapshot builds and Docker images with commit SHA
- **Release Tags**: Full releases with changelogs and multi-architecture builds

See `DEPLOYMENT.md` for detailed deployment procedures.

## Go Documentation Tips

- Use the `go doc <package>` command to access comprehensive API documentation for any Go package, including interfaces, types, functions, and usage examples. This works for both standard library packages and third-party dependencies.

## Dependency Management

- Use `go get` to add dependencies to the go.mod instead of manually editing go.mod