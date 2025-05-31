package voice

import (
	"sync"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/diamondburned/arikawa/v3/discord"
	"go.uber.org/zap"
)

type SessionManager interface {
	// Create new session
	CreateSession(guildID discord.GuildID, channelID discord.ChannelID, initiatorID discord.UserID) (*VoiceSession, error)

	// Get active session by guild
	GetSessionByGuild(guildID discord.GuildID) (*VoiceSession, error)

	// Update session activity
	UpdateActivity(guildID discord.GuildID) error

	// Check for inactive sessions
	CleanupInactiveSessions(timeout time.Duration) []discord.GuildID

	// End session
	EndSession(guildID discord.GuildID) error

	// Get all active sessions
	GetActiveSessions() map[discord.GuildID]*VoiceSession

	// Update session cost
	UpdateSessionCost(guildID discord.GuildID, cost float64) error

	// Update audio time
	UpdateAudioTime(guildID discord.GuildID) error

	// Update token usage
	UpdateTokenUsage(guildID discord.GuildID, inputTokens, outputTokens int) error

	// Add active user
	AddActiveUser(guildID discord.GuildID, userID discord.UserID, ssrc uint32) error

	// Remove active user
	RemoveActiveUser(guildID discord.GuildID, userID discord.UserID) error

	// Update user speaking state
	UpdateUserSpeakingState(guildID discord.GuildID, userID discord.UserID, speaking bool) error
}

type sessionManager struct {
	logger   *zap.Logger
	cfg      *config.VoiceConfig
	sessions map[discord.GuildID]*VoiceSession
	mu       sync.RWMutex
}

func NewSessionManager(logger *zap.Logger, cfg *config.Config) SessionManager {
	return &sessionManager{
		logger:   logger,
		cfg:      &cfg.Voice,
		sessions: make(map[discord.GuildID]*VoiceSession),
	}
}

func (sm *sessionManager) CreateSession(guildID discord.GuildID, channelID discord.ChannelID, initiatorID discord.UserID) (*VoiceSession, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if session already exists
	if _, exists := sm.sessions[guildID]; exists {
		return nil, ErrSessionAlreadyExists
	}

	// Check session limit
	if len(sm.sessions) >= sm.cfg.MaxConcurrentSessions {
		return nil, ErrMaxSessionsReached
	}

	// Create new session
	session := &VoiceSession{
		GuildID:       guildID,
		ChannelID:     channelID,
		InitiatorID:   initiatorID,
		StartTime:     time.Now(),
		LastActivity:  time.Now(),
		LastAudioTime: time.Now(),
		State:         SessionStateStarting,
		ActiveUsers:   make(map[discord.UserID]*UserState),
		AudioBuffer:   &AudioBuffer{},
	}

	sm.sessions[guildID] = session

	sm.logger.Info("Voice session created",
		zap.String("guild_id", guildID.String()),
		zap.String("channel_id", channelID.String()),
		zap.String("initiator_id", initiatorID.String()))

	return session, nil
}

func (sm *sessionManager) GetSessionByGuild(guildID discord.GuildID) (*VoiceSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return nil, ErrSessionNotFound
	}

	return session, nil
}

func (sm *sessionManager) UpdateActivity(guildID discord.GuildID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return ErrSessionNotFound
	}

	session.LastActivity = time.Now()

	sm.logger.Debug("Session activity updated",
		zap.String("guild_id", guildID.String()))

	return nil
}

func (sm *sessionManager) CleanupInactiveSessions(timeout time.Duration) []discord.GuildID {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var inactiveGuilds []discord.GuildID
	now := time.Now()

	for guildID, session := range sm.sessions {
		// Check for inactivity
		if now.Sub(session.LastAudioTime) > timeout {
			inactiveGuilds = append(inactiveGuilds, guildID)
			delete(sm.sessions, guildID)

			sm.logger.Info("Session cleaned up due to inactivity",
				zap.String("guild_id", guildID.String()),
				zap.Duration("inactive_for", now.Sub(session.LastAudioTime)))
		}

		// Check for maximum session length
		maxDuration := time.Duration(sm.cfg.MaxSessionLength) * time.Minute
		if now.Sub(session.StartTime) > maxDuration {
			inactiveGuilds = append(inactiveGuilds, guildID)
			delete(sm.sessions, guildID)

			sm.logger.Info("Session cleaned up due to maximum length",
				zap.String("guild_id", guildID.String()),
				zap.Duration("session_length", now.Sub(session.StartTime)))
		}
	}

	return inactiveGuilds
}

