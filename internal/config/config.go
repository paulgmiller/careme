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
	SecretKey string
	Domain    string
}

func (c *ClerkConfig) IsEnabled() bool {
	return c.SecretKey != "" && c.Domain != ""
}

func (c *ClerkConfig) Signin() string {
	return fmt.Sprintf("https://%s/sign-in?redirect_url=%s", c.Domain, "http://localhost:8080/")
}

func (c *ClerkConfig) Signup() string {
	return fmt.Sprintf("https://%s/sign-up?redirect_url=%s", c.Domain, "http://localhost:8080/")
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
			SecretKey: os.Getenv("CLERK_SECRET_KEY"),
			Domain:    os.Getenv("CLERK_DOMAIN"),
		},
	}

	return config, validate(config)
}

func validate(cfg *Config) error {
	if cfg.Mocks.Enable {
		return nil
	}
	if !cfg.Clerk.IsEnabled() {
		return fmt.Errorf("clerk configuration must be set")
	}

	if cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "" {
		return fmt.Errorf("kroger client ID and secret must be set")
	}
	if cfg.AI.APIKey == "" {
		return fmt.Errorf("AI API  key must be set")
	}
	return nil
}
