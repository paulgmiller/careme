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
	if params.ServiceTier != responses.ResponseNewParamsServiceTierFlex {
		return c.oai.Responses.New(ctx, params)
	}
	started := time.Now()
	flexCtx, cancel := context.WithTimeout(ctx, flexRetryBudget)
	defer cancel()

	attempts, lastStatus := 0, 0
	observeAttempt := func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		// A retry timer and the deadline can become ready together. Do not send
		// another attempt when the Flex budget has already expired.
		if !time.Now().Before(started.Add(flexRetryBudget)) {
			return nil, context.DeadlineExceeded
		}
		attempts++
		resp, err := next(req)
		lastStatus = 0
		if resp != nil {
			lastStatus = resp.StatusCode
		}
		return resp, err
	}
	resp, err := c.oai.Responses.New(flexCtx, params,
		option.WithMaxRetries(math.MaxInt),
		option.WithMaxRetryDelay(flexRetryBudget),
		option.WithMiddleware(observeAttempt))
	if err == nil || ctx.Err() != nil || lastStatus != http.StatusTooManyRequests || !errors.Is(err, context.DeadlineExceeded) {
		return resp, err
	}
	slog.ErrorContext(ctx, "Flex capacity retry budget exhausted; falling back to standard service tier",
		"error", err, "model", params.Model, "attempts", attempts, "elapsed", time.Since(started), "status", lastStatus)
	params.ServiceTier = responses.ResponseNewParamsServiceTierDefault
	return c.oai.Responses.New(ctx, params)
}
