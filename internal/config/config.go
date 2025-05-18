package config

import (
	"os"

	"github.com/diamondburned/arikawa/v3/discord"
	"gopkg.in/yaml.v3"
)

// Config stores the application configuration.
type Config struct {
	Token         string             `yaml:"token"`
	ApplicationID *discord.Snowflake `yaml:"application_id"`
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
