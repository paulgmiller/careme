package config

import (
	"fmt"
	"net/http"
	"os"
	"strings"
)

type Config struct {
	AI      AIConfig      `json:"ai"`
	Kroger  KrogerConfig  `json:"kroger"`
	Walmart WalmartConfig `json:"walmart"`
	Mocks   MockConfig    `json:"mocks"`
	Clerk   ClerkConfig   `json:"clerk"`
	Admin   AdminConfig   `json:"admin"`
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
	Email  string
}

type ClerkConfig struct {
	SecretKey      string
	PublishableKey string
	Domain         string
	Prod           bool
}

type AdminConfig struct {
	Emails []string `json:"emails"`
}

// Config defines the required Walmart affiliate credentials and client options.
type WalmartConfig struct {
	ConsumerID     string
	KeyVersion     string
	PrivateKeyPath string
	BaseURL        string
	HTTPClient     *http.Client
}

func (c *ClerkConfig) IsEnabled() bool {
	return c.SecretKey != "" && c.Domain != "" && c.PublishableKey != ""
}

var locahostredirect = "?redirect_url=http://localhost:8080/auth/establish"

// move to auth pacakage?
func (c *ClerkConfig) Signin() string {
	url := fmt.Sprintf("https://%s/sign-in", c.Domain)
	if !c.Prod {
		url += locahostredirect
	}
	return url
}

func (c *ClerkConfig) Signup() string {
	url := fmt.Sprintf("https://%s/sign-up", c.Domain)
	if !c.Prod {
		url += locahostredirect
	}
	return url
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
			Email:  os.Getenv("MOCK_USER_EMAIL"),
		},
		Clerk: ClerkConfig{
			SecretKey:      os.Getenv("CLERK_SECRET_KEY"),
			PublishableKey: os.Getenv("CLERK_PUBLISHABLE_KEY"),
			Domain:         os.Getenv("CLERK_DOMAIN"),
		},
		Admin: AdminConfig{
			Emails: parseAdminEmails(os.Getenv("ADMIN_EMAILS")),
		},
		Walmart: WalmartConfig{
			ConsumerID:     os.Getenv("WALMART_CONSUMER_ID"),
			KeyVersion:     os.Getenv("WALMART_KEY_VERSION"),
			PrivateKeyPath: os.Getenv("WALMART_PRIVATE_KEY_PATH"),
			BaseURL:        os.Getenv("WALMART_BASE_URL"),
		},
	}
	if strings.HasSuffix(config.Clerk.Domain, "careme.cooking") {
		config.Clerk.Prod = true
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

func parseAdminEmails(value string) []string {
	if value == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	emails := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		email := strings.ToLower(strings.TrimSpace(part))
		if email == "" {
			continue
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		emails = append(emails, email)
	}

	return emails
}
