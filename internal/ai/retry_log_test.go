package ai

import (
	"bytes"
	"encoding/json"
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

func TestOpenAIRetryHeadersLoggedWhenSDKStopsRetrying(t *testing.T) {
	for _, name := range []string{"seconds", "milliseconds", "http date"} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			synctest.Test(t, func(t *testing.T) {
				retryAfter, retryAfterMs := "", ""
				switch name {
				case "seconds":
					retryAfter = "7200"
				case "milliseconds":
					retryAfterMs = "7200000"
				case "http date":
					retryAfter = time.Now().Add(2 * time.Hour).UTC().Format(http.TimeFormat)
				}
				cfg := testAIConfig(config.DefaultRecipeModel)
				cfg.ServiceTier = "flex"
				calls := 0
				c := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					resp := flexUnavailableResponse(req)
					resp.Header.Set("Retry-After", retryAfter)
					resp.Header.Set("Retry-After-Ms", retryAfterMs)
					resp.Header.Set("X-Should-Retry", "true")
					resp.Header.Set("X-Request-Id", "req-capacity")
					return resp, nil
				})}, &capturePromptRecorder{})
				started := time.Now()
				_, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
				var apiErr *openai.Error
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, "resource_unavailable", apiErr.Code, "logging must preserve the API error body")
				assert.Equal(t, 1, calls, "logging must not add retries or fallback")
				assert.Zero(t, time.Since(started))

				var fields map[string]any
				require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &fields))
				assert.Equal(t, "INFO", fields["level"])
				assert.Equal(t, "OpenAI returned retry instructions", fields["msg"])
				assert.Equal(t, "openai", fields["source"])
				assert.Equal(t, float64(429), fields["status"])
				assert.Equal(t, float64(1), fields["attempt"])
				assert.Equal(t, retryAfter, fields["retry_after"])
				assert.Equal(t, retryAfterMs, fields["retry_after_ms"])
				assert.Equal(t, "true", fields["x_should_retry"])
				assert.Equal(t, "req-capacity", fields["request_id"])
			})
		})
	}
}

func TestOpenAIRetryHeadersNotLoggedForOtherStatuses(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusServiceUnavailable} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.openai.com/v1/responses", nil)
		require.NoError(t, err)
		resp := &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"7200"}}}
		observer := retryObserver{}
		got, err := observer.middleware(req, func(*http.Request) (*http.Response, error) { return resp, nil })
		require.NoError(t, err)
		assert.Same(t, resp, got)
	}
	assert.Empty(t, output.String())
}

func TestOpenAIRetryInstructionsLoggedOncePerAttempt(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		c := NewClient(testAIConfig(config.DefaultRecipeModel), &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			resp := flexUnavailableResponse(req)
			resp.Header.Set("Retry-After", "0.001")
			return resp, nil
		})}, &capturePromptRecorder{})
		_, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
		require.Error(t, err)
		assert.Equal(t, 3, calls)
	})
	entries := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, entries, 3, "retry headers and attempt tracking must share one log entry")
	for i, entry := range entries {
		var fields map[string]any
		require.NoError(t, json.Unmarshal([]byte(entry), &fields))
		assert.Equal(t, "OpenAI returned retry instructions", fields["msg"])
		assert.Equal(t, float64(i+1), fields["attempt"])
	}
}