func (sm *sessionManager) EndSession(guildID discord.GuildID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return ErrSessionNotFound
	}

	// Mark session as ended
	session.State = SessionStateEnded

	// Remove from active sessions
	delete(sm.sessions, guildID)

	sessionDuration := time.Since(session.StartTime)

	sm.logger.Info("Voice session ended",
		zap.String("guild_id", guildID.String()),
		zap.String("channel_id", session.ChannelID.String()),
		zap.Duration("duration", sessionDuration),
		zap.Float64("cost", session.SessionCost))

	return nil
}

func (sm *sessionManager) GetActiveSessions() map[discord.GuildID]*VoiceSession {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Return a copy to avoid concurrent access issues
	sessions := make(map[discord.GuildID]*VoiceSession)
	for guildID, session := range sm.sessions {
		sessions[guildID] = session
	}

	return sessions
}

// Helper methods for session state management

func (sm *sessionManager) UpdateSessionCost(guildID discord.GuildID, cost float64) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return ErrSessionNotFound
	}

	session.SessionCost = cost
	session.LastCostUpdate = time.Now()

	return nil
}

func (sm *sessionManager) UpdateAudioTime(guildID discord.GuildID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return ErrSessionNotFound
	}

	session.LastAudioTime = time.Now()
	session.LastActivity = time.Now()

	return nil
}

func (sm *sessionManager) UpdateTokenUsage(guildID discord.GuildID, inputTokens, outputTokens int) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return ErrSessionNotFound
	}

	session.InputAudioTokens += inputTokens
	session.OutputAudioTokens += outputTokens

	return nil
}

func (sm *sessionManager) AddActiveUser(guildID discord.GuildID, userID discord.UserID, ssrc uint32) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return ErrSessionNotFound
	}

	userState := &UserState{
		UserID:       userID,
		SSRC:         ssrc,
		LastActivity: time.Now(),
		IsSpeaking:   false,
		AudioBuffer:  make([]byte, 0),
	}

	session.ActiveUsers[userID] = userState

	sm.logger.Debug("User added to voice session",
		zap.String("guild_id", guildID.String()),
		zap.String("user_id", userID.String()),
		zap.Uint32("ssrc", ssrc))

	return nil
}

func (sm *sessionManager) RemoveActiveUser(guildID discord.GuildID, userID discord.UserID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return ErrSessionNotFound
	}

	delete(session.ActiveUsers, userID)

	sm.logger.Debug("User removed from voice session",
		zap.String("guild_id", guildID.String()),
		zap.String("user_id", userID.String()))

	return nil
}

func (sm *sessionManager) UpdateUserSpeakingState(guildID discord.GuildID, userID discord.UserID, speaking bool) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[guildID]
	if !exists {
		return ErrSessionNotFound
	}

	userState, userExists := session.ActiveUsers[userID]
	if !userExists {
		return ErrUserNotInSession
	}

	userState.IsSpeaking = speaking
	userState.LastActivity = time.Now()

	return nil
}

// GetSessionStats returns statistics about active sessions
func (sm *sessionManager) GetSessionStats() SessionStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := SessionStats{
		ActiveSessions: len(sm.sessions),
		TotalUsers:     0,
		TotalCost:      0.0,
	}

	for _, session := range sm.sessions {
		stats.TotalUsers += len(session.ActiveUsers)
		stats.TotalCost += session.SessionCost
	}

	return stats
}

// SessionStats provides statistics about voice sessions
type SessionStats struct {
	ActiveSessions int
	TotalUsers     int
	TotalCost      float64
}

// Error definitions
var (
	ErrSessionAlreadyExists = NewVoiceError("session already exists for this guild")
	ErrSessionNotFound      = NewVoiceError("session not found")
	ErrMaxSessionsReached   = NewVoiceError("maximum concurrent sessions reached")
	ErrUserNotInSession     = NewVoiceError("user not found in session")
)

// VoiceError represents errors specific to voice operations
type VoiceError struct {
	message string
}

func NewVoiceError(message string) *VoiceError {
	return &VoiceError{message: message}
}

func (e *VoiceError) Error() string {
	return e.message
}