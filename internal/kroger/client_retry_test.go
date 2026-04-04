package kroger

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"careme/internal/config"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
)

func TestFromConfigUsesRetryingHTTPClient(t *testing.T) {
	t.Parallel()

	client, err := FromConfig(&config.Config{
		Kroger: config.KrogerConfig{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
		},
	})
	if err != nil {
		t.Fatalf("FromConfig() error = %v", err)
	}

	rawClient, ok := client.ClientInterface.(*Client)
	if !ok {
		t.Fatalf("expected *Client, got %T", client.ClientInterface)
	}

	httpClient, ok := rawClient.Client.(*http.Client)
	if !ok {
		t.Fatalf("expected *http.Client, got %T", rawClient.Client)
	}

	if _, ok := httpClient.Transport.(*retryablehttp.RoundTripper); !ok {
		t.Fatalf("expected retryable transport, got %T", httpClient.Transport)
	}
}

func TestKrogerRetriable(t *testing.T) {
	t.Parallel()

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	postReq, err := http.NewRequest(http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}

	tests := []struct {
		name string
		ctx  context.Context
		resp *http.Response
		err  error
		want bool
	}{
		{
			name: "retries network errors",
			ctx:  context.Background(),
			err:  errors.New("temporary network failure"),
			want: true,
		},
		{
			name: "does not retry canceled context",
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			err:  context.Canceled,
			want: false,
		},
		{
			name: "retries get 5xx",
			ctx:  context.Background(),
			resp: &http.Response{
				StatusCode: http.StatusBadGateway,
				Request:    req,
			},
			want: true,
		},
		{
			name: "does not retry post 5xx",
			ctx:  context.Background(),
			resp: &http.Response{
				StatusCode: http.StatusBadGateway,
				Request:    postReq,
			},
			want: false,
		},
		{
			name: "does not retry 4xx",
			ctx:  context.Background(),
			resp: &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Request:    req,
			},
			want: false,
		},
	}

	for _, tc := range tests {
		got, _ := krogerRetriable(tc.ctx, tc.resp, tc.err)
		if got != tc.want {
			t.Fatalf("%s: krogerRetriable() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
