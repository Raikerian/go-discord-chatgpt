---
applyTo: '**'
---
# GitHub Copilot Prompt: Using the Arikawa Discord API Library

**Note:** File paths and examples referenced in this document link to the `v3.5.0` tag of the `github.com/diamondburned/arikawa/v3` repository, which corresponds to the version used in this project.

This document provides instructions and guidelines for using the Arikawa Discord API library in Go projects. Arikawa is a comprehensive library for interacting with the Discord API, covering REST, Gateway, and Voice.

## Core Concepts

### 1. Client and Session
- **Client (`api.Client`)**: The primary way to interact with the Discord REST API.
  - Create a new client: `api.NewClient("Bot YOUR_BOT_TOKEN")` (see [api/api.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/api.go)).
  - The client handles HTTP requests, rate limiting, and authorization.
- **Session (`session.Session`)**: Manages the WebSocket connection to the Discord Gateway for receiving real-time events. It often wraps an `api.Client`.
  - Create a new session: `session.New("Bot YOUR_BOT_TOKEN")` (see [session/session.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/session/session.go)).
- **State (`state.State`)**: A wrapper around `session.Session` that includes an in-memory cache (`state.Cabinet`) for Discord entities like guilds, channels, users, roles, and presences. This is generally recommended for most bots.
  - Create a new state: `s := state.New("Bot " + token)` (as seen in [0-examples/autocomplete/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/autocomplete/main.go)).

