package config

import (
	"os"

	"github.com/diamondburned/arikawa/v3/discord"
	"gopkg.in/yaml.v3"
)

// Config stores the application configuration.
type Config struct {
	BotToken      string             `yaml:"bot_token"`
	ApplicationID *discord.Snowflake `yaml:"application_id"`
	GuildIDs      []string           `yaml:"guild_ids"`
	LogLevel      string             `yaml:"log_level"` // Added LogLevel field
}

// LoadConfig loads the configuration from the given file path.
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
