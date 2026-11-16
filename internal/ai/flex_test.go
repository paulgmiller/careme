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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlexFallback(t *testing.T) {
	for _, tc := range []struct {
		name          string
		retryAfter    string
		standardFails bool
	}{
		{name: "exponential backoff"},
		{name: "server requests an hour", retryAfter: "3600"},
		{name: "server requests two hours", retryAfter: "7200"},
		{name: "standard also fails", retryAfter: "3600", standardFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			synctest.Test(t, func(t *testing.T) {
				cfg := testAIConfig(config.DefaultRecipeModel)
				cfg.ServiceTier = "flex"
				start := time.Now()
				flexCalls, standardCalls := 0, 0
				var flexBody map[string]any
				c := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					var body map[string]any
					require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
					if body["service_tier"] == "flex" {
						flexCalls++
						flexBody = body
						assert.Less(t, time.Since(start), time.Hour)
						resp := flexUnavailableResponse(req)
						resp.Header.Set("Retry-After", tc.retryAfter)
						return resp, nil
					}
					standardCalls++
					assert.Equal(t, "default", body["service_tier"])
					assert.GreaterOrEqual(t, time.Since(start), time.Hour)
					delete(body, "service_tier")
					delete(flexBody, "service_tier")
					assert.Equal(t, flexBody, body, "fallback preserves the full prompt and continuation")
					if tc.standardFails {
						return flexUnavailableResponse(req), nil
					}
					return menuPlanHTTPResponse(req, "resp-recipe", `{"title":"Tofu stir-fry"}`), nil
				})}, &capturePromptRecorder{})
				recipe, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
				if tc.standardFails {
					require.Error(t, err)
					assert.Nil(t, recipe)
					assert.Equal(t, 3, standardCalls)
				} else {
					require.NoError(t, err)
					assert.Equal(t, "Tofu stir-fry", recipe.Title)
					assert.Equal(t, time.Hour, time.Since(start))
					assert.Equal(t, 1, standardCalls)
				}
				assert.Positive(t, flexCalls)
			})
			errorLogs := 0
			for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
				var fields map[string]any
				require.NoError(t, json.Unmarshal([]byte(line), &fields))
				if fields["level"] == "ERROR" {
					errorLogs++
					assert.Contains(t, fields["msg"], "falling back to standard")
					assert.Equal(t, float64(429), fields["status"])
				}
			}
			assert.Equal(t, 1, errorLogs)
		})
	}
}

func TestFlexDoesNotFallbackOnOtherFailures(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := testAIConfig(config.DefaultRecipeModel)
				cfg.ServiceTier = "flex"
				calls := 0
				c := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					assert.Contains(t, string(body), `"service_tier":"flex"`)
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"failed"}}`)), Request: req}, nil
				})}, &capturePromptRecorder{})
				_, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
				require.Error(t, err)
				if status == http.StatusBadRequest {
					assert.Equal(t, 1, calls)
				} else {
					assert.Equal(t, 3, calls)
				}
			})
		})
	}
}

func TestFlexCancellationDoesNotFallback(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := testAIConfig(config.DefaultRecipeModel)
		cfg.ServiceTier = "flex"
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		c := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			cancel()
			return flexUnavailableResponse(req), nil
		})}, &capturePromptRecorder{})
		_, err := c.GenerateRecipe(ctx, nil, ResponseRef{ID: "resp-menu"})
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, calls)
	})
}
