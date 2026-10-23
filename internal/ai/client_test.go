package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
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

func TestNewClientTrimsModels(t *testing.T) {
	client := NewClient(config.AIConfig{
		APIKey:      "test-key",
		RecipeModel: " candidate-model ",
		ImageModel:  " candidate-image-model ",
	}, nil, &capturePromptRecorder{})

	assert.Equal(t, "candidate-model", client.model)
	assert.Equal(t, "candidate-image-model", string(client.imageModel))
	assert.Equal(t, defaultWineModel, client.wineModel)
}

func TestNewClientUsesModelsByRole(t *testing.T) {
	client := NewClient(testAIConfig(config.DefaultRecipeModel), nil, &capturePromptRecorder{})

	assert.Equal(t, "gpt-6.1-sol", client.model)
	if client.model != config.DefaultRecipeModel {
		t.Fatalf("expected primary recipe model to be %q, got %q", config.DefaultRecipeModel, client.model)
	}
	if client.wineModel != gpt6Luna {
		t.Fatalf("expected wine model to use low-cost Luna path, got %q", client.wineModel)
	}
}

func TestResponseSpendForTier(t *testing.T) {
	standard := estimateOpenAIResponseSpend(config.DefaultRecipeModel, 1200, 900, 200, 350)
	assert.Equal(t, standard, responseSpendForTier(standard, "default"))
	assert.Equal(t, standard, responseSpendForTier(standard, ""))
	flex := responseSpendForTier(standard, "flex")
	assert.InDelta(t, standard.totalUSD()/2, flex.totalUSD(), 1e-9)
	assert.Equal(t, standard.cacheWriteUSD/2, flex.cacheWriteUSD)
	assert.Equal(t, standard.cachedInputUSD/2, flex.cachedInputUSD)
}

func TestFlexGenerationReturnsAPIErrorWithoutStandardFallback(t *testing.T) {
	aiConfig := testAIConfig(config.DefaultRecipeModel)
	aiConfig.ServiceTier = "flex"
	calls := 0
	c := NewClient(aiConfig, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		var body map[string]any
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		assert.Equal(t, "flex", body["service_tier"])
		resp := menuPlanHTTPResponse(req, "", "")
		resp.StatusCode = http.StatusBadRequest
		return resp, nil
	})}, nil)
	recipe, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
	require.ErrorContains(t, err, "failed to generate recipe")
	assert.Nil(t, recipe)
	assert.Equal(t, 1, calls)
}

func TestFlexRetries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failures   int
		retryAfter string
		wantCalls  int
		wantWait   time.Duration
		wantError  bool
	}{
		{name: "capacity recovers", failures: 1, wantCalls: 2},
		{name: "capacity recovers on last attempt", failures: 10, wantCalls: 11},
		{name: "capacity remains unavailable", failures: 11, wantCalls: 11, wantError: true},
		{name: "short retry after", failures: 1, retryAfter: "1", wantCalls: 2, wantWait: time.Second},
		{name: "minute retry after", failures: 1, retryAfter: "60", wantCalls: 2, wantWait: time.Minute},
		{name: "retry after exceeds cap", failures: 1, retryAfter: "120", wantCalls: 1, wantError: true},
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
				if tc.wantError {
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
				if tc.retryAfter != "" {
					assert.Equal(t, tc.wantWait, time.Since(start))
				} else if tc.wantCalls == 2 {
					assert.GreaterOrEqual(t, time.Since(start), 375*time.Millisecond)
					assert.LessOrEqual(t, time.Since(start), 500*time.Millisecond)
				} else {
					// 0.5, 1, 2, 4, 8, 16, 32, 60, 60, 60 seconds, with up to 25% jitter.
					assert.GreaterOrEqual(t, time.Since(start), 182625*time.Millisecond)
					assert.LessOrEqual(t, time.Since(start), 243500*time.Millisecond)
				}
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
			resp := flexUnavailableResponse(req)
			resp.Header.Set("Retry-After", "60")
			return resp, nil
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
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

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
	entries := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, entries, 2, "only retry attempts should be logged")
	for i, entry := range entries {
		var fields map[string]any
		require.NoError(t, json.Unmarshal([]byte(entry), &fields))
		assert.Equal(t, "Retrying HTTP request", fields["msg"])
		assert.Equal(t, "openai", fields["source"])
		assert.Equal(t, float64(i+2), fields["attempt"])
	}
}

func flexUnavailableResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"Flex capacity unavailable","type":"server_error","code":"resource_unavailable"}}`)),
		Request:    req,
	}
}
