---
description: 'Comprehensive developer instructions for the go-openai-realtime project - a Go SDK for OpenAI Realtime API'
---

# Go OpenAI Realtime API SDK - Developer Instructions

This document provides comprehensive instructions for working with the go-openai-realtime project, a Go SDK for OpenAI's Realtime API that enables real-time voice and text conversations with GPT models.

## Project Overview

**Repository**: `github.com/WqyJh/go-openai-realtime`  
**Purpose**: Unofficial Go client library for OpenAI Realtime API  
**Language**: Go 1.19+  
**License**: See LICENSE file  

The library provides:
- Full support for all 9 client events and 28 server events
- WebSocket-based real-time communication
- Voice and text modalities
- Function calling capabilities
- Multiple WebSocket adapter support
- Azure OpenAI compatibility

## Architecture Overview

### Core Components

1. **Client (`client.go`)**: Main entry point for API interactions
2. **Connection (`conn.go`)**: WebSocket connection management  
3. **Events**: Client events (`client_event.go`) and Server events (`server_event.go`)
4. **Configuration (`config.go`)**: API configuration and authentication
5. **WebSocket Abstraction (`ws.go`, `ws_coder.go`)**: Pluggable WebSocket implementations
6. **Types (`types.go`)**: Core data structures and enums

### Supported Models

- `gpt-4o-realtime-preview`
- `gpt-4o-realtime-preview-2024-10-01` 
- `gpt-4o-realtime-preview-2024-12-17`
- `gpt-4o-mini-realtime-preview`
- `gpt-4o-mini-realtime-preview-2024-12-17`

## Key Concepts

### Modalities
- **Text**: Text-only conversations
- **Audio**: Voice conversations (requires audio processing libraries)
- **Mixed**: Combined text and audio interactions

### Event System
- **Client Events**: Sent from client to server (9 types)
- **Server Events**: Received from server (28 types)
- **Event Handlers**: Functions that process server events

### WebSocket Adapters
- **Default**: `coder/websocket` (recommended)
- **Alternative**: `gorilla/websocket` (in contrib/)
- **Custom**: Implement `WebSocketConn` and `WebSocketDialer` interfaces

## Client Events (9 types)

1. `session.update` - Update session configuration
2. `input_audio_buffer.append` - Add audio data to buffer
3. `input_audio_buffer.commit` - Commit audio buffer to conversation
4. `input_audio_buffer.clear` - Clear audio buffer
5. `conversation.item.create` - Add item to conversation
6. `conversation.item.truncate` - Truncate conversation item
7. `conversation.item.delete` - Delete conversation item
8. `response.create` - Request model response
9. `response.cancel` - Cancel ongoing response

## Server Events (28 types)

**Session Events:**
- `session.created`, `session.updated`

**Conversation Events:**
- `conversation.created`

**Audio Buffer Events:**
- `input_audio_buffer.committed`, `input_audio_buffer.cleared`
- `input_audio_buffer.speech_started`, `input_audio_buffer.speech_stopped`

**Item Events:**
- `conversation.item.created`, `conversation.item.truncated`, `conversation.item.deleted`
- `conversation.item.input_audio_transcription.completed`
- `conversation.item.input_audio_transcription.failed`

**Response Events:**
- `response.created`, `response.done`
- `response.output_item.added`, `response.output_item.done`
- `response.content_part.added`, `response.content_part.done`
- `response.text.delta`, `response.text.done`
- `response.audio_transcript.delta`, `response.audio_transcript.done`
- `response.audio.delta`, `response.audio.done`
- `response.function_call_arguments.delta`, `response.function_call_arguments.done`

**System Events:**
- `error`, `rate_limits.updated`

## Development Guidelines

### Code Organization

```
go-openai-realtime/
├── api.go                 # HTTP API operations (session creation)
├── client.go             # Main client implementation
├── client_event.go       # Client event types and marshaling
├── server_event.go       # Server event types and unmarshaling
├── conn.go               # WebSocket connection wrapper
├── config.go             # Configuration structures
├── types.go              # Core data types and enums
├── utils.go              # Utility functions
├── ws.go                 # WebSocket interface definitions
├── ws_coder.go          # Coder WebSocket implementation
├── permanent_error.go    # Error handling utilities
├── int_or_inf.go        # Special numeric type handling
├── log.go               # Logging interfaces
├── contrib/              # Alternative implementations
│   └── ws-gorilla/      # Gorilla WebSocket adapter
└── examples/            # Usage examples
    ├── text-only/       # Text-only chat
    └── voice/           # Voice examples
        ├── text-voice/  # Text input, voice output
        └── voice-voice/ # Voice input and output
```

