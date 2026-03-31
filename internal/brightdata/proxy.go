package brightdata

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"

	"careme/internal/httpx"
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
		return httpx.WrapClient(client, httpx.RetryConfig{}), nil
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
	return httpx.WrapClient(client, httpx.RetryConfig{}), nil
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
