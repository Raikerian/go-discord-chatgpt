# Go Discord ChatGPT Bot

A Discord bot built with Go and the Arikawa v3 library, with a focus on slash commands and integration with ChatGPT (to be implemented).

## Project Structure

- `cmd/discordbot/main.go`: Main application entry point.
- `internal/bot/`: Core bot logic, including session management and event handling.
- `internal/commands/`: Slash command definitions and handling.
- `internal/config/`: Configuration loading (from `config.yaml`).
- `pkg/`: Reusable packages (currently empty).
- `go.mod`: Go module definition.
- `config.yaml`: Configuration file for bot token and application ID.

## Setup

1.  **Install Go**: Make sure you have Go installed (version 1.18 or higher is recommended).
2.  **Clone the repository (if applicable)**.
3.  **Configure**: 
    *   Rename `config.example.yaml` to `config.yaml` (or create `config.yaml`).
    *   Update `config.yaml` with your Discord Bot Token and Application ID.
    ```yaml
    token: "YOUR_DISCORD_BOT_TOKEN"
    application_id: "YOUR_APPLICATION_ID"
    ```
4.  **Install Dependencies**:
    ```sh
    go get github.com/diamondburned/arikawa/v3@latest
    go get gopkg.in/yaml.v3@latest
    ```
5.  **Build the bot**:
    ```sh
    go build -o discordbot cmd/discordbot/main.go
    ```
6.  **Run the bot**:
    ```sh
    ./discordbot
    ```

## Adding New Slash Commands

1.  Create a new Go file in the `internal/commands/` directory (e.g., `mycommand.go`).
2.  Define a struct that implements the `Command` interface (from `internal/commands/command_loader.go`).
3.  Implement the `Name()`, `Description()`, `CommandData()`, and `Execute()` methods.
4.  In the `init()` function of your new command file, register the command using `commands.RegisterCommand(&MyCommand{})`.

The bot will automatically register the new command with Discord when it starts.
