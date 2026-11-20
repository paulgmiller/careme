# AI retries and Standard fallback

[`internal/ai/retry.go`](../internal/ai/retry.go) configures the OpenAI SDK's native
retries for Flex Responses API calls. Campaigns and mail select Flex.

## Retry settings

All recipe/menu and ingredient-grading Responses calls share OpenAI client
construction, native SDK retries, retry observation, and provider-error
reporting. Images and readiness use the same client construction and observer.
The injected HTTP client is copied and its transport is instrumented without
changing its timeout, redirect policy, or cookies.

Standard recipe/menu calls, images, readiness, and Standard fallback allow the
SDK's usual two retries. Ingredient grading allows four retries. Flex calls use
`option.WithMaxRetries(math.MaxInt)` and `option.WithMaxRetryDelay(time.Hour)`,
with a one-hour child context bounding all attempts and waits. An earlier caller
deadline or cancellation wins. Request options configure retries separately from
the `ResponseNewParams` API body.

The SDK owns retry decisions and backoff. Retry headers take precedence over
exponential backoff. The extended Flex budget covers connection errors, 408,
409, 429, and 5xx, respecting `X-Should-Retry` overrides. Exponential waits may
grow up to one hour. A server-directed wait exceeding one hour stops Flex
retries immediately and preserves the API error without Standard fallback.

One per-call observer logs retry attempts and HTTP 429 retry instructions at
INFO. Other HTTP failures include model, status, attempt, request ID, and retry
headers without consuming the body. Recipe/menu and grading share these logs.

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

Campaign and email progress records store `kind: "background"`. Polling and
publication derive a 70-minute timeout from that kind, including in other web or
worker processes. Interactive and legacy records use ten minutes. Publication
errors include the failure, timeout, or completed result.

## Ingredient grading

Ingredient grading uses Standard tier with a separate 90-second budget per batch,
including all attempts and waits. An earlier caller deadline or cancellation
wins. The shared SDK retry path allows four retries. Grading is always Standard,
including when campaign/mail recipe generation uses Flex. Retry observations
use the same `openai` source and provider diagnostics as recipe/menu requests.
Exhausted grading fails the
generation; successful batches remain cached for the next attempt. The staples
layer adds store context without logging the same failure a second time.
