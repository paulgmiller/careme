package logsetup

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// InstrumentHTTPClient clones client and wraps its transport to log outbound HTTP responses.
func InstrumentHTTPClient(client *http.Client, provider string) *http.Client {
	return InstrumentHTTPClientWithLogger(client, provider, nil)
}

// InstrumentHTTPClientWithLogger clones client and wraps its transport to log outbound HTTP responses.
func InstrumentHTTPClientWithLogger(client *http.Client, provider string, logger *slog.Logger) *http.Client {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return cloneHTTPClient(client)
	}

	if logger == nil {
		logger = slog.Default()
	}

	cloned := cloneHTTPClient(client)
	cloned.Transport = &instrumentedRoundTripper{
		base:     baseTransport(cloned.Transport),
		logger:   logger,
		provider: provider,
	}
	return cloned
}

type instrumentedRoundTripper struct {
	base     http.RoundTripper
	logger   *slog.Logger
	provider string
}

func (t *instrumentedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		t.logger.WarnContext(req.Context(), "outbound http request failed",
			"provider", t.provider,
			"method", req.Method,
			"url", req.URL.String(),
			"error", err,
		)
		return nil, err
	}

	if resp.Body == nil {
		t.logger.InfoContext(req.Context(), "outbound http response",
			"provider", t.provider,
			"method", req.Method,
			"url", req.URL.String(),
			"status_code", resp.StatusCode,
			"response_bytes", 0,
		)
		return resp, nil
	}

	resp.Body = &instrumentedBody{
		ReadCloser: resp.Body,
		ctx:        req.Context(),
		logger:     t.logger,
		provider:   t.provider,
		method:     req.Method,
		url:        req.URL.String(),
		statusCode: resp.StatusCode,
	}
	return resp, nil
}

type instrumentedBody struct {
	io.ReadCloser

	ctx        context.Context
	logger     *slog.Logger
	provider   string
	method     string
	url        string
	statusCode int

	bytesRead int64
	logOnce   sync.Once
}

func (b *instrumentedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.bytesRead += int64(n)

	if err != nil && err != io.EOF {
		b.log(err)
	}
	if err == io.EOF {
		b.log(nil)
	}

	return n, err
}

func (b *instrumentedBody) Close() error {
	err := b.ReadCloser.Close()
	b.log(err)
	return err
}

func (b *instrumentedBody) log(err error) {
	b.logOnce.Do(func() {
		attrs := []any{
			"provider", b.provider,
			"method", b.method,
			"url", b.url,
			"status_code", b.statusCode,
			"response_bytes", b.bytesRead,
		}
		if err != nil {
			attrs = append(attrs, "error", err)
			b.logger.WarnContext(b.ctx, "outbound http response", attrs...)
			return
		}
		b.logger.InfoContext(b.ctx, "outbound http response", attrs...)
	})
}

func cloneHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{}
	}
	cloned := *client
	return &cloned
}

func baseTransport(transport http.RoundTripper) http.RoundTripper {
	if transport != nil {
		return transport
	}
	return http.DefaultTransport
}
