package eval

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type usageRoundTripFunc func(*http.Request) (*http.Response, error)

func (f usageRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestIngredientEvalCost(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint, model, tier  string
		input, cached, write, output int64
		want                         float64
		wantErr                      bool
	}{
		{"decisions input only", "/v1/decisions", "gpt-6-luna", "", 1000, 200, 300, 100, 0.0001, false},
		{"luna mixed usage", "/v1/responses", "gpt-6-luna", "default", 1000, 200, 300, 100, 0.0001395, false},
		{"luna flex", "/v1/responses", "gpt-6-luna", "flex", 1000, 200, 300, 100, 0.00006975, false},
		{"luna fast", "/v1/responses", "gpt-6-luna", "fast", 1000, 200, 300, 100, 0.000279, false},
		{"older luna", "/v1/responses", "gpt-5.6-luna", "default", 1000, 200, 300, 100, 0.000299, false},
		{"missing usage", "/v1/decisions", "gpt-6-luna", "", 0, 0, 0, 0, 0, true},
		{"invalid cache", "/v1/responses", "gpt-6-luna", "", 100, 200, 0, 1, 0, true},
		{"unknown model", "/v1/responses", "other", "", 100, 0, 0, 1, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost, err := ingredientEvalCost(usageRecord{Endpoint: tc.endpoint, Model: tc.model, ServiceTier: tc.tier, InputTokens: tc.input, CachedTokens: tc.cached, CacheWriteTokens: tc.write, OutputTokens: tc.output})
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-12)
		})
	}
}

func TestUsageTransportPreservesResponseAndAccumulatesCost(t *testing.T) {
	body := `{"model":"gpt-6-luna","usage":{"input_tokens":1000},"answers":[{"type":"score","score":8}]}`
	usage := &usageTransport{base: usageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/decisions", nil)
	require.NoError(t, err)
	for range 4 {
		resp, err := usage.RoundTrip(req)
		require.NoError(t, err)
		restored, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		assert.Equal(t, body, string(restored))
	}
	records, cost, err := usage.snapshot()
	require.NoError(t, err)
	require.Len(t, records, 4)
	assert.InDelta(t, 0.0004, cost, 1e-12)
}

func TestUsageTransportRejectsMissingUsage(t *testing.T) {
	usage := &usageTransport{base: usageRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"model":"gpt-6-luna"}`)), Request: req}, nil
	})}
	req, err := http.NewRequest(http.MethodPost, "https://api.openai.com/v1/decisions", nil)
	require.NoError(t, err)
	resp, err := usage.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	_, _, err = usage.snapshot()
	require.ErrorContains(t, err, "omitted token usage")
}
