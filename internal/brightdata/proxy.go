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
	client := &http.Client{}
	if !cfg.Enabled() {
		return withRetries(client), nil
	}

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

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(cfg.proxyURL())
	transport.TLSClientConfig = &tls.Config{RootCAs: rootCAs}
	client.Transport = transport
	return withRetries(client), nil
}

func withRetries(baseClient *http.Client) *http.Client {
	retryClient := retryablehttp.NewClient()
	retryClient.HTTPClient = baseClient
	retryClient.Logger = slog.Default()

	// Keep the library defaults for now:
	// RetryMax=4, RetryWaitMin=1s, RetryWaitMax=30s, Backoff=DefaultBackoff.
	// We'll tune these once we have a clearer sense of how often these retries fire.
	retryClient.CheckRetry = func(ctx context.Context, resp *http.Response, err error) (bool, error) {
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
