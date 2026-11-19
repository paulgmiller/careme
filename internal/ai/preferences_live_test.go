package ai

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"careme/internal/config"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Explicit opt-in keeps ordinary tests deterministic and free of API calls.
func TestPreferenceExtractionLive(t *testing.T) {
	if os.Getenv("CAREME_PREFERENCE_EVAL") != "1" {
		t.Skip("set CAREME_PREFERENCE_EVAL=1 and AI_API_KEY for uncached live extraction checks")
	}
	key := os.Getenv("AI_API_KEY")
	require.NotEmpty(t, key, "AI_API_KEY is required for live evaluation")
	for _, tier := range []responses.ResponseNewParamsServiceTier{responses.ResponseNewParamsServiceTierDefault, responses.ResponseNewParamsServiceTierFlex} {
		t.Run(string(tier), func(t *testing.T) {
			cfg := testAIConfig(config.DefaultRecipeModel)
			cfg.APIKey = key
			cfg.ServiceTier = tier
			client := NewClient(cfg, &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}, nil)
			for _, tc := range []struct {
				name, notes, saved string
				none               bool
				contains, excludes []string
			}{
				{name: "mixed lasting and temporary", notes: "I never eat pork. Use up my zucchini tonight.", saved: "Cook for four.", contains: []string{"pork"}, excludes: []string{"zucchini"}},
				{name: "temporary only", notes: "Make a vegetarian dinner tonight using the zucchini.", none: true},
				{name: "semantic duplicate", notes: "I do not eat pork.", saved: "Avoid pork.", none: true},
				{name: "uncertain preference", notes: "If a vegetarian friend visits next month I might need meatless ideas.", none: true},
				{name: "conflict", notes: "I now cook for four people instead of two.", saved: "Cook for two people. Avoid shellfish.", contains: []string{"shellfish"}, excludes: []string{"two"}},
				{name: "multiple durable preferences", notes: "I do not own an oven. Always keep cooking time under 30 minutes.", contains: []string{"oven", "30"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
					defer cancel()
					changes, err := client.SuggestPreferences(ctx, tc.notes, tc.saved)
					require.NoError(t, err)
					if tc.none {
						require.Empty(t, changes)
						return
					}
					require.NotEmpty(t, changes)
					updated, err := ApplyPreferenceChanges(tc.saved, changes)
					require.NoError(t, err)
					for _, word := range tc.contains {
						require.Contains(t, strings.ToLower(updated), word)
					}
					for _, word := range tc.excludes {
						require.NotContains(t, strings.ToLower(updated), word)
					}
					if tc.saved == "Cook for four." {
						require.True(t, strings.HasPrefix(updated, tc.saved))
					}
				})
			}
		})
	}
}
