package voice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/pkg/audio"
	"github.com/Raikerian/go-discord-chatgpt/pkg/openai"
	"github.com/Raikerian/go-discord-chatgpt/pkg/util"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"
	"go.uber.org/zap"
)

type Service struct {
	logger         *zap.Logger
	cfg            *config.VoiceConfig
	discordSession *session.Session
	pricingService openai.PricingService

	voiceManager     DiscordManager
	audioProcessor   audio.AudioProcessor
	realtimeProvider RealtimeProvider
	sessionManager   SessionManager
	audioMixer       audio.AudioMixer

	// Optimized lookups for permissions
	allowedUsersMap  map[string]struct{}
	allowedModelsMap map[string]struct{}

	// watchdogCancel for stopping the watchdog goroutine
	watchdogCancel context.CancelFunc
}

func NewService(
	logger *zap.Logger,
	cfg *config.Config,
	sess *session.Session,
	pricingService openai.PricingService,
	voiceManager DiscordManager,
	audioProcessor audio.AudioProcessor,
	realtimeProvider RealtimeProvider,
	sessionManager SessionManager,
	audioMixer audio.AudioMixer,
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
		discordSession:   sess,
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

func (s *Service) Start(ctx context.Context, guildID discord.GuildID, channelID, textChannelID discord.ChannelID, initiatorID discord.UserID, model string) (*VoiceSession, error) {
	// Check if session already exists for this guild
	if _, err := s.sessionManager.GetSessionByGuild(guildID); err == nil {
		return nil, errors.New("voice session already active in this guild")
	}

	// Check user permissions
	if !s.canExecuteCommand(initiatorID) {
		return nil, errors.New("user does not have permission to use voice commands")
	}

	// Check concurrent session limit
	sessionCount := s.sessionManager.GetSessionCount()
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

	// Create session using session manager
	voiceSession, err := s.sessionManager.CreateSession(guildID, channelID, textChannelID, initiatorID, model)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	// Join voice channel
	_, err = s.voiceManager.JoinChannel(ctx, channelID)
	if err != nil {
		if endErr := s.sessionManager.EndSession(guildID); endErr != nil {
			s.logger.Error("failed to clean up session after voice join failure", zap.Error(endErr))
		}

		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	// Connect to OpenAI Realtime
	connection, err := s.realtimeProvider.Connect(ctx, model)
	if err != nil {
		if leaveErr := s.voiceManager.LeaveChannel(ctx, channelID); leaveErr != nil {
			s.logger.Error("failed to leave voice channel", zap.Error(leaveErr))
		}
		if endErr := s.sessionManager.EndSession(guildID); endErr != nil {
			s.logger.Error("failed to clean up session after OpenAI connection failure", zap.Error(endErr))
		}

		return nil, fmt.Errorf("failed to connect to OpenAI Realtime: %w", err)
	}

	if err := s.sessionManager.SetConnection(guildID, connection); err != nil {
		return nil, fmt.Errorf("failed to set session connection: %w", err)
	}
	if err := s.sessionManager.UpdateSessionState(guildID, SessionStateActive); err != nil {
		return nil, fmt.Errorf("failed to update session state: %w", err)
	}

	// Create session context for cancellation
	sessionCtx, sessionCancel := context.WithCancel(ctx)
	if err := s.sessionManager.SetCancelFunc(guildID, sessionCancel); err != nil {
		return nil, fmt.Errorf("failed to set session cancel func: %w", err)
	}

	// Start audio processing loop
	go s.processAudio(sessionCtx, voiceSession)

	s.logger.Info("Voice session started",
		zap.String("guild_id", guildID.String()),
		zap.String("channel_id", channelID.String()),
		zap.String("model", model))

	return voiceSession, nil
}

func (s *Service) Stop(ctx context.Context, guildID discord.GuildID, userID discord.UserID) error {
	voiceSession, err := s.sessionManager.GetSessionByGuild(guildID)
	if err != nil {
		return errors.New("no active voice session in this guild")
	}

	// Check permissions
	if !s.canStopSession(userID, voiceSession) {
		return errors.New("user does not have permission to stop this session")
	}

	return s.endSession(ctx, voiceSession, "stopped by user")
}

func (s *Service) GetStatus(guildID discord.GuildID) (*SessionStatus, error) {
	voiceSession, err := s.sessionManager.GetSessionByGuild(guildID)
	if err != nil {
		return &SessionStatus{Active: false}, nil
	}

	voiceSession.mu.Lock()
	activeUsers := make([]discord.UserID, 0, len(voiceSession.ActiveUsers))
	for userID := range voiceSession.ActiveUsers {
		activeUsers = append(activeUsers, userID)
	}

	status := &SessionStatus{
		Active:      true,
		GuildID:     voiceSession.GuildID,
		ChannelID:   voiceSession.ChannelID,
		StartTime:   voiceSession.StartTime,
		ActiveUsers: activeUsers,
		SessionCost: voiceSession.SessionCost,
		Model:       voiceSession.Model,
	}
	voiceSession.mu.Unlock()

	return status, nil
}

func (s *Service) canExecuteCommand(userID discord.UserID) bool {
	// Check allowed users list
	return s.isAllowedUser(userID)
}

func (s *Service) canStopSession(userID discord.UserID, voiceSession *VoiceSession) bool {
	// Check if user is initiator
	if voiceSession.InitiatorID == userID {
		return true
	}

	// TODO: Implement proper permission checking
	// The discord.Member struct in arikawa v3 doesn't have direct Permissions field
	// We need to check roles and their permissions
	// For now, only allow the initiator to stop the session
	// For example, administrators and bot owners should always be able to stop the session
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

func (s *Service) processAudio(ctx context.Context, voiceSession *VoiceSession) {
	if err := s.setupAudioHandlers(ctx, voiceSession); err != nil {
		return
	}

	audioChannel, err := s.voiceManager.StartReceiving(ctx, voiceSession.ChannelID)
	if err != nil {
		s.logger.Error("Failed to start receiving audio", zap.Error(err))
		if endErr := s.endSession(ctx, voiceSession, fmt.Sprintf("failed to start receiving audio: %v", err)); endErr != nil {
			s.logger.Error("failed to end session", zap.Error(endErr))
		}

		return
	}

	s.runAudioLoop(ctx, voiceSession, audioChannel)
}

func (s *Service) setupAudioHandlers(ctx context.Context, voiceSession *VoiceSession) error {
	handlers := ResponseHandlers{
		OnAudioDelta: func(ctx context.Context, audioData []byte) {
			s.handleAudioResponse(ctx, voiceSession, audioData)
		},
		OnTranscript: func(ctx context.Context, transcript string) {
			s.handleTranscript(voiceSession, transcript)
		},
		OnUserTranscript: func(ctx context.Context, transcript string) {
			s.handleUserTranscript(voiceSession, transcript)
		},
		OnResponseDone: func(ctx context.Context, usage *Usage) {
			s.handleResponseDone(ctx, voiceSession, usage)
		},
		OnError: func(ctx context.Context, err error) {
			s.logger.Error("OpenAI Realtime error", zap.Error(err))
		},
	}

	err := s.realtimeProvider.SetResponseHandlers(handlers)
	if err != nil {
		s.logger.Error("Failed to set response handlers", zap.Error(err))
		if endErr := s.endSession(ctx, voiceSession, fmt.Sprintf("failed to set response handlers: %v", err)); endErr != nil {
			s.logger.Error("failed to end session", zap.Error(endErr))
		}

		return err
	}

	return nil
}

func (s *Service) runAudioLoop(ctx context.Context, voiceSession *VoiceSession, audioChannel <-chan *AudioPacket) {
	// Use a debouncer for clean timeout handling
	timeoutDuration := time.Duration(s.cfg.SilenceDuration) * time.Millisecond
	debouncer := util.NewDebouncer(timeoutDuration)
	defer debouncer.Stop()

	s.logger.Info("Started audio processing loop",
		zap.String("guild_id", voiceSession.GuildID.String()),
		zap.Duration("timeout_duration", timeoutDuration))

	for {
		select {
		case packet, ok := <-audioChannel:
			if !ok || packet == nil {
				s.logger.Debug("Audio channel closed, exiting processAudio")

				return
			}

			s.processAudioPacket(voiceSession, packet)
			debouncer.Reset()

		case <-debouncer.C():
			s.logger.Info("Audio timeout reached, committing audio")
			s.commitMixerAudio(ctx, voiceSession)

		case <-ctx.Done():
			if err := s.endSession(ctx, voiceSession, "context canceled"); err != nil {
				s.logger.Error("failed to end session", zap.Error(err))
			}

			return
		}
	}
}

func (s *Service) processAudioPacket(voiceSession *VoiceSession, packet *AudioPacket) {
	s.logger.Debug("Processing audio packet",
		zap.String("user_id", packet.UserID.String()),
		zap.Uint32("ssrc", packet.SSRC),
		zap.Int("opus_length", len(packet.Opus)),
		zap.Uint32("rtp_timestamp", packet.RTPTimestamp),
		zap.Uint16("sequence", packet.Sequence))

	pcm, err := s.audioProcessor.OpusToPCM48(packet.Opus)
	if err != nil {
		s.logger.Error("Failed to convert Opus to PCM",
			zap.Error(err),
			zap.String("user_id", packet.UserID.String()))

		return
	}

	err = s.audioMixer.AddFrame(packet.SSRC, packet.RTPTimestamp, pcm)
	if err != nil {
		s.logger.Warn("Failed to push frame to mixer",
			zap.Error(err),
			zap.String("user_id", packet.UserID.String()))
	}

	// Update session activity and audio time
	if err := s.sessionManager.UpdateActivity(voiceSession.GuildID); err != nil {
		s.logger.Warn("failed to update session activity", zap.Error(err))
	}
	if err := s.sessionManager.UpdateAudioTime(voiceSession.GuildID); err != nil {
		s.logger.Warn("failed to update session audio time", zap.Error(err))
	}

	// Update session ActiveUsers
	voiceSession.mu.Lock()
	voiceSession.ActiveUsers[packet.UserID] = &UserState{
		UserID:       packet.UserID,
		SSRC:         packet.SSRC,
		LastActivity: time.Now(),
	}
	voiceSession.mu.Unlock()

	s.logger.Debug("Added audio to mixer",
		zap.String("user_id", packet.UserID.String()),
		zap.Uint32("rtp_timestamp", packet.RTPTimestamp))
}

// commitMixerAudio gets mixed audio from the mixer and sends it to OpenAI.
func (s *Service) commitMixerAudio(ctx context.Context, voiceSession *VoiceSession) {
	mixedAudio := s.audioMixer.Drain()

	// Check if we got any audio
	if len(mixedAudio) == 0 {
		s.logger.Debug("No audio to commit")

		return
	}

	// Update LastAudioTime
	voiceSession.mu.Lock()
	voiceSession.LastAudioTime = time.Now()
	voiceSession.mu.Unlock()

	s.logger.Info("Committing mixer audio",
		zap.String("guild_id", voiceSession.GuildID.String()),
		zap.Int("audio_bytes", len(mixedAudio)))

	// // Use DetectSilence from audio processor to avoid sending silence
	// isSilent, energy := s.audioProcessor.DetectSilence(mixedAudio)
	// if isSilent {
	// 	s.logger.Debug("Mixed audio is silent, skipping send", zap.Float32("energy", energy))

	// 	return
	// }

	s.logger.Debug("Mixed audio obtained",
		zap.Int("size", len(mixedAudio)),
		// zap.Float32("energy_level", energyLevel),
		zap.Duration("actual_duration", time.Duration(len(mixedAudio)/48000*1000)))

	// Continue with the rest of the processing
	s.processMixedAudio(ctx, voiceSession, mixedAudio)
}

func (s *Service) processMixedAudio(ctx context.Context, voiceSession *VoiceSession, mixedAudio []int16) {
	s.logger.Info("Processing mixed audio",
		zap.String("guild_id", voiceSession.GuildID.String()),
		zap.Int("size", len(mixedAudio)))

	// DEBUG: Save audio to WAV files for debugging
	// Set this to true to enable WAV file saving
	const debugSaveWAV = true
	if debugSaveWAV {
		// Save the mixed audio
		if err := s.saveDebugWAV(mixedAudio, 48000, voiceSession.GuildID, "mixed"); err != nil {
			s.logger.Error("Failed to save mixed audio WAV", zap.Error(err))
		}

		return
	}

	downsampledAudio, err := s.audioProcessor.DownsamplePCM(mixedAudio, audio.DiscordSampleRate, audio.OpenAISampleRate)
	if err != nil {
		s.logger.Error("Failed to downsample audio", zap.Error(err))

		return
	}

	// Convert PCM to base64 for OpenAI
	audioBase64, err := s.audioProcessor.PCMToBase64(audio.PCMInt16ToLE(downsampledAudio))
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

func (s *Service) handleAudioResponse(ctx context.Context, voiceSession *VoiceSession, audioData []byte) {
	s.logger.Debug("Received audio chunk from OpenAI",
		zap.Int("pcm_size", len(audioData)))

	// Queue audio data for sequential playback to avoid interference
	s.queueAudioForPlayback(ctx, voiceSession, audioData)
}

func (s *Service) queueAudioForPlayback(ctx context.Context, voiceSession *VoiceSession, audioData []byte) {
	// Send audio data to the queue
	select {
	case voiceSession.AudioQueue <- audioData:
		s.logger.Debug("Queued audio chunk",
			zap.Int("pcm_size", len(audioData)),
			zap.Int("queue_length", len(voiceSession.AudioQueue)))
	case <-ctx.Done():
		return
	default:
		s.logger.Warn("Audio queue full, dropping chunk",
			zap.Int("pcm_size", len(audioData)))
	}

	// Start playback worker if not already running
	voiceSession.PlaybackMutex.Lock()
	if !voiceSession.PlaybackActive {
		voiceSession.PlaybackActive = true
		go s.audioPlaybackWorker(ctx, voiceSession)
	}
	voiceSession.PlaybackMutex.Unlock()
}

func (s *Service) audioPlaybackWorker(ctx context.Context, voiceSession *VoiceSession) {
	defer func() {
		voiceSession.PlaybackMutex.Lock()
		voiceSession.PlaybackActive = false
		voiceSession.PlaybackMutex.Unlock()
		s.logger.Debug("Audio playback worker stopped")
	}()

	s.logger.Debug("Audio playback worker started")

	for {
		select {
		case audioData, ok := <-voiceSession.AudioQueue:
			if !ok {
				return
			}
			// Process this audio chunk sequentially
			s.splitAndPlayAudio(ctx, voiceSession, audioData)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Service) splitAndPlayAudio(ctx context.Context, voiceSession *VoiceSession, audioData []byte) {
	// Discord expects 20ms Opus frames at 48kHz stereo sent at precise 20ms intervals
	// Critical: Frame timing must be exact to prevent audio artifacts

	// 20ms at 24kHz mono = 480 samples = 960 bytes (16-bit PCM)
	const frameSizeBytes = audio.OpenAIFrameSize * 2 // 20ms at 24kHz mono in bytes (16-bit samples)
	const frameDurationMs = 20                       // Each frame represents 20ms of audio

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
		opusData, err := s.audioProcessor.PCM48MonoToOpus(audio.LEToPCMInt16(frameData))
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
		err = s.voiceManager.PlayAudio(ctx, voiceSession.ChannelID, opusData)
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

func (s *Service) handleTranscript(voiceSession *VoiceSession, transcript string) {
	s.logger.Info("AI transcript",
		zap.String("guild_id", voiceSession.GuildID.String()),
		zap.String("transcript", transcript))

	// Optionally send transcript to text channel
	// (This could be configured via a setting)
}

func (s *Service) handleUserTranscript(voiceSession *VoiceSession, transcript string) {
	s.logger.Info("User transcript",
		zap.String("guild_id", voiceSession.GuildID.String()),
		zap.String("user_id", voiceSession.InitiatorID.String()),
		zap.String("transcript", transcript))

	// Optionally send user transcript to text channel
	// (This could be configured via a setting)
}

func (s *Service) handleResponseDone(ctx context.Context, voiceSession *VoiceSession, usage *Usage) {
	if usage == nil {
		return
	}

	// Check if session still exists
	if _, err := s.sessionManager.GetSessionByGuild(voiceSession.GuildID); err != nil {
		s.logger.Debug("Session no longer active, skipping response processing",
			zap.String("guild_id", voiceSession.GuildID.String()))

		return
	}

	// Update token usage
	err := s.sessionManager.UpdateTokenUsage(voiceSession.GuildID, usage.InputAudioTokens, usage.OutputAudioTokens)
	if err != nil {
		s.logger.Debug("Failed to update token usage", zap.Error(err),
			zap.String("guild_id", voiceSession.GuildID.String()))

		return // Session was likely cleaned up
	}

	// Calculate cost using pricing service
	voiceSession.mu.Lock()
	cost, err := s.pricingService.CalculateAudioTokenCost(voiceSession.Model, voiceSession.InputAudioTokens, voiceSession.OutputAudioTokens)
	voiceSession.mu.Unlock()

	if err != nil {
		s.logger.Error("Failed to calculate session cost", zap.Error(err))
	} else {
		// Update session cost
		err = s.sessionManager.UpdateSessionCost(voiceSession.GuildID, cost)
		if err != nil {
			s.logger.Debug("Failed to update session cost", zap.Error(err),
				zap.String("guild_id", voiceSession.GuildID.String()))

			return // Session was likely cleaned up
		}

		// Check cost warnings and limits
		s.checkCostLimits(ctx, voiceSession, cost)
	}

	s.logger.Debug("Response completed",
		zap.String("guild_id", voiceSession.GuildID.String()),
		zap.Int("input_tokens", usage.InputAudioTokens),
		zap.Int("output_tokens", usage.OutputAudioTokens),
		zap.Float64("cost", cost))
}

func (s *Service) checkCostLimits(ctx context.Context, voiceSession *VoiceSession, cost float64) {
	// Check if cost limit exceeded
	if cost >= s.cfg.MaxCostPerSession {
		s.logger.Warn("Cost limit exceeded, ending session",
			zap.String("guild_id", voiceSession.GuildID.String()),
			zap.Float64("cost", cost),
			zap.Float64("limit", s.cfg.MaxCostPerSession))

		if err := s.endSession(ctx, voiceSession, fmt.Sprintf("cost limit exceeded ($%.2f)", cost)); err != nil {
			s.logger.Error("failed to end session", zap.Error(err))
		}

		return
	}

	// Show cost updates if enabled
	voiceSession.mu.Lock()
	shouldUpdate := s.cfg.TrackSessionCosts && time.Since(voiceSession.LastCostUpdate) > 30*time.Second
	if shouldUpdate {
		// TODO: Send cost update to text channel
		s.logger.Info("Session cost update",
			zap.String("guild_id", voiceSession.GuildID.String()),
			zap.Float64("cost", cost),
			zap.Int("input_tokens", voiceSession.InputAudioTokens),
			zap.Int("output_tokens", voiceSession.OutputAudioTokens))

		// Update the last cost update time so we don't spam logs
		voiceSession.LastCostUpdate = time.Now()
	}
	voiceSession.mu.Unlock()
}

func (s *Service) endSession(ctx context.Context, voiceSession *VoiceSession, reason string) error {
	// Prevent double-ending a session
	voiceSession.mu.Lock()
	if voiceSession.State == SessionStateEnding || voiceSession.State == SessionStateEnded {
		voiceSession.mu.Unlock()

		return nil
	}
	voiceSession.State = SessionStateEnding
	voiceSession.mu.Unlock()

	// Cancel session context
	if voiceSession.CancelFunc != nil {
		voiceSession.CancelFunc()
	}

	// Close OpenAI connection
	if voiceSession.Connection != nil {
		// Try to close the connection if it implements io.Closer
		if closer, ok := voiceSession.Connection.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				s.logger.Warn("Failed to close OpenAI connection", zap.Error(err))
			} else {
				s.logger.Debug("Successfully closed OpenAI connection")
			}
		}
	}

	// Close audio queue to signal workers to stop
	close(voiceSession.AudioQueue)

	// Leave voice channel
	err := s.voiceManager.LeaveChannel(ctx, voiceSession.ChannelID)
	if err != nil {
		s.logger.Warn("Failed to leave voice channel", zap.Error(err))
	}

	// Remove from session manager
	err = s.sessionManager.EndSession(voiceSession.GuildID)
	if err != nil {
		s.logger.Warn("Failed to end session in session manager", zap.Error(err))
	}

	voiceSession.mu.Lock()
	voiceSession.State = SessionStateEnded
	voiceSession.mu.Unlock()

	s.logger.Info("Voice session ended",
		zap.String("guild_id", voiceSession.GuildID.String()),
		zap.String("reason", reason),
		zap.Float64("cost", voiceSession.SessionCost))

	return nil
}

func (s *Service) runWatchdog(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			activeSessions := s.sessionManager.GetActiveSessions()
			for _, voiceSession := range activeSessions {
				voiceSession.mu.Lock()
				lastAudioTime := voiceSession.LastAudioTime
				startTime := voiceSession.StartTime
				sessionCost := voiceSession.SessionCost

				// Clean up stale ActiveUsers entries (users who haven't been seen for 30 seconds)
				for userID, userState := range voiceSession.ActiveUsers {
					if time.Since(userState.LastActivity) > 30*time.Second {
						delete(voiceSession.ActiveUsers, userID)
						s.logger.Debug("Removed stale user from ActiveUsers",
							zap.String("guild_id", voiceSession.GuildID.String()),
							zap.String("user_id", userID.String()),
							zap.Duration("inactive_duration", time.Since(userState.LastActivity)))
					}
				}

				voiceSession.mu.Unlock()

				// Check inactivity timeout
				if time.Since(lastAudioTime) > time.Duration(s.cfg.InactivityTimeout)*time.Second {
					if err := s.endSession(ctx, voiceSession, "inactivity timeout"); err != nil {
						s.logger.Error("failed to end session", zap.Error(err))
					}

					continue
				}

				// Check session duration
				if time.Since(startTime) > time.Duration(s.cfg.MaxSessionLength)*time.Minute {
					if err := s.endSession(ctx, voiceSession, "maximum session length reached"); err != nil {
						s.logger.Error("failed to end session", zap.Error(err))
					}

					continue
				}

				// Check cost limit
				if sessionCost >= s.cfg.MaxCostPerSession {
					if err := s.endSession(ctx, voiceSession, fmt.Sprintf("cost limit reached ($%.2f)", sessionCost)); err != nil {
						s.logger.Error("failed to end session", zap.Error(err))
					}

					continue
				}
			}
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
	activeSessions := s.sessionManager.GetActiveSessions()
	for _, voiceSession := range activeSessions {
		if err := s.endSession(ctx, voiceSession, "service shutdown"); err != nil {
			s.logger.Error("failed to end session during shutdown", zap.Error(err))
		}
	}

	return nil
}