### 2. Intents (`gateway.Intents`)
- Discord requires bots to specify which events they want to receive through Gateway Intents.
- Common intents include:
  - [`gateway.IntentGuilds`](https://github.com/diamondburned/arikawa/blob/v3.5.0/gateway/intents.go)
  - [`gateway.IntentGuildMessages`](https://github.com/diamondburned/arikawa/blob/v3.5.0/gateway/intents.go)
  - [`gateway.IntentDirectMessages`](https://github.com/diamondburned/arikawa/blob/v3.5.0/gateway/intents.go)
  - [`gateway.IntentGuildMembers`](https://github.com/diamondburned/arikawa/blob/v3.5.0/gateway/intents.go) (privileged)
  - [`gateway.IntentGuildPresences`](https://github.com/diamondburned/arikawa/blob/v3.5.0/gateway/intents.go) (privileged)
  - [`gateway.IntentMessageContent`](https://github.com/diamondburned/arikawa/blob/v3.5.0/gateway/intents.go) (privileged, required for reading message content beyond mentions and commands)
- Add intents to your session/state: `s.AddIntents(gateway.IntentGuilds | gateway.IntentGuildMessages)` (as seen in [0-examples/buttons/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/buttons/main.go)).
- For voice functionality, use `voice.AddIntents(s)` which adds `gateway.IntentGuilds | gateway.IntentGuildVoiceStates` (as seen in [0-examples/voice/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/voice/main.go) and defined in [voice/voice.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/voice/voice.go)).
- List of intents: [gateway/intents.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/gateway/intents.go).

### 3. Connecting to the Gateway
- After creating a session/state and adding intents, connect to Discord:
  `err := s.Open(context.Background())` or `s.Connect(context.TODO())` (as seen in [0-examples/buttons/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/buttons/main.go) and [README.md](https://github.com/diamondburned/arikawa/blob/v3.5.0/README.md)).
- Remember to `defer s.Close()`.

## Event Handling
- Add event handlers to the session/state using `s.AddHandler(func(event *gateway.EventType) { ... })`.
- Example: Handling message creation:
  ```go
  // filepath: main.go
  // ...existing code...
  s.AddHandler(func(e *gateway.MessageCreateEvent) {
      if e.Author.Bot {
          return
      }
      log.Printf("Received message from %s: %s", e.Author.Username, e.Content)
      // Respond to the message
      s.SendMessage(e.ChannelID, "Hello, "+e.Author.Username)
  })
  // ...existing code...
  ```
- Example: Handling interaction creation (for slash commands, buttons, etc.):
  `s.AddHandler(func(e *gateway.InteractionCreateEvent) { ... })` (as seen in [0-examples/buttons/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/buttons/main.go)).

## Application Commands (Slash Commands)

### 1. Defining Commands
- Commands are defined using `api.CreateCommandData` structs.
  ```go
  // filepath: main.go
  // ...existing code...
  var commands = []api.CreateCommandData{
      {
          Name:        "ping",
          Description: "Responds with Pong!",
      },
      {
          Name:        "echo",
          Description: "Echoes your input.",
          Options: []discord.CommandOption{
              &discord.StringOption{
                  OptionName:  "message",
                  Description: "The message to echo.",
                  Required:    true,
              },
          },
      },
  }
  // ...existing code...
  ```
  (See [0-examples/commands/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/commands/main.go) and [`api.CreateCommandData`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/application.go))
- Command options can be various types like `discord.StringOption`, `discord.IntegerOption`, `discord.UserOption`, etc. (defined in [discord/command.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/command.go)).
- `DefaultMemberPermissions` can restrict command usage.
- `NoDMPermission` can disable command usage in DMs.

### 2. Registering/Overwriting Commands
- Get your application ID: `app, err := s.CurrentApplication()` ([`api.Client.CurrentApplication`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/application.go), example in [0-examples/buttons/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/buttons/main.go)).
- **Globally**: `err := cmdroute.OverwriteCommands(s, commands)` (example in [README.md](https://github.com/diamondburned/arikawa/blob/v3.5.0/README.md)). This updates commands for all guilds and can take up to an hour to propagate.
- **For a specific guild (recommended for testing)**: `s.BulkOverwriteGuildCommands(app.ID, guildID, commands)` ([`api.Client.BulkOverwriteGuildCommands`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/application.go)).
  - Example: `s.BulkOverwriteGuildCommands(app.ID, guildID, newCommands)` (from [0-examples/autocomplete/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/autocomplete/main.go)).
- Individual command management:
  - `s.CreateCommand(appID, data)`
  - `s.EditCommand(appID, commandID, data)`
  - `s.DeleteCommand(appID, commandID)`
  - `s.CreateGuildCommand(appID, guildID, data)`
  - `s.EditGuildCommand(appID, guildID, commandID, data)`
  - `s.DeleteGuildCommand(appID, guildID, commandID)`
  (All available in [api/application.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/application.go))

### 3. Handling Command Interactions
- Use `cmdroute.Router` for structured command handling.
  ```go
  // filepath: main.go
  import (
      "context"
      "log"
      "github.com/diamondburned/arikawa/v3/api"
      "github.com/diamondburned/arikawa/v3/api/cmdroute"
      "github.com/diamondburned/arikawa/v3/discord"
      "github.com/diamondburned/arikawa/v3/gateway"
      "github.com/diamondburned/arikawa/v3/state"
      "github.com/diamondburned/arikawa/v3/utils/json/option"
  )

  type Handler struct {
      rt *cmdroute.Router
  }

  func NewHandler(s *state.State) *Handler {
      r := cmdroute.NewRouter()
      // Optional: Add middleware
      // r.Use(cmdroute.Deferrable(s, cmdroute.DeferOpts{}))
      h := &Handler{rt: r}
      r.AddFunc("ping", h.cmdPing)
      r.AddFunc("echo", h.cmdEcho)
      return h
  }

  func (h *Handler) OnInteraction(s *state.State, ev *gateway.InteractionCreateEvent) {
      resp := h.rt.HandleInteraction(ev) // ev is *discord.InteractionEvent
      if resp != nil {
          if err := s.RespondInteraction(ev.ID, ev.Token, *resp); err != nil {
              log.Println("Failed to send interaction response:", err)
          }
      }
  }

  func (h *Handler) cmdPing(ctx context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
      return &api.InteractionResponseData{
          Content: option.NewNullableString("Pong!"),
      }
  }

  func (h *Handler) cmdEcho(ctx context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
      var options struct {
          Message string `discord:"message"` // Tag matches OptionName
      }
      if err := data.Options.Unmarshal(&options); err != nil {
          return &api.InteractionResponseData{
              Content: option.NewNullableString("Error: " + err.Error()),
              Flags:   discord.EphemeralMessage,
          }
      }
      return &api.InteractionResponseData{
          Content: option.NewNullableString(options.Message),
      }
  }
  ```
  (Inspired by [0-examples/commands/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/commands/main.go), router in [api/cmdroute/router.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/cmdroute/router.go))
- The [`cmdroute.CommandData`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/cmdroute/fntypes.go) struct contains `Event` (the `*discord.InteractionEvent`) and `Options` (`discord.CommandInteractionOptions`).
- Parse options using `data.Options.Unmarshal(&yourStruct)` where struct fields are tagged with `discord:"option_name"`. (Unmarshalling details in [discord/interaction.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/interaction.go)).

## Interactions (Buttons, Select Menus, Modals, Autocomplete)

### 1. Responding to Interactions
- All interactions (slash commands, button clicks, select menu choices, modal submissions, autocomplete requests) trigger a `gateway.InteractionCreateEvent`.
- Respond using `s.RespondInteraction(interactionID, interactionToken, response)`.
- [`api.InteractionResponse`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/interaction.go) has a `Type` and `Data`.
  - `Type`: e.g., `api.MessageInteractionWithSource`, `api.DeferredMessageInteractionWithSource`, `api.UpdateMessage`, `api.AutocompleteResult`.
  - `Data`: `*api.InteractionResponseData` containing content, embeds, components, flags, etc.
  (Defined in [api/interaction.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/interaction.go))
- Example for button click:
  ```go
  // filepath: main.go
  // ...existing code...
  // In s.AddHandler(func(e *gateway.InteractionCreateEvent) { ... })
  var resp api.InteractionResponse
  switch data := e.Data.(type) {
  case *discord.CommandInteraction:
      // ... handle slash command ...
      // Send a message with a button:
      resp = api.InteractionResponse{
          Type: api.MessageInteractionWithSource,
          Data: &api.InteractionResponseData{
              Content: option.NewNullableString("Click the button!"),
              Components: &discord.ContainerComponents{
                  &discord.ActionRowComponent{
                      &discord.ButtonComponent{
                          Label:    "Click Me",
                          Style:    discord.PrimaryButtonStyle,
                          CustomID: "my_button_id",
                      },
                  },
              },
          },
      }
  case discord.ComponentInteraction: // Covers buttons and select menus
      customID := data.ID() // This is discord.ComponentCustomID
      if customID == "my_button_id" {
          resp = api.InteractionResponse{
              Type: api.UpdateMessage, // Or MessageInteractionWithSource for a new message
              Data: &api.InteractionResponseData{
                  Content: option.NewNullableString("Button clicked! Custom ID: " + string(customID)),
              },
          }
      }
  // ... other interaction types
  }
  if err := s.RespondInteraction(e.ID, e.Token, resp); err != nil {
      log.Println("failed to send interaction callback:", err)
  }
  // ...existing code...
  ```
  (Inspired by [0-examples/buttons/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/buttons/main.go), components in [discord/component.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/component.go))

### 2. Autocomplete
- Define a command option with `Autocomplete: true`.
  ```go
  // filepath: main.go
  // ...existing code...
  &discord.StringOption{
      OptionName:   "query",
      Description:  "Search query",
      Autocomplete: true,
      Required:     true,
  }
  // ...existing code...
  ```
- Handle `*discord.AutocompleteInteraction` in your `InteractionCreateEvent` handler or use [`cmdroute.Router.AddAutocompleterFunc`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/cmdroute/router.go).
  ```go
  // filepath: main.go
  // ...existing code...
  // Using InteractionCreateEvent handler:
  case *discord.AutocompleteInteraction:
      focusedOption := d.Focused() // discord.AutocompleteOption
      // inputValue := focusedOption.Value // This is a json.RawMessage, needs unmarshalling or .String()
      var choices api.AutocompleteChoices
      // Generate choices based on focusedOption.Name and focusedOption.String()
      if focusedOption.Name == "query" {
          queryString := strings.ToLower(focusedOption.String())
          var stringChoices api.AutocompleteStringChoices
          // ... logic to find matching choices ...
          if strings.HasPrefix("apple", queryString) {
              stringChoices = append(stringChoices, discord.StringChoice{Name: "Apple", Value: "apple_id"})
          }
          choices = stringChoices
      }
      resp = api.InteractionResponse{
          Type: api.AutocompleteResult,
          Data: &api.InteractionResponseData{Choices: choices},
      }
  // ...existing code...
  ```
  (Inspired by [0-examples/autocomplete/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/autocomplete/main.go), [`api.AutocompleteChoices`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/interaction.go) types)

### 3. Follow-up Messages
- After an initial response (including deferred), send follow-up messages using `s.FollowUpInteraction(appID, interactionToken, data)` ([`api.Client.FollowUpInteraction`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/interaction.go)).
- Edit the original response: `s.EditInteractionResponse(appID, interactionToken, data)` ([`api.Client.EditInteractionResponse`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/interaction.go)).
- Delete the original response: `s.DeleteInteractionResponse(appID, interactionToken)` ([`api.Client.DeleteInteractionResponse`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/interaction.go)).

## Sending Messages

### 1. Basic Messages
- `s.SendMessage(channelID, content, embeds...)` ([`api.Client.SendMessage`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/message.go)).
  - `embeds` is an optional variadic argument of `discord.Embed`.
- `s.SendMessageReply(channelID, content, referenceMessageID, embeds...)` to reply to a message ([`api.Client.SendMessageReply`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/message.go)).

### 2. Complex Messages (`api.SendMessageData`)
- For more control (files, components, allowed mentions, TTS, nonce, flags):
  ```go
  // filepath: main.go
  import (
      "os"
      "github.com/diamondburned/arikawa/v3/api"
      "github.com/diamondburned/arikawa/v3/discord"
      "github.com/diamondburned/arikawa/v3/utils/sendpart"
  )
  // ...existing code...
  file, _ := os.Open("image.png") // Handle error appropriately
  defer file.Close()

  data := api.SendMessageData{
      Content: "Here's a message with an embed and a file!",
      Embeds: []discord.Embed{
          {
              Title:       "My Embed",
              Description: "This is a cool embed.",
              Color:       0x00FF00, // Green
          },
      },
      Files: []sendpart.File{
          {Name: "image.png", Reader: file},
      },
      AllowedMentions: &api.AllowedMentions{ /* configure as needed */ },
      // Components: &discord.ContainerComponents{ /* add buttons, etc. */ },
  }
  msg, err := s.SendMessageComplex(channelID, data)
  // ...existing code...
  ```
  ([`api.SendMessageData`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/send.go), [`api.Client.SendMessageComplex`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/send.go))

### 3. Embeds (`discord.Embed`)
- Struct for rich message content.
- Key fields: `Title`, `Description`, `URL`, `Timestamp`, `Color`, `Footer`, `Image`, `Thumbnail`, `Author`, `Fields`.
- Validate embeds using `embed.Validate()` before sending. Total length of text in an embed must not exceed 6000 characters. (Struct in [discord/embed.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/embed.go))

### 4. Allowed Mentions (`api.AllowedMentions`)
- Control which mentions (users, roles, @everyone) are parsed and ping users.
- `Parse`: `[]api.AllowedMentionType` (e.g., `api.AllowUserMention`, `api.AllowRoleMention`, `api.AllowEveryoneMention`).
- `Users`: `[]discord.UserID` (allowlist of users to ping).
- `Roles`: `[]discord.RoleID` (allowlist of roles to ping).
- `RepliedUser`: `option.Bool` (whether to ping the user being replied to).
- Call `am.Verify()` to check constraints. (Struct and constants in [api/send.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/send.go))

### 5. Editing and Deleting Messages
- `s.EditMessage(channelID, messageID, newContent, newEmbeds...)` ([`api.Client.EditText`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/message.go) or [`api.Client.EditEmbeds`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/message.go))
- `s.EditMessageComplex(channelID, messageID, api.EditMessageData{...})` ([`api.Client.EditMessageComplex`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/message.go))
- `s.DeleteMessage(channelID, messageID, reason)` ([`api.Client.DeleteMessage`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/message.go))
- `s.BulkDeleteMessages(channelID, messageIDs, reason)` ([`api.Client.DeleteMessages`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/message.go))

## Guild Management
- `s.Guild(guildID)`: Get guild by ID ([`api.Client.Guild`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/guild.go)).
- `s.GuildsBefore(limit, beforeID)`, `s.GuildsAfter(limit, afterID)`, `s.GuildsRange(limit, beforeID, afterID)`: Get guilds the bot is in.
- `s.CreateGuild(api.CreateGuildData{...})` ([`api.Client.CreateGuild`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/guild.go))
- `s.ModifyGuild(guildID, api.ModifyGuildData{...})` ([`api.Client.ModifyGuild`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/guild.go))
- `s.DeleteGuild(guildID)` ([`api.Client.DeleteGuild`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/guild.go))
- `s.GuildPreview(guildID)` ([`api.Client.GuildPreview`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/guild.go))
- ([`discord.Guild`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/guild.go) and [`discord.GuildPreview`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/guild.go) structs)

## Channel Management
- `s.Channels(guildID)`: Get channels in a guild ([`api.Client.Channels`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/channel.go)).
- `s.Channel(channelID)`: Get channel by ID ([`api.Client.Channel`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/channel.go)).
- `s.CreateChannel(guildID, api.CreateChannelData{...})` ([`api.Client.CreateChannel`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/channel.go))
- `s.ModifyChannel(channelID, api.ModifyChannelData{...})` ([`api.Client.ModifyChannel`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/channel.go))
- `s.DeleteChannel(channelID, reason)` ([`api.Client.DeleteChannel`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/channel.go))
- `s.EditChannelPermission(channelID, overwriteID, api.EditChannelPermissionData{...})` ([`api.Client.EditChannelPermission`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/channel.go))
- `s.Typing(channelID)`: Send typing indicator ([`api.Client.Typing`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/channel.go)).
- `s.PinnedMessages(channelID)`, `s.PinMessage(...)`, `s.UnpinMessage(...)` ([api/channel.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/channel.go))
- ([`discord.Channel`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/channel.go) struct)

## User Management
- `s.User(userID)`: Get user by ID ([`api.Client.User`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/user.go)).
- `s.Me()`: Get current bot user ([`api.Client.Me`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/user.go)).
- `s.ModifyCurrentUser(api.ModifyCurrentUserData{...})` ([`api.Client.ModifyCurrentUser`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/user.go))
- `s.CreatePrivateChannel(recipientID)`: Create a DM channel ([`api.Client.CreatePrivateChannel`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/user.go)).
- ([`discord.User`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/user.go) struct)

## Role Management
- `s.Roles(guildID)`: Get roles in a guild ([`api.Client.Roles`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/role.go)).
- `s.CreateRole(guildID, api.CreateRoleData{...})` ([`api.Client.CreateRole`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/role.go))
- `s.ModifyRole(guildID, roleID, api.ModifyRoleData{...})` ([`api.Client.ModifyRole`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/role.go))
- `s.DeleteRole(guildID, roleID, reason)` ([`api.Client.DeleteRole`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/role.go))
- `s.AddRole(guildID, userID, roleID, reason)` ([`api.Client.AddRole`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/member.go))
- `s.RemoveRole(guildID, userID, roleID, reason)` ([`api.Client.RemoveRole`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/member.go))
- ([`discord.Role`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/role.go) struct)

## Permissions (`discord.Permissions`)
- Bitwise flags representing permissions.
- Constants like `discord.PermissionSendMessages`, `discord.PermissionAdministrator`, etc.
- Check permissions: `perms.Has(discord.PermissionSendMessages)`
- Combine permissions: `discord.PermissionSendMessages | discord.PermissionEmbedLinks`
- (Defined in [discord/permission.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/permission.go))

## Voice (`voice` package)
- **Intents**: `voice.AddIntents(s)` is crucial ([`voice.AddIntents`](https://github.com/diamondburned/arikawa/blob/v3.5.0/voice/voice.go)).
- **Joining a channel**:
  ```go
  // filepath: main.go
  import (
      "context"
      "github.com/diamondburned/arikawa/v3/discord"
      "github.com/diamondburned/arikawa/v3/state"
      "github.com/diamondburned/arikawa/v3/voice"
  )

  // Assuming 's' is your *state.State and 'vChID' is discord.ChannelID
  func joinVoice(ctx context.Context, s *state.State, vChID discord.ChannelID) (*voice.Session, error) {
      vs, err := voice.NewSession(s) // or voice.NewSessionWithGateway(s.Gateway())
      if err != nil {
          return nil, err
      }
      // These handlers are automatically added by NewSession if state.State is used.
      // If using a raw gateway, you might need:
      // s.AddHandler(vs.UpdateVoiceState)
      // s.AddHandler(vs.UpdateVoiceServer)

      err = vs.JoinChannel(ctx, vChID, false, false) // mute, deaf
      if err != nil {
          return nil, err
      }
      return vs, nil
  }
  // Somewhere later: defer vs.Leave(context.Background())
  ```
  ([`voice.Session`](https://github.com/diamondburned/arikawa/blob/v3.5.0/voice/session.go), example in [0-examples/voice/main.go](https://github.com/diamondburned/arikawa/blob/v3.5.0/0-examples/voice/main.go))
- **Sending Audio**: Requires an Opus stream. The library provides helpers for this, often involving `voice.NewMediaSession()` and sending Opus packets. Refer to `0-examples/voice/main.go` for a detailed example of playing an audio file.
- Key components:
  - `voice.Session`: Manages a voice connection.
  - `voice.NewSession(state *state.State)` or `voice.NewSessionWithGateway(g *gateway.Gateway)`.
  - `vs.Speaking(ctx, voice.MicrophoneSource)` before sending audio.
- The `voice` package handles the voice gateway connection, UDP, and encryption. (See [voice/README.md](https://github.com/diamondburned/arikawa/blob/v3.5.0/voice/README.md))

## Error Handling
- API calls return an `error`. Check `err != nil`.
- Discord API errors are often `*httputil.HTTPError`. You can type-assert to inspect status codes and error messages from Discord.
  ```go
  // filepath: main.go
  import (
      "errors"
      "log"
      "github.com/diamondburned/arikawa/v3/utils/httputil"
  )
  // ...existing code...
  // _, err := s.SendMessage(channelID, "Test")
  // if err != nil {
  //     var httpErr *httputil.HTTPError
  //     if errors.As(err, &httpErr) {
  //         log.Printf("Discord API Error: Status %d, Code %d, Message: %s",
  //             httpErr.Status(), httpErr.ErrorCode, httpErr.ErrorMessage())
  //     } else {
  //         log.Println("Generic error:", err)
  //     }
  // }
  // ...existing code...
  ```
- Specific errors like `api.ErrEmptyMessage` ([`api.ErrEmptyMessage`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/send.go)) or `discord.OverboundError` ([`discord.OverboundError`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/discord.go)) may be returned.

## Important Data Structures
- `discord.Snowflake`: Base type for IDs (User, Channel, Guild, Message, etc.).
- [`discord.Message`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/message.go): Represents a Discord message.
- [`discord.User`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/user.go): Represents a Discord user.
- [`discord.Channel`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/channel.go): Represents a Discord channel.
- [`discord.Guild`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/guild.go): Represents a Discord guild.
- [`discord.Role`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/role.go): Represents a Discord role.
- [`discord.InteractionEvent`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/interaction.go): Base for interaction data.
- [`discord.CommandInteraction`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/interaction.go), [`discord.ComponentInteraction`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/interaction.go), [`discord.AutocompleteInteraction`](https://github.com/diamondburned/arikawa/blob/v3.5.0/discord/interaction.go): Specific interaction data types.
- `api.*Data` structs for API call parameters (e.g., [`api.SendMessageData`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/send.go), [`api.CreateCommandData`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/application.go), [`api.ModifyGuildData`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/guild.go)).
- `option.NullableString`, `option.Uint`, etc. for optional JSON fields (from `github.com/diamondburned/arikawa/v3/utils/json/option`).

## General Tips
- Refer to the `0-examples/` directory in the arikawa repository for practical usage examples of various features ([0-examples/](https://github.com/diamondburned/arikawa/tree/v3.5.0/0-examples/)).
- The official Discord API documentation is your best friend for understanding endpoints, parameters, and limitations. Arikawa often links to relevant docs in its comments.
- Use `context.Context` for request cancellation and timeouts, e.g., `s.WithContext(ctx).SendMessage(...)` ([`api.Client.WithContext`](https://github.com/diamondburned/arikawa/blob/v3.5.0/api/api.go)).
- Be mindful of rate limits. Arikawa's client handles them automatically, but understanding them can help design more efficient bots.

This prompt should guide Copilot in assisting with Go projects using the Arikawa library.