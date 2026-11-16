package ai

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"time"

	"careme/internal/httpretry"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const flexRetryBudget = time.Hour

// newResponse bounds Flex capacity retries independently of the caller's deadline,
// leaving the parent context available for a standard-tier fallback.
func (c *client) newResponse(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	if params.ServiceTier != responses.ResponseNewParamsServiceTierFlex {
		return c.oai.Responses.New(ctx, params)
	}
	started := time.Now()
	flexCtx, cancel := context.WithTimeout(ctx, flexRetryBudget)
	defer cancel()

	retries := retryablehttp.NewClient()
	retries.Logger = nil
	retries.HTTPClient = c.httpClient
	if retries.HTTPClient == nil {
		retries.HTTPClient = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	}
	retries.RetryMax = math.MaxInt
	retries.RetryWaitMin = time.Second
	retries.RetryWaitMax = 5 * time.Minute
	retries.RequestLogHook = httpretry.LogRetry("openai")
	retries.ErrorHandler = retryablehttp.PassthroughErrorHandler
	retries.Backoff = flexBackoff
	retries.PrepareRetry = func(req *http.Request) error {
		if !time.Now().Before(started.Add(flexRetryBudget)) {
			return context.DeadlineExceeded
		}
		return req.Context().Err()
	}
	attempts, lastStatus := 0, 0
	retries.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		attempts++
		lastStatus = 0
		if resp != nil {
			lastStatus = resp.StatusCode
		}
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if lastStatus == http.StatusTooManyRequests {
			return true, nil
		}
		// Other transient failures retain a short retry budget and never trigger
		// the capacity fallback.
		if attempts >= 3 {
			return false, nil
		}
		return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
	}
	resp, err := c.oai.Responses.New(flexCtx, params,
		option.WithHTTPClient(retries.StandardClient()), option.WithMaxRetries(0))
	if err == nil || ctx.Err() != nil || lastStatus != http.StatusTooManyRequests || !errors.Is(err, context.DeadlineExceeded) {
		return resp, err
	}
	slog.ErrorContext(ctx, "Flex capacity retry budget exhausted; falling back to standard service tier",
		"error", err, "model", params.Model, "attempts", attempts, "elapsed", time.Since(started), "status", lastStatus)
	params.ServiceTier = responses.ResponseNewParamsServiceTierDefault
	return c.oai.Responses.New(ctx, params)
}

func flexBackoff(min, max time.Duration, attempt int, resp *http.Response) time.Duration {
	// Honor server-directed waits, including waits beyond the exponential cap.
	// The request context enforces the overall one-hour limit.
	if resp != nil && resp.Header.Get("Retry-After") != "" {
		return retryablehttp.DefaultBackoff(min, max, attempt, resp)
	}
	if resp != nil {
		if milliseconds, err := time.ParseDuration(resp.Header.Get("Retry-After-Ms") + "ms"); err == nil && milliseconds > 0 {
			return milliseconds
		}
	}
	delay := retryablehttp.DefaultBackoff(min, max, attempt, resp)
	return delay - time.Duration(rand.Int64N(int64(delay/4)+1))
}
