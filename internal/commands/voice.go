package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Raikerian/go-discord-chatgpt/internal/config"
	"github.com/Raikerian/go-discord-chatgpt/internal/voice"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"go.uber.org/zap"
)

type VoiceCommand struct {
	logger       *zap.Logger
	cfg          *config.Config
	voiceService *voice.Service
	session      *session.Session
	state        *state.State
}

func NewVoiceCommand(
	logger *zap.Logger,
	cfg *config.Config,
	voiceService *voice.Service,
	sess *session.Session,
	st *state.State,
) Command {
	return &VoiceCommand{
		logger:       logger,
		cfg:          cfg,
		voiceService: voiceService,
		session:      sess,
		state:        st,
	}
}

func (c *VoiceCommand) Name() string {
	return "voice"
}

func (c *VoiceCommand) Description() string {
	return "Control voice chat AI assistant"
}

func (c *VoiceCommand) Options() []discord.CommandOption {
	return []discord.CommandOption{
		&discord.StringOption{
			OptionName:  "action",
			Description: "Action to perform",
			Required:    true,
			Choices: []discord.StringChoice{
				{Name: "start", Value: "start"},
				{Name: "stop", Value: "stop"},
				{Name: "status", Value: "status"},
			},
		},
		&discord.StringOption{
			OptionName:  "model",
			Description: "AI model to use (optional)",
			Required:    false,
		},
	}
}

func (c *VoiceCommand) Execute(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error {
	// Get action parameter
	var action string
	var model string

	for _, option := range data.Options {
		switch option.Name {
		case "action":
			action = option.String()
			c.logger.Debug("Extracted action parameter", zap.String("action", action))
		case "model":
			if len(option.Value) > 0 {
				model = option.String()
				c.logger.Debug("Extracted model parameter", zap.String("model", model))
			}
		}
	}

	// Get guild and channel information
	if e.GuildID == 0 {
		return c.respondError(s, e.ID, e.Token, "Voice commands can only be used in servers")
	}

	guildID := e.GuildID
	channelID := e.ChannelID
	userID := e.SenderID() // This method handles both guild and DM contexts

	// Execute action
	switch action {
	case "start":
		return c.handleStart(ctx, s, e, guildID, channelID, userID, model)
	case "stop":
		return c.handleStop(ctx, s, e, guildID, userID)
	case "status":
		return c.handleStatus(ctx, s, e, guildID)
	default:
		return c.respondError(s, e.ID, e.Token, "Unknown action: "+action)
	}
}

func (c *VoiceCommand) handleStart(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, guildID discord.GuildID, textChannelID discord.ChannelID, userID discord.UserID, model string) error {
	// Try to get the user's voice channel
	voiceChannelID, err := c.getUserVoiceChannel(s, guildID, userID)
	if err != nil {
		c.logger.Debug("Failed to get user voice channel",
			zap.Error(err),
			zap.String("user_id", userID.String()),
			zap.String("guild_id", guildID.String()))

		return c.respondError(s, e.ID, e.Token, "Please join a voice channel first, or ensure the bot can see voice channels in this server")
	}

	// Send immediate response to avoid timeout
	initialResp := api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString("🎤 Starting voice AI session..."),
		},
	}

	err = s.RespondInteraction(e.ID, e.Token, initialResp)
	if err != nil {
		c.logger.Error("Failed to respond to voice start interaction", zap.Error(err))

		return err
	}

	// Start voice session asynchronously to avoid blocking the interaction response
	go func() {
		voiceSession, err := c.voiceService.Start(ctx, guildID, voiceChannelID, textChannelID, userID, model)
		if err != nil {
			c.logger.Error("Failed to start voice session",
				zap.Error(err),
				zap.String("guild_id", guildID.String()),
				zap.String("user_id", userID.String()))

			// Send follow-up message with error
			errorMsg := "❌ Failed to start voice session: " + err.Error()
			_, followUpErr := s.SendMessage(textChannelID, errorMsg)
			if followUpErr != nil {
				c.logger.Error("Failed to send error follow-up message", zap.Error(followUpErr))
			}

			return
		}

		usedModel := voiceSession.Model
		if usedModel == "" {
			usedModel = c.cfg.Voice.DefaultModel
		}

		successMsg := fmt.Sprintf("✅ Voice AI started in <#%s>\n🤖 Model: `%s`\n\nJust speak in the voice channel and I'll respond!",
			voiceChannelID, usedModel)

		// Send success follow-up message
		_, followUpErr := s.SendMessage(textChannelID, successMsg)
		if followUpErr != nil {
			c.logger.Error("Failed to send success follow-up message", zap.Error(followUpErr))
		}

		// Show cost warning if enabled
		if c.cfg.Voice.ShowCostWarnings {
			costWarning := "⚠️ Voice sessions cost approximately $0.30/minute. "
			if c.cfg.Voice.MaxCostPerSession > 0 {
				costWarning += fmt.Sprintf("Session will auto-stop at $%.2f.", c.cfg.Voice.MaxCostPerSession)
			}

			time.Sleep(1 * time.Second) // Brief delay before cost warning
			_, costErr := s.SendMessage(textChannelID, costWarning)
			if costErr != nil {
				c.logger.Error("Failed to send cost warning message", zap.Error(costErr))
			}
		}
	}()

	return nil
}

