package ai

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

const flexRetryBudget = time.Hour

// FlexJobTimeout allows an hour of Flex retries plus time for standard fallback
// and the remaining campaign or email work.
const FlexJobTimeout = flexRetryBudget + 10*time.Minute

// newResponse bounds Flex capacity retries independently of the caller's deadline,
// leaving the parent context available for a standard-tier fallback.
// The SDK owns retry decisions and backoff; this wrapper only tracks the last
// status and handles Standard fallback. See docs/ai-retries.md.
func (c *client) newResponse(ctx context.Context, params responses.ResponseNewParams) (*responses.Response, error) {
	observer := retryObserver{}
	if params.ServiceTier != responses.ResponseNewParamsServiceTierFlex {
		return c.oai.Responses.New(ctx, params, option.WithMiddleware(observer.middleware))
	}
	started := time.Now()
	flexCtx, cancel := context.WithTimeout(ctx, flexRetryBudget)
	defer cancel()

	resp, err := c.oai.Responses.New(flexCtx, params,
		option.WithMaxRetries(math.MaxInt),
		option.WithMaxRetryDelay(flexRetryBudget),
		option.WithMiddleware(observer.middleware))
	if err == nil || ctx.Err() != nil || observer.lastStatus != http.StatusTooManyRequests || !errors.Is(err, context.DeadlineExceeded) {
		return resp, err
	}
	slog.ErrorContext(ctx, "Flex capacity retry budget exhausted; falling back to standard service tier",
		"error", err, "model", params.Model, "attempts", observer.attempts, "elapsed", time.Since(started), "status", observer.lastStatus)
	params.ServiceTier = responses.ResponseNewParamsServiceTierDefault
	return c.newResponse(ctx, params)
}

// retryObserver belongs to one SDK call, including all of its retry attempts.
type retryObserver struct {
	attempts   int
	lastStatus int
}

func (o *retryObserver) middleware(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	o.attempts++
	resp, err := next(req)
	o.lastStatus = 0
	if resp != nil {
		o.lastStatus = resp.StatusCode
	}
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests &&
		(resp.Header.Get("Retry-After") != "" || resp.Header.Get("Retry-After-Ms") != "") {
		slog.InfoContext(req.Context(), "OpenAI returned retry instructions",
			"source", "openai", "url", req.URL.String(), "attempt", o.attempts,
			"status", resp.StatusCode, "retry_after", resp.Header.Get("Retry-After"), "retry_after_ms", resp.Header.Get("Retry-After-Ms"),
			"x_should_retry", resp.Header.Get("X-Should-Retry"), "request_id", resp.Header.Get("X-Request-Id"))
	} else if o.attempts > 1 {
		slog.InfoContext(req.Context(), "Retried HTTP request", "source", "openai", "url", req.URL.String(), "attempt", o.attempts)
	}
	return resp, err
}
