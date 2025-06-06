// Package config provides configuration loading and management functionality.
package config

import (
	"os"

	"github.com/diamondburned/arikawa/v3/discord"
	"gopkg.in/yaml.v3"
)

type DiscordConfig struct {
	BotToken                  string             `yaml:"bot_token"`
	ApplicationID             *discord.Snowflake `yaml:"application_id"`
	GuildIDs                  []string           `yaml:"guild_ids"`
	InteractionTimeoutSeconds int                `yaml:"interaction_timeout_seconds"`
}

type OpenAIConfig struct {
	APIKey                  string   `yaml:"api_key"`
	Models                  []string `yaml:"models"`
	MessageCacheSize        int      `yaml:"message_cache_size"`
	NegativeThreadCacheSize int      `yaml:"negative_thread_cache_size"`
	MaxConcurrentRequests   int      `yaml:"max_concurrent_requests"`
}

type VoiceConfig struct {
	// Model Configuration
	DefaultModel  string   `yaml:"default_model"`  // Default: "gpt-4o-mini-realtime-preview"
	AllowedModels []string `yaml:"allowed_models"` // List of allowed realtime models

	// Voice Configuration
	VoiceProfile string `yaml:"voice_profile"` // "shimmer", "alloy", "echo" (default: "shimmer")

	// Audio Configuration
	SilenceThreshold float32 `yaml:"silence_threshold"`   // Energy threshold for silence detection
	SilenceDuration  int     `yaml:"silence_duration_ms"` // MS of silence before processing (default: 1500)

	// Session Configuration
	InactivityTimeout     int `yaml:"inactivity_timeout"`      // Seconds before leaving channel (default: 120)
	MaxSessionLength      int `yaml:"max_session_length"`      // Max minutes per session (default: 10)
	MaxConcurrentSessions int `yaml:"max_concurrent_sessions"` // Max concurrent sessions (default: 10)

	// Permission Configuration
	AllowedUserIDs []string `yaml:"allowed_user_ids"` // User IDs allowed to use voice command

	// Cost Management
	ShowCostWarnings  bool    `yaml:"show_cost_warnings"`   // Show cost warnings when starting sessions (default: true)
	TrackSessionCosts bool    `yaml:"track_session_costs"`  // Track and display costs in real-time (default: true)
	MaxCostPerSession float64 `yaml:"max_cost_per_session"` // Auto-stop session if cost exceeds this (default: 5.0)

	// OpenAI Realtime Configuration
	RealtimeAPIKey string `yaml:"realtime_api_key"` // Optional separate API key
	VADMode        string `yaml:"vad_mode"`         // "server_vad", "client_vad", or "none" (default: "client_vad")
	TurnDetection  bool   `yaml:"turn_detection"`   // Enable OpenAI turn detection (default: false)
}

type Config struct {
	Discord  DiscordConfig `yaml:"discord"`
	OpenAI   OpenAIConfig  `yaml:"openai"`
	Voice    VoiceConfig   `yaml:"voice"`
	LogLevel string        `yaml:"log_level"`
}

func LoadConfig(filePath string) (*Config, error) {
	// #nosec G304 - filePath is provided by application during startup, not user input
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
