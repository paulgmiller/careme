package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"careme/internal/config"
	locationtypes "careme/internal/locations/types"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMenuPlanAPIErrorDiagnostics(t *testing.T) {
	for _, tier := range []responses.ResponseNewParamsServiceTier{"default", "flex"} {
		t.Run(string(tier), func(t *testing.T) {
			cfg := testAIConfig(config.DefaultRecipeModel)
			cfg.ServiceTier = tier
			calls := 0
			c := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{
					StatusCode: http.StatusBadRequest,
					Header: http.Header{
						"Content-Type": {"application/json"}, "X-Request-Id": {"req-menu-failed"},
						"Set-Cookie": {"private-cookie"},
					},
					Body:    io.NopCloser(strings.NewReader(`{"error":{"message":"Unsupported service tier for this model","type":"invalid_request_error","code":"unsupported_value","param":"service_tier","private":"private-body-field"}}`)),
					Request: req,
				}, nil
			})}, &capturePromptRecorder{})
			_, err := c.CreateMenuPlan(t.Context(), &locationtypes.Location{ID: "70100023", State: "WA"}, nil,
				[]string{"private-prompt"}, time.Now(), nil, 3)
			require.Error(t, err)
			assert.Equal(t, 1, calls, "400 must not retry or fall back")
			joined := errors.Join(fmt.Errorf("generate campaign: %w", err), errors.New("other location failed"))
			for _, detail := range []string{
				"400 Bad Request", string(cfg.MenuModel), string(tier), "req-menu-failed",
				"invalid_request_error", "unsupported_value", "service_tier", "Unsupported service tier for this model",
			} {
				assert.Contains(t, joined.Error(), detail)
			}
			for _, private := range []string{"test-key", "private-cookie", "private-body-field", "private-prompt"} {
				assert.NotContains(t, joined.Error(), private)
			}
			var apiErr *openai.Error
			require.ErrorAs(t, joined, &apiErr)
			assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
			assert.Equal(t, "unsupported_value", apiErr.Code)
		})
	}
}

func TestResponseErrorPreservesNonAPIErrors(t *testing.T) {
	assert.NoError(t, responseError(responses.ResponseNewParams{}, nil))
	err := fmt.Errorf("request stopped: %w", context.Canceled)
	assert.Same(t, err, responseError(responses.ResponseNewParams{}, err))
}

func TestFlexFallback(t *testing.T) {
	for _, tc := range []struct {
		name          string
		retryAfter    string
		standardFails bool
	}{
		{name: "exponential backoff"},
		{name: "server requests an hour", retryAfter: "3600"},
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
						assert.LessOrEqual(t, time.Since(start), time.Hour)
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
					return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": {"3600"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"failed"}}`)), Request: req}, nil
				})}, &capturePromptRecorder{})
				_, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
				require.Error(t, err)
				if status == http.StatusInternalServerError {
					assert.LessOrEqual(t, calls, 2, "an attempt at the deadline boundary is allowed")
					require.ErrorIs(t, err, context.DeadlineExceeded)
				} else {
					assert.Equal(t, 1, calls)
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

func TestFlexUsesNativeRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		shouldRetry string
		retryAfter  string
		wantError   bool
	}{
		{name: "request timeout", status: http.StatusRequestTimeout},
		{name: "conflict", status: http.StatusConflict},
		{name: "not implemented", status: http.StatusNotImplemented},
		{name: "server requests retry", status: http.StatusBadRequest, shouldRetry: "true"},
		{name: "server refuses retry", status: http.StatusTooManyRequests, shouldRetry: "false", wantError: true},
		{name: "server wait exceeds hour", status: http.StatusTooManyRequests, retryAfter: "7200", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cfg := testAIConfig(config.DefaultRecipeModel)
				cfg.ServiceTier = "flex"
				calls := 0
				start := time.Now()
				c := NewClient(cfg, &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					var body map[string]any
					require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
					assert.Equal(t, "flex", body["service_tier"], "native retry decisions must not trigger early fallback")
					if calls == 1 {
						resp := flexUnavailableResponse(req)
						resp.StatusCode = tc.status
						resp.Header.Set("X-Should-Retry", tc.shouldRetry)
						resp.Header.Set("Retry-After", tc.retryAfter)
						return resp, nil
					}
					return menuPlanHTTPResponse(req, "resp-recipe", `{"title":"Tofu stir-fry"}`), nil
				})}, &capturePromptRecorder{})
				recipe, err := c.GenerateRecipe(t.Context(), nil, ResponseRef{ID: "resp-menu"})
				if tc.wantError {
					require.Error(t, err)
					assert.Nil(t, recipe)
					assert.Equal(t, 1, calls)
					assert.Zero(t, time.Since(start))
				} else {
					require.NoError(t, err)
					assert.Equal(t, "Tofu stir-fry", recipe.Title)
					assert.Equal(t, 2, calls)
				}
			})
		})
	}
}

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
	assert.NotContains(t, output.String(), "OpenAI returned retry instructions")
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
