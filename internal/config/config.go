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
	SecretKey  string
	SignInURL  string
	SignUpURL  string
	SignOutURL string
}

const (
	clerkProdBaseURL = "https://accounts.careme.cooking"
	clerkDevBaseURL  = "https://bold-salmon-53.accounts.dev"
)

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
	}

	clerkBaseURL := defaultClerkBaseURL(config.Mocks.Enable)
	signInURL := os.Getenv("CLERK_SIGN_IN_URL")
	if signInURL == "" {
		signInURL = clerkBaseURL + "/sign-in"
	}
	signUpURL := os.Getenv("CLERK_SIGN_UP_URL")
	if signUpURL == "" {
		signUpURL = clerkBaseURL + "/sign-up"
	}
	signOutURL := os.Getenv("CLERK_SIGN_OUT_URL")
	if signOutURL == "" {
		signOutURL = clerkBaseURL + "/sign-out"
	}

	config.Clerk = ClerkConfig{
		SecretKey:  os.Getenv("CLERK_SECRET_KEY"),
		SignInURL:  signInURL,
		SignUpURL:  signUpURL,
		SignOutURL: signOutURL,
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
		return fmt.Errorf("AI API  key must be set")
	}
	if cfg.Clerk.SecretKey == "" {
		return fmt.Errorf("clerk secret key must be set")
	}
	return nil
}

func defaultClerkBaseURL(isMock bool) string {
	if isMock {
		return clerkDevBaseURL
	}
	return clerkProdBaseURL
}
