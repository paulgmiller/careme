package brightdata

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
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

type proxySessionContextKey struct{}

type proxySession struct {
	mu sync.RWMutex
	id string
}

type proxySessionRoundTripper struct {
	next http.RoundTripper
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

	client := withRetries(&http.Client{Transport: transport})
	if cfg.Enabled() {
		client.Transport = proxySessionRoundTripper{next: client.Transport}
	}
	return client, nil
}

func newSessionID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate bright data session ID: %w", err)
	}
	return hex.EncodeToString(id[:]), nil
}

func newProxySession() (*proxySession, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	return &proxySession{id: id}, nil
}

func (s *proxySession) ID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

func (s *proxySession) rotate() error {
	id, err := newSessionID()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.id = id
	s.mu.Unlock()
	return nil
}

func (t proxySessionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	session, err := newProxySession()
	if err != nil {
		return nil, err
	}
	ctx := context.WithValue(req.Context(), proxySessionContextKey{}, session)
	return t.next.RoundTrip(req.WithContext(ctx))
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
		session, ok := req.Context().Value(proxySessionContextKey{}).(*proxySession)
		if !ok {
			return proxyURL, nil
		}
		requestProxyURL := *proxyURL
		requestProxyURL.User = url.UserPassword(cfg.Username+"-session-"+session.ID(), cfg.Password)
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
		if isTimeout(err) {
			session, ok := ctx.Value(proxySessionContextKey{}).(*proxySession)
			if ok {
				if rotateErr := session.rotate(); rotateErr != nil {
					return false, rotateErr
				}
				slog.InfoContext(ctx, "rotated Bright Data proxy session after timeout")
			}
		}
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

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
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
