package config

import (
	"strings"
	"testing"
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
	if got, want := cfg.Albertsons.SearchReese84, "cookie-value"; got != want {
		t.Fatalf("expected Albertsons reese84 %q, got %q", want, got)
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
		"PUBLIX_ENABLE",
		"HEB_ENABLE",
	} {
		t.Setenv(name, "")
	}
}

func contains(got, want string) bool {
	return strings.Contains(got, want)
}
