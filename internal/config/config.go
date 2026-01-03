package config

import (
	"fmt"
	"os"
)

type Config struct {
	AI     AIConfig     `json:"ai"`
	Kroger KrogerConfig `json:"kroger"`
	Clerk  ClerkConfig  `json:"clerk"`
	Mocks  MockConfig   `json:"mocks"`
}

type AIConfig struct {
	APIKey string `json:"api_key"`
}

type KrogerConfig struct {
	ClientID     string
	ClientSecret string
}

type ClerkConfig struct {
	SecretKey      string
	PublishableKey string
	SignInURL      string
	SignUpURL      string
}

type MockConfig struct {
	Enable bool
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
		Clerk: ClerkConfig{
			SecretKey:      os.Getenv("CLERK_SECRET_KEY"),
			PublishableKey: os.Getenv("CLERK_PUBLISHABLE_KEY"),
			SignInURL:      getEnvOrDefault("CLERK_SIGN_IN_URL", "https://bold-salmon-53.accounts.dev/sign-in"),
			SignUpURL:      getEnvOrDefault("CLERK_SIGN_UP_URL", "https://bold-salmon-53.accounts.dev/sign-up"),
		},
		Mocks: MockConfig{
			Enable: os.Getenv("ENABLE_MOCKS") != "", // strconv
		},
	}

	return config, validate(config)
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func validate(cfg *Config) error {
	if cfg.Mocks.Enable {
		return nil
	}
	if cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "" {
		return fmt.Errorf("kroger client ID and secret must be set")
	}
	if cfg.AI.APIKey == "" {
		return fmt.Errorf("AI API  key must be set")
	}
	if cfg.Clerk.SecretKey == "" {
		return fmt.Errorf("CLERK_SECRET_KEY must be set")
	}
	return nil
}
