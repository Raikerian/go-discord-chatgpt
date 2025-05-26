# GitHub Copilot Instructions for go-discord-chatgpt

This document provides instructions and context for GitHub Copilot to effectively assist with the development of the `go-discord-chatgpt` project.

## Project Overview

`go-discord-chatgpt` is a Discord bot written in Go. It uses the Arikawa library for Discord API interaction, Uber Fx for dependency injection and application lifecycle management, and integrates with OpenAI's GPT models. The primary goal is to provide slash command functionalities, including interactions with ChatGPT, such as the `/chat` command.

## Key Technologies & Libraries

*   **Go**: The primary programming language.
*   **Arikawa (v3)**: Go library for the Discord API. Used for session management, event handling, and command registration. (See also: [`arikawa.instructions.md`](.github/instructions/arikawa.instructions.md) for detailed guidance on using Arikawa).
*   **Uber Fx**: Dependency injection framework. Manages the application's lifecycle and the creation and wiring of components (services, configuration, logger, OpenAI client, cache, etc.).
*   **Zap**: Structured logging library. Used for all application logging, including Fx's internal logs.
*   **YAML V3**: For parsing the `config.yaml` file.
*   **Testify (mock, require, assert)**: For writing unit tests and making assertions.
*   **Mockery (vektra/mockery)**: For generating mock implementations of interfaces, configured via `.mockery.yml`.
*   **go-openai (sashabaranov/go-openai)**: Go client library for the OpenAI API.
*   **go-openai-realtime (WqyJh/go-openai-realtime)**: Go SDK for OpenAI Realtime API enabling real-time voice and text conversations. (See also: [`go-openai-realtime.instructions.md`](.github/instructions/go-openai-realtime.instructions.md) for detailed guidance on using the OpenAI Realtime API for voice features).
*   **golang-lru (hashicorp/golang-lru/v2)**: LRU cache implementation, used for caching OpenAI message history.
*   **GoReleaser v2**: Professional release management tool for building, packaging, and releasing Go applications with multi-architecture support.
*   **GitHub Actions**: CI/CD automation platform for testing, building, and deploying the application.
*   **Docker**: Containerization platform for creating reproducible, multi-architecture builds and deployments.
*   **golangci-lint**: Go linting tool for code quality and consistency checks.

## Project Structure

```
.
├── config.yaml             # Application configuration
├── config.example.yaml     # Example configuration file
├── go.mod                  # Go module definition
├── go.sum                  # Go module checksums
├── LICENSE                 # Project license
├── README.md               # Project README
├── DEPLOYMENT.md           # Deployment and CI/CD setup guide
├── Dockerfile              # Multi-stage Docker build for development
├── Dockerfile.goreleaser   # Optimized Docker build for GoReleaser
├── .dockerignore           # Docker context exclusions
├── .goreleaser.yml         # GoReleaser v2 configuration for releases and builds
├── .golangci.yml           # golangci-lint configuration
├── .mockery.yml            # Configuration for Mockery mock generation
├── test-pipeline.sh        # Local pipeline validation script
├── .github/
│   ├── workflows/
│   │   ├── ci.yml          # CI pipeline for pull requests (testing, validation)
│   │   └── cd.yml          # CD pipeline for releases and deployments
│   ├── scripts/
│   │   └── deploy.sh       # Production deployment script for DigitalOcean
│   ├── instructions/
│   │   ├── arikawa.instructions.md    # Arikawa-specific development guidelines
│   │   └── golang.instructions.md    # Go best practices and conventions
├── cmd/
│   └── main.go             # Main application entry point, Fx setup, and lifecycle management
├── internal/
│   ├── bot/
│   │   ├── bot.go          # Core bot service, handles startup/shutdown logic, interaction event routing
│   │   └── handlers.go     # Interaction event handlers (e.g., for slash commands and thread messages)
│   ├── chat/               # Modular chat service architecture with specialized components
│   │   ├── service.go      # Main chat service orchestrator that coordinates all chat components
│   │   ├── ai_provider.go  # AIProvider interface and OpenAI implementation for chat completions
│   │   ├── conversation_store.go # ConversationStore interface for managing conversation history and caching
│   │   ├── interaction_manager.go # DiscordInteractionManager for Discord API interactions (responses, threads, typing)
│   │   ├── model_selector.go # ModelSelector interface for choosing AI models based on user preferences
│   │   ├── summary_parser.go # SummaryParser interface for parsing thread summary messages to extract metadata
│   │   └── util.go         # Utility functions: GetUserDisplayName, SanitizeOpenAIName, MakeThreadName, SendLongMessage
│   ├── commands/
│   │   ├── chat.go         # Implementation of the `/chat` command, which delegates to the `chat.Service`
│   │   ├── command_loader.go # CommandManager: loads commands from Fx, registers/unregisters with Discord
│   │   ├── command_loader_test.go # Unit tests for CommandManager
│   │   ├── command.go      # Defines the `Command` interface that all slash commands must implement
│   │   ├── mocks_test.go   # Mocks generated by Mockery (e.g., for Command interface)
│   │   ├── ping.go         # Implementation of the `/ping` command
│   │   └── version.go      # Implementation of the `/version` command with embedded version info
│   ├── config/
│   │   └── config.go       # Configuration struct and loading logic
│   └── gpt/
│       ├── cache.go        # LRU Cache implementation for OpenAI messages (MessagesCache)
│       └── negative_cache.go # LRU cache for threads that should be ignored (NegativeThreadCache)

```

