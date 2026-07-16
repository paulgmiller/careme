package brightdata

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"

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

// WithProxySessionID requests a sticky Bright Data proxy peer for the lifetime
// of ctx. Callers should use a different alphanumeric ID when they want a
// different peer.
func WithProxySessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, proxySessionIDContextKey{}, sessionID)
}

// ProxySessionIDFromContext returns the Bright Data proxy session associated
// with ctx.
func ProxySessionIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	sessionID, ok := ctx.Value(proxySessionIDContextKey{}).(string)
	return sessionID, ok && sessionID != ""
}

func LoadConfig() ProxyConfig {
	return ProxyConfig{
		Host:     os.Getenv("BRIGHTDATA_PROXY_HOST"),
		Port:     os.Getenv("BRIGHTDATA_PROXY_PORT"),
		Username: os.Getenv("BRIGHTDATA_PROXY_USERNAME"),
		Password: os.Getenv("BRIGHTDATA_PROXY_PASSWORD"),
	}
}

func (c ProxyConfig) Enabled() bool {
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
	if cfg.Enabled() {
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
	retryClient.RequestLogHook = httpretry.LogRetry("brightdata")

	// Keep the library defaults for now:
	// RetryMax=4, RetryWaitMin=1s, RetryWaitMax=30s, Backoff=DefaultBackoff.
	// We'll tune these once we have a clearer sense of how often these retries fire.
	retryClient.CheckRetry = retriable
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