func (c *VoiceCommand) handleStop(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, guildID discord.GuildID, userID discord.UserID) error {
	err := c.voiceService.Stop(ctx, guildID, userID)
	if err != nil {
		if strings.Contains(err.Error(), "no active voice session") {
			return c.respondError(s, e.ID, e.Token, "No active voice session in this server")
		}
		if strings.Contains(err.Error(), "permission") {
			return c.respondError(s, e.ID, e.Token, "You don't have permission to stop this voice session")
		}

		c.logger.Error("Failed to stop voice session",
			zap.Error(err),
			zap.String("guild_id", guildID.String()),
			zap.String("user_id", userID.String()))

		return c.respondError(s, e.ID, e.Token, "Failed to stop voice session: "+err.Error())
	}

	resp := api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString("🔇 Voice AI session stopped"),
		},
	}

	return s.RespondInteraction(e.ID, e.Token, resp)
}

func (c *VoiceCommand) handleStatus(_ context.Context, s *session.Session, e *gateway.InteractionCreateEvent, guildID discord.GuildID) error {
	status, err := c.voiceService.GetStatus(guildID)
	if err != nil {
		c.logger.Error("Failed to get voice session status",
			zap.Error(err),
			zap.String("guild_id", guildID.String()))

		return c.respondError(s, e.ID, e.Token, "Failed to get voice session status")
	}

	var responseText string
	if !status.Active {
		responseText = "No active voice session in this server"
	} else {
		// Format status information
		duration := time.Since(status.StartTime).Round(time.Second)
		activeUsersList := ""

		if len(status.ActiveUsers) > 0 {
			userMentions := make([]string, len(status.ActiveUsers))
			for i, userID := range status.ActiveUsers {
				userMentions[i] = fmt.Sprintf("<@%s>", userID)
			}
			activeUsersList = "\n👥 Active users: " + strings.Join(userMentions, ", ")
		}

		costInfo := ""
		if c.cfg.Voice.TrackSessionCosts && status.SessionCost > 0 {
			costInfo = fmt.Sprintf("\n💰 Session cost: $%.2f", status.SessionCost)
		}

		responseText = fmt.Sprintf("🎤 Voice AI Status\n🔊 Channel: <#%s>\n🤖 Model: `%s`\n⏱️ Duration: %s%s%s",
			status.ChannelID, status.Model, duration, activeUsersList, costInfo)
	}

	resp := api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString(responseText),
		},
	}

	return s.RespondInteraction(e.ID, e.Token, resp)
}

func (c *VoiceCommand) getUserVoiceChannel(s *session.Session, guildID discord.GuildID, userID discord.UserID) (discord.ChannelID, error) {
	// Try to get the user's voice state from the state manager
	voiceState, err := c.state.VoiceState(guildID, userID)
	if err != nil {
		c.logger.Debug("Failed to get voice state",
			zap.Error(err),
			zap.String("user_id", userID.String()),
			zap.String("guild_id", guildID.String()))

		// If state lookup fails, try the fallback approach
		return c.getUserVoiceChannelFallback(s, guildID, userID)
	}

	if voiceState == nil {
		c.logger.Debug("User not in voice channel",
			zap.String("user_id", userID.String()),
			zap.String("guild_id", guildID.String()))

		return 0, errors.New("user is not currently in a voice channel")
	}

	c.logger.Debug("Found user in voice channel",
		zap.String("user_id", userID.String()),
		zap.String("channel_id", voiceState.ChannelID.String()))

	return voiceState.ChannelID, nil
}

// getUserVoiceChannelFallback attempts to find the user by checking all voice channels.
func (c *VoiceCommand) getUserVoiceChannelFallback(_ *session.Session, guildID discord.GuildID, userID discord.UserID) (discord.ChannelID, error) {
	// Get all voice states for the guild as a fallback
	voiceStates, err := c.state.VoiceStates(guildID)
	if err != nil {
		c.logger.Debug("Failed to get guild voice states",
			zap.Error(err),
			zap.String("guild_id", guildID.String()))

		return 0, errors.New("unable to query voice states - ensure bot has GUILD_VOICE_STATES intent and permissions")
	}

	// Search through all voice states for the user
	for _, voiceState := range voiceStates {
		if voiceState.UserID == userID {
			c.logger.Debug("Found user in voice channel via fallback",
				zap.String("user_id", userID.String()),
				zap.String("channel_id", voiceState.ChannelID.String()))

			return voiceState.ChannelID, nil
		}
	}

	return 0, errors.New("user is not currently in a voice channel")
}

func (c *VoiceCommand) respondError(s *session.Session, interactionID discord.InteractionID, token, message string) error {
	resp := api.InteractionResponse{
		Type: api.MessageInteractionWithSource,
		Data: &api.InteractionResponseData{
			Content: option.NewNullableString("❌ " + message),
			Flags:   discord.EphemeralMessage,
		},
	}

	err := s.RespondInteraction(interactionID, token, resp)
	if err != nil {
		c.logger.Error("Failed to send error response", zap.Error(err), zap.String("message", message))
	}

	return err
}
