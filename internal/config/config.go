package config

import (
	"fmt"
	"os"
)

type Config struct {
	AI     AIConfig     `json:"ai"`
	Kroger KrogerConfig `json:"kroger"`
	Mocks  MockConfig   `json:"mocks"`
	Clerk  ClerkConfig  `json:"clerk"`
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

type ClerkConfig struct {
	SecretKey      string
	PublishableKey string
	FrontendAPI    string
	SignInURL      string
	SignUpURL      string
	SignOutURL     string
}

func (c ClerkConfig) Enabled() bool {
	return c.SecretKey != "" && c.PublishableKey != "" && c.FrontendAPI != "" && c.SignInURL != "" && c.SignUpURL != ""
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
		Clerk: ClerkConfig{
			SecretKey:      os.Getenv("CLERK_SECRET_KEY"),
			PublishableKey: os.Getenv("CLERK_PUBLISHABLE_KEY"),
			FrontendAPI:    os.Getenv("CLERK_FRONTEND_API"),
			SignInURL:      os.Getenv("CLERK_SIGN_IN_URL"),
			SignUpURL:      os.Getenv("CLERK_SIGN_UP_URL"),
			SignOutURL:     os.Getenv("CLERK_SIGN_OUT_URL"),
		},
	}

	return config, validate(config)
}

func validate(cfg *Config) error {
	if cfg.Mocks.Enable {
		return nil
	}
	if !cfg.Clerk.Enabled() {
		return fmt.Errorf("clerk secret key, publishable key, frontend API, and sign-in/sign-up URLs must be set")
	}
	if cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "" {
		return fmt.Errorf("kroger client ID and secret must be set")
	}
	if cfg.AI.APIKey == "" {
		return fmt.Errorf("AI API  key must be set")
	}
	return nil
}
