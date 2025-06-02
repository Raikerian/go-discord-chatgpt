package voice

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	// Optimized lookups for permissions
	allowedUsersMap  map[string]struct{}
	allowedModelsMap map[string]struct{}

	// watchdogCancel for stopping the watchdog goroutine
	watchdogCancel context.CancelFunc
}

type VoiceSession struct {
	mu            sync.Mutex        // Protect concurrent access to fields
	GuildID       discord.GuildID   // Guild ID
	ChannelID     discord.ChannelID // Voice channel
	TextChannelID discord.ChannelID // Text channel where /voice was invoked
	InitiatorID   discord.UserID
	StartTime     time.Time
	LastActivity  time.Time
	LastAudioTime time.Time // Last time non-silent audio was received
	State         SessionState
	ActiveUsers   map[discord.UserID]*UserState
	Connection    any                // WebSocket connection to OpenAI
	CancelFunc    context.CancelFunc // Cancel function for session context

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
	// Convert slices to maps for O(1) lookups
	allowedUsersMap := make(map[string]struct{}, len(cfg.Voice.AllowedUserIDs))
	for _, id := range cfg.Voice.AllowedUserIDs {
		allowedUsersMap[id] = struct{}{}
	}

	allowedModelsMap := make(map[string]struct{}, len(cfg.Voice.AllowedModels))
	for _, model := range cfg.Voice.AllowedModels {
		allowedModelsMap[model] = struct{}{}
	}

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
		allowedUsersMap:  allowedUsersMap,
		allowedModelsMap: allowedModelsMap,
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
		return nil, errors.New("voice session already active in this guild")
	}

	// Check user permissions
	if !s.canExecuteCommand(initiatorID) {
		return nil, errors.New("user does not have permission to use voice commands")
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

	// Create session context for cancellation
	sessionCtx, sessionCancel := context.WithCancel(ctx)
	session.CancelFunc = sessionCancel

	// Start audio processing loop
	go s.processAudio(sessionCtx, session)

	s.logger.Info("Voice session started",
		zap.String("guild_id", guildID.String()),
		zap.String("channel_id", channelID.String()),
		zap.String("model", model))

	return session, nil
}

func (s *Service) Stop(ctx context.Context, guildID discord.GuildID, userID discord.UserID) error {
	sessionInterface, exists := s.activeSessions.Load(guildID)
	if !exists {
		return errors.New("no active voice session in this guild")
	}

	session := sessionInterface.(*VoiceSession)

	// Check permissions
	if !s.canStopSession(userID, session) {
		return errors.New("user does not have permission to stop this session")
	}

	return s.endSession(ctx, session, "stopped by user")
}

func (s *Service) GetStatus(guildID discord.GuildID) (*SessionStatus, error) {
	sessionInterface, exists := s.activeSessions.Load(guildID)
	if !exists {
		return &SessionStatus{Active: false}, nil
	}

	session := sessionInterface.(*VoiceSession)

	session.mu.Lock()
	activeUsers := make([]discord.UserID, 0, len(session.ActiveUsers))
	for userID := range session.ActiveUsers {
		activeUsers = append(activeUsers, userID)
	}

	status := &SessionStatus{
		Active:      true,
		GuildID:     session.GuildID,
		ChannelID:   session.ChannelID,
		StartTime:   session.StartTime,
		ActiveUsers: activeUsers,
		SessionCost: session.SessionCost,
		Model:       session.Model,
	}
	session.mu.Unlock()

	return status, nil
}

func (s *Service) canExecuteCommand(userID discord.UserID) bool {
	// Check allowed users list
	return s.isAllowedUser(userID)
}

func (s *Service) canStopSession(userID discord.UserID, session *VoiceSession) bool {
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
	if len(s.allowedUsersMap) == 0 {
		return true // If no restrictions, allow everyone
	}

	userIDStr := userID.String()
	_, allowed := s.allowedUsersMap[userIDStr]

	return allowed
}

func (s *Service) isModelAllowed(model string) bool {
	if len(s.allowedModelsMap) == 0 {
		return true // If no restrictions, allow all models
	}

	_, allowed := s.allowedModelsMap[model]

	return allowed
}

func (s *Service) getActiveSessionCount() int {
	count := 0
	s.activeSessions.Range(func(key, value any) bool {
		count++

		return true
	})

	return count
}

