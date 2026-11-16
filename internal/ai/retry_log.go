package ai

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/openai/openai-go/v3/option"
)

// retryObserver belongs to one SDK call, including all of its retry attempts.
type retryObserver struct {
	deadline   time.Time
	attempts   int
	lastStatus int
}

func (o *retryObserver) middleware(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	// A retry timer and the deadline can become ready together. Do not send
	// another attempt when the Flex budget has already expired.
	if !o.deadline.IsZero() && !time.Now().Before(o.deadline) {
		return nil, context.DeadlineExceeded
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
