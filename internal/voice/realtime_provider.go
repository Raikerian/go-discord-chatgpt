package voice

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	openairt "github.com/WqyJh/go-openai-realtime"
	"github.com/sashabaranov/go-openai"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
)

type RealtimeProvider interface {
	// Establish connection to OpenAI Realtime
	Connect(ctx context.Context, model string) (*RealtimeConnection, error)

	// Send audio for processing (audio must be base64 encoded)
	SendAudio(ctx context.Context, audioBase64 string) error

	// Commit audio buffer (triggers processing)
	CommitAudio(ctx context.Context) error

	// Generate response from committed audio
	GenerateResponse(ctx context.Context) error

	// Receive AI response through event handlers
	SetResponseHandlers(handlers ResponseHandlers) error

	// Configure session with modalities and voice
	ConfigureSession(config SessionConfig) error

	// Close connection
	Close() error
}

type RealtimeConnection struct {
	Connected bool
	Model     string
	SessionID string
}

type SessionConfig struct {
	Modalities              []string // ["text", "audio"]
	Voice                   string   // e.g., "shimmer"
	OutputAudioFormat       string   // "pcm16"
	InputAudioTranscription bool     // Enable Whisper transcription
	VADMode                 string   // "server_vad" or "none"
}

type AudioResponse struct {
	Audio      []byte
	Text       string // Transcript of AI response
	TokenUsage *Usage
}

type Usage struct {
	InputTokens       int
	OutputTokens      int
	InputAudioTokens  int
	OutputAudioTokens int
}

type ResponseHandlers struct {
	OnAudioDelta     func(ctx context.Context, audioData []byte)
	OnTranscript     func(ctx context.Context, transcript string) // AI response transcript
	OnUserTranscript func(ctx context.Context, transcript string) // User input transcript
	OnResponseDone   func(ctx context.Context, usage *Usage)
	OnError          func(ctx context.Context, err error)
}

type openAIRealtimeProvider struct {
	logger     *zap.Logger
	cfg        *config.VoiceConfig
	apiKey     string
	connection *RealtimeConnection
	handlers   ResponseHandlers
	client     *openairt.Client
	conn       *openairt.Conn
	handler    *openairt.ConnHandler
}

func NewRealtimeProvider(logger *zap.Logger, cfg *config.Config) RealtimeProvider {
	voiceCfg := &cfg.Voice

	// Use separate realtime API key if provided, otherwise use main OpenAI key
	apiKey := voiceCfg.RealtimeAPIKey
	if apiKey == "" {
		apiKey = cfg.OpenAI.APIKey
	}

	// Create OpenAI Realtime client
	client := openairt.NewClient(apiKey)

	return &openAIRealtimeProvider{
		logger: logger,
		cfg:    voiceCfg,
		apiKey: apiKey,
		client: client,
	}
}

func (p *openAIRealtimeProvider) Connect(ctx context.Context, model string) (*RealtimeConnection, error) {
	if p.connection != nil && p.connection.Connected {
		return p.connection, nil
	}

	p.logger.Info("Connecting to OpenAI Realtime API",
		zap.String("model", model))

	// Establish WebSocket connection
	conn, err := p.client.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to OpenAI Realtime: %w", err)
	}

	p.conn = conn

	// Create handler with event handling function
	p.handler = openairt.NewConnHandler(ctx, conn, p.handleServerEvent)

	// Start the handler to begin processing events
	go p.handler.Start()

	connection := &RealtimeConnection{
		Connected: true,
		Model:     model,
		SessionID: "session-" + model,
	}

	p.connection = connection

	// Configure the session with default settings
	sessionConfig := SessionConfig{
		Modalities:              []string{"text", "audio"},
		Voice:                   p.cfg.VoiceProfile,
		OutputAudioFormat:       "pcm16",
		InputAudioTranscription: true,
		VADMode:                 p.cfg.VADMode,
	}

	err = p.ConfigureSession(sessionConfig)
	if err != nil {
		p.Close()

		return nil, fmt.Errorf("failed to configure session: %w", err)
	}

	p.logger.Info("Connected to OpenAI Realtime API",
		zap.String("model", model),
		zap.String("session_id", connection.SessionID))

	return connection, nil
}

