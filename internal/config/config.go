package config

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const additionalStoresEnableEnv = "EXTRA_STORES_ENABLE"

const (
	defaultLocalOrigin = "http://localhost:8080"
)

type Config struct {
	AI           AIConfig         `json:"ai"`
	Kroger       KrogerConfig     `json:"kroger"`
	Walmart      WalmartConfig    `json:"walmart"`
	Aldi         AldiConfig       `json:"aldi"`
	WholeFoods   WholeFoodsConfig `json:"wholefoods"`
	Albertsons   AlbertsonsConfig `json:"albertsons"`
	Publix       PublixConfig     `json:"publix"`
	HEB          HEBConfig        `json:"heb"`
	Wegmans      WegmansConfig    `json:"wegmans"`
	Mocks        MockConfig       `json:"mocks"`
	Clerk        ClerkConfig      `json:"clerk"`
	Admin        AdminConfig      `json:"admin"`
	PublicOrigin string           `json:"public_origin"`
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
}

func (c *ClerkConfig) IsEnabled() bool {
	return c.SecretKey != "" && c.Domain != "" && c.PublishableKey != ""
}

type AdminConfig struct {
	Emails []string `json:"emails"`
}

type WholeFoodsConfig struct {
	Enable bool `json:"enable"`
}

func (c *WholeFoodsConfig) IsEnabled() bool {
	return c.Enable
}

type AldiConfig struct {
	Enable bool `json:"enable"`
}

func (c *AldiConfig) IsEnabled() bool {
	return c.Enable
}

type AlbertsonsConfig struct {
	Enable                bool   `json:"enable"`
	SearchSubscriptionKey string `json:"search_subscription_key"`
	SearchReese84         string `json:"search_reese84"`
}

func (c *AlbertsonsConfig) IsEnabled() bool {
	return c.Enable
}

// can we dynamically disable if our key  expires
func (c *AlbertsonsConfig) HasInventory() bool {
	return c.SearchReese84 != ""
}

type PublixConfig struct {
	Enable bool `json:"enable"`
}

func (c *PublixConfig) IsEnabled() bool {
	return c.Enable
}

type HEBConfig struct {
	Enable bool `json:"enable"`
}

func (c *HEBConfig) IsEnabled() bool {
	return c.Enable
}

type WegmansConfig struct {
	Enable bool `json:"enable"`
}

func (c *WegmansConfig) IsEnabled() bool {
	return c.Enable
}

// Config defines the required Walmart affiliate credentials and client options.
type WalmartConfig struct {
	ConsumerID string
	KeyVersion string
	PrivateKey string // base 64 the ssh key you give to Walmart (eg bas64 -w0 keys/walmart_prod)
	BaseURL    string
	HTTPClient *http.Client
}

func (c *WalmartConfig) IsEnabled() bool {
	return c.ConsumerID != "" && c.PrivateKey != ""
}

func (c *ClerkConfig) Signin() string {
	return fmt.Sprintf("https://%s/sign-in", c.Domain)
}

func (c *ClerkConfig) Signup() string {
	return fmt.Sprintf("https://%s/sign-up", c.Domain)
}

func (c *Config) ResolvedPublicOrigin() string {
	if origin := strings.TrimRight(strings.TrimSpace(c.PublicOrigin), "/"); origin != "" {
		return origin
	}
	return defaultLocalOrigin
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
		PublicOrigin: os.Getenv("PUBLIC_ORIGIN"),
		Aldi: AldiConfig{
			Enable: envEnabled("ALDI_ENABLE"),
		},
		WholeFoods: WholeFoodsConfig{
			Enable: envEnabled("WHOLEFOODS_ENABLE"),
		},
		Albertsons: AlbertsonsConfig{
			Enable:                envEnabled("ALBERTSONS_ENABLE"),
			SearchSubscriptionKey: os.Getenv("ALBERTSONS_SEARCH_SUBSCRIPTION_KEY"),
			SearchReese84:         os.Getenv("ALBERTSONS_SEARCH_REESE84"),
		},
		Publix: PublixConfig{
			Enable: envEnabled("PUBLIX_ENABLE"),
		},
		HEB: HEBConfig{
			Enable: envEnabled("HEB_ENABLE"),
		},
		Wegmans: WegmansConfig{
			Enable: envEnabled("WEGMANS_ENABLE"),
		},
		Walmart: WalmartConfig{
			ConsumerID: os.Getenv("WALMART_CONSUMER_ID"),
			KeyVersion: os.Getenv("WALMART_KEY_VERSION"),
			PrivateKey: os.Getenv("WALMART_PRIVATE_KEY"),
			BaseURL:    os.Getenv("WALMART_BASE_URL"),
		},
	}

	return config, validate(config)
}

func envEnabled(name string) bool {
	return os.Getenv(name) != "false"
}

func validate(cfg *Config) error {
	if err := validateAbsoluteURL("public origin", cfg.ResolvedPublicOrigin()); err != nil {
		return err
	}

	if cfg.Clerk.IsEnabled() {
		if err := validateAbsoluteURL("clerk sign-in URL", cfg.Clerk.Signin()); err != nil {
			return err
		}
		if err := validateAbsoluteURL("clerk sign-up URL", cfg.Clerk.Signup()); err != nil {
			return err
		}
	}

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

func validateAbsoluteURL(name, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	if parsed == nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", name)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
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