## Core Architectural Decisions & Patterns

1.  **Dependency Injection with Uber Fx**:
    *   The application's components (config, logger, Discord session, OpenAI client, message cache, negative thread cache, **chat service components**, command manager, bot service, commands like [`ChatCommand`](internal/commands/chat.go)) are managed by Fx.
    *   **Chat Service Components**: The chat system is now modular with several specialized services:
        *   [`chat.Service`](internal/chat/service.go) - Main orchestrator that coordinates all chat interactions
        *   [`chat.AIProvider`](internal/chat/ai_provider.go) - Interface for AI completions (OpenAI implementation)
        *   [`chat.ConversationStore`](internal/chat/conversation_store.go) - Manages conversation history and caching
        *   [`chat.DiscordInteractionManager`](internal/chat/interaction_manager.go) - Handles Discord API interactions
        *   [`chat.ModelSelector`](internal/chat/model_selector.go) - Selects appropriate AI models
        *   [`chat.SummaryParser`](internal/chat/summary_parser.go) - Parses thread summary messages
    *   Providers for these components are defined in `cmd/main.go` using a modular approach where the [`chat.ConversationStore`](internal/chat/conversation_store.go) is created via a custom provider function (`newConversationStoreProvider`) that combines multiple dependencies.
    *   Fx handles the lifecycle (start/stop) of these components. For example, the Discord session is opened on start and closed on stop, and commands are registered/unregistered accordingly.

2.  **Configuration Management**:
    *   Configuration is loaded from `config.yaml` into the `config.Config` struct ([`internal/config/config.go`](internal/config/config.go)).
    *   This includes Discord settings (bot token, app ID, guild IDs, interaction timeout) and OpenAI settings (API key, preferred models, message cache size, negative thread cache size, max concurrent requests).
    *   The path to `config.yaml` is supplied to Fx in [`main.go`](main.go).
    *   The `*config.Config` object is then available for injection into other components. For example, `discord.interaction_timeout_seconds` is used by the [`Bot`](internal/bot/bot.go) service, `openai.message_cache_size` configures the conversation store cache, and `openai.negative_thread_cache_size` configures the negative thread cache.

3.  **Logging**:
    *   Zap is used for structured logging.
    *   A `*zap.Logger` is configured and provided by Fx.
    *   Fx's internal logging is also adapted to use this Zap logger via the `zapFxPrinter` in `cmd/main.go`.

4.  **Command Handling**:
    *   **Interface**: All slash commands implement the `commands.Command` interface defined in `internal/commands/command.go`. This interface specifies methods like `Name()`, `Description()`, `Options()`, and `Execute()`.
    *   **Constructors & Fx Groups**: Each command (e.g., [`PingCommand`](internal/commands/ping.go), [`VersionCommand`](internal/commands/version.go), [`ChatCommand`](internal/commands/chat.go)) has a constructor function (e.g., `NewPingCommand() commands.Command`). The [`ChatCommand`](internal/commands/chat.go) now takes the [`chat.Service`](internal/chat/service.go) as a dependency.
    *   **Fx Provisioning**: These constructors are provided to Fx in `cmd/main.go` and tagged with `fx.ResultTags(\`group:"commands"\`)`.
    *   **CommandManager**: The [`commands.CommandManager`](internal/commands/command_loader.go) ([`internal/commands/command_loader.go`](internal/commands/command_loader.go)) receives all [`commands.Command`](internal/commands/command.go) implementations from the "commands" Fx group.
    *   **Registration**: On startup, [`CommandManager.RegisterCommands()`](internal/commands/command_loader.go) iterates through the loaded commands and registers them with Discord (globally or for specific guilds listed in `config.yaml`). It unregisters them on shutdown.
    *   **Dispatch**: The [`Bot`](internal/bot/bot.go) service ([`internal/bot/bot.go`](internal/bot/bot.go)) receives interaction create events from Arikawa. The [`handleInteraction`](internal/bot/handlers.go) function ([`internal/bot/handlers.go`](internal/bot/handlers.go)) uses the [`CommandManager`](internal/commands/command_loader.go) to find the appropriate command handler based on the interaction data and then executes it. The [`ChatCommand`](internal/commands/chat.go) delegates its core execution logic to the [`chat.Service`](internal/chat/service.go). Additionally, the bot handles thread message events via the [`handleMessageCreate`](internal/bot/handlers.go) function, which also delegates to the [`chat.Service`](internal/chat/service.go) for thread message processing.

