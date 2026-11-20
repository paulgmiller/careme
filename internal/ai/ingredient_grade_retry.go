package ai

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"careme/internal/httpretry"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const ingredientGradeTimeout = 90 * time.Second

func ingredientGradeHTTPClient(base *http.Client, model string) *http.Client {
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	client.Transport = otelhttp.NewTransport(client.Transport)
	retry := retryablehttp.NewClient()
	retry.HTTPClient = &client
	retry.Logger = nil
	retry.RetryMax = 4
	retry.RequestLogHook = httpretry.LogRetry("openai_ingredient_grading")
	// Preserve the final response body for the SDK's typed API error and diagnostics.
	retry.ErrorHandler = retryablehttp.PassthroughErrorHandler
	retry.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if resp != nil {
			switch resp.Header.Get("X-Should-Retry") {
			case "false":
				return false, nil
			case "true":
				return true, nil
			}
			if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusConflict {
				return true, nil
			}
		}
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}
	retry.Backoff = func(min, max time.Duration, attempt int, resp *http.Response) time.Duration {
		if resp != nil {
			if wait, err := time.ParseDuration(resp.Header.Get("Retry-After-Ms") + "ms"); err == nil && wait > 0 {
				return wait
			}
		}
		return retryablehttp.DefaultBackoff(min, max, attempt, resp)
	}
	retry.ResponseLogHook = func(_ retryablehttp.Logger, resp *http.Response) {
		if resp.StatusCode >= http.StatusBadRequest {
			slog.InfoContext(resp.Request.Context(), "OpenAI ingredient grading request failed",
				"model", model, "status", resp.StatusCode, "request_id", resp.Header.Get("X-Request-Id"),
				"retry_after", resp.Header.Get("Retry-After"), "retry_after_ms", resp.Header.Get("Retry-After-Ms"),
				"x_should_retry", resp.Header.Get("X-Should-Retry"))
		}
	}
	return retry.StandardClient()
}
