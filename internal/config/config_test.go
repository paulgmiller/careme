package config

import "testing"

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
	t.Setenv("PUBLIX_ENABLE", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Aldi.IsEnabled() {
		t.Fatalf("expected ALDI to remain disabled")
	}
	if cfg.WholeFoods.IsEnabled() {
		t.Fatalf("expected Whole Foods to remain disabled")
	}
	if cfg.Albertsons.IsEnabled() {
		t.Fatalf("expected Albertsons to remain disabled")
	}
	if !cfg.Publix.IsEnabled() {
		t.Fatalf("expected Publix to be enabled")
	}
	if cfg.HEB.IsEnabled() {
		t.Fatalf("expected HEB to remain disabled")
	}
}

func resetStoreEnvs(t *testing.T) {
	t.Helper()

	for _, name := range []string{
		"ENABLE_MOCKS",
		additionalStoresEnableEnv,
		"ALDI_ENABLE",
		"WHOLEFOODS_ENABLE",
		"ALBERTSONS_ENABLE",
		"PUBLIX_ENABLE",
		"HEB_ENABLE",
	} {
		t.Setenv(name, "")
	}
}
