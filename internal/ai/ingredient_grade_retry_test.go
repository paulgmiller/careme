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

	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIngredientGradingRetriesTransientFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		failures     int
		retryAfter   string
		retryAfterMs string
		wantWait     time.Duration
	}{
		{name: "503 recovers after four retries", status: http.StatusServiceUnavailable, failures: 4, wantWait: 15 * time.Second},
		{name: "429 retry instructions", status: http.StatusTooManyRequests, failures: 1, retryAfter: "20", wantWait: 20 * time.Second},
		{name: "503 retry instructions", status: http.StatusServiceUnavailable, failures: 1, retryAfter: "30", wantWait: 30 * time.Second},
		{name: "millisecond retry instructions take precedence", status: http.StatusServiceUnavailable, failures: 1, retryAfter: "30", retryAfterMs: "2500", wantWait: 2500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var originalBody string
				calls := 0
				transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					if calls == 1 {
						originalBody = string(body)
					}
					assert.Equal(t, originalBody, string(body), "every retry must replay the complete request")
					var params map[string]any
					require.NoError(t, json.Unmarshal(body, &params))
					assert.Equal(t, defaultIngredientGradeModel, params["model"])
					assert.Equal(t, "default", params["service_tier"])
					assert.Contains(t, string(body), `\"id\": \"ingredient-1\"`)
					if calls <= tc.failures {
						resp := ingredientGradeFailureResponse(req, tc.status)
						resp.Header.Set("Retry-After", tc.retryAfter)
						resp.Header.Set("Retry-After-Ms", tc.retryAfterMs)
						return resp, nil
					}
					return ingredientGradeHTTPResponse(req, `{"grades":[{"id":"ingredient-1","score":8,"reason":"Fresh vegetable."}]}`), nil
				})
				base := &http.Client{Transport: transport, Timeout: 45 * time.Second}
				grader := NewIngredientGrader("test-key", "", base)
				assert.Equal(t, 45*time.Second, base.Timeout)
				assert.IsType(t, transport, base.Transport)
				start := time.Now()
				graded, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "ingredient-1"}})
				require.NoError(t, err)
				require.Len(t, graded, 1)
				assert.Equal(t, 8, graded[0].Grade.Score)
				assert.Equal(t, tc.failures+1, calls)
				assert.Equal(t, tc.wantWait, time.Since(start))
			})
		})
	}
}

func TestIngredientGradingFailureDiagnosticsAndRetryLimit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		shouldRetry string
		wantCalls   int
	}{
		{name: "503 exhausted", status: http.StatusServiceUnavailable, wantCalls: 5},
		{name: "400 is permanent", status: http.StatusBadRequest, wantCalls: 1},
		{name: "provider disables retries", status: http.StatusServiceUnavailable, shouldRetry: "false", wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				grader := NewIngredientGrader("test-key", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					resp := ingredientGradeFailureResponse(req, tc.status)
					resp.Header.Set("X-Should-Retry", tc.shouldRetry)
					return resp, nil
				})})
				graded, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "ingredient-1"}})
				require.Error(t, err)
				assert.Nil(t, graded)
				assert.Equal(t, tc.wantCalls, calls)
				var apiErr *openai.Error
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, tc.status, apiErr.StatusCode)
				assert.Contains(t, err.Error(), `request_id="req-grading"`)
				assert.Contains(t, err.Error(), `model="`+defaultIngredientGradeModel+`"`)
				assert.Contains(t, err.Error(), `service_tier="default"`)
				assert.Contains(t, err.Error(), `message="Grading temporarily unavailable"`)
			})
		})
	}
}

func TestIngredientGradingRetryDeadline(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
		stall   bool
	}{
		{name: "grading budget", timeout: ingredientGradeTimeout},
		{name: "earlier caller deadline", timeout: 5 * time.Second},
		{name: "in flight request", timeout: ingredientGradeTimeout, stall: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				grader := NewIngredientGrader("test-key", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					if tc.stall {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					resp := ingredientGradeFailureResponse(req, http.StatusServiceUnavailable)
					resp.Header.Set("Retry-After", "120")
					return resp, nil
				})})
				ctx := t.Context()
				if tc.timeout < ingredientGradeTimeout {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tc.timeout)
					defer cancel()
				}
				start := time.Now()
				graded, err := grader.GradeIngredients(ctx, []InputIngredient{{ProductID: "ingredient-1"}})
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Nil(t, graded)
				assert.Equal(t, 1, calls)
				assert.Equal(t, tc.timeout, time.Since(start))
			})
		})
	}
}

func TestIngredientGradingCancellationStopsRetries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		grader := NewIngredientGrader("test-key", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			cancel()
			return ingredientGradeFailureResponse(req, http.StatusServiceUnavailable), nil
		})})
		graded, err := grader.GradeIngredients(ctx, []InputIngredient{{ProductID: "ingredient-1"}})
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, graded)
		assert.Equal(t, 1, calls)
	})
}

func TestIngredientGradingLogsRetriesAndProviderDetails(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		grader := NewIngredientGrader("secret-test-key", "", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return ingredientGradeFailureResponse(req, http.StatusServiceUnavailable), nil
			}
			return ingredientGradeHTTPResponse(req, `{"grades":[{"id":"ingredient-1","score":8,"reason":"Fresh vegetable."}]}`), nil
		})})
		_, err := grader.GradeIngredients(t.Context(), []InputIngredient{{ProductID: "ingredient-1", Description: "private-test-description"}})
		require.NoError(t, err)
	})
	assert.Contains(t, logs.String(), `msg="Retrying HTTP request"`)
	assert.Contains(t, logs.String(), "source=openai_ingredient_grading")
	assert.Contains(t, logs.String(), "attempt=2")
	assert.Contains(t, logs.String(), "status=503")
	assert.Contains(t, logs.String(), "request_id=req-grading")
	assert.NotContains(t, logs.String(), "secret-test-key")
	assert.NotContains(t, logs.String(), "private-test-description")
}

func ingredientGradeFailureResponse(req *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": {"application/json"},
			"X-Request-Id": {"req-grading"},
		},
		Body:    io.NopCloser(strings.NewReader(`{"error":{"message":"Grading temporarily unavailable","type":"server_error","code":"service_unavailable"}}`)),
		Request: req,
	}
}
