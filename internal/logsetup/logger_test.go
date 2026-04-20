package logsetup

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/stretchr/testify/require"
)

func TestAzureMonitorTransportAddsBearerToken(t *testing.T) {
	t.Parallel()

	credential := &stubTokenCredential{
		token: azcore.AccessToken{
			Token:     "secret-token",
			ExpiresOn: time.Now().Add(time.Hour),
		},
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid/otlp/v1/traces", http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-protobuf")

	var seenAuth string
	transport := &azureMonitorTransport{
		credential: credential,
		base: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seenAuth = r.Header.Get("Authorization")
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Body:       io.NopCloser(http.NoBody),
				Header:     make(http.Header),
			}, nil
		}),
	}

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)
	require.Equal(t, "Bearer secret-token", seenAuth)
	require.Empty(t, req.Header.Get("Authorization"))
	require.Equal(t, []string{azureMonitorScope}, credential.scopes)
}

func TestExportEnablementIncludesAzureMonitorEndpoints(t *testing.T) {
	t.Setenv(azureMonitorTracesEndpointEnv, " https://example.invalid/otlp/v1/traces ")
	t.Setenv(azureMonitorLogsEndpointEnv, " https://example.invalid/otlp/v1/logs ")

	require.True(t, tracesExportEnabled())
	require.True(t, logsExportEnabled())
	require.True(t, azureMonitorExportEnabled())
}

type stubTokenCredential struct {
	token  azcore.AccessToken
	scopes []string
}

func (c *stubTokenCredential) GetToken(_ context.Context, opts policy.TokenRequestOptions) (azcore.AccessToken, error) {
	c.scopes = append([]string(nil), opts.Scopes...)
	return c.token, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
