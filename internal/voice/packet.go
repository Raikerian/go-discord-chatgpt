package voice

import (
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/voice/udp"
)

type AudioPacket struct {
	UserID       discord.UserID
	SSRC         uint32
	Opus         []byte
	RTPTimestamp uint32
	Sequence     uint16
}

func NewAudioPacket(userID discord.UserID, packet *udp.Packet) *AudioPacket {
	return &AudioPacket{
		UserID:       userID,
		SSRC:         packet.SSRC(),
		Opus:         packet.Opus,
		RTPTimestamp: packet.Timestamp(),
		Sequence:     packet.Sequence(),
	}
}
