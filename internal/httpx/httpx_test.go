package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetHTMLContentType(t *testing.T) {
	response := httptest.NewRecorder()

	SetHTMLContentType(response)

	assert.Equal(t, "text/html; charset=utf-8", response.Header().Get("Content-Type"))
}

func TestIsHTMX(t *testing.T) {
	tests := []struct {
		name    string
		request *http.Request
		want    bool
	}{
		{name: "nil request"},
		{name: "header missing", request: httptest.NewRequest(http.MethodGet, "/", nil)},
		{name: "true", request: requestWithHeader("HX-Request", "true"), want: true},
		{name: "case insensitive", request: requestWithHeader("HX-Request", "TRUE"), want: true},
		{name: "false", request: requestWithHeader("HX-Request", "false")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsHTMX(tt.request))
		})
	}
}

func TestRequestPath(t *testing.T) {
	tests := []struct {
		name    string
		request *http.Request
		want    string
	}{
		{name: "nil request", want: "/"},
		{name: "request URI", request: httptest.NewRequest(http.MethodGet, "/recipes?h=hash", nil), want: "/recipes?h=hash"},
		{name: "path fallback", request: &http.Request{URL: &url.URL{Path: "/recipes"}}, want: "/recipes"},
		{name: "root fallback", request: &http.Request{}, want: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RequestPath(tt.request))
		})
	}
}

func TestLocalReferrerPath(t *testing.T) {
	tests := []struct {
		name     string
		referrer string
		host     string
		want     string
	}{
		{name: "missing", host: "careme.test", want: "/"},
		{name: "local path", referrer: "/recipes?h=hash", host: "careme.test", want: "/recipes?h=hash"},
		{name: "same host absolute URL", referrer: "https://careme.test/recipes?h=hash", host: "careme.test", want: "/recipes?h=hash"},
		{name: "other host", referrer: "https://example.com/recipes?h=hash", host: "careme.test", want: "/"},
		{name: "malformed", referrer: "://bad", host: "careme.test", want: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "https://"+tt.host+"/recipes", nil)
			request.Header.Set("Referer", tt.referrer)

			assert.Equal(t, tt.want, LocalReferrerPath(request))
		})
	}
}

func requestWithHeader(name, value string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set(name, value)
	return request
}
