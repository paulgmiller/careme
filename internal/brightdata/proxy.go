package brightdata

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type ProxyConfig struct {
	Host     string `json:"host"`
	Port     string `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func (c ProxyConfig) Enabled() bool {
	return c.Host != "" || c.Port != "" || c.Username != "" || c.Password != ""
}

func (c ProxyConfig) Validate() error {
	fields := []struct {
		name  string
		value string
	}{
		{name: "BRIGHTDATA_PROXY_HOST", value: c.Host},
		{name: "BRIGHTDATA_PROXY_PORT", value: c.Port},
		{name: "BRIGHTDATA_PROXY_USERNAME", value: c.Username},
		{name: "BRIGHTDATA_PROXY_PASSWORD", value: c.Password},
	}

	var missing []string
	var present []string
	for _, field := range fields {
		if field.value == "" {
			missing = append(missing, field.name)
			continue
		}
		present = append(present, field.name)
	}

	if len(present) == 0 {
		return nil
	}
	if len(missing) != 0 {
		return fmt.Errorf("bright data proxy requires all proxy env vars when enabled; missing %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c ProxyConfig) ProxyURL() (*url.URL, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if !c.Enabled() {
		return nil, nil
	}

	return &url.URL{
		Scheme: "http",
		User:   url.UserPassword(c.Username, c.Password),
		Host:   net.JoinHostPort(c.Host, c.Port),
	}, nil
}

func NewProxyAwareHTTPClient(cfg ProxyConfig) (*http.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	client := &http.Client{}
	if !cfg.Enabled() {
		return client, nil
	}

	proxyURL, err := cfg.ProxyURL()
	if err != nil {
		return nil, err
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	client.Transport = transport
	return client, nil
}
