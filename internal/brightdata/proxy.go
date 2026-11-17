package brightdata

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"

	"careme/internal/httpretry"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

//go:embed brightdata.crt
var brightDataRootCA []byte

type ProxyConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type proxySessionIDContextKey struct{}

type proxySession struct {
	mu     sync.Mutex
	id     string
	failed bool
}

// WithProxySessionID requests a sticky Bright Data proxy peer. A timeout marks
// the session for rotation on the next request, including an outer client's
// retry. Successful requests retain the current peer across calls using ctx.
func WithProxySessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, proxySessionIDContextKey{}, &proxySession{id: sessionID})
}

// ProxySessionIDFromContext returns the Bright Data proxy session associated
// with ctx.
func ProxySessionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	session, ok := ctx.Value(proxySessionIDContextKey{}).(*proxySession)
	if !ok {
		return "", false
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.id, true
}

func LoadConfig() ProxyConfig {
	return ProxyConfig{
		Host:     os.Getenv("BRIGHTDATA_PROXY_HOST"),
		Port:     os.Getenv("BRIGHTDATA_PROXY_PORT"),
		Username: os.Getenv("BRIGHTDATA_PROXY_USERNAME"),
		Password: os.Getenv("BRIGHTDATA_PROXY_PASSWORD"),
	}
}

func (c ProxyConfig) enabled() bool {
	return c.Host != "" && c.Port != "" && c.Username != "" && c.Password != ""
}

func (c ProxyConfig) proxyURL() *url.URL {
	return &url.URL{
		Scheme: "http",
		User:   url.UserPassword(c.Username, c.Password),
		Host:   net.JoinHostPort(c.Host, c.Port),
	}
}

func NewProxyAwareHTTPClient(cfg ProxyConfig) (*http.Client, error) {
	transport := http.DefaultTransport
	if cfg.enabled() {
		var err error
		transport, err = newProxyTransport(cfg)
		if err != nil {
			return nil, err
		}
	}

	client := &http.Client{Transport: transport}

	return withRetries(client), nil
}

func newProxyTransport(cfg ProxyConfig) (*http.Transport, error) {
	rootCAs, err := proxyRootCAs()
	if err != nil {
		return nil, err
	}

	slog.Info(
		"Configuring HTTP client to use BrightData proxy",
		"host", cfg.Host,
		"port", cfg.Port,
		"username", cfg.Username,
	)

	// this feels funny
	proxyTransport := http.DefaultTransport.(*http.Transport).Clone()
	proxyURL := cfg.proxyURL()
	proxyTransport.Proxy = func(req *http.Request) (*url.URL, error) {
		sessionID, ok := ProxySessionIDFromContext(req.Context())
		if !ok {
			return proxyURL, nil
		}

		requestProxyURL := *proxyURL
		requestProxyURL.User = url.UserPassword(cfg.Username+"-session-"+sessionID, cfg.Password)
		return &requestProxyURL, nil
	}
	proxyTransport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
	return proxyTransport, nil
}

// retrying 5xx errors and network errors, but not context cancellations or 4xx errors.
func retriable(ctx context.Context, resp *http.Response, err error) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		return true, err // retry these as theya re non canceled?
	}
	if resp == nil || resp.Request == nil {
		return false, nil
	}
	switch resp.Request.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false, nil
	}
	return resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode <= 599, nil
}

func withRetries(baseClient *http.Client) *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = baseClient
	logRetry := httpretry.LogRetry("brightdata")
	retryClient.RequestLogHook = func(logger retryablehttp.Logger, req *http.Request, attempt int) {
		session, ok := req.Context().Value(proxySessionIDContextKey{}).(*proxySession)
		if !ok {
			*req = *req.WithContext(WithProxySessionID(req.Context(), rand.Text()))
			session = req.Context().Value(proxySessionIDContextKey{}).(*proxySession)
		}
		session.mu.Lock()
		rotate := session.failed
		if rotate {
			session.id = rand.Text()
			session.failed = false
		}
		session.mu.Unlock()
		if rotate {
			slog.InfoContext(req.Context(), "Rotating Bright Data proxy session",
				"source", "brightdata", "url", req.URL.String(), "reason", "timeout")
		}
		logRetry(logger, req, attempt)
	}

	// Keep the library defaults for now:
	// RetryMax=4, RetryWaitMin=1s, RetryWaitMax=30s, Backoff=DefaultBackoff.
	// We'll tune these once we have a clearer sense of how often these retries fire.
	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
		var netErr net.Error
		if err != nil && !errors.Is(err, context.Canceled) && (errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout()) {
			if session, ok := ctx.Value(proxySessionIDContextKey{}).(*proxySession); ok {
				session.mu.Lock()
				session.failed = true
				session.mu.Unlock()
			}
		}
		return retriable(ctx, resp, err)
	}
	return retryClient.StandardClient()
}

func proxyRootCAs() (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if pool == nil {
		pool = x509.NewCertPool()
	}
	if ok := pool.AppendCertsFromPEM(brightDataRootCA); !ok {
		return nil, fmt.Errorf("append embedded BrightData root CA")
	}
	return pool, nil
}