5.  **Discord Session**:
    *   The Arikawa `*session.Session` is created and managed by Fx ([`NewSession`](main.go) in [`main.go`](main.go)).
    *   Its lifecycle (Open/Close) is tied to Fx's OnStart/OnStop hooks.
    *   Intents are configured within the `NewSession` provider.

6.  **Testing Strategy**:
    *   Unit tests are written using the `testify` suite (`assert`, `require`).
    *   Testing should primarily focus on the public interfaces of components.
    *   Mocks, generated with `vektra/mockery` (configured via `.mockery.yml`), should be used judiciously and only when necessary to isolate the unit under test. Avoid overuse of mocks.
    *   Test files should follow the Go convention of `*_test.go` (e.g., [`command_loader_test.go`](internal/commands/command_loader_test.go)). For black-box testing, they are often placed in a separate test package (e.g., `package commands_test` to test the `commands` package).
    *   Tests for components like [`CommandManager`](internal/commands/command_loader.go) ([`internal/commands/command_loader_test.go`](internal/commands/command_loader_test.go)) utilize these practices.

7.  **OpenAI Integration & Caching**:
    *   **OpenAI Client**: An `*openai.Client` is created and managed by Fx ([`NewOpenAIClient`](main.go) in [`main.go`](main.go)), configured with the API key from `config.yaml`.
    *   **Specialized Components**: Various specialized components for the chat system are now provided individually:
        *   [`gpt.MessagesCache`](internal/gpt/cache.go) - LRU cache for OpenAI message history, configured by `openai.message_cache_size`
        *   [`gpt.NegativeThreadCache`](internal/gpt/negative_cache.go) - LRU cache for threads to ignore, configured by `openai.negative_thread_cache_size`
        *   [`chat.ConversationStore`](internal/chat/conversation_store.go) - Created via a custom provider that creates internal caches based on configuration settings
    *   These components are available for injection into services that require interaction with the OpenAI API or its message history, primarily the **[`chat.Service`](internal/chat/service.go)** (which is then used by commands like [`ChatCommand`](internal/commands/chat.go)).

8.  **CI/CD Pipeline & Deployment**:
    *   **GoReleaser v2**: Professional release management with Linux-focused builds (amd64/arm64), Docker image creation, and automated GitHub releases with changelogs.
    *   **GitHub Actions Workflows**:
        *   [`.github/workflows/ci.yml`](.github/workflows/ci.yml) - CI pipeline for pull requests (testing, linting, GoReleaser validation)
        *   [`.github/workflows/cd.yml`](.github/workflows/cd.yml) - CD pipeline for releases and snapshot builds with automated deployment
    *   **Docker Multi-Architecture Support**: Builds for Linux amd64/arm64 with optimized Alpine-based images via [`Dockerfile.goreleaser`](Dockerfile.goreleaser).
    *   **Version Management**: Embedded version information using GoReleaser ldflags targeting `internal/commands.AppVersion` for the `/version` command.
    *   **Container Registry**: Images published to GitHub Container Registry (ghcr.io) with proper tagging (semantic versions for releases, commit SHA for snapshots).
    *   **Automated Deployment**: Production deployment to DigitalOcean via [`.github/scripts/deploy.sh`](.github/scripts/deploy.sh) with health checks and rollback capabilities.
    *   **Pipeline Validation**: Local testing via [`test-pipeline.sh`](test-pipeline.sh) script for validating complete CI/CD setup.

## Local Development

### Running Tests
```bash
go test -v ./...
```

### Running Linting
```bash
golangci-lint run
```

### Building the Application
```bash
go build -o go-discord-chatgpt ./cmd/main.go
```

### Running the Bot Locally
```bash
go run cmd/main.go
```

## Development Guidelines & Preferences

