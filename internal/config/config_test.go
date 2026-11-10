package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEnablesAdditionalStoresFromSharedEnv(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv(additionalStoresEnableEnv, "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Aldi.IsEnabled() {
		t.Fatalf("expected ALDI to be enabled")
	}
	if !cfg.WholeFoods.IsEnabled() {
		t.Fatalf("expected Whole Foods to be enabled")
	}
	if !cfg.Albertsons.IsEnabled() {
		t.Fatalf("expected Albertsons to be enabled")
	}
	if !cfg.Publix.IsEnabled() {
		t.Fatalf("expected Publix to be enabled")
	}
	if !cfg.HEB.IsEnabled() {
		t.Fatalf("expected HEB to be enabled")
	}
}

func TestLoadRetainsIndividualStoreFlags(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv("PUBLIX_ENABLE", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Aldi.IsEnabled() {
		t.Fatalf("expected ALDI to remain enabled")
	}
	if !cfg.WholeFoods.IsEnabled() {
		t.Fatalf("expected Whole Foods to remain enabled")
	}
	if !cfg.Albertsons.IsEnabled() {
		t.Fatalf("expected Albertsons to remain enabled")
	}
	if cfg.Publix.IsEnabled() {
		t.Fatalf("expected Publix to be disabled ")
	}
	if !cfg.HEB.IsEnabled() {
		t.Fatalf("expected HEB to remain enaabled")
	}
}

func TestLoadUsesConfiguredPublicOrigin(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv("PUBLIC_ORIGIN", "https://staging.careme.test/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.ResolvedPublicOrigin(), "https://staging.careme.test"; got != want {
		t.Fatalf("expected resolved public origin %q, got %q", want, got)
	}
}

func TestLoadReadsAlbertsonsSearchCredentials(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv("ALBERTSONS_SEARCH_SUBSCRIPTION_KEY", "sub-key")
	t.Setenv("ALBERTSONS_SEARCH_REESE84", "cookie-value")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.Albertsons.SearchSubscriptionKey, "sub-key"; got != want {
		t.Fatalf("expected Albertsons subscription key %q, got %q", want, got)
	}
}

func TestLoadReadsBrightDataProxyConfig(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv("BRIGHTDATA_PROXY_HOST", "brd.superproxy.io")
	t.Setenv("BRIGHTDATA_PROXY_PORT", "33335")
	t.Setenv("BRIGHTDATA_PROXY_USERNAME", "brd-customer-test-zone-residential")
	t.Setenv("BRIGHTDATA_PROXY_PASSWORD", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.BrightDataProxy.Host, "brd.superproxy.io"; got != want {
		t.Fatalf("expected bright data host %q, got %q", want, got)
	}
	if got, want := cfg.BrightDataProxy.Port, "33335"; got != want {
		t.Fatalf("expected bright data port %q, got %q", want, got)
	}
	if got, want := cfg.BrightDataProxy.Username, "brd-customer-test-zone-residential"; got != want {
		t.Fatalf("expected bright data username %q, got %q", want, got)
	}
	if got, want := cfg.BrightDataProxy.Password, "secret"; got != want {
		t.Fatalf("expected bright data password %q, got %q", want, got)
	}
}

func TestLoadReadsOpenRouterCritiqueConfig(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv("OPENROUTER_API_KEY", "openrouter-key")
	t.Setenv("OPENROUTER_CRITIQUE_MODEL", "anthropic/claude-sonnet-4.5")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.OpenRouter.APIKey, "openrouter-key"; got != want {
		t.Fatalf("expected OpenRouter API key %q, got %q", want, got)
	}
	if got, want := cfg.OpenRouter.CritiqueModel, "anthropic/claude-sonnet-4.5"; got != want {
		t.Fatalf("expected OpenRouter critique model %q, got %q", want, got)
	}
	if !cfg.OpenRouter.IsEnabled() {
		t.Fatal("expected OpenRouter critique config to be enabled")
	}
}

func TestLoadDefaultsAIModels(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("ENABLE_MOCKS", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	assert.Equal(t, DefaultRecipeModel, cfg.AI.RecipeModel)
	assert.Equal(t, DefaultImageModel, cfg.AI.ImageModel)
	assert.Equal(t, DefaultCritiqueModel, cfg.OpenRouter.CritiqueModel)
}

func TestLoadValidatesIngredientGradingCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, model, enabled, apiKey string
		wantErr                      bool
	}{
		{"default grader without key", "", "1", "", true},
		{"OpenAI grader without key", "gpt-6-luna", "1", "", true},
		{"OpenAI grader with blank key", "gpt-6-luna", "1", " \t ", true},
		{"OpenAI grader with key", "gpt-6-luna", "1", "test-key", false},
		{"Decisions without OpenAI key", "decisions", "1", "", true},
		{"Decisions with OpenAI key", "decisions", "1", "test-key", false},
		{"disabled grader without key", "gpt-6-luna", "false", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resetStoreEnvs(t)
			t.Setenv("ENABLE_MOCKS", "1")
			t.Setenv("INGREDIENT_GRADING_MODEL", tc.model)
			t.Setenv("INGREDIENT_GRADING_ENABLE", tc.enabled)
			t.Setenv("AI_API_KEY", tc.apiKey)

			cfg, err := Load()
			if tc.wantErr {
				require.ErrorContains(t, err, "AI_API_KEY is required for ingredient grading")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.model, cfg.IngredientGrading.Model)
		})
	}
}

