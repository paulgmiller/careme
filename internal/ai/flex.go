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

	observer.deadline = started.Add(flexRetryBudget)
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
