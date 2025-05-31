# GitHub Copilot Instructions for go-discord-chatgpt

This document provides instructions and context for GitHub Copilot to effectively assist with the development of the `go-discord-chatgpt` project.

## Project Overview

`go-discord-chatgpt` is a Discord bot written in Go. It uses the Arikawa library for Discord API interaction, Uber Fx for dependency injection and application lifecycle management, and integrates with OpenAI's GPT models. The primary goal is to provide slash command functionalities, including interactions with ChatGPT, such as the `/chat` command.

## Key Technologies & Libraries

**API Documentation**: For all Go libraries and packages, use the `go doc <package>` command to access comprehensive API documentation, interfaces, types, and usage examples. This includes both standard library packages and third-party dependencies.

*   **Go**: The primary programming language.
*   **Arikawa (v3)**: Go library for the Discord API. Used for session management, event handling, and command registration.
*   **Uber Fx**: Dependency injection framework. Manages the application's lifecycle and the creation and wiring of components (services, configuration, logger, OpenAI client, cache, etc.).
*   **Zap**: Structured logging library. Used for all application logging, including Fx's internal logs.
*   **YAML V3**: For parsing the `config.yaml` file.
*   **Testify (mock, require, assert)**: For writing unit tests and making assertions.
*   **Mockery (vektra/mockery)**: For generating mock implementations of interfaces, configured via `.mockery.yml`.
*   **go-openai (sashabaranov/go-openai)**: Go client library for the OpenAI API.

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
├── models.json             # OpenAI model pricing and configuration data
├── .github/
│   ├── workflows/
│   │   ├── ci.yml          # CI pipeline for pull requests (testing, validation)
│   │   └── cd.yml          # CD pipeline for releases and deployments
│   ├── scripts/
│   │   └── deploy.sh       # Production deployment script for DigitalOcean
│   ├── instructions/
│   │   └── golang.instructions.md    # Go best practices and conventions
│   ├── prompts/                # Empty directory (reserved for future AI prompts)
│   └── copilot-instructions.md # This document - GitHub Copilot development instructions
├── main.go                  # Main application entry point, Fx setup, and lifecycle management
├── internal/
│   ├── app/
│   │   └── app.go          # Main application structure and lifecycle management
│   ├── bot/
│   │   ├── bot.go          # Core bot service, handles startup/shutdown logic, interaction event routing
│   │   ├── handlers.go     # Interaction event handlers (e.g., for slash commands and thread messages)
│   │   └── module.go       # Bot Fx module configuration
│   ├── chat/               # Modular chat service architecture with specialized components
│   │   ├── service.go      # Main chat service orchestrator that coordinates all chat components
│   │   ├── ai_provider.go  # AIProvider interface and OpenAI implementation for chat completions
│   │   ├── conversation_store.go # ConversationStore interface for managing conversation history and caching
│   │   ├── cache.go        # Cache implementations for conversation storage
│   │   ├── interaction_manager.go # DiscordInteractionManager for Discord API interactions (responses, threads, typing)
│   │   ├── model_selector.go # ModelSelector interface for choosing AI models based on user preferences
│   │   ├── summary_parser.go # SummaryParser interface for parsing thread summary messages to extract metadata
│   │   ├── title_generator.go # ThreadTitleGenerator for creating meaningful thread titles
│   │   ├── usage_formatter.go # UsageFormatter for formatting OpenAI usage information with cost calculations
│   │   ├── usage_formatter_test.go # Unit tests for UsageFormatter
│   │   ├── embed_service.go # MessageEmbedService for creating Discord embeds with usage information
│   │   ├── module.go       # Chat Fx module configuration
│   │   └── util.go         # Utility functions: GetUserDisplayName, SanitizeOpenAIName, MakeThreadName, SendLongMessage
│   ├── commands/
│   │   ├── chat.go         # Implementation of the `/chat` command, which delegates to the `chat.Service`
│   │   ├── command_loader.go # CommandManager: loads commands from Fx, registers/unregisters with Discord
│   │   ├── command_loader_test.go # Unit tests for CommandManager
│   │   ├── command.go      # Defines the `Command` interface that all slash commands must implement
│   │   ├── mocks_test.go   # Mocks generated by Mockery (e.g., for Command interface)
│   │   ├── module.go       # Commands Fx module configuration
│   │   ├── ping.go         # Implementation of the `/ping` command
│   │   └── version.go      # Implementation of the `/version` command with embedded version info
│   ├── config/
│   │   ├── config.go       # Configuration struct and loading logic
│   │   └── module.go       # Config Fx module configuration
│   ├── discord/
│   │   └── module.go       # Discord session and related Fx module configuration
│   ├── infrastructure/
│   │   └── module.go       # Infrastructure services and logging Fx module configuration
│   └── openai/
│       ├── module.go       # OpenAI client and pricing service Fx module configuration
│       └── module_test.go  # Unit tests for OpenAI module
├── pkg/
│   ├── infrastructure/
│   │   ├── logging.go      # Reusable FxLoggerAdapter for integrating Zap with Fx framework
│   │   └── logging_test.go # Unit tests for logging infrastructure
│   └── openai/
│       ├── pricing.go      # OpenAI pricing service with cost calculations and model management
│       ├── pricing_test.go # Comprehensive unit tests for pricing service
│       ├── example_test.go # Example usage tests demonstrating PricingService API
│       └── mocks.go        # Mock implementations for OpenAI services

