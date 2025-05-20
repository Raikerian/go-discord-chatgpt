<!-- filepath: /root/Dev/go-discord-chatgpt/README.md -->
# Go Discord ChatGPT Bot

A Discord bot built with Go, [Arikawa v3](https://github.com/diamondburned/arikawa), and [Uber Fx](https://github.com/uber-go/fx). It integrates with OpenAI's GPT models to provide intelligent chat functionalities via slash commands.

## Features

*   **Slash Commands**: Interacts with users through Discord's slash command interface.
*   **ChatGPT Integration**: Offers a `/chat` command to converse with OpenAI's GPT models.
*   **Configurable**: Bot behavior, API keys, and model preferences can be configured via `config.yaml`.
*   **Dependency Injection**: Uses Uber Fx for managing application components and lifecycle.
*   **Structured Logging**: Employs Zap for clear and structured logging.

## Project Structure

- `cmd/main.go`: Main application entry point, Fx setup, and lifecycle management.
- `internal/bot/`: Core bot logic, including Discord session management and interaction event handling.
- `internal/commands/`: Slash command definitions (e.g., `ping`, `version`, `chat`) and the command loading mechanism.
- `internal/config/`: Configuration loading and struct definition for `config.yaml`.
- `internal/gpt/`: OpenAI client integration and message caching.
- `go.mod`: Go module definition.
- `config.yaml`: Configuration file for Discord settings (bot token, application ID, guild IDs, interaction timeout), OpenAI settings (API key, models, message cache size), and log level.
- `.mockery.yml`: Configuration for `vektra/mockery` mock generation.

## Setup

1.  **Install Go**: Ensure you have Go installed (version 1.21 or higher is recommended).
2.  **Clone the repository**.
3.  **Configure**:
    *   Copy or rename `config.example.yaml` (if it exists) to `config.yaml`.
    *   Update `config.yaml` with your details. Minimally, you'll need:
        ```yaml
        discord:
          bot_token: "YOUR_DISCORD_BOT_TOKEN"
          application_id: "YOUR_APPLICATION_ID" # Can be a string or number
          # guild_ids: # Optional: list of guild IDs for testing commands
          #   - "YOUR_GUILD_ID_1"
          #   - "YOUR_GUILD_ID_2"
          interaction_timeout_seconds: 10 # Optional: timeout for interactions
        openai:
          api_key: "YOUR_OPENAI_API_KEY"
          # models: # Optional: list of preferred OpenAI models
          #  - "gpt-4"
          # message_cache_size: 100 # Optional: size of the LRU cache for message history
        log_level: "info" # Optional: "debug", "info", "warn", "error"
        ```
4.  **Install Dependencies**:
    ```sh
    go mod tidy
    ```
5.  **Generate Mocks (Optional, for development)**:
    If you plan to modify interfaces and need to update mocks:
    ```sh
    go generate ./...
    ```
    (This relies on `vektra/mockery` being installed. You can install it via `go install github.com/vektra/mockery/v2@latest`)
6.  **Build the bot**:
    ```sh
    go build -o discordbot cmd/main.go
    ```
7.  **Run the bot**:
    ```sh
    ./discordbot
    ```
    Alternatively, you can run directly:
    ```sh
    go run cmd/main.go
    ```

## Adding New Slash Commands

The project uses Uber Fx for dependency injection and managing commands.

1.  **Define your Command**:
    *   Create a new Go file in the `internal/commands/` directory (e.g., `mycommand.go`).
    *   Define a struct for your command.
    *   Implement the `commands.Command` interface (from `internal/commands/command.go`):
        *   `Name() string`
        *   `Description() string`
        *   `Options() []discord.ApplicationCommandOption`
        *   `Execute(ctx context.Context, data discord.CommandInteractionData) (*api.InteractionResponseData, error)`
2.  **Create a Constructor**:
    *   Write a constructor function for your command struct (e.g., `NewMyCommand(...) commands.Command`). This function should take any dependencies (like `*config.Config`, `*zap.Logger`, `*openai.Client`, etc.) as arguments. These will be provided by Fx.
3.  **Provide to Fx**:
    *   In `cmd/main.go`, add your command's constructor to the `fx.Provide` call, tagging it for the "commands" group:
        ```go
        fx.Provide(
            // ... other providers
            fx.Annotate(
                commands.NewMyCommand, // Your constructor
                fx.As(new(commands.Command)),
                fx.ResultTags(`group:"commands"`),
            ),
            // ...
        ),
        ```
The `CommandManager` will automatically discover, load, and register your command with Discord when the bot starts.