### Error Handling

- Use `PermanentError` for unrecoverable connection errors
- Handle temporary errors with retries in read loops
- Check error types with `errors.As()` for proper error classification

### Memory Management

- Always call `conn.Close()` to clean up connections
- Use context cancellation for proper cleanup
- Avoid goroutine leaks in event handlers

### Testing

- Unit tests for all major components (*_test.go files)
- Integration tests require `OPENAI_API_KEY` environment variable
- Use `testify` for assertions
- Mock WebSocket connections for isolated testing

## Common Usage Patterns

### Basic Text Chat

```go
client := openairt.NewClient("your-api-key")
conn, err := client.Connect(ctx)
if err != nil {
    return err
}
defer conn.Close()

// Update session for text-only
err = conn.SendMessage(ctx, &openairt.SessionUpdateEvent{
    Session: openairt.ClientSession{
        Modalities: []openairt.Modality{openairt.ModalityText},
    },
})

// Send user message
err = conn.SendMessage(ctx, &openairt.ConversationItemCreateEvent{
    Item: openairt.MessageItem{
        Type: openairt.MessageItemTypeMessage,
        Role: openairt.MessageRoleUser,
        Content: []openairt.MessageContentPart{{
            Type: openairt.MessageContentTypeInputText,
            Text: "Hello!",
        }},
    },
})

// Request response
err = conn.SendMessage(ctx, &openairt.ResponseCreateEvent{})
```

### Event Handling

```go
// Define event handlers
textHandler := func(ctx context.Context, event openairt.ServerEvent) {
    switch event.ServerEventType() {
    case openairt.ServerEventTypeResponseTextDelta:
        delta := event.(openairt.ResponseTextDeltaEvent)
        fmt.Print(delta.Delta)
    case openairt.ServerEventTypeResponseDone:
        done := event.(openairt.ResponseDoneEvent)
        fmt.Printf("\nFull response: %s\n", done.Response.Output[0].Content[0].Text)
    }
}

// Set up connection handler
connHandler := openairt.NewConnHandler(ctx, conn, textHandler)
connHandler.Start()

// Wait for completion
err := <-connHandler.Err()
```

### Audio Configuration

```go
// For voice input/output, configure audio
session := openairt.ClientSession{
    Modalities: []openairt.Modality{
        openairt.ModalityAudio, 
        openairt.ModalityText,
    },
    Voice:             openairt.VoiceAlloy,
    InputAudioFormat:  openairt.AudioFormatPcm16,
    OutputAudioFormat: openairt.AudioFormatPcm16,
    InputAudioTranscription: &openairt.InputAudioTranscription{
        Model: "whisper-1",
    },
    TurnDetection: &openairt.ClientTurnDetection{
        Type: openairt.ClientTurnDetectionTypeServerVad,
        TurnDetectionParams: openairt.TurnDetectionParams{
            Threshold:         0.5,
            PrefixPaddingMs:   300,
            SilenceDurationMs: 200,
        },
    },
}
```

### Function Calling

```go
session := openairt.ClientSession{
    Tools: []openairt.Tool{{
        Type:        openairt.ToolTypeFunction,
        Name:        "get_weather",
        Description: "Get current weather",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "location": map[string]interface{}{
                    "type": "string",
                },
            },
            "required": []string{"location"},
        },
    }},
    ToolChoice: openairt.ToolChoice{
        Type: openairt.ToolTypeFunction,
        Function: openairt.ToolFunction{Name: "get_weather"},
    },
}
```

## WebSocket Implementation

### Default (Coder WebSocket)

```go
client := openairt.NewClient("api-key")
conn, err := client.Connect(ctx) // Uses coder/websocket by default
```

### Alternative (Gorilla WebSocket)