```

## Core Architectural Decisions & Patterns

1.  **Dependency Injection with Uber Fx**:
    *   The application's components are managed by Fx using a modular architecture with dedicated modules for each layer.
    *   **Application Architecture**: The application uses a clean layered architecture:
        *   [`app.Application`](internal/app/app.go) - Main application structure with lifecycle management
        *   Infrastructure modules - Provide core services (logging, Discord session, OpenAI client)
        *   Service modules - Business logic components (chat system, commands, bot service)
    *   **Modular Structure**: Each major component has its own Fx module:
        *   [`config.Module`](internal/config/module.go) - Configuration loading
        *   [`infrastructure.LoggerModule`](internal/infrastructure/module.go) - Zap logger setup
        *   [`discord.Module`](internal/discord/module.go) - Discord session management
        *   [`openai.Module`](internal/openai/module.go) - OpenAI client and pricing service
        *   [`chat.Module`](internal/chat/module.go) - Complete chat service ecosystem
        *   [`commands.Module`](internal/commands/module.go) - Command registration and management
        *   [`bot.Module`](internal/bot/module.go) - Bot service coordination
    *   **Chat Service Components**: The chat system is modular with specialized services:
        *   [`chat.Service`](internal/chat/service.go) - Main orchestrator that coordinates all chat interactions
        *   [`chat.AIProvider`](internal/chat/ai_provider.go) - Interface for AI completions (OpenAI implementation)
        *   [`chat.ConversationStore`](internal/chat/conversation_store.go) - Manages conversation history and caching
        *   [`chat.DiscordInteractionManager`](internal/chat/interaction_manager.go) - Handles Discord API interactions
        *   [`chat.ModelSelector`](internal/chat/model_selector.go) - Selects appropriate AI models
        *   [`chat.SummaryParser`](internal/chat/summary_parser.go) - Parses thread summary messages
        *   [`chat.UsageFormatter`](internal/chat/usage_formatter.go) - Formats OpenAI usage with cost calculations
        *   [`chat.MessageEmbedService`](internal/chat/embed_service.go) - Creates Discord embeds with usage information
        *   [`chat.ThreadTitleGenerator`](internal/chat/title_generator.go) - Generates meaningful thread titles
    *   Fx handles the lifecycle (start/stop) of components with proper dependency injection and error handling.

2.  **Configuration Management**:
    *   Configuration is loaded from `config.yaml` into the `config.Config` struct ([`internal/config/config.go`](internal/config/config.go)) via the [`config.Module`](internal/config/module.go).
    *   Supports multiple configuration files: `config.yaml` (development), `config-prod.yaml` (production), and `config.example.yaml` (template).
    *   Configuration includes Discord settings (bot token, app ID, guild IDs, interaction timeout) and OpenAI settings (API key, preferred models, cache sizes, concurrency limits).
    *   The configuration path is supplied to Fx in [`main.go`](main.go) and processed by the [`config.LoadConfig`](internal/config/config.go) provider.

3.  **Logging Infrastructure**:
    *   Structured logging using Zap is provided by [`infrastructure.LoggerModule`](internal/infrastructure/module.go).
    *   The [`pkg/infrastructure`](pkg/infrastructure/) package provides reusable logging infrastructure:
        *   [`FxLoggerAdapter`](pkg/infrastructure/logging.go) - Integrates Zap with Fx framework's internal logging
        *   Comprehensive lifecycle event logging for debugging and monitoring
    *   Logger configuration adapts to environment (development/production) settings.

4.  **Command Handling Architecture**:
    *   **Interface-Based Design**: All slash commands implement the [`commands.Command`](internal/commands/command.go) interface with standardized methods.
    *   **Fx Group Management**: Commands are registered using Fx groups in [`commands.Module`](internal/commands/module.go):
        *   [`PingCommand`](internal/commands/ping.go) - Health check command
        *   [`VersionCommand`](internal/commands/version.go) - Version information with embedded build data
        *   [`ChatCommand`](internal/commands/chat.go) - AI chat integration using [`chat.Service`](internal/chat/service.go)
    *   **Registration & Lifecycle**: The [`CommandManager`](internal/commands/command_loader.go) handles Discord registration/unregistration with comprehensive error handling and lifecycle management.
    *   **Event Processing**: The [`Bot`](internal/bot/bot.go) service routes interaction events through proper handlers with context management.

5.  **Discord Session Management**:
    *   The Arikawa [`session.Session`](https://pkg.go.dev/github.com/diamondburned/arikawa/v3/session) is managed by [`discord.Module`](internal/discord/module.go).
    *   Proper lifecycle management with OnStart/OnStop hooks for session opening/closing.
    *   Intent configuration and error handling for robust connection management.

6.  **Testing & Quality Assurance**:
    *   **Testing Framework**: Uses `testify` suite with `assert`, `require`, and `mock` packages.
    *   **Mock Generation**: Automated mocks via `vektra/mockery` configured in [`.mockery.yml`](.mockery.yml).
    *   **Test Organization**: 
        *   Unit tests in `*_test.go` files following Go conventions
        *   Example tests in `pkg/openai/example_test.go` demonstrating API usage
        *   Black-box testing in separate test packages (e.g., `package commands_test`)
    *   **Test Coverage**: Comprehensive testing of public interfaces with minimal mock usage.

7.  **OpenAI Integration & Cost Management**:
    *   **Dual-Layer Architecture**: 
        *   [`internal/openai`](internal/openai/) - Internal Fx module providing OpenAI client and pricing service
        *   [`pkg/openai`](pkg/openai/) - Reusable pricing service package with comprehensive API
    *   **Pricing Service Features**:
        *   Token cost calculations with cached input support via [`PricingService`](pkg/openai/pricing.go)
        *   Model context size management and availability checking
        *   Dynamic pricing data loading from [`models.json`](models.json)
        *   Comprehensive error handling and graceful degradation
    *   **Usage Tracking**: 
        *   [`UsageFormatter`](internal/chat/usage_formatter.go) - Formats usage information with cost calculations
        *   [`MessageEmbedService`](internal/chat/embed_service.go) - Embeds cost information in Discord responses
    *   **Chat Integration**: Direct integration with [`chat.AIProvider`](internal/chat/ai_provider.go) for cost-aware AI interactions.

8.  **CI/CD Pipeline & Professional Deployment**:
    *   **GoReleaser v2**: Professional release management with multi-architecture builds (Linux amd64/arm64).
    *   **GitHub Actions Workflows**:
        *   [`.github/workflows/ci.yml`](.github/workflows/ci.yml) - Comprehensive CI with testing, linting, and validation
        *   [`.github/workflows/cd.yml`](.github/workflows/cd.yml) - Automated CD with deployment and health checks
    *   **Container Strategy**: 
        *   Development [`Dockerfile`](Dockerfile) for local development
        *   Production [`Dockerfile.goreleaser`](Dockerfile.goreleaser) optimized for releases
        *   Multi-architecture support with Alpine-based security
    *   **Deployment Infrastructure**: 
        *   GitHub Container Registry (ghcr.io) for image hosting
        *   DigitalOcean deployment with automated rollback capabilities
        *   Comprehensive health checks and monitoring
        *   Version embedding via GoReleaser ldflags for runtime reporting

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
go build -o go-discord-chatgpt main.go
```

