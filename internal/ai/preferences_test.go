package ai

import (
	"encoding/json"
	"net/http"
	"testing"

	"careme/internal/config"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyPreferenceChanges(t *testing.T) {
	for _, tc := range []struct {
		name, saved string
		changes     []PreferenceChange
		want        string
		bad         bool
	}{
		{name: "no suggestions", saved: "Cook for four", want: "Cook for four"},
		{name: "addition", saved: "Cook for four", changes: []PreferenceChange{{After: "Avoid pork."}}, want: "Cook for four\nAvoid pork."},
		{name: "conflict preserves unrelated text", saved: "Cook for two. No shellfish.", changes: []PreferenceChange{{Before: "two", After: "four"}, {After: "Under 30 minutes."}}, want: "Cook for four. No shellfish.\nUnder 30 minutes."},
		{name: "unknown replacement", saved: "No pork", changes: []PreferenceChange{{Before: "No fish", After: "Fish is fine"}}, bad: true},
		{name: "ambiguous replacement", saved: "two meals for two", changes: []PreferenceChange{{Before: "two", After: "four"}}, bad: true},
		{name: "empty", changes: []PreferenceChange{{After: " "}}, bad: true},
		{name: "overlapping", saved: "abcdef", changes: []PreferenceChange{{Before: "abc", After: "x"}, {Before: "bcd", After: "y"}}, bad: true},
		{name: "replacements never target additions", saved: "one two", changes: []PreferenceChange{{Before: "one", After: "two"}, {Before: "two", After: "three"}}, want: "two three"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ApplyPreferenceChanges(tc.saved, tc.changes)
			if tc.bad {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestSuggestPreferencesRequest(t *testing.T) {
	for _, tier := range []string{"default", "flex"} {
		t.Run(tier, func(t *testing.T) {
			cfg := testAIConfig(config.DefaultRecipeModel)
			cfg.ServiceTier = responses.ResponseNewParamsServiceTier(tier)
			client := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var body struct {
					Model, Instructions string
					ServiceTier         string `json:"service_tier"`
					Text                struct{ Format map[string]any }
					Input               []struct{ Content string }
				}
				require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
				assert.Equal(t, cfg.MenuModel, body.Model)
				assert.Equal(t, tier, body.ServiceTier)
				assert.Contains(t, body.Instructions, "temporary")
				assert.Contains(t, body.Instructions, "semantic duplicates")
				assert.Equal(t, true, body.Text.Format["strict"])
				require.Len(t, body.Input, 1)
				assert.JSONEq(t, `{"chef_notes":"No pork; use zucchini tonight","saved_preferences":"Cook for four"}`, body.Input[0].Content)
				return shoppingMergeHTTPResponse(req, `{"changes":[{"before":"","after":"Avoid pork."}]}`), nil
			})}, nil)
			got, err := client.SuggestPreferences(t.Context(), "No pork; use zucchini tonight", "Cook for four")
			require.NoError(t, err)
			assert.Equal(t, []PreferenceChange{{After: "Avoid pork."}}, got)
		})
	}
}

func TestSuggestPreferencesRejectsInvalidOutput(t *testing.T) {
	for _, output := range []string{`not json`, `{"changes":[{"before":"missing","after":"four"}]}`, `{"changes":[{"before":"","after":""}]}`} {
		client := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) { return shoppingMergeHTTPResponse(req, output), nil })}, nil)
		_, err := client.SuggestPreferences(t.Context(), "Cook for four", "Cook for two")
		require.Error(t, err)
	}
}
