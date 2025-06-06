package voice

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
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
	logger       *zap.Logger
	cfg          *config.VoiceConfig
	sessions     sync.Map // map[discord.GuildID]*VoiceSession
	sessionCount int64    // atomic counter for active sessions
}

func NewSessionManager(logger *zap.Logger, cfg *config.Config) SessionManager {
	return &sessionManager{
		logger: logger,
		cfg:    &cfg.Voice,
	}
}

func (sm *sessionManager) CreateSession(guildID discord.GuildID, channelID discord.ChannelID, initiatorID discord.UserID) (*VoiceSession, error) {
	// Check if session already exists
	if _, exists := sm.sessions.Load(guildID); exists {
		return nil, ErrSessionAlreadyExists
	}

	// Check session limit
	if atomic.LoadInt64(&sm.sessionCount) >= int64(sm.cfg.MaxConcurrentSessions) {
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

	// Use LoadOrStore to handle race condition
	if _, loaded := sm.sessions.LoadOrStore(guildID, session); loaded {
		return nil, ErrSessionAlreadyExists
	}

	// Increment session counter
	atomic.AddInt64(&sm.sessionCount, 1)

	sm.logger.Info("Voice session created",
		zap.String("guild_id", guildID.String()),
		zap.String("channel_id", channelID.String()),
		zap.String("initiator_id", initiatorID.String()))

	return session, nil
}

func (sm *sessionManager) GetSessionByGuild(guildID discord.GuildID) (*VoiceSession, error) {
	value, exists := sm.sessions.Load(guildID)
	if !exists {
		return nil, ErrSessionNotFound
	}

	session := value.(*VoiceSession)

	return session, nil
}

func (sm *sessionManager) UpdateActivity(guildID discord.GuildID) error {
	value, exists := sm.sessions.Load(guildID)
	if !exists {
		return ErrSessionNotFound
	}

	session := value.(*VoiceSession)
	session.LastActivity = time.Now()

	sm.logger.Debug("Session activity updated",
		zap.String("guild_id", guildID.String()))

	return nil
}

func (sm *sessionManager) EndSession(guildID discord.GuildID) error {
	value, exists := sm.sessions.LoadAndDelete(guildID)
	if !exists {
		return ErrSessionNotFound
	}

	session := value.(*VoiceSession)

	// Mark session as ended
	session.State = SessionStateEnded

	// Decrement session counter
	atomic.AddInt64(&sm.sessionCount, -1)

	sessionDuration := time.Since(session.StartTime)

	sm.logger.Info("Voice session ended",
		zap.String("guild_id", guildID.String()),
		zap.String("channel_id", session.ChannelID.String()),
		zap.Duration("duration", sessionDuration),
		zap.Float64("cost", session.SessionCost))

	return nil
}

func (sm *sessionManager) GetActiveSessions() map[discord.GuildID]*VoiceSession {
	sessions := make(map[discord.GuildID]*VoiceSession)

	sm.sessions.Range(func(key, value interface{}) bool {
		guildID := key.(discord.GuildID)
		session := value.(*VoiceSession)
		sessions[guildID] = session

		return true
	})

	return sessions
}

// Helper methods for session state management

func (sm *sessionManager) UpdateSessionCost(guildID discord.GuildID, cost float64) error {
	value, exists := sm.sessions.Load(guildID)
	if !exists {
		return ErrSessionNotFound
	}

	session := value.(*VoiceSession)
	session.SessionCost = cost
	session.LastCostUpdate = time.Now()

	return nil
}

func (sm *sessionManager) UpdateAudioTime(guildID discord.GuildID) error {
	value, exists := sm.sessions.Load(guildID)
	if !exists {
		return ErrSessionNotFound
	}

	session := value.(*VoiceSession)
	session.LastAudioTime = time.Now()
	session.LastActivity = time.Now()

	return nil
}

func (sm *sessionManager) UpdateTokenUsage(guildID discord.GuildID, inputTokens, outputTokens int) error {
	value, exists := sm.sessions.Load(guildID)
	if !exists {
		return ErrSessionNotFound
	}

	session := value.(*VoiceSession)
	session.InputAudioTokens += inputTokens
	session.OutputAudioTokens += outputTokens

	return nil
}

// Error definitions.
var (
	ErrSessionAlreadyExists = NewVoiceError("session already exists for this guild")
	ErrSessionNotFound      = NewVoiceError("session not found")
	ErrMaxSessionsReached   = NewVoiceError("maximum concurrent sessions reached")
)

// VoiceError represents errors specific to voice operations.
type VoiceError struct {
	message string
}

func NewVoiceError(message string) *VoiceError {
	return &VoiceError{message: message}
}

func (e *VoiceError) Error() string {
	return e.message
}