### Running the Bot Locally
```bash
go run main.go
```

## Development Guidelines & Preferences

*   **Modularity**: Keep components decoupled and use Fx for wiring them together.
*   **Interfaces**: Use interfaces (like `commands.Command`) to define contracts between components.
*   **Structured Logging**: Use Zap for all logging. Provide context with logs where possible.
*   **Commenting**: Avoid redundant and long comments. Only comment where necessary to explain complex logic or non-obvious decisions.
*   **Error Handling**: Handle errors explicitly. Fx's lifecycle management will also report errors during startup/shutdown.
*   **Configuration-Driven**: Make behavior configurable via [`config.yaml`](config.yaml) where appropriate (e.g., guild IDs, OpenAI models, cache size, interaction timeouts).
*   **Project Management**: The project includes Taskmaster integration for structured development workflows. Configuration is maintained in `.taskmasterconfig` for AI model settings and task management preferences.
*   **Go Best Practices**: Adhere to the guidelines outlined in [`golang.instructions.md`](.github/instructions/golang.instructions.md) for consistent and high-quality Go code.
*   **CI/CD & Deployment**:
    *   Use GoReleaser v2 for professional release management and multi-architecture builds.
    *   Follow the deployment patterns established in the GitHub Actions workflows for consistent and reliable deployments.
    *   Maintain version embedding through GoReleaser ldflags for runtime version reporting.
    *   Ensure Docker images are optimized and secure using Alpine base images with non-root users.
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

This script validates Go dependencies, runs tests, checks GoReleaser configuration, and verifies all required files are present.

### Deployment Configuration:
See [`DEPLOYMENT.md`](DEPLOYMENT.md) for detailed setup instructions including:
- GitHub repository configuration
- DigitalOcean droplet setup
- SSH key configuration
- Secret management
- Production deployment procedures

This document should help Copilot understand the project's design and assist in a way that aligns with these established patterns.