*   **Modularity**: Keep components decoupled and use Fx for wiring them together.
*   **Interfaces**: Use interfaces (like `commands.Command`) to define contracts between components.
*   **Structured Logging**: Use Zap for all logging. Provide context with logs where possible.
*   **Commenting**: Avoid redundant and long comments. Only comment where necessary to explain complex logic or non-obvious decisions.
*   **Error Handling**: Handle errors explicitly. Fx's lifecycle management will also report errors during startup/shutdown.
*   **Configuration-Driven**: Make behavior configurable via [`config.yaml`](config.yaml) where appropriate (e.g., guild IDs, OpenAI models, cache size, interaction timeouts).
*   **Go Best Practices**: Adhere to the guidelines outlined in [`golang.instructions.md`](.github/instructions/golang.instructions.md) for consistent and high-quality Go code.
*   **CI/CD & Deployment**:
    *   Use GoReleaser v2 for professional release management and multi-architecture builds.
    *   Follow the deployment patterns established in the GitHub Actions workflows for consistent and reliable deployments.
    *   Maintain version embedding through GoReleaser ldflags for runtime version reporting.
    *   Ensure Docker images are optimized and secure using Alpine base images with non-root users.
    *   Use the [`test-pipeline.sh`](test-pipeline.sh) script to validate pipeline setup before pushing changes.
*   **Testing**:
    *   Write unit tests for individual components and commands, focusing on public interfaces.
    *   Utilize the `testify` suite for assertions.
    *   Employ `mockery` for generating mocks, but only when essential for test isolation. Prefer real implementations or test doubles where feasible.
    *   Ensure test files are named with the `_test.go` suffix and organized appropriately (e.g., in a `package <name>_test`).
    *   Fx's structure facilitates mocking dependencies when necessary.
*   **Continuous Improvement**:
    *   Monitor emerging code patterns and update documentation when new conventions are established
    *   Add new rules or guidelines when patterns are used consistently across 3+ files
    *   Update existing guidelines when better examples or edge cases are discovered
    *   Remove or deprecate outdated patterns that no longer apply to the codebase

## Documentation Maintenance Guidelines

### Rule Improvement Triggers
*   New code patterns not covered by existing rules
*   Repeated similar implementations across files
*   Common error patterns that could be prevented
*   New libraries or tools being used consistently
*   Emerging best practices in the codebase

### Documentation Updates
*   **Add New Guidelines When:**
    *   A new technology/pattern is used in 3+ files
    *   Common bugs could be prevented by a guideline
    *   Code reviews repeatedly mention the same feedback
    *   New security or performance patterns emerge

*   **Modify Existing Guidelines When:**
    *   Better examples exist in the codebase
    *   Additional edge cases are discovered
    *   Related guidelines have been updated
    *   Implementation details have changed

### Quality Checks
*   Guidelines should be actionable and specific
*   Examples should come from actual code
*   References should be up to date
*   Patterns should be consistently enforced

## Iterative Development Workflow

When implementing features or fixes, follow this structured approach:

### 1. Feature Understanding & Planning
*   Thoroughly understand the specific goals and requirements
*   Explore the codebase to identify precise files, functions, and code locations that need modification
*   Determine intended code changes (diffs) and their locations
*   Gather all relevant details from this exploration phase

### 2. Implementation Documentation
*   Document detailed implementation plans including:
    *   File paths and line numbers for changes
    *   Proposed diffs and reasoning
    *   Potential challenges identified
    *   Decisions made, especially if confirmed with user input
*   Create a rich log of implementation decisions

### 3. Code Quality & Documentation Updates
*   After functional completion, review all code changes and patterns
*   Identify new or modified conventions established during implementation
*   Update relevant documentation files in `.github/instructions/`
*   Ensure new patterns are documented if used consistently

## CI/CD Pipeline Usage

The project includes a complete CI/CD pipeline with the following capabilities:

### For Pull Requests:
- Automated testing and linting via [`.github/workflows/ci.yml`](.github/workflows/ci.yml)
- GoReleaser configuration validation
- Snapshot build testing
- Status reporting back to the PR

### For Main Branch Pushes:
- Snapshot builds with commit SHA tagging
- Docker image creation and publishing to GitHub Container Registry
- Automated deployment to DigitalOcean (when configured)

### For Release Tags:
- Full release builds with semantic versioning
- Automated GitHub releases with changelogs
- Multi-architecture Docker images (Linux amd64/arm64)
- Production deployment with health checks

### Local Validation:
Use the [`test-pipeline.sh`](test-pipeline.sh) script to validate the complete pipeline setup:
```bash
./test-pipeline.sh
```

This script validates Go dependencies, runs tests, checks GoReleaser configuration, and verifies all required files are present.

### Deployment Configuration:
See [`DEPLOYMENT.md`](DEPLOYMENT.md) for detailed setup instructions including:
- GitHub repository configuration
- DigitalOcean droplet setup
- SSH key configuration
- Secret management
- Production deployment procedures

This document should help Copilot understand the project's design and assist in a way that aligns with these established patterns.
