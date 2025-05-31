package voice

import (
	"maps"
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
	maps.Copy(sessions, sm.sessions)

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



// Error definitions
var (
	ErrSessionAlreadyExists = NewVoiceError("session already exists for this guild")
	ErrSessionNotFound      = NewVoiceError("session not found")
	ErrMaxSessionsReached   = NewVoiceError("maximum concurrent sessions reached")
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