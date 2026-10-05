package smithbrothersfarms

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func fixtureClient(t *testing.T, failURL string, status int, failure error) (*Client, *[]string, *[]*trackedBody) {
	t.Helper()
	pages := map[string]string{
		stapleURLs[0]: fixture(t, "produce"), stapleURLs[1]: fixture(t, "meat"),
		baseURL + "/" + harvestBoxes[0].slug: fixture(t, "organic-box"),
		baseURL + "/" + harvestBoxes[1].slug: fixture(t, "standard-box"),
	}
	var calls []string
	var bodies []*trackedBody
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.URL.String())
		assert.Equal(t, http.MethodGet, req.Method)
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		body := &trackedBody{Reader: strings.NewReader(pages[req.URL.String()])}
		code := http.StatusOK
		if req.URL.String() == failURL {
			if failure != nil {
				return nil, failure
			}
			code = status
			body.Reader = strings.NewReader("login page")
		}
		bodies = append(bodies, body)
		return &http.Response{StatusCode: code, Body: body}, nil
	})})
	return client, &calls, &bodies
}

func TestFetchStaples(t *testing.T) {
	client, calls, bodies := fixtureClient(t, "", 0, nil)
	got, err := NewStaplesProvider(client).FetchStaples(t.Context(), "smithbrothersfarms_delivery")
	require.NoError(t, err)
	require.Len(t, got, 25)
	assert.Equal(t, []string{stapleURLs[0], stapleURLs[1], baseURL + "/" + harvestBoxes[0].slug, baseURL + "/" + harvestBoxes[1].slug}, *calls)
	assert.Equal(t, "Organic Produce Box — Local Organic Sugar Bee Apples", got[6].Description)
	assert.Equal(t, "Produce Box — Avocados", got[18].Description)
	for _, body := range *bodies {
		assert.True(t, body.closed)
	}
	*calls = nil
	got, err = NewStaplesProvider(client).FetchStaples(t.Context(), "other_delivery")
	require.ErrorContains(t, err, "invalid Smith Brothers Farms location ID")
	assert.Nil(t, got)
	assert.Empty(t, *calls)
}

func TestFetchStaplesFailures(t *testing.T) {
	urls := []string{stapleURLs[0], stapleURLs[1], baseURL + "/" + harvestBoxes[0].slug, baseURL + "/" + harvestBoxes[1].slug}
	wantErr := errors.New("network failed")
	for i, url := range urls {
		for _, failure := range []struct {
			name   string
			status int
			err    error
			want   string
		}{
			{"HTTP", http.StatusServiceUnavailable, nil, "HTTP 503"},
			{"parse", http.StatusOK, nil, "HTML has no"},
			{"network", 0, wantErr, "network failed"},
		} {
			t.Run(url+"/"+failure.name, func(t *testing.T) {
				client, calls, bodies := fixtureClient(t, url, failure.status, failure.err)
				got, err := NewStaplesProvider(client).FetchStaples(t.Context(), "smithbrothersfarms_delivery")
				require.ErrorContains(t, err, failure.want)
				assert.Contains(t, err.Error(), url)
				if failure.err != nil {
					assert.ErrorIs(t, err, wantErr)
				}
				assert.Nil(t, got)
				assert.Len(t, *calls, i+1)
				for _, body := range *bodies {
					assert.True(t, body.closed)
				}
			})
		}
	}
}

func TestFetchContext(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(t.Context(), contextKey{}, "propagated")
	client := NewClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "propagated", req.Context().Value(contextKey{}))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(fixture(t, "produce")))}, nil
	})})
	_, err := client.Fetch(ctx, stapleURLs[0])
	require.NoError(t, err)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	cancelClient, _, _ := fixtureClient(t, "", 0, nil)
	got, err := NewStaplesProvider(cancelClient).FetchStaples(canceled, "smithbrothersfarms_delivery")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, got)
	got, err = client.Fetch(t.Context(), "https://example.com/%zz")
	require.ErrorContains(t, err, "create request")
	assert.Nil(t, got)
}

func TestIdentityAndWineLookup(t *testing.T) {
	identity := NewIdentityProvider()
	assert.True(t, identity.IsID("smithbrothersfarms_delivery"))
	assert.True(t, identity.IsID("smithbrothersfarms_other"))
	for _, id := range []string{"", "smithbrothersfarms", "other_smithbrothersfarms_delivery"} {
		assert.False(t, identity.IsID(id))
	}
	original := identity.Signature()
	assert.Len(t, original, 64)
	assert.Equal(t, original, identity.Signature())
	saved := stapleURLs[0]
	t.Cleanup(func() { stapleURLs[0] = saved })
	stapleURLs[0] += "?changed=1"
	assert.NotEqual(t, original, identity.Signature())
	stapleURLs[0] = saved
	box := harvestBoxes[0].slug
	t.Cleanup(func() { harvestBoxes[0].slug = box })
	harvestBoxes[0].slug += "-changed"
	assert.NotEqual(t, original, identity.Signature())
	client, calls, _ := fixtureClient(t, "", 0, nil)
	provider := NewStaplesProvider(client)
	_, err := provider.FetchWines(t.Context(), "smithbrothersfarms_delivery", nil)
	require.ErrorContains(t, err, "not supported")
	_, err = provider.FetchWines(t.Context(), "other_delivery", nil)
	require.ErrorContains(t, err, "invalid")
	assert.Empty(t, *calls)
}