func (p *openAIRealtimeProvider) SendAudio(ctx context.Context, audioBase64 string) error {
	if p.connection == nil || !p.connection.Connected {
		return errors.New("not connected to OpenAI Realtime API")
	}

	p.logger.Info("Sending audio to OpenAI",
		zap.Int("audio_size", len(audioBase64)))

	// Create and send InputAudioBufferAppendEvent
	event := &openairt.InputAudioBufferAppendEvent{
		Audio: audioBase64,
	}

	return p.conn.SendMessage(ctx, event)
}

func (p *openAIRealtimeProvider) CommitAudio(ctx context.Context) error {
	if p.connection == nil || !p.connection.Connected {
		return errors.New("not connected to OpenAI Realtime API")
	}

	p.logger.Info("Committing audio buffer to OpenAI")

	// Create and send InputAudioBufferCommitEvent
	event := &openairt.InputAudioBufferCommitEvent{}

	return p.conn.SendMessage(ctx, event)
}

func (p *openAIRealtimeProvider) GenerateResponse(ctx context.Context) error {
	if p.connection == nil || !p.connection.Connected {
		return errors.New("not connected to OpenAI Realtime API")
	}

	p.logger.Info("Requesting response generation from OpenAI")

	// Create and send ResponseCreateEvent to trigger response generation
	event := &openairt.ResponseCreateEvent{
		Response: openairt.ResponseCreateParams{
			Modalities: []openairt.Modality{openairt.ModalityText, openairt.ModalityAudio},
		},
	}

	return p.conn.SendMessage(ctx, event)
}

func (p *openAIRealtimeProvider) SetResponseHandlers(handlers ResponseHandlers) error {
	p.handlers = handlers

	p.logger.Debug("Response handlers configured")

	return nil
}

func (p *openAIRealtimeProvider) ConfigureSession(config SessionConfig) error {
	if p.connection == nil || !p.connection.Connected {
		return errors.New("not connected to OpenAI Realtime API")
	}

	p.logger.Info("Configuring OpenAI session",
		zap.Strings("modalities", config.Modalities),
		zap.String("voice", config.Voice),
		zap.String("output_format", config.OutputAudioFormat),
		zap.Bool("transcription", config.InputAudioTranscription),
		zap.String("vad_mode", config.VADMode))

	// Convert our config to the library's format
	modalities := make([]openairt.Modality, len(config.Modalities))
	for i, mod := range config.Modalities {
		switch mod {
		case "text":
			modalities[i] = openairt.ModalityText
		case "audio":
			modalities[i] = openairt.ModalityAudio
		}
	}

	// Convert voice profile
	var voice openairt.Voice
	switch config.Voice {
	case "shimmer":
		voice = openairt.VoiceShimmer
	case "alloy":
		voice = openairt.VoiceAlloy
	case "echo":
		voice = openairt.VoiceEcho
	default:
		voice = openairt.VoiceShimmer
	}

	// Create session update event
	sessionUpdate := &openairt.SessionUpdateEvent{
		Session: openairt.ClientSession{
			Modalities:        modalities,
			Voice:             voice,
			OutputAudioFormat: openairt.AudioFormatPcm16,
			InputAudioTranscription: &openairt.InputAudioTranscription{
				Model: openai.Whisper1,
			},
		},
	}

	// Configure VAD mode if not using server VAD
	if config.VADMode != "server_vad" {
		sessionUpdate.Session.TurnDetection = nil // Disable server-side turn detection
	}

	return p.conn.SendMessage(context.Background(), sessionUpdate)
}

