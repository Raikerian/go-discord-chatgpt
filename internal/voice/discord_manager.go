package voice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/voice"
	"github.com/diamondburned/arikawa/v3/voice/voicegateway"
	"go.uber.org/zap"
)

type DiscordManager interface {
	// Connect to a voice channel
	JoinChannel(ctx context.Context, channelID discord.ChannelID) (*VoiceConnection, error)

	// Disconnect from a voice channel
	LeaveChannel(ctx context.Context, channelID discord.ChannelID) error

	// Play audio to the channel
	PlayAudio(ctx context.Context, channelID discord.ChannelID, audio []byte) error

	// Start receiving audio packets
	StartReceiving(ctx context.Context, channelID discord.ChannelID) (<-chan *AudioPacket, error)
}

type VoiceConnection struct {
	ChannelID   discord.ChannelID
	GuildID     discord.GuildID
	ConnectedAt time.Time
	Session     *voice.Session // Arikawa voice session
}

type discordManager struct {
	logger  *zap.Logger
	session *session.Session

	activeConnections sync.Map // map[discord.ChannelID]*VoiceConnection
}

func NewDiscordVoiceManager(logger *zap.Logger, session *session.Session) DiscordManager {
	return &discordManager{
		logger:  logger,
		session: session,
	}
}

func (m *discordManager) JoinChannel(ctx context.Context, channelID discord.ChannelID) (*VoiceConnection, error) {
	// Check if already connected using sync.Map.Load
	if value, exists := m.activeConnections.Load(channelID); exists {
		if conn, ok := value.(*VoiceConnection); ok {
			return conn, nil
		}
	}

	// Get channel info to determine guild
	channel, err := m.session.Channel(channelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get channel info: %w", err)
	}

	if channel.Type != discord.GuildVoice {
		return nil, fmt.Errorf("channel %s is not a voice channel", channelID)
	}

	// Create voice session using arikawa
	voiceSession, err := voice.NewSession(m.session)
	if err != nil {
		return nil, fmt.Errorf("failed to create voice session: %w", err)
	}

	// Join the voice channel
	err = voiceSession.JoinChannel(ctx, channelID, false, false)
	if err != nil {
		return nil, fmt.Errorf("failed to join voice channel: %w", err)
	}

	// Set speaking mode to properly initialize voice connection
	// This is required to receive audio packets
	err = voiceSession.Speaking(ctx, voicegateway.Microphone)
	if err != nil {
		return nil, fmt.Errorf("failed to set speaking mode: %w", err)
	}

	// CRITICAL: Force UDP connection initialization for packet reception
	// arikawa v3 doesn't fully establish the UDP socket until the first Write() call.
	// Without this, ReadPacket() will block indefinitely because the UDP connection
	// is not bidirectionally ready. The empty write triggers the UDP handshake with
	// Discord's voice servers and enables both sending AND receiving of audio packets.
	// This is a known requirement in Discord's voice protocol implementation.
	testData := make([]byte, 0) // Empty data to trigger UDP initialization
	_, _ = voiceSession.Write(testData)

	// Add debug logging
	m.logger.Info("Voice session configured",
		zap.String("channel_id", channelID.String()),
		zap.String("guild_id", channel.GuildID.String()))

	conn := &VoiceConnection{
		ChannelID:   channelID,
		GuildID:     channel.GuildID,
		ConnectedAt: time.Now(),
		Session:     voiceSession,
	}

	m.activeConnections.Store(channelID, conn)

	m.logger.Info("Joined voice channel",
		zap.String("channel_id", channelID.String()),
		zap.String("guild_id", channel.GuildID.String()))

	return conn, nil
}

func (m *discordManager) LeaveChannel(ctx context.Context, channelID discord.ChannelID) error {
	value, exists := m.activeConnections.LoadAndDelete(channelID)
	if !exists {
		return nil // Already disconnected
	}

	conn, ok := value.(*VoiceConnection)
	if !ok {
		return nil // Invalid connection type, just return
	}

	// Leave the voice channel using arikawa
	if conn.Session != nil {
		err := conn.Session.Leave(ctx)
		if err != nil {
			m.logger.Warn("Failed to leave voice channel cleanly", zap.Error(err))
		}
	}

	m.logger.Info("Left voice channel",
		zap.String("channel_id", channelID.String()),
		zap.String("guild_id", conn.GuildID.String()))

	return nil
}