func (s *Service) processAudio(ctx context.Context, session *VoiceSession) {
	if err := s.setupAudioHandlers(ctx, session); err != nil {
		return
	}

	audioChannel, err := s.voiceManager.StartReceiving(ctx, session.ChannelID)
	if err != nil {
		s.logger.Error("Failed to start receiving audio", zap.Error(err))
		s.endSession(ctx, session, fmt.Sprintf("failed to start receiving audio: %v", err))

		return
	}

	s.runAudioLoop(ctx, session, audioChannel)
}

func (s *Service) setupAudioHandlers(ctx context.Context, session *VoiceSession) error {
	handlers := ResponseHandlers{
		OnAudioDelta: func(ctx context.Context, audioData []byte) {
			s.handleAudioResponse(ctx, session, audioData)
		},
		OnTranscript: func(ctx context.Context, transcript string) {
			s.handleTranscript(session, transcript)
		},
		OnUserTranscript: func(ctx context.Context, transcript string) {
			s.handleUserTranscript(session, transcript)
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

		return err
	}

	return nil
}

func (s *Service) runAudioLoop(ctx context.Context, session *VoiceSession, audioChannel <-chan *AudioPacket) {
	var lastPacketTime = time.Now() // Initialize to prevent initial commit
	// Use a shorter timeout for more responsive audio processing
	timeoutDuration := 200 * time.Millisecond // 200ms of no packets = commit audio

	audioTimeoutTicker := time.NewTicker(100 * time.Millisecond) // Check every 100ms
	defer audioTimeoutTicker.Stop()

	s.logger.Info("Started audio processing loop",
		zap.String("guild_id", session.GuildID.String()),
		zap.Duration("timeout_duration", timeoutDuration))

	for {
		select {
		case packet, ok := <-audioChannel:
			if !ok || packet == nil {
				s.logger.Debug("Audio channel closed, exiting processAudio")

				return
			}

			s.processAudioPacket(session, packet)
			lastPacketTime = time.Now()

		case <-audioTimeoutTicker.C:
			// Check if we haven't received packets for the timeout duration
			if !lastPacketTime.IsZero() && time.Since(lastPacketTime) > timeoutDuration {
				s.logger.Info("Audio timeout reached, committing audio",
					zap.Duration("since_last_packet", time.Since(lastPacketTime)))

				// Commit audio from mixer
				s.commitMixerAudio(ctx, session)
				lastPacketTime = time.Time{} // Reset to prevent multiple commits
			}

		case <-ctx.Done():
			s.endSession(ctx, session, "context canceled")

			return
		}
	}
}

func (s *Service) processAudioPacket(session *VoiceSession, packet *AudioPacket) {
	s.logger.Debug("Processing audio packet",
		zap.String("user_id", packet.UserID.String()),
		zap.Uint32("ssrc", packet.SSRC),
		zap.Int("opus_length", len(packet.Opus)),
		zap.Uint32("rtp_timestamp", packet.RTPTimestamp),
		zap.Uint16("sequence", packet.Sequence))

	pcm, err := s.audioProcessor.OpusToPCM(packet.Opus)
	if err != nil {
		s.logger.Debug("Failed to convert Opus to PCM",
			zap.Error(err),
			zap.String("user_id", packet.UserID.String()))

		return
	}

	// Scale RTP timestamp to match PCM sample rate after Opus→PCM conversion
	// Discord RTP timestamps are at 48kHz, but we process 24kHz PCM after conversion
	adjustedRTP := packet.RTPTimestamp / 2 // 48kHz → 24kHz scaling

	s.logger.Debug("RTP timestamp scaling applied",
		zap.String("user_id", packet.UserID.String()),
		zap.Uint32("original_rtp", packet.RTPTimestamp),
		zap.Uint32("scaled_rtp", adjustedRTP),
		zap.Int("pcm_size", len(pcm)))

	// Add PCM audio to mixer with adjusted RTP timing info
	s.audioMixer.AddUserAudioWithRTP(packet.UserID, pcm, adjustedRTP, packet.Sequence)
	s.sessionManager.UpdateActivity(session.GuildID)
	s.sessionManager.UpdateAudioTime(session.GuildID)

	// Update session ActiveUsers
	session.mu.Lock()
	session.ActiveUsers[packet.UserID] = &UserState{
		UserID:       packet.UserID,
		SSRC:         packet.SSRC,
		LastActivity: time.Now(),
	}
	session.mu.Unlock()

	s.logger.Debug("Added audio to mixer",
		zap.String("user_id", packet.UserID.String()),
		zap.Int("pcm_length", len(pcm)),
		zap.Uint32("rtp_timestamp", packet.RTPTimestamp))
}

// commitMixerAudio gets mixed audio from the mixer and sends it to OpenAI.
func (s *Service) commitMixerAudio(ctx context.Context, session *VoiceSession) {
	// Get all available audio and immediately flush buffers
	// This atomic operation ensures we don't miss any audio or leave stale data
	mixedAudio, actualDuration, err := s.audioMixer.GetAllAvailableMixedAudioAndFlush()
	if err != nil {
		s.logger.Error("Failed to mix audio", zap.Error(err))

		return
	}

	// Check if we got any audio
	if len(mixedAudio) == 0 || actualDuration == 0 {
		s.logger.Debug("No audio to commit")

		return
	}

	// Update LastAudioTime
	session.mu.Lock()
	session.LastAudioTime = time.Now()
	session.mu.Unlock()

	s.logger.Info("Committing mixer audio",
		zap.String("guild_id", session.GuildID.String()),
		zap.Duration("actual_duration", actualDuration),
		zap.Int("audio_bytes", len(mixedAudio)))

	// Use DetectSilence from audio processor to avoid sending silence
	isSilent, energy := s.audioProcessor.DetectSilence(mixedAudio)
	if isSilent {
		s.logger.Debug("Mixed audio is silent, skipping send", zap.Float32("energy", energy))

		return
	}

	s.logger.Debug("Mixed audio obtained",
		zap.Int("size", len(mixedAudio)),
		// zap.Float32("energy_level", energyLevel),
		zap.Duration("actual_duration", actualDuration))

	// Continue with the rest of the processing
	s.processMixedAudio(ctx, session, mixedAudio)
}

func (s *Service) processMixedAudio(ctx context.Context, session *VoiceSession, mixedAudio []byte) {
	s.logger.Info("Processing mixed audio",
		zap.String("guild_id", session.GuildID.String()),
		zap.Int("size", len(mixedAudio)))

	// DEBUG: Save audio to WAV files for debugging
	// Set this to true to enable WAV file saving
	const debugSaveWAV = true
	if debugSaveWAV {
		// Save the mixed audio
		if err := s.saveDebugWAV(mixedAudio, session.GuildID, "mixed"); err != nil {
			s.logger.Error("Failed to save mixed audio WAV", zap.Error(err))
		}

		return
	}

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
		case audioData, ok := <-session.AudioQueue:
			if !ok {
				return
			}
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
	const frameSizeBytes = OpenAIFrameSize * 2 // 20ms at 24kHz mono in bytes (16-bit samples)
	const frameDurationMs = 20                 // Each frame represents 20ms of audio

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

		end := min(offset+frameSizeBytes, len(audioData))

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

		// Adjust frame timing to correct for accumulated drift
		drift := time.Since(expectedFrameTime)
		if drift > 5*time.Millisecond {
			// Resync: push frameStartTime forward by drift to catch up
			frameStartTime = frameStartTime.Add(drift)
			s.logger.Debug("Corrected frame timing drift",
				zap.Int("frame_index", frameIndex),
				zap.Duration("drift", drift))
		}

		frameIndex++
	}

	s.logger.Debug("Completed audio chunk playback",
		zap.Int("total_frames", frameIndex),
		zap.Int("total_duration_ms", frameIndex*frameDurationMs),
		zap.Int("input_size", len(audioData)),
		zap.Duration("total_elapsed", time.Since(frameStartTime)))
}

