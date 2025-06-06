package voice

import (
	"context"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/voice/udp"
)

// VoiceSession represents an active voice session in a guild.
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

// SessionState represents the current state of a voice session.
type SessionState int

const (
	SessionStateStarting SessionState = iota
	SessionStateActive
	SessionStateEnding
	SessionStateEnded
)

// UserState tracks individual user activity within a session.
type UserState struct {
	UserID       discord.UserID
	SSRC         uint32
	LastActivity time.Time
}

// SessionStatus provides a read-only view of session status.
type SessionStatus struct {
	Active      bool
	GuildID     discord.GuildID
	ChannelID   discord.ChannelID
	StartTime   time.Time
	ActiveUsers []discord.UserID
	SessionCost float64
	Model       string
}

// AudioPacket represents an audio packet received from Discord.
type AudioPacket struct {
	UserID       discord.UserID
	SSRC         uint32
	Opus         []byte
	RTPTimestamp uint32
	Sequence     uint16
}

// NewAudioPacket creates a new AudioPacket from a UDP packet.
func NewAudioPacket(userID discord.UserID, packet *udp.Packet) *AudioPacket {
	return &AudioPacket{
		UserID:       userID,
		SSRC:         packet.SSRC(),
		Opus:         packet.Opus,
		RTPTimestamp: packet.Timestamp(),
		Sequence:     packet.Sequence(),
	}
}