func (m *discordManager) PlayAudio(ctx context.Context, channelID discord.ChannelID, audio []byte) error {
	value, exists := m.activeConnections.Load(channelID)
	if !exists {
		return fmt.Errorf("not connected to voice channel %s", channelID)
	}

	conn, ok := value.(*VoiceConnection)
	if !ok {
		return fmt.Errorf("invalid connection type for channel %s", channelID)
	}

	if conn.Session == nil {
		return fmt.Errorf("voice session not available for channel %s", channelID)
	}

	// Send audio data using arikawa voice session
	// Note: This assumes the audio is already in the correct format (Opus)
	_, err := conn.Session.Write(audio)
	if err != nil {
		return fmt.Errorf("failed to play audio: %w", err)
	}

	m.logger.Debug("Playing audio",
		zap.String("channel_id", channelID.String()),
		zap.Int("audio_size", len(audio)))

	return nil
}

func (m *discordManager) StartReceiving(ctx context.Context, channelID discord.ChannelID) (<-chan *AudioPacket, error) {
	value, exists := m.activeConnections.Load(channelID)
	if !exists {
		return nil, fmt.Errorf("not connected to voice channel %s", channelID)
	}

	conn, ok := value.(*VoiceConnection)
	if !ok {
		return nil, fmt.Errorf("invalid connection type for channel %s", channelID)
	}

	if conn.Session == nil {
		return nil, fmt.Errorf("voice session not available for channel %s", channelID)
	}

	audioChannel := make(chan *AudioPacket, 100)

	// Start audio receiving using arikawa voice session
	go func() {
		defer close(audioChannel)

		m.logger.Info("Started receiving audio",
			zap.String("channel_id", channelID.String()),
			zap.String("guild_id", conn.GuildID.String()))

		// Set up packet receiver from arikawa voice session
		// The arikawa voice session provides a ReadPacket method to receive audio
		for {
			select {
			case <-ctx.Done():
				m.logger.Info("Stopped receiving audio",
					zap.String("channel_id", channelID.String()))

				return
			default:
				// Read audio packet from the voice session
				// Note: This is a blocking call, so we check context periodically
				m.logger.Debug("Waiting for audio packet...")
				packet, err := conn.Session.ReadPacket()
				if err != nil {
					// Check if context was canceled
					if ctx.Err() != nil {
						m.logger.Info("Context canceled, stopping audio receive")

						return
					}
					m.logger.Debug("Failed to read voice packet",
						zap.Error(err),
						zap.String("channel_id", channelID.String()))

					continue
				}

				// For now, we'll use SSRC as a temporary identifier
				// TODO: Implement proper SSRC to UserID mapping via voice gateway events
				ssrc := packet.SSRC()
				userID := discord.UserID(ssrc)

				// Log packet reception for debugging
				m.logger.Debug("Received audio packet",
					zap.Uint32("ssrc", ssrc),
					zap.Int("opus_length", len(packet.Opus)),
					zap.Uint32("rtp_timestamp", packet.Timestamp()),
					zap.Uint16("sequence", packet.Sequence()),
					zap.String("channel_id", channelID.String()))

				// Convert arikawa voice packet to our AudioPacket format
				audioPacket := NewAudioPacket(userID, packet)

				// Send packet to channel (non-blocking)
				select {
				case audioChannel <- audioPacket:
					m.logger.Debug("Sent audio packet to processing channel",
						zap.String("user_id", userID.String()))
				case <-ctx.Done():
					return
				default:
					// Channel is full, drop packet
					m.logger.Warn("Audio channel full, dropping packet",
						zap.String("user_id", userID.String()))
				}
			}
		}
	}()

	return audioChannel, nil
}