func (s *Service) handleTranscript(session *VoiceSession, transcript string) {
	s.logger.Info("AI transcript",
		zap.String("guild_id", session.GuildID.String()),
		zap.String("transcript", transcript))

	// Optionally send transcript to text channel
	// (This could be configured via a setting)
}

func (s *Service) handleUserTranscript(session *VoiceSession, transcript string) {
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
	session.mu.Lock()
	cost, err := s.pricingService.CalculateAudioTokenCost(session.Model, session.InputAudioTokens, session.OutputAudioTokens)
	session.mu.Unlock()

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
	session.mu.Lock()
	shouldUpdate := s.cfg.TrackSessionCosts && time.Since(session.LastCostUpdate) > 30*time.Second
	if shouldUpdate {
		// TODO: Send cost update to text channel
		s.logger.Info("Session cost update",
			zap.String("guild_id", session.GuildID.String()),
			zap.Float64("cost", cost),
			zap.Int("input_tokens", session.InputAudioTokens),
			zap.Int("output_tokens", session.OutputAudioTokens))

		// Update the last cost update time so we don't spam logs
		session.LastCostUpdate = time.Now()
	}
	session.mu.Unlock()
}

func (s *Service) endSession(ctx context.Context, session *VoiceSession, reason string) error {
	// Prevent double-ending a session
	session.mu.Lock()
	if session.State == SessionStateEnding || session.State == SessionStateEnded {
		session.mu.Unlock()

		return nil
	}
	session.State = SessionStateEnding
	session.mu.Unlock()

	// Cancel session context
	if session.CancelFunc != nil {
		session.CancelFunc()
	}

	// Close OpenAI connection
	if session.Connection != nil {
		// Try to close the connection if it implements io.Closer
		if closer, ok := session.Connection.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				s.logger.Warn("Failed to close OpenAI connection", zap.Error(err))
			} else {
				s.logger.Debug("Successfully closed OpenAI connection")
			}
		}
	}

	// Close audio queue to signal workers to stop
	close(session.AudioQueue)

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

	session.mu.Lock()
	session.State = SessionStateEnded
	session.mu.Unlock()

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
			s.activeSessions.Range(func(key, value any) bool {
				session := value.(*VoiceSession)

				session.mu.Lock()
				lastAudioTime := session.LastAudioTime
				startTime := session.StartTime
				sessionCost := session.SessionCost

				// Clean up stale ActiveUsers entries (users who haven't been seen for 30 seconds)
				for userID, userState := range session.ActiveUsers {
					if time.Since(userState.LastActivity) > 30*time.Second {
						delete(session.ActiveUsers, userID)
						s.logger.Debug("Removed stale user from ActiveUsers",
							zap.String("guild_id", session.GuildID.String()),
							zap.String("user_id", userID.String()),
							zap.Duration("inactive_duration", time.Since(userState.LastActivity)))
					}
				}

				session.mu.Unlock()

				// Check inactivity timeout
				if time.Since(lastAudioTime) > time.Duration(s.cfg.InactivityTimeout)*time.Second {
					s.endSession(ctx, session, "inactivity timeout")

					return true
				}

				// Check session duration
				if time.Since(startTime) > time.Duration(s.cfg.MaxSessionLength)*time.Minute {
					s.endSession(ctx, session, "maximum session length reached")

					return true
				}

				// Check cost limit
				if sessionCost >= s.cfg.MaxCostPerSession {
					s.endSession(ctx, session, fmt.Sprintf("cost limit reached ($%.2f)", sessionCost))

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
	s.activeSessions.Range(func(key, value any) bool {
		session := value.(*VoiceSession)
		s.endSession(ctx, session, "service shutdown")

		return true
	})

	return nil
}

// saveDebugWAV saves PCM audio as a WAV file for debugging.
func (s *Service) saveDebugWAV(pcmData []byte, guildID discord.GuildID, prefix string) error {
	// Create debug directory if it doesn't exist
	debugDir := "debug_audio"
	if err := os.MkdirAll(debugDir, 0755); err != nil {
		return fmt.Errorf("failed to create debug directory: %w", err)
	}

	// Generate filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := filepath.Join(debugDir, fmt.Sprintf("%s_audio_%s_%s.wav", prefix, guildID.String(), timestamp))

	// Create WAV file
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create WAV file: %w", err)
	}
	defer file.Close()

	// WAV file format: 24kHz, mono, 16-bit PCM
	const (
		numChannels   = 1
		sampleRate    = 24000
		bitsPerSample = 16
		byteRate      = sampleRate * numChannels * bitsPerSample / 8
		blockAlign    = numChannels * bitsPerSample / 8
	)

	// Calculate sizes
	dataSize := uint32(len(pcmData))
	fileSize := dataSize + 36 // 36 = header size without data chunk size

	// Write RIFF header
	file.WriteString("RIFF")
	binary.Write(file, binary.LittleEndian, fileSize)
	file.WriteString("WAVE")

	// Write fmt chunk
	file.WriteString("fmt ")
	binary.Write(file, binary.LittleEndian, uint32(16)) // fmt chunk size
	binary.Write(file, binary.LittleEndian, uint16(1))  // PCM format
	binary.Write(file, binary.LittleEndian, uint16(numChannels))
	binary.Write(file, binary.LittleEndian, uint32(sampleRate))
	binary.Write(file, binary.LittleEndian, uint32(byteRate))
	binary.Write(file, binary.LittleEndian, uint16(blockAlign))
	binary.Write(file, binary.LittleEndian, uint16(bitsPerSample))

	// Write data chunk
	file.WriteString("data")
	binary.Write(file, binary.LittleEndian, dataSize)
	file.Write(pcmData)

	s.logger.Info("Saved debug WAV file",
		zap.String("filename", filename),
		zap.Int("size_bytes", len(pcmData)),
		zap.Float64("duration_seconds", float64(len(pcmData))/float64(byteRate)))

	return nil
}
