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
	APIKey           string   `yaml:"api_key"`
	Models           []string `yaml:"models"`
	MessageCacheSize int      `yaml:"message_cache_size"`
}

type Config struct {
	Discord  DiscordConfig `yaml:"discord"`
	OpenAI   OpenAIConfig  `yaml:"openai"`
	LogLevel string        `yaml:"log_level"`
}

func LoadConfig(filePath string) (*Config, error) {
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
