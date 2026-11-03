package config

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"careme/internal/brightdata"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/paulgmiller/kage/pkg/kage"
)

const additionalStoresEnableEnv = "EXTRA_STORES_ENABLE"

const (
	defaultLocalOrigin = "http://localhost:8080"

	// DefaultRecipeModel is the production model for recipe and menu generation.
	DefaultRecipeModel openai.ResponsesModel = "gpt-6.1-sol"
	// DefaultImageModel is the production model for recipe image generation.
	DefaultImageModel openai.ImageModel = openai.ImageModelGPTImage2_5Flare
	// DefaultCritiqueModel is the production OpenRouter recipe critique model.
	DefaultCritiqueModel = "google/gemini-4-argon"
)

type Config struct {
	Instacart         InstacartConfig         `json:"instacart"`
	AI                AIConfig                `json:"ai"`
	OpenRouter        openRouterConfig        `json:"openrouter"`
	IngredientGrading IngredientGradingConfig `json:"ingredient_grading"`
	Kroger            KrogerConfig            `json:"kroger"`
	MNFoodClub        MNFoodClubConfig        `json:"mnfoodclub"`
	Walmart           WalmartConfig           `json:"walmart"`
	Aldi              AldiConfig              `json:"aldi"`
	WholeFoods        WholeFoodsConfig        `json:"wholefoods"`
	Albertsons        AlbertsonsConfig        `json:"albertsons"`
	Publix            PublixConfig            `json:"publix"`
	HEB               HEBConfig               `json:"heb"`
	Wegmans           wegmansConfig           `json:"wegmans"`
	BrightDataProxy   brightdata.ProxyConfig  `json:"brightdata_proxy"`
	Mocks             MockConfig              `json:"mocks"`
	Clerk             ClerkConfig             `json:"clerk"`
	Admin             AdminConfig             `json:"admin"`
	PublicOrigin      string                  `json:"public_origin"`
}

type AIConfig struct {
	// ServiceTier selects processing for recipe and menu requests on this client.
	ServiceTier responses.ResponseNewParamsServiceTier `json:"service_tier,omitempty"`
	APIKey      string                                 `json:"api_key"`
	RecipeModel openai.ResponsesModel                  `json:"recipe_model"`
	ImageModel  openai.ImageModel                      `json:"image_model"`
}

type IngredientGradingConfig struct {
	Enable bool   `json:"enable"`
	Model  string `json:"model"`
}

type openRouterConfig struct {
	APIKey        string `json:"api_key"`
	CritiqueModel string `json:"critique_model"`
}

func (c *openRouterConfig) IsEnabled() bool {
	return strings.TrimSpace(c.APIKey) != ""
}

type InstacartConfig struct {
	APIKey  string `json:"-"`
	BaseURL string `json:"base_url"`
}

type KrogerConfig struct {
	ClientID     string
	ClientSecret string
	CartTokenKey string
}

