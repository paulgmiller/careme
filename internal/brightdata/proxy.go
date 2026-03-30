package brightdata

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
)

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

// should this take a http client?
func NewProxyAwareHTTPClient(cfg ProxyConfig) *http.Client {
	client := &http.Client{}
	if !cfg.Enabled() {
		return client
	}

	purl := cfg.proxyURL()
	slog.Info("Configuring HTTP client to use BrightData proxy", "url", purl.String())

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(purl)
	client.Transport = transport
	return client
}