func TestResolvedPublicOriginDefaultsToLocalhostOutsideProd(t *testing.T) {
	cfg := &Config{}
	if got, want := cfg.ResolvedPublicOrigin(), "http://localhost:8080"; got != want {
		t.Fatalf("expected default local origin %q, got %q", want, got)
	}
}

func TestValidate_RejectsInvalidConfiguredPublicOrigin(t *testing.T) {
	cfg := &Config{
		Mocks:        MockConfig{Enable: true},
		PublicOrigin: "://bad-origin",
	}

	err := validate(cfg)
	if err == nil || !contains(err.Error(), "public origin") {
		t.Fatalf("expected public origin validation error, got %v", err)
	}
}

func TestValidate_RejectsInvalidDerivedClerkURLs(t *testing.T) {
	cfg := &Config{
		Mocks: MockConfig{Enable: true},
		Clerk: ClerkConfig{
			SecretKey:      "sk_test",
			PublishableKey: "pk_test",
			Domain:         "bad host with spaces",
		},
	}

	err := validate(cfg)
	if err == nil || !contains(err.Error(), "clerk sign-in URL") {
		t.Fatalf("expected clerk sign-in validation error, got %v", err)
	}
}

func resetStoreEnvs(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"ENABLE_MOCKS",
		"PUBLIC_ORIGIN",
		"MNFOODCLUB_API_PATH",
		"MNFOODCLUB_ACCESS_TOKEN",
		additionalStoresEnableEnv,
		"ALDI_ENABLE",
		"WHOLEFOODS_ENABLE",
		"ALBERTSONS_ENABLE",
		"ALBERTSONS_SEARCH_SUBSCRIPTION_KEY",
		"ALBERTSONS_SEARCH_REESE84",
		"BRIGHTDATA_PROXY_HOST",
		"BRIGHTDATA_PROXY_PORT",
		"BRIGHTDATA_PROXY_USERNAME",
		"BRIGHTDATA_PROXY_PASSWORD",
		"OPENROUTER_API_KEY",
		"OPENROUTER_CRITIQUE_MODEL",
		"PUBLIX_ENABLE",
		"PUBLIX_ABCK",
		"HEB_ENABLE",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("AI_API_KEY", "")
	t.Setenv("INGREDIENT_GRADING_MODEL", "")
	t.Setenv("INGREDIENT_GRADING_ENABLE", "false")
}

func contains(got, want string) bool {
	return strings.Contains(got, want)
}

func TestLoadMNFoodClubCredentials(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("ENABLE_MOCKS", "1")
	t.Setenv("MNFOODCLUB_API_PATH", "https://api.bigcommerce.com/stores/test/v3/")
	t.Setenv("MNFOODCLUB_ACCESS_TOKEN", "test-token")
	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "https://api.bigcommerce.com/stores/test/v3/", cfg.MNFoodClub.APIPath)
	assert.Equal(t, "test-token", cfg.MNFoodClub.AccessToken)
}

func TestLoadOnlyProvidersIgnoresAppConfiguration(t *testing.T) {
	resetStoreEnvs(t)
	t.Setenv("KROGER_CLIENT_ID", "test-client")
	t.Setenv("KROGER_CLIENT_SECRET", "test-secret")
	t.Setenv("CLERK_SECRET_KEY", "")
	t.Setenv("CLERK_PUBLISHABLE_KEY", "")
	t.Setenv("CLERK_DOMAIN", "")
	t.Setenv("PUBLIC_ORIGIN", "://invalid")
	t.Setenv("INGREDIENT_GRADING_ENABLE", "true")
	t.Setenv("MNFOODCLUB_API_PATH", "https://provider.example.test")
	t.Setenv("MNFOODCLUB_ACCESS_TOKEN", "test-token")
	t.Setenv("WALMART_CONSUMER_ID", "test-consumer")
	t.Setenv("WALMART_PRIVATE_KEY", "test-key")
	t.Setenv("PUBLIX_ENABLE", "false")

	cfg, err := LoadOnlyProviders()
	require.NoError(t, err)
	assert.Equal(t, "test-client", cfg.Kroger.ClientID)
	assert.Equal(t, "test-secret", cfg.Kroger.ClientSecret)
	assert.Equal(t, "test-token", cfg.MNFoodClub.AccessToken)
	assert.Equal(t, "test-consumer", cfg.Walmart.ConsumerID)
	assert.Equal(t, "test-key", cfg.Walmart.PrivateKey)
	assert.False(t, cfg.Publix.Enable)
	assert.True(t, cfg.Aldi.Enable)
	assert.Zero(t, cfg.AI)
	assert.Zero(t, cfg.IngredientGrading)
	assert.Zero(t, cfg.Clerk)
	assert.Empty(t, cfg.PublicOrigin)
	_, err = Load()
	require.Error(t, err, "full app configuration must still be validated")
}

func TestLoadOnlyProvidersRequiresKrogerUnlessMocked(t *testing.T) {
	for _, tt := range []struct {
		name, clientID, secret, mocks string
		wantError                     bool
	}{
		{"missing credentials", "", "", "", true},
		{"missing secret", "test-client", "", "", true},
		{"missing client", "", "test-secret", "", true},
		{"mocked", "", "", "1", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetStoreEnvs(t)
			t.Setenv("KROGER_CLIENT_ID", tt.clientID)
			t.Setenv("KROGER_CLIENT_SECRET", tt.secret)
			t.Setenv("ENABLE_MOCKS", tt.mocks)
			_, err := LoadOnlyProviders()
			if tt.wantError {
				require.ErrorContains(t, err, "kroger client ID and secret")
				return
			}
			require.NoError(t, err)
		})
	}
}
