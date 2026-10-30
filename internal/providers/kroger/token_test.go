package kroger

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKrogerTokenGrants(t *testing.T) {
	for _, grant := range []string{"client_credentials", "authorization_code", "refresh_token"} {
		t.Run(grant, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: cartRoundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, http.MethodPost, req.Method)
				assert.Equal(t, "/v1/connect/oauth2/token", req.URL.Path)
				assert.Equal(t, "application/x-www-form-urlencoded", req.Header.Get("Content-Type"))
				id, secret, ok := req.BasicAuth()
				assert.True(t, ok)
				assert.Equal(t, "client", id)
				assert.Equal(t, "secret", secret)
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				values, err := url.ParseQuery(string(body))
				require.NoError(t, err)
				want := url.Values{"grant_type": {grant}}
				switch grant {
				case "client_credentials":
					want.Set("scope", "product.compact")
				case "authorization_code":
					want.Set("code", "code+&=")
					want.Set("redirect_uri", "https://careme.test/callback?a=b&c=d")
				case "refresh_token":
					want.Set("refresh_token", "old+&=")
				}
				assert.Equal(t, want, values)
				return cartResponse(http.StatusOK, `{"access_token":"access","refresh_token":"rotated","expires_in":3600}`), nil
			})}
			before := time.Now()
			cart := &CartClient{ClientID: "client", ClientSecret: "secret", RedirectURI: "https://careme.test/callback?a=b&c=d", HTTPClient: client}
			switch grant {
			case "client_credentials":
				manager := NewKrogerTokenManager("client", "secret", client)
				token, err := manager.getToken(t.Context())
				require.NoError(t, err)
				assert.Equal(t, "access", token)
				token, err = manager.getToken(t.Context())
				require.NoError(t, err)
				assert.Equal(t, "access", token)
				assert.WithinDuration(t, before.Add(time.Hour), manager.expiresAt, time.Second)
			case "authorization_code":
				token, err := cart.exchange(t.Context(), "code+&=")
				require.NoError(t, err)
				assert.Equal(t, "access", token.AccessToken)
				assert.Equal(t, "rotated", token.RefreshToken)
				assert.WithinDuration(t, before.Add(time.Hour), token.ExpiresAt, time.Second)
			case "refresh_token":
				token, err := cart.refresh(t.Context(), "old+&=")
				require.NoError(t, err)
				assert.Equal(t, "access", token.AccessToken)
				assert.Equal(t, "rotated", token.RefreshToken)
			}
			assert.Equal(t, 1, calls)
		})
	}
}

func TestKrogerTokenFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"rejected", 400, `{"error":"invalid_grant"}`, "400"},
		{"server error", 503, `{}`, "503"},
		{"malformed", 200, `{`, "request Kroger token"},
		{"missing access", 200, `{"expires_in":3600}`, "incomplete"},
		{"empty access", 200, `{"access_token":"","expires_in":3600}`, "incomplete"},
		{"missing expiry", 200, `{"access_token":"access"}`, "incomplete"},
		{"expired", 200, `{"access_token":"access","expires_in":0}`, "incomplete"},
		{"negative expiry", 200, `{"access_token":"access","expires_in":-1}`, "incomplete"},
		{"missing refresh", 200, `{"access_token":"access","expires_in":3600}`, "refresh token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) { calls++; return cartResponse(tc.status, tc.body), nil })}
			cart := &CartClient{HTTPClient: client}
			_, err := cart.exchange(t.Context(), "code")
			require.ErrorContains(t, err, tc.want)
			assert.Equal(t, 1, calls)
			if tc.name != "missing refresh" {
				manager := NewKrogerTokenManager("client", "secret", client)
				_, err = manager.getToken(t.Context())
				require.Error(t, err)
				assert.Empty(t, manager.token)
			}
		})
	}
}

func TestKrogerRefreshPreservesExistingToken(t *testing.T) {
	client := &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
		return cartResponse(200, `{"access_token":"access","expires_in":3600}`), nil
	})}
	cart := &CartClient{HTTPClient: client}
	token, err := cart.refresh(t.Context(), "existing")
	require.NoError(t, err)
	assert.Equal(t, "existing", token.RefreshToken)
}

func TestKrogerMutatingRequestsDoNotRetryTransportErrors(t *testing.T) {
	for _, operation := range []string{"exchange", "refresh", "add"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			failure := errors.New("connection lost")
			client := &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) { calls++; return nil, failure })}
			cart := &CartClient{HTTPClient: client}
			var err error
			switch operation {
			case "exchange":
				_, err = cart.exchange(t.Context(), "code")
			case "refresh":
				_, err = cart.refresh(t.Context(), "refresh")
			case "add":
				err = cart.add(t.Context(), "access", []cartItem{{UPC: "123", Quantity: 1}})
			}
			require.ErrorIs(t, err, failure)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestKrogerTokenRequiresJSONResponse(t *testing.T) {
	client := &http.Client{Transport: cartRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader("login page"))}, nil
	})}
	cart := &CartClient{HTTPClient: client}
	_, err := cart.exchange(t.Context(), "code")
	require.ErrorContains(t, err, "incomplete")
}
