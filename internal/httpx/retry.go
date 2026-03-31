package httpx

import (
	"context"
	"io"
	"math/rand"
	"net/http"
	"time"
)

const (
	defaultMaxRetries      = 3
	defaultBaseDelay       = 200 * time.Millisecond
	defaultMaxBodyDrain    = 4096
	jitterMultiplierSpread = 0.5
)

type RetryConfig struct {
	MaxRetries        int
	BaseDelay         time.Duration
	MaxBodyDrainBytes int64
	Sleep             func(context.Context, time.Duration) error
	RandFloat64       func() float64
}

type RetryTransport struct {
	Base http.RoundTripper
	RetryConfig
}

func WrapClient(client *http.Client, cfg RetryConfig) *http.Client {
	if client == nil {
		client = &http.Client{}
	}

	wrapped := *client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	wrapped.Transport = &RetryTransport{
		Base:        base,
		RetryConfig: cfg.withDefaults(),
	}
	return &wrapped
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base()
	cfg := t.config()

	for attempt := 0; ; attempt++ {
		resp, err := base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		if !shouldRetry(req, resp) || attempt >= cfg.MaxRetries {
			return resp, nil
		}

		drainAndClose(resp.Body, cfg.MaxBodyDrainBytes)

		if err := cfg.Sleep(req.Context(), cfg.backoff(attempt)); err != nil {
			return nil, err
		}
	}
}

func (t *RetryTransport) base() http.RoundTripper {
	if t != nil && t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func (t *RetryTransport) config() RetryConfig {
	if t == nil {
		return RetryConfig{}.withDefaults()
	}
	return t.RetryConfig.withDefaults()
}

func (c RetryConfig) withDefaults() RetryConfig {
	if c.MaxRetries <= 0 {
		c.MaxRetries = defaultMaxRetries
	}
	if c.BaseDelay <= 0 {
		c.BaseDelay = defaultBaseDelay
	}
	if c.MaxBodyDrainBytes <= 0 {
		c.MaxBodyDrainBytes = defaultMaxBodyDrain
	}
	if c.Sleep == nil {
		c.Sleep = sleepContext
	}
	if c.RandFloat64 == nil {
		c.RandFloat64 = rand.Float64
	}
	return c
}

func (c RetryConfig) backoff(attempt int) time.Duration {
	delay := c.BaseDelay * time.Duration(1<<attempt)

	// If retry needs expand beyond simple GET-on-5xx transport behavior,
	// hashicorp/go-retryablehttp is the likely library to revisit.
	jitterMultiplier := 1 - jitterMultiplierSpread + (c.RandFloat64() * jitterMultiplierSpread * 2)
	return time.Duration(float64(delay) * jitterMultiplier)
}

func shouldRetry(req *http.Request, resp *http.Response) bool {
	if req == nil || resp == nil {
		return false
	}
	if req.Context() != nil && req.Context().Err() != nil {
		return false
	}
	switch req.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false
	}
	return resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode <= 599
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func drainAndClose(body io.ReadCloser, maxBytes int64) {
	if body == nil {
		return
	}
	_, _ = io.CopyN(io.Discard, body, maxBytes)
	_ = body.Close()
}
