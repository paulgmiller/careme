# AI retries and Standard fallback

[`internal/ai/retry.go`](../internal/ai/retry.go) configures the OpenAI SDK's native
retries for Flex Responses API calls. Campaigns and mail select Flex.

## Retry settings

Flex calls use `option.WithMaxRetries(math.MaxInt)` and
`option.WithMaxRetryDelay(time.Hour)`, with a one-hour child context bounding all
attempts and waits. The caller's earlier deadline or cancellation still wins.
These are client options passed to `Responses.New`; `ResponseNewParams` describes
the API request body and does not configure retries.

The SDK owns retry decisions and backoff. Its retry headers take precedence over
exponential backoff. With the pinned `openai-go/v3 v3.96.0`, this means:

- Without retry headers, exponential delays can grow beyond five minutes, up to
  the one-hour cap. There is no separate five-minute polling cap.
- The extended budget applies to all SDK-retryable failures, including connection
  errors, 408, 409, 429, and 5xx. Other failures do not have a separate short retry
  budget. The SDK honors `x-should-retry` overrides.
- A server-directed wait exceeding one hour stops retries immediately and returns
  the API error. It does not trigger Standard fallback.

One per-call `retryObserver` middleware tracks attempts and last status and logs
each attempt at most once, at INFO. Calls using other tiers, including
Standard fallback, use the SDK's default retry settings.

The same middleware logs HTTP 429 responses with retry headers at INFO as
`OpenAI returned retry instructions`. Fields include the raw `retry_after`
(seconds or HTTP date), `retry_after_ms`, `status`, `attempt`, `x_should_retry`,
and `request_id`. These logs appear even when a server-directed wait exceeds the
SDK's cap and no retry follows. They observe headers without changing SDK retry
decisions or consuming the response body.

## API error diagnostics

The SDK's `Error()` returns only an HTTP status summary. Responses API failures
explicitly add the requested model and service tier, OpenAI's `X-Request-Id`, and
the provider error's `type`, `code`, `param`, and `message` to the returned error.
These details survive wrapping and joined campaign errors, so they appear in
Grafana's `exception_message`. The original SDK error remains available through
`errors.As`. Requests, headers, and raw response bodies are not dumped; provider
messages are included verbatim and can quote rejected input values.

In Grafana Explore, select the application logs datasource and use
`{service_name="careme"} |= "campaign generation failed"`. Inspect
`exception_message` for the rejected parameter and provider explanation, and keep
the request ID for provider support. The diagnostics require deploying this
change and observing a new failure; older status-only logs cannot recover the
provider message.

## Fallback and job deadlines

If the Flex deadline expires after a 429 while the parent context remains active,
our wrapper logs ERROR with attempts, elapsed time, and last status, then sends
the same parameters with `service_tier=default`. Successful Standard fallback
succeeds; failed fallback returns its error. Cancellation, an earlier caller
deadline, or a different final failure returns an error without fallback.

Campaign locations and email deliveries share `ai.FlexJobTimeout`, equal to the
one-hour Flex budget plus ten minutes for fallback and remaining work. This
70-minute deadline covers the whole location or delivery. Each Flex response has
its own one-hour budget, limited by the time remaining under that parent deadline.
