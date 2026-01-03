package config

import (
	"fmt"
	"os"
)

type Config struct {
	AI       AIConfig       `json:"ai"`
	Kroger   KrogerConfig   `json:"kroger"`
	Mocks    MockConfig     `json:"mocks"`
	SendGrid SendGridConfig `json:"sendgrid"`
}

type AIConfig struct {
	APIKey string `json:"api_key"`
}

type KrogerConfig struct {
	ClientID     string
	ClientSecret string
}

type MockConfig struct {
	Enable bool
}

type SendGridConfig struct {
	APIKey string
}

func Load() (*Config, error) {
	config := &Config{
		AI: AIConfig{
			APIKey: os.Getenv("AI_API_KEY"),
		},
		Kroger: KrogerConfig{
			ClientID:     os.Getenv("KROGER_CLIENT_ID"),
			ClientSecret: os.Getenv("KROGER_CLIENT_SECRET"),
		},
		Mocks: MockConfig{
			Enable: os.Getenv("ENABLE_MOCKS") != "", // strconv
		},
		SendGrid: SendGridConfig{
			APIKey: os.Getenv("SENDGRID_API_KEY"),
		},
	}

	return config, validate(config)
}

func validate(cfg *Config) error {
	if cfg.Mocks.Enable {
		return nil
	}
	if cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "" {
		return fmt.Errorf("kroger client ID and secret must be set")
	}
	if cfg.AI.APIKey == "" {
		return fmt.Errorf("AI API key must be set")
	}
	if cfg.SendGrid.APIKey == "" {
		return fmt.Errorf("SendGrid API key must be set")
	}
	return nil
}
