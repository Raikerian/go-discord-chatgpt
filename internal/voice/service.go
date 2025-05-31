package voice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/pkg/openai"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"
	"go.uber.org/zap"
)

type Service struct {
	logger         *zap.Logger
	cfg            *config.VoiceConfig
	discordSession *session.Session
	pricingService openai.PricingService

	voiceManager     DiscordVoiceManager
	audioProcessor   AudioProcessor
	realtimeProvider RealtimeProvider
	sessionManager   SessionManager
	audioMixer       AudioMixer

	// activeSessions stores currently active voice sessions by guild ID
	activeSessions sync.Map // map[discord.GuildID]*VoiceSession

	// watchdogCancel for stopping the watchdog goroutine
	watchdogCancel context.CancelFunc
}

type VoiceSession struct {
	GuildID       discord.GuildID   // Guild ID
	ChannelID     discord.ChannelID // Voice channel
	TextChannelID discord.ChannelID // Text channel where /voice was invoked
	InitiatorID   discord.UserID
	StartTime     time.Time
	LastActivity  time.Time
	LastAudioTime time.Time // Last time non-silent audio was received
	State         SessionState
	ActiveUsers   map[discord.UserID]*UserState
	AudioBuffer   *AudioBuffer // Stores pending audio segments
	Connection    interface{}  // WebSocket connection to OpenAI (using interface{} for now)

	// Audio playback queue to prevent interference
	AudioQueue     chan []byte
	PlaybackActive bool
	PlaybackMutex  sync.Mutex

	// Cost tracking
	InputAudioTokens  int       // Total input audio tokens used
	OutputAudioTokens int       // Total output audio tokens used
	SessionCost       float64   // Running total cost
	Model             string    // Model being used
	LastCostUpdate    time.Time // Last time cost was displayed
}

type SessionState int

const (
	SessionStateStarting SessionState = iota
	SessionStateActive
	SessionStateEnding
	SessionStateEnded
)

type UserState struct {
	UserID       discord.UserID
	SSRC         uint32
	LastActivity time.Time
	IsSpeaking   bool
	AudioBuffer  []byte
}

type AudioBuffer struct {
	segments [][]byte
	mu       sync.RWMutex
}

type SessionStatus struct {
	Active      bool
	GuildID     discord.GuildID
	ChannelID   discord.ChannelID
	StartTime   time.Time
	ActiveUsers []discord.UserID
	SessionCost float64
	Model       string
}

func NewService(
	logger *zap.Logger,
	cfg *config.Config,
	session *session.Session,
	pricingService openai.PricingService,
	voiceManager DiscordVoiceManager,
	audioProcessor AudioProcessor,
	realtimeProvider RealtimeProvider,
	sessionManager SessionManager,
	audioMixer AudioMixer,
) *Service {
	s := &Service{
		logger:           logger,
		cfg:              &cfg.Voice,
		discordSession:   session,
		pricingService:   pricingService,
		voiceManager:     voiceManager,
		audioProcessor:   audioProcessor,
		realtimeProvider: realtimeProvider,
		sessionManager:   sessionManager,
		audioMixer:       audioMixer,
	}

	// Start watchdog
	ctx, cancel := context.WithCancel(context.Background())
	s.watchdogCancel = cancel
	go s.runWatchdog(ctx)

	return s
}

