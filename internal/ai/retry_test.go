package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"careme/internal/config"

	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlexRetries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failures   int
		retryAfter string
		wantCalls  int
		wantWait   time.Duration
	}{
		{name: "capacity recovers", failures: 1, wantCalls: 2, wantWait: time.Minute},
		{name: "capacity recovers on last attempt", failures: 2, wantCalls: 3, wantWait: 2 * time.Minute},
		{name: "capacity remains unavailable", failures: 3, wantCalls: 3, wantWait: 2 * time.Minute},
		{name: "short retry after", failures: 1, retryAfter: "1", wantCalls: 2, wantWait: time.Minute},
		{name: "long retry after", failures: 1, retryAfter: "120", wantCalls: 2, wantWait: 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := testAIConfig(config.DefaultRecipeModel)
				cfg.ServiceTier = "flex"
				calls := 0
				var firstBody string
				c := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					if calls == 1 {
						firstBody = string(body)
					}
					assert.Equal(t, firstBody, string(body), "retry must replay the complete request")
					var params map[string]any
					require.NoError(t, json.Unmarshal(body, &params))
					assert.Equal(t, "flex", params["service_tier"])
					assert.Equal(t, "resp-menu", params["previous_response_id"])
					if calls <= tc.failures {
						resp := flexUnavailableResponse(req)
						resp.Header.Set("Retry-After", tc.retryAfter)
						return resp, nil
					}
					return menuPlanHTTPResponse(req, "resp-recipe", `{"title":"Tofu stir-fry"}`), nil
				})}, &capturePromptRecorder{})

				start := time.Now()
				recipe, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
				if tc.failures >= tc.wantCalls {
					require.ErrorContains(t, err, "failed to generate recipe")
					var apiErr *openai.Error
					require.ErrorAs(t, err, &apiErr)
					assert.Equal(t, http.StatusTooManyRequests, apiErr.StatusCode)
					assert.Equal(t, "resource_unavailable", apiErr.Code)
					assert.Nil(t, recipe)
				} else {
					require.NoError(t, err)
					require.NotNil(t, recipe)
					assert.Equal(t, "Tofu stir-fry", recipe.Title)
				}
				assert.Equal(t, tc.wantCalls, calls)
				assert.Equal(t, tc.wantWait, time.Since(start))
			})
		})
	}
}

func TestFlexRetryStopsAtContextDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := testAIConfig(config.DefaultRecipeModel)
		cfg.ServiceTier = "flex"
		calls := 0
		c := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return flexUnavailableResponse(req), nil
		})}, &capturePromptRecorder{})
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		start := time.Now()
		recipe, err := c.GenerateRecipe(ctx, nil, ResponseRef{ID: "resp-menu"})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, recipe)
		assert.Equal(t, 1, calls)
		assert.Equal(t, 30*time.Second, time.Since(start))
	})
}

func TestStandardTierKeepsShortRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		c := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return flexUnavailableResponse(req), nil
		})}, &capturePromptRecorder{})
		start := time.Now()
		_, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
		require.Error(t, err)
		assert.Equal(t, 3, calls)
		assert.Less(t, time.Since(start), time.Minute)
	})
}

func flexUnavailableResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Flex capacity unavailable","type":"server_error","code":"resource_unavailable"}}`)),
		Request:    req,
	}
}
