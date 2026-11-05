package providerregistry

import (
	"testing"

	"careme/internal/config"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicOriginControlsPreviewProviders(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("AZURE_STORAGE_ACCOUNT_NAME", "")
	t.Setenv("AZURE_STORAGE_PRIMARY_ACCOUNT_KEY", "")
	t.Setenv("BRIGHTDATA_BROWSER_WS_ENDPOINT", "wss://test:test@browser.test")
	for _, tt := range []struct {
		origin  string
		enabled bool
	}{
		{"https://careme.cooking", false},
		{"https://careme.cooking/", false},
		{"https://test.careme.cooking", true},
		{"http://localhost:8080", true},
		{"", true},
	} {
		t.Run(tt.origin, func(t *testing.T) {
			cfg := &config.Config{
				PublicOrigin: tt.origin,
				Kroger:       config.KrogerConfig{ClientID: "test", ClientSecret: "test"},
				MNFoodClub:   config.MNFoodClubConfig{APIPath: "https://catalog.test", AccessToken: "test"},
			}
			factory := NewFactory(cfg)
			locationBackends, err := factory.NewLocationBackends(locations.LoadCentroids())
			require.NoError(t, err)
			staples, err := factory.NewStaplesBackends()
			require.NoError(t, err)
			for id, enabled := range map[string]bool{
				"imperfectfoods_demo":              tt.enabled,
				"localfoodmarektplace_localyokels": tt.enabled,
				"70500874":                         true,
			} {
				var hasLocation, hasStaples bool
				for _, backend := range locationBackends {
					hasLocation = hasLocation || backend.IsID(id)
				}
				for _, backend := range staples {
					hasStaples = hasStaples || backend.IsID(id)
				}
				assert.Equal(t, enabled, hasLocation, "location %s", id)
				assert.Equal(t, enabled, hasStaples, "staples %s", id)
			}
		})
	}
}