func (s *Service) Start(ctx context.Context, guildID discord.GuildID, channelID discord.ChannelID, textChannelID discord.ChannelID, initiatorID discord.UserID, model string) (*VoiceSession, error) {
	// Check if session already exists for this guild
	if _, exists := s.activeSessions.Load(guildID); exists {
		return nil, fmt.Errorf("voice session already active in this guild")
	}

	// Check user permissions
	if !s.canExecuteCommand(initiatorID, guildID, "start") {
		return nil, fmt.Errorf("user does not have permission to use voice commands")
	}

	// Check concurrent session limit
	sessionCount := s.getActiveSessionCount()
	if sessionCount >= s.cfg.MaxConcurrentSessions {
		return nil, fmt.Errorf("maximum concurrent sessions reached (%d)", s.cfg.MaxConcurrentSessions)
	}

	// Use default model if not specified
	if model == "" {
		model = s.cfg.DefaultModel
	}

	// Validate model
	if !s.isModelAllowed(model) {
		return nil, fmt.Errorf("model %s is not allowed", model)
	}

	// Create session
	session := &VoiceSession{
		GuildID:        guildID,
		ChannelID:      channelID,
		TextChannelID:  textChannelID,
		InitiatorID:    initiatorID,
		StartTime:      time.Now(),
		LastActivity:   time.Now(),
		LastAudioTime:  time.Now(),
		State:          SessionStateStarting,
		ActiveUsers:    make(map[discord.UserID]*UserState),
		AudioBuffer:    &AudioBuffer{},
		AudioQueue:     make(chan []byte, 100), // Buffer up to 100 audio chunks
		PlaybackActive: false,
		Model:          model,
		LastCostUpdate: time.Now(),
	}

	// Store session
	s.activeSessions.Store(guildID, session)

	// Join voice channel
	_, err := s.voiceManager.JoinChannel(ctx, channelID)
	if err != nil {
		s.activeSessions.Delete(guildID)
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	// Connect to OpenAI Realtime
	connection, err := s.realtimeProvider.Connect(ctx, model)
	if err != nil {
		s.voiceManager.LeaveChannel(ctx, channelID)
		s.activeSessions.Delete(guildID)
		return nil, fmt.Errorf("failed to connect to OpenAI Realtime: %w", err)
	}

	session.Connection = connection
	session.State = SessionStateActive

	// Start audio processing loop
	go s.processAudio(ctx, session)

	s.logger.Info("Voice session started",
		zap.String("guild_id", guildID.String()),
		zap.String("channel_id", channelID.String()),
		zap.String("model", model))

	return session, nil
}

func (s *Service) Stop(ctx context.Context, guildID discord.GuildID, userID discord.UserID) error {
	sessionInterface, exists := s.activeSessions.Load(guildID)
	if !exists {
		return fmt.Errorf("no active voice session in this guild")
	}

	session := sessionInterface.(*VoiceSession)

	// Check permissions
	if !s.canStopSession(userID, guildID, session) {
		return fmt.Errorf("user does not have permission to stop this session")
	}

	return s.endSession(ctx, session, "stopped by user")
}

func (s *Service) GetStatus(guildID discord.GuildID) (*SessionStatus, error) {
	sessionInterface, exists := s.activeSessions.Load(guildID)
	if !exists {
		return &SessionStatus{Active: false}, nil
	}

	session := sessionInterface.(*VoiceSession)
	activeUsers := make([]discord.UserID, 0, len(session.ActiveUsers))
	for userID := range session.ActiveUsers {
		activeUsers = append(activeUsers, userID)
	}

	return &SessionStatus{
		Active:      true,
		GuildID:     session.GuildID,
		ChannelID:   session.ChannelID,
		StartTime:   session.StartTime,
		ActiveUsers: activeUsers,
		SessionCost: session.SessionCost,
		Model:       session.Model,
	}, nil
}

func (s *Service) canExecuteCommand(userID discord.UserID, guildID discord.GuildID, action string) bool {
	// Check allowed users list
	if !s.isAllowedUser(userID) {
		return false
	}

	return true
}

func (s *Service) canStopSession(userID discord.UserID, guildID discord.GuildID, session *VoiceSession) bool {
	// Check if user is initiator
	if session.InitiatorID == userID {
		return true
	}

	// TODO: Implement proper permission checking
	// The discord.Member struct in arikawa v3 doesn't have direct Permissions field
	// We need to check roles and their permissions
	// For now, only allow the initiator to stop the session
	return false
}

func (s *Service) isAllowedUser(userID discord.UserID) bool {
	if len(s.cfg.AllowedUserIDs) == 0 {
		return true // If no restrictions, allow everyone
	}

	userIDStr := userID.String()
	for _, allowedID := range s.cfg.AllowedUserIDs {
		if allowedID == userIDStr {
			return true
		}
	}
	return false
}

func (s *Service) isModelAllowed(model string) bool {
	if len(s.cfg.AllowedModels) == 0 {
		return true // If no restrictions, allow all models
	}

	for _, allowedModel := range s.cfg.AllowedModels {
		if allowedModel == model {
			return true
		}
	}
	return false
}

func (s *Service) getActiveSessionCount() int {
	count := 0
	s.activeSessions.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

func (s *Service) processAudio(ctx context.Context, session *VoiceSession) {
	// Set up response handlers for OpenAI Realtime
	handlers := ResponseHandlers{
		OnAudioDelta: func(ctx context.Context, audioData []byte) {
			s.handleAudioResponse(ctx, session, audioData)
		},
		OnTranscript: func(ctx context.Context, transcript string) {
			s.handleTranscript(ctx, session, transcript)
		},
		OnUserTranscript: func(ctx context.Context, transcript string) {
			s.handleUserTranscript(ctx, session, transcript)
		},
		OnResponseDone: func(ctx context.Context, usage *Usage) {
			s.handleResponseDone(ctx, session, usage)
		},
		OnError: func(ctx context.Context, err error) {
			s.logger.Error("OpenAI Realtime error", zap.Error(err))
		},
	}

	err := s.realtimeProvider.SetResponseHandlers(handlers)
	if err != nil {
		s.logger.Error("Failed to set response handlers", zap.Error(err))
		s.endSession(ctx, session, fmt.Sprintf("failed to set response handlers: %v", err))
		return
	}

	// Start receiving audio
	audioChannel, err := s.voiceManager.StartReceiving(ctx, session.ChannelID)
	if err != nil {
		s.logger.Error("Failed to start receiving audio", zap.Error(err))
		s.endSession(ctx, session, fmt.Sprintf("failed to start receiving audio: %v", err))
		return
	}

	// Audio accumulator for silence detection
	var audioAccumulator [][]byte
	var lastCommitTime time.Time
	var lastPacketTime time.Time
	silenceDuration := time.Duration(s.cfg.SilenceDuration) * time.Millisecond

	// Create a ticker to check for audio timeouts periodically
	audioTimeoutTicker := time.NewTicker(100 * time.Millisecond) // Check every 100ms
	defer audioTimeoutTicker.Stop()

	s.logger.Info("Started audio processing loop",
		zap.String("guild_id", session.GuildID.String()),
		zap.Duration("silence_duration", silenceDuration))

	for {
		select {
		case packet := <-audioChannel:
			if packet == nil {
				s.logger.Debug("Audio channel closed, exiting processAudio")
				return // Channel closed
			}

			// Update last packet time
			lastPacketTime = time.Now()

			s.logger.Debug("Processing audio packet",
				zap.String("user_id", packet.UserID.String()),
				zap.Uint32("ssrc", packet.SSRC),
				zap.Int("opus_length", len(packet.Opus)))

			// Convert Opus to PCM
			pcm, err := s.audioProcessor.OpusToPCM(packet.Opus)
			if err != nil {
				s.logger.Debug("Failed to convert Opus to PCM",
					zap.Error(err),
					zap.String("user_id", packet.UserID.String()))
				continue
			}

			// Detect silence
			isSilent, energy := s.audioProcessor.DetectSilence(pcm)

			// Add to mixer
			s.audioMixer.AddUserAudio(packet.UserID, pcm, packet.Timestamp)

			// Update session activity
			s.sessionManager.UpdateActivity(session.GuildID)

			if !isSilent {
				// Non-silent audio - accumulate it
				audioAccumulator = append(audioAccumulator, pcm)
				s.sessionManager.UpdateAudioTime(session.GuildID)
				lastCommitTime = time.Now()

				s.logger.Debug("Non-silent audio detected",
					zap.String("user_id", packet.UserID.String()),
					zap.Float32("energy", energy),
					zap.Int("accumulated_chunks", len(audioAccumulator)),
					zap.Int("pcm_length", len(pcm)))
			} else {
				// Silent audio - check if we should commit accumulated audio
				if len(audioAccumulator) > 0 && time.Since(lastCommitTime) > silenceDuration {
					s.logger.Info("Silence detected, committing accumulated audio",
						zap.String("user_id", packet.UserID.String()),
						zap.Int("chunks", len(audioAccumulator)),
						zap.Duration("silence_duration", time.Since(lastCommitTime)))

					s.commitAccumulatedAudio(ctx, session, audioAccumulator)
					audioAccumulator = nil
				} else if len(audioAccumulator) > 0 {
					s.logger.Debug("Silence detected but not long enough yet",
						zap.Duration("time_since_last_commit", time.Since(lastCommitTime)),
						zap.Duration("required_silence", silenceDuration))
				}
			}

		case <-audioTimeoutTicker.C:
			// Check if we have accumulated audio and enough time has passed without new packets
			if len(audioAccumulator) > 0 && !lastPacketTime.IsZero() {
				timeSinceLastPacket := time.Since(lastPacketTime)
				timeSinceLastCommit := time.Since(lastCommitTime)

				// Commit if we haven't received packets for the silence duration
				// and enough time has passed since last commit
				if timeSinceLastPacket >= silenceDuration && timeSinceLastCommit >= silenceDuration {
					s.logger.Info("No new audio packets received, committing accumulated audio",
						zap.Int("chunks", len(audioAccumulator)),
						zap.Duration("time_since_last_packet", timeSinceLastPacket),
						zap.Duration("time_since_last_commit", timeSinceLastCommit))

					s.commitAccumulatedAudio(ctx, session, audioAccumulator)
					audioAccumulator = nil
					lastCommitTime = time.Now()
				}
			}

		case <-ctx.Done():
			// Commit any remaining audio before exit
			if len(audioAccumulator) > 0 {
				s.commitAccumulatedAudio(ctx, session, audioAccumulator)
			}
			return
		}
	}
}

func (s *Service) commitAccumulatedAudio(ctx context.Context, session *VoiceSession, audioChunks [][]byte) {
	s.logger.Info("commitAccumulatedAudio called",
		zap.String("guild_id", session.GuildID.String()),
		zap.Int("chunks", len(audioChunks)))

	if len(audioChunks) == 0 {
		s.logger.Warn("commitAccumulatedAudio called with empty chunks")
		return
	}

	// Get mixed audio from the accumulated chunks
	duration := time.Duration(len(audioChunks)) * 20 * time.Millisecond
	s.logger.Debug("Getting mixed audio",
		zap.Duration("duration", duration))

	mixedAudio, err := s.audioMixer.GetMixedAudio(duration)
	if err != nil {
		s.logger.Error("Failed to mix audio", zap.Error(err))
		return
	}

	s.logger.Debug("Mixed audio obtained",
		zap.Int("size", len(mixedAudio)))

	// Convert PCM to base64 for OpenAI
	audioBase64, err := s.audioProcessor.PCMToBase64(mixedAudio)
	if err != nil {
		s.logger.Error("Failed to convert PCM to base64", zap.Error(err))
		return
	}

	s.logger.Info("Sending audio to OpenAI",
		zap.Int("base64_size", len(audioBase64)))

	// Send audio to OpenAI
	err = s.realtimeProvider.SendAudio(ctx, audioBase64)
	if err != nil {
		s.logger.Error("Failed to send audio to OpenAI", zap.Error(err))
		return
	}

	// Commit the audio buffer
	err = s.realtimeProvider.CommitAudio(ctx)
	if err != nil {
		s.logger.Error("Failed to commit audio buffer", zap.Error(err))
		return
	}

	// Request response generation
	err = s.realtimeProvider.GenerateResponse(ctx)
	if err != nil {
		s.logger.Error("Failed to request response generation", zap.Error(err))
		return
	}

	s.logger.Info("Audio successfully committed to OpenAI",
		zap.Int("chunks", len(audioChunks)),
		zap.Int("mixed_audio_size", len(mixedAudio)),
		zap.Int("base64_size", len(audioBase64)))
}

func (s *Service) handleAudioResponse(ctx context.Context, session *VoiceSession, audioData []byte) {
	s.logger.Debug("Received audio chunk from OpenAI",
		zap.Int("pcm_size", len(audioData)))

	// Queue audio data for sequential playback to avoid interference
	s.queueAudioForPlayback(ctx, session, audioData)
}

func (s *Service) queueAudioForPlayback(ctx context.Context, session *VoiceSession, audioData []byte) {
	// Send audio data to the queue
	select {
	case session.AudioQueue <- audioData:
		s.logger.Debug("Queued audio chunk",
			zap.Int("pcm_size", len(audioData)),
			zap.Int("queue_length", len(session.AudioQueue)))
	case <-ctx.Done():
		return
	default:
		s.logger.Warn("Audio queue full, dropping chunk",
			zap.Int("pcm_size", len(audioData)))
	}

	// Start playback worker if not already running
	session.PlaybackMutex.Lock()
	if !session.PlaybackActive {
		session.PlaybackActive = true
		go s.audioPlaybackWorker(ctx, session)
	}
	session.PlaybackMutex.Unlock()
}

func (s *Service) audioPlaybackWorker(ctx context.Context, session *VoiceSession) {
	defer func() {
		session.PlaybackMutex.Lock()
		session.PlaybackActive = false
		session.PlaybackMutex.Unlock()
		s.logger.Debug("Audio playback worker stopped")
	}()

	s.logger.Debug("Audio playback worker started")

	for {
		select {
		case audioData := <-session.AudioQueue:
			// Process this audio chunk sequentially
			s.splitAndPlayAudio(ctx, session, audioData)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) splitAndPlayAudio(ctx context.Context, session *VoiceSession, audioData []byte) {
	// Discord expects 20ms Opus frames at 48kHz stereo sent at precise 20ms intervals
	// Critical: Frame timing must be exact to prevent audio artifacts

	// 20ms at 24kHz mono = 480 samples = 960 bytes (16-bit PCM)
	const frameSizeBytes = 960 // 20ms at 24kHz mono in bytes
	const frameDurationMs = 20 // Each frame represents 20ms of audio

	frameIndex := 0
	frameStartTime := time.Now()

	s.logger.Debug("Processing audio chunk for Discord playback",
		zap.Int("input_pcm_size", len(audioData)),
		zap.Int("frame_size_bytes", frameSizeBytes),
		zap.Int("estimated_frames", (len(audioData)+frameSizeBytes-1)/frameSizeBytes))

	// Split audio into 20ms frames and send each frame with frame-paced timing
	for offset := 0; offset < len(audioData); offset += frameSizeBytes {
		// Calculate when this frame should be sent (frame-paced timing)
		expectedFrameTime := frameStartTime.Add(time.Duration(frameIndex) * 20 * time.Millisecond)

		end := offset + frameSizeBytes
		if end > len(audioData) {
			end = len(audioData)
		}

		frameData := audioData[offset:end]
		actualFrameSize := len(frameData)

		// For Discord compatibility, ensure exactly 20ms frames
		if actualFrameSize < frameSizeBytes {
			paddedFrame := make([]byte, frameSizeBytes)
			copy(paddedFrame, frameData)
			frameData = paddedFrame

			s.logger.Debug("Padded short frame with silence",
				zap.Int("original_size", actualFrameSize),
				zap.Int("padded_size", frameSizeBytes),
				zap.Int("frame_index", frameIndex))
		}

		// Convert this 20ms PCM frame to Opus for Discord
		opusData, err := s.audioProcessor.PCMToOpus(frameData)
		if err != nil {
			s.logger.Error("Failed to convert PCM frame to Opus",
				zap.Error(err),
				zap.Int("frame_index", frameIndex),
				zap.Int("frame_size", len(frameData)))
			return
		}

		// Wait until the exact time this frame should be sent
		now := time.Now()
		if now.Before(expectedFrameTime) {
			waitDuration := expectedFrameTime.Sub(now)
			timer := time.NewTimer(waitDuration)
			select {
			case <-timer.C:
				// Time to send frame
			case <-ctx.Done():
				timer.Stop()
				s.logger.Debug("Context canceled during frame timing wait")
				return
			}
			timer.Stop()
		} else if now.Sub(expectedFrameTime) > 5*time.Millisecond {
			// We're running late - log timing drift
			s.logger.Warn("Frame timing drift detected",
				zap.Int("frame_index", frameIndex),
				zap.Duration("drift", now.Sub(expectedFrameTime)))
		}

		// Send frame to Discord at the precise expected time
		sendStartTime := time.Now()
		err = s.voiceManager.PlayAudio(ctx, session.ChannelID, opusData)
		if err != nil {
			s.logger.Error("Failed to send audio frame to Discord",
				zap.Error(err),
				zap.Int("frame_index", frameIndex))
			return
		}
		sendDuration := time.Since(sendStartTime)

		s.logger.Debug("Sent audio frame to Discord",
			zap.Int("frame_index", frameIndex),
			zap.Int("pcm_frame_size", len(frameData)),
			zap.Int("opus_frame_size", len(opusData)),
			zap.Duration("send_duration", sendDuration),
			zap.Time("expected_time", expectedFrameTime),
			zap.Time("actual_time", sendStartTime))

		frameIndex++
	}

	s.logger.Debug("Completed audio chunk playback",
		zap.Int("total_frames", frameIndex),
		zap.Int("total_duration_ms", frameIndex*frameDurationMs),
		zap.Int("input_size", len(audioData)),
		zap.Duration("total_elapsed", time.Since(frameStartTime)))
}

func (s *Service) playAudioWithTiming(ctx context.Context, session *VoiceSession, opusData []byte, duration time.Duration) {
	// Play the audio immediately, but then wait for the calculated duration
	// before allowing the next chunk to be processed

	err := s.voiceManager.PlayAudio(ctx, session.ChannelID, opusData)
	if err != nil {
		s.logger.Error("Failed to play audio in Discord", zap.Error(err))
		return
	}

	s.logger.Debug("Played audio chunk with timing",
		zap.Int("opus_size", len(opusData)),
		zap.Duration("duration", duration))

	// Wait for the duration of this audio chunk to maintain proper timing
	// This prevents audio chunks from being played faster than they should be heard
	if duration > 0 {
		time.Sleep(duration)
	}
}

func (s *Service) handleTranscript(ctx context.Context, session *VoiceSession, transcript string) {
	if transcript == "" {
		return
	}

	s.logger.Info("AI transcript",
		zap.String("guild_id", session.GuildID.String()),
		zap.String("transcript", transcript))

	// Optionally send transcript to text channel
	// (This could be configured via a setting)
}

func (s *Service) handleUserTranscript(ctx context.Context, session *VoiceSession, transcript string) {
	if transcript == "" {
		return
	}

	s.logger.Info("User transcript",
		zap.String("guild_id", session.GuildID.String()),
		zap.String("user_id", session.InitiatorID.String()),
		zap.String("transcript", transcript))

	// Optionally send user transcript to text channel
	// (This could be configured via a setting)
}

func (s *Service) handleResponseDone(ctx context.Context, session *VoiceSession, usage *Usage) {
	if usage == nil {
		return
	}

	// Check if session still exists in activeSessions
	if _, exists := s.activeSessions.Load(session.GuildID); !exists {
		s.logger.Debug("Session no longer active, skipping response processing", 
			zap.String("guild_id", session.GuildID.String()))
		return
	}

	// Update token usage
	err := s.sessionManager.UpdateTokenUsage(session.GuildID, usage.InputAudioTokens, usage.OutputAudioTokens)
	if err != nil {
		s.logger.Debug("Failed to update token usage", zap.Error(err), 
			zap.String("guild_id", session.GuildID.String()))
		return // Session was likely cleaned up
	}

	// Calculate cost using pricing service
	cost, err := s.calculateSessionCost(session)
	if err != nil {
		s.logger.Error("Failed to calculate session cost", zap.Error(err))
	} else {
		// Update session cost
		err = s.sessionManager.UpdateSessionCost(session.GuildID, cost)
		if err != nil {
			s.logger.Debug("Failed to update session cost", zap.Error(err), 
				zap.String("guild_id", session.GuildID.String()))
			return // Session was likely cleaned up
		}

		// Check cost warnings and limits
		s.checkCostLimits(ctx, session, cost)
	}

	s.logger.Debug("Response completed",
		zap.String("guild_id", session.GuildID.String()),
		zap.Int("input_tokens", usage.InputAudioTokens),
		zap.Int("output_tokens", usage.OutputAudioTokens),
		zap.Float64("cost", cost))
}

func (s *Service) calculateSessionCost(session *VoiceSession) (float64, error) {
	// Use the pricing service to calculate audio token costs
	return s.pricingService.CalculateAudioTokenCost(session.Model, session.InputAudioTokens, session.OutputAudioTokens)
}

func (s *Service) checkCostLimits(ctx context.Context, session *VoiceSession, cost float64) {
	// Check if cost limit exceeded
	if cost >= s.cfg.MaxCostPerSession {
		s.logger.Warn("Cost limit exceeded, ending session",
			zap.String("guild_id", session.GuildID.String()),
			zap.Float64("cost", cost),
			zap.Float64("limit", s.cfg.MaxCostPerSession))

		s.endSession(ctx, session, fmt.Sprintf("cost limit exceeded ($%.2f)", cost))
		return
	}

	// Show cost updates if enabled
	if s.cfg.TrackSessionCosts && time.Since(session.LastCostUpdate) > 30*time.Second {
		// TODO: Send cost update to text channel
		s.logger.Info("Session cost update",
			zap.String("guild_id", session.GuildID.String()),
			zap.Float64("cost", cost),
			zap.Int("input_tokens", session.InputAudioTokens),
			zap.Int("output_tokens", session.OutputAudioTokens))
		
		// Update the last cost update time so we don't spam logs
		session.LastCostUpdate = time.Now()
	}
}

func (s *Service) endSession(ctx context.Context, session *VoiceSession, reason string) error {
	session.State = SessionStateEnding

	// Close OpenAI connection
	if session.Connection != nil {
		// TODO: Close connection properly when we implement the interface
	}

	// Leave voice channel
	err := s.voiceManager.LeaveChannel(ctx, session.ChannelID)
	if err != nil {
		s.logger.Warn("Failed to leave voice channel", zap.Error(err))
	}

	// Remove from active sessions
	s.activeSessions.Delete(session.GuildID)

	// Remove from session manager as well
	err = s.sessionManager.EndSession(session.GuildID)
	if err != nil {
		s.logger.Warn("Failed to end session in session manager", zap.Error(err))
	}

	session.State = SessionStateEnded

	s.logger.Info("Voice session ended",
		zap.String("guild_id", session.GuildID.String()),
		zap.String("reason", reason),
		zap.Float64("cost", session.SessionCost))

	return nil
}

func (s *Service) runWatchdog(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.activeSessions.Range(func(key, value interface{}) bool {
				session := value.(*VoiceSession)

				// Check inactivity timeout
				if time.Since(session.LastAudioTime) > time.Duration(s.cfg.InactivityTimeout)*time.Second {
					s.endSession(ctx, session, "inactivity timeout")
					return true
				}

				// Check session duration
				if time.Since(session.StartTime) > time.Duration(s.cfg.MaxSessionLength)*time.Minute {
					s.endSession(ctx, session, "maximum session length reached")
					return true
				}

				// Check cost limit
				if session.SessionCost >= s.cfg.MaxCostPerSession {
					s.endSession(ctx, session, fmt.Sprintf("cost limit reached ($%.2f)", session.SessionCost))
					return true
				}

				return true
			})

		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) Shutdown(ctx context.Context) error {
	if s.watchdogCancel != nil {
		s.watchdogCancel()
	}

	// End all active sessions
	s.activeSessions.Range(func(key, value interface{}) bool {
		session := value.(*VoiceSession)
		s.endSession(ctx, session, "service shutdown")
		return true
	})

	return nil
}