func (p *openAIRealtimeProvider) Close() error {
	if p.connection == nil {
		return nil
	}

	p.logger.Info("Closing OpenAI Realtime connection",
		zap.String("session_id", p.connection.SessionID))

	// Close the WebSocket connection
	if p.conn != nil {
		err := p.conn.Close()
		if err != nil {
			p.logger.Warn("Error closing connection", zap.Error(err))
		}
		p.conn = nil
	}

	p.handler = nil
	p.connection.Connected = false
	p.connection = nil

	return nil
}

// handleServerEvent handles incoming server events from the WebSocket.
func (p *openAIRealtimeProvider) handleServerEvent(ctx context.Context, event openairt.ServerEvent) {
	p.logger.Debug("Received server event",
		zap.String("event_type", string(event.ServerEventType())))

	switch event.ServerEventType() {
	case openairt.ServerEventTypeResponseAudioDelta:
		delta := event.(openairt.ResponseAudioDeltaEvent)
		if p.handlers.OnAudioDelta != nil && delta.Delta != "" {
			// Decode base64 audio data
			audioData, err := base64.StdEncoding.DecodeString(delta.Delta)
			if err != nil {
				p.logger.Error("Failed to decode audio delta", zap.Error(err))

				return
			}
			p.logger.Debug("Received audio delta from OpenAI",
				zap.Int("audio_size", len(audioData)))
			p.handlers.OnAudioDelta(ctx, audioData)
		}

	case openairt.ServerEventTypeResponseAudioTranscriptDone:
		transcript := event.(openairt.ResponseAudioTranscriptDoneEvent)
		if p.handlers.OnTranscript != nil {
			p.logger.Debug("Received AI transcript from OpenAI",
				zap.String("transcript", transcript.Transcript))
			p.handlers.OnTranscript(ctx, transcript.Transcript)
		}

	case openairt.ServerEventTypeConversationItemInputAudioTranscriptionCompleted:
		inputTranscript := event.(openairt.ConversationItemInputAudioTranscriptionCompletedEvent)
		if p.handlers.OnUserTranscript != nil {
			p.logger.Debug("Received user transcript from OpenAI",
				zap.String("transcript", inputTranscript.Transcript),
				zap.String("item_id", inputTranscript.ItemID))
			p.handlers.OnUserTranscript(ctx, inputTranscript.Transcript)
		}

	case openairt.ServerEventTypeConversationItemInputAudioTranscriptionFailed:
		failedTranscript := event.(openairt.ConversationItemInputAudioTranscriptionFailedEvent)
		p.logger.Warn("User audio transcription failed",
			zap.String("item_id", failedTranscript.ItemID),
			zap.String("error", failedTranscript.Error.Message))

	case openairt.ServerEventTypeResponseDone:
		done := event.(openairt.ResponseDoneEvent)
		if p.handlers.OnResponseDone != nil && done.Response.Usage != nil {
			usage := &Usage{
				InputTokens:       done.Response.Usage.InputTokens,
				OutputTokens:      done.Response.Usage.OutputTokens,
				InputAudioTokens:  done.Response.Usage.InputTokenDetails.AudioTokens,
				OutputAudioTokens: done.Response.Usage.OutputTokenDetails.AudioTokens,
			}
			p.logger.Info("Response completed",
				zap.Int("input_tokens", usage.InputTokens),
				zap.Int("output_tokens", usage.OutputTokens),
				zap.Int("input_audio_tokens", usage.InputAudioTokens),
				zap.Int("output_audio_tokens", usage.OutputAudioTokens))
			p.handlers.OnResponseDone(ctx, usage)
		}

	case openairt.ServerEventTypeError:
		errorEvent := event.(openairt.ErrorEvent)
		if p.handlers.OnError != nil {
			p.handlers.OnError(ctx, fmt.Errorf("OpenAI error: %s", errorEvent.Error.Message))
		}
	}
}