```go
import gorilla "github.com/WqyJh/go-openai-realtime/contrib/ws-gorilla"

dialer := gorilla.NewWebSocketDialer(gorilla.WebSocketOptions{})
conn, err := client.Connect(ctx, openairt.WithDialer(dialer))
```

### Custom WebSocket Adapter

Implement these interfaces:

```go
type WebSocketConn interface {
    ReadMessage(ctx context.Context) (MessageType, []byte, error)
    WriteMessage(ctx context.Context, messageType MessageType, data []byte) error
    Close() error
    Response() *http.Response
    Ping(ctx context.Context) error
}

type WebSocketDialer interface {
    Dial(ctx context.Context, url string, header http.Header) (WebSocketConn, error)
}
```

## Azure OpenAI Support

```go
config := openairt.DefaultAzureConfig("api-key", "wss://your-resource.openai.azure.com/openai/realtime")
client := openairt.NewClientWithConfig(config)
```

## Configuration Options

### Client Configuration

```go
config := openairt.ClientConfig{
    BaseURL:    "wss://api.openai.com/v1/realtime",  // WebSocket URL
    APIBaseURL: "https://api.openai.com/v1",         // HTTP API URL
    APIType:    openairt.APITypeOpenAI,              // Or APITypeAzure
    APIVersion: "2024-10-01-preview",                // For Azure
    HTTPClient: &http.Client{},                      // Custom HTTP client
}
```

### Connection Options

```go
conn, err := client.Connect(ctx,
    openairt.WithModel("gpt-4o-realtime-preview-2024-12-17"),
    openairt.WithLogger(openairt.StdLogger{}),
    openairt.WithDialer(customDialer),
)
```

## Audio Requirements

For voice examples:
- **macOS**: `brew install pkg-config portaudio`
- **Linux**: `apt-get install portaudio19-dev`

Audio formats supported:
- `pcm16` (16-bit PCM, recommended)
- `g711_ulaw` (G.711 μ-law)
- `g711_alaw` (G.711 A-law)

## Environment Variables

- `OPENAI_API_KEY`: Required for API access
- `HTTPS_PROXY`: Optional proxy configuration (e.g., `socks5://127.0.0.1:1080`)

## Examples Directory

1. **text-only/**: Simple text-to-text chat
2. **voice/text-voice/**: Text input, voice output
3. **voice/voice-voice/**: Full voice conversation
4. **voice/audio-player/**: Audio playback utilities
5. **voice/recorder/**: Audio recording utilities

## Performance Considerations

- Use connection pooling for multiple conversations
- Implement proper buffering for audio streams
- Handle rate limits with exponential backoff
- Monitor memory usage with large audio buffers

## Security Best Practices

- Never commit API keys to version control
- Use environment variables for sensitive configuration
- Implement proper authentication for client-side usage
- Validate all user inputs before sending to API

## Debugging and Logging

Enable logging for debugging:

```go
conn, err := client.Connect(ctx, openairt.WithLogger(openairt.StdLogger{}))
```

Available log levels:
- `Debug`: Detailed protocol information
- `Info`: General operational messages  
- `Warn`: Recoverable errors
- `Error`: Serious problems

## Common Issues and Solutions

1. **Connection Timeouts**: Implement retry logic with exponential backoff
2. **Audio Buffer Overflows**: Use streaming with proper chunk sizes
3. **Event Ordering**: Process events sequentially in handlers
4. **Memory Leaks**: Ensure proper cleanup of goroutines and connections
5. **Rate Limiting**: Monitor `rate_limits.updated` events

## Contributing Guidelines

- Follow Go formatting standards (`go fmt`)
- Add unit tests for new features
- Update documentation for API changes
- Use conventional commit messages
- Ensure all tests pass before submitting PRs

## Dependencies

Core dependencies:
- `github.com/coder/websocket` - Default WebSocket implementation
- `github.com/sashabaranov/go-openai` - OpenAI API types
- `github.com/WqyJh/jsontools` - JSON utilities
- `github.com/stretchr/testify` - Testing framework

Optional:
- `github.com/gorilla/websocket` - Alternative WebSocket implementation

## Future Considerations

- Monitor OpenAI API updates for new features
- Consider gRPC support for better performance
- Implement connection pooling for enterprise usage
- Add metrics and observability features
- Support for additional audio codecs
