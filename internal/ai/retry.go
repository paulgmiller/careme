package ai

import (
	"context"
	"net/http"
	"time"

	"careme/internal/httpretry"

	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func flexHTTPClient(base *http.Client) *http.Client {
	if base == nil {
		base = &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	}
	retries := retryablehttp.NewClient()
	retries.HTTPClient = base
	retries.Logger = nil
	retries.RequestLogHook = httpretry.LogRetry("openai")
	retries.RetryMax = 2
	retries.CheckRetry = flexRetryPolicy
	retries.Backoff = flexRetryBackoff
	// Preserve the final API response so the SDK can report its status and body.
	retries.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return retries.StandardClient()
}

func flexRetryPolicy(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if resp != nil {
		switch resp.Header.Get("x-should-retry") {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		// Keep the SDK's timeout and conflict retries when disabling SDK retries.
		if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusConflict {
			return true, nil
		}
	}
	return retryablehttp.DefaultRetryPolicy(ctx, resp, err)
}

func flexRetryBackoff(minWait, maxWait time.Duration, attempt int, resp *http.Response) time.Duration {
	wait := retryablehttp.DefaultBackoff(minWait, maxWait, attempt, resp)
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		// Flex capacity can take longer to recover than the SDK's short backoff.
		return max(time.Minute, wait)
	}
	return wait
}