// MNFoodClubConfig supplies the store-level credentials used for MNFoodClub.
type MNFoodClubConfig struct {
	APIPath          string `json:"api_path"`
	AccessToken      string `json:"-"`
	OrderAPIURL      string `json:"order_api_url"`
	OrderAccessToken string `json:"-"`
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

func (c *ClerkConfig) isEnabled() bool {
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
	Enable bool `json:"enable"`
	// removing this keeps stores but disables inventory
	SearchSubscriptionKey string `json:"search_subscription_key"`
	// SearchReese84 string `json:"search_reese84"`
}

func (c *AlbertsonsConfig) IsEnabled() bool {
	return c.Enable
}

type PublixConfig struct {
	Enable bool `json:"enable"`
	// Abck   string `json:"abck"`
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

type wegmansConfig struct {
	Enable bool `json:"enable"`
}

func (c *wegmansConfig) IsEnabled() bool {
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
	cfg, err := LoadOnlyProviders()
	if err != nil {
		return nil, err
	}
	cfg.AI = AIConfig{
		APIKey:      os.Getenv("AI_API_KEY"),
		RecipeModel: DefaultRecipeModel,
		ImageModel:  DefaultImageModel,
	}
	cfg.IngredientGrading = IngredientGradingConfig{
		Enable: envEnabled("INGREDIENT_GRADING_ENABLE"),
		Model:  os.Getenv("INGREDIENT_GRADING_MODEL"),
	}
	cfg.OpenRouter = openRouterConfig{
		APIKey:        os.Getenv("OPENROUTER_API_KEY"),
		CritiqueModel: envOrDefault("OPENROUTER_CRITIQUE_MODEL", DefaultCritiqueModel),
	}
	cfg.Clerk = ClerkConfig{
		SecretKey:      os.Getenv("CLERK_SECRET_KEY"),
		PublishableKey: os.Getenv("CLERK_PUBLISHABLE_KEY"),
		Domain:         os.Getenv("CLERK_DOMAIN"),
	}
	cfg.Admin = AdminConfig{
		Emails: parseAdminEmails(os.Getenv("ADMIN_EMAILS")),
	}
	cfg.PublicOrigin = os.Getenv("PUBLIC_ORIGIN")
	return cfg, validate(cfg)
}

// LoadOnlyProviders loads provider credentials and flags without app, AI, or
// authentication settings. Provider clients validate their remaining credentials.
func LoadOnlyProviders() (*Config, error) {
	if err := kage.Load(); err != nil {
		return nil, err
	}
	cfg := &Config{
		Instacart: InstacartConfig{APIKey: os.Getenv("INSTACART_API_KEY"), BaseURL: os.Getenv("INSTACART_BASE_URL")},
		Kroger: KrogerConfig{
			ClientID:     os.Getenv("KROGER_CLIENT_ID"),
			ClientSecret: os.Getenv("KROGER_CLIENT_SECRET"),
			CartTokenKey: os.Getenv("KROGER_CART_TOKEN_KEY"),
		},
		MNFoodClub: MNFoodClubConfig{
			APIPath:          os.Getenv("MNFOODCLUB_API_PATH"),
			AccessToken:      os.Getenv("MNFOODCLUB_ACCESS_TOKEN"),
			OrderAPIURL:      os.Getenv("MNFOODCLUB_ORDER_API_URL"),
			OrderAccessToken: os.Getenv("MNFOODCLUB_ORDER_ACCESS_TOKEN"),
		},
		Mocks: MockConfig{
			Enable: os.Getenv("ENABLE_MOCKS") != "", // strconv
			Email:  os.Getenv("MOCK_USER_EMAIL"),
		},
		Aldi: AldiConfig{
			Enable: envEnabled("ALDI_ENABLE"),
		},
		WholeFoods: WholeFoodsConfig{
			Enable: envEnabled("WHOLEFOODS_ENABLE"),
		},
		Albertsons: AlbertsonsConfig{
			Enable:                envEnabled("ALBERTSONS_ENABLE"),
			SearchSubscriptionKey: os.Getenv("ALBERTSONS_SEARCH_SUBSCRIPTION_KEY"),
			// SearchReese84:         os.Getenv("ALBERTSONS_SEARCH_REESE84"),
		},
		Publix: PublixConfig{
			Enable: envEnabled("PUBLIX_ENABLE"),
			//	Abck:   os.Getenv("PUBLIX_ABCK"),
		},
		HEB: HEBConfig{
			Enable: envEnabled("HEB_ENABLE"),
		},
		Wegmans: wegmansConfig{
			Enable: envEnabled("WEGMANS_ENABLE"),
		},
		BrightDataProxy: brightdata.LoadConfig(),
		Walmart: WalmartConfig{
			ConsumerID: os.Getenv("WALMART_CONSUMER_ID"),
			KeyVersion: os.Getenv("WALMART_KEY_VERSION"),
			PrivateKey: os.Getenv("WALMART_PRIVATE_KEY"),
			BaseURL:    os.Getenv("WALMART_BASE_URL"),
		},
	}
	if !cfg.Mocks.Enable && (cfg.Kroger.ClientID == "" || cfg.Kroger.ClientSecret == "") {
		return nil, fmt.Errorf("kroger client ID and secret must be set")
	}
	return cfg, nil
}

func envEnabled(name string) bool {
	return os.Getenv(name) != "false"
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func validate(cfg *Config) error {
	if err := validateAbsoluteURL("public origin", cfg.ResolvedPublicOrigin()); err != nil {
		return err
	}

	if cfg.Clerk.isEnabled() {
		if err := validateAbsoluteURL("clerk sign-in URL", cfg.Clerk.Signin()); err != nil {
			return err
		}
		if err := validateAbsoluteURL("clerk sign-up URL", cfg.Clerk.Signup()); err != nil {
			return err
		}
	}

	if cfg.IngredientGrading.Enable && strings.TrimSpace(cfg.AI.APIKey) == "" {
		return fmt.Errorf("AI_API_KEY is required for ingredient grading")
	}

	if cfg.Mocks.Enable {
		return nil
	}
	if !cfg.Clerk.isEnabled() {
		return fmt.Errorf("clerk configuration must be set")
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
