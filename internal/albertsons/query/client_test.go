package query

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNewSearchClientRequiresSubscriptionKey(t *testing.T) {
	t.Parallel()

	_, err := NewSearchClient(SearchClientConfig{})
	if err == nil || !strings.Contains(err.Error(), "subscription key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSearchBuildsExpectedRequest(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client, err := NewSearchClient(SearchClientConfig{
		BaseURL:         "https://www.acmemarkets.com",
		SubscriptionKey: "test-subscription-key",
		Reese84:         "reese-cookie",
		VisitorID:       "visitor-123",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				capturedReq = r
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
				}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSearchClient returned error: %v", err)
	}

	resp, err := client.Search(context.Background(), "806", "19711", SearchOptions{
		HouseID:  "729022287633",
		ClubCard: "49549812729",
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != defaultSearchPath {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}

	query := capturedReq.URL.Query()
	assertQueryValue(t, query, "url", "https://www.acmemarkets.com")
	assertQueryValue(t, query, "q", "")
	assertQueryValue(t, query, "rows", "30")
	assertQueryValue(t, query, "start", "0")
	assertQueryValue(t, query, "channel", "instore")
	assertQueryValue(t, query, "storeid", "806")
	assertQueryValue(t, query, "sort", "")
	assertQueryValue(t, query, "widget-id", "GR-C-Categ-6090cd27")
	assertQueryValue(t, query, "dvid", "web-4.1search")
	assertQueryValue(t, query, "visitorId", "visitor-123")
	assertQueryValue(t, query, "uuid", "null")
	assertQueryValue(t, query, "pgm", "abs")
	assertQueryValue(t, query, "includeOffer", "true")
	assertQueryValue(t, query, "banner", "acmemarkets")
	assertQueryValue(t, query, "facet", "false")

	if got := capturedReq.Header.Get("Accept"); got != "application/json, text/plain, */*" {
		t.Fatalf("unexpected accept header: %q", got)
	}
	if got := capturedReq.Header.Get("Accept-Language"); got != "en-US,en;q=0.9" {
		t.Fatalf("unexpected accept-language header: %q", got)
	}
	if got := capturedReq.Header.Get("Ocp-Apim-Subscription-Key"); got != "test-subscription-key" {
		t.Fatalf("unexpected subscription header: %q", got)
	}

	previousLoginValue, err := capturedReq.Cookie("ACI_S_abs_previouslogin")
	if err != nil {
		t.Fatalf("expected ACI_S_abs_previouslogin cookie: %v", err)
	}

	decodedCookie, err := url.QueryUnescape(previousLoginValue.Value)
	if err != nil {
		t.Fatalf("QueryUnescape returned error: %v", err)
	}

	var previousLogin previousLoginCookie
	if err := json.Unmarshal([]byte(decodedCookie), &previousLogin); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	common := previousLogin.Info.Common
	if common.HouseID != "729022287633" {
		t.Fatalf("unexpected houseId: %q", common.HouseID)
	}
	if common.ClubCard != "49549812729" {
		t.Fatalf("unexpected clubCard: %q", common.ClubCard)
	}
	if common.UserType != "G" {
		t.Fatalf("unexpected userType: %q", common.UserType)
	}
	if common.StoreID != "806" {
		t.Fatalf("unexpected storeId: %q", common.StoreID)
	}
	if common.ZipCode != "19711" {
		t.Fatalf("unexpected zipcode: %q", common.ZipCode)
	}

	reese84Cookie, err := capturedReq.Cookie("reese84")
	if err != nil {
		t.Fatalf("expected reese84 cookie: %v", err)
	}
	if reese84Cookie.Value != "reese-cookie" {
		t.Fatalf("unexpected reese84 cookie: %q", reese84Cookie.Value)
	}

	if string(resp.Body) != `{"ok":true}` {
		t.Fatalf("unexpected body: %s", resp.Body)
	}
}

func TestSearchInfersSafewayBannerByDefault(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client, err := NewSearchClient(SearchClientConfig{
		SubscriptionKey: "test-subscription-key",
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				capturedReq = r
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			}),
		},
	})
	if err != nil {
		t.Fatalf("NewSearchClient returned error: %v", err)
	}

	if _, err := client.Search(context.Background(), "1444", "98006", SearchOptions{}); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if got := capturedReq.URL.Query().Get("banner"); got != "safeway" {
		t.Fatalf("unexpected banner: %q", got)
	}
	if got := capturedReq.URL.Query().Get("url"); got != DefaultSearchBaseURL {
		t.Fatalf("unexpected url query value: %q", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func assertQueryValue(t *testing.T, values url.Values, key, want string) {
	t.Helper()
	if got := values.Get(key); got != want {
		t.Fatalf("unexpected %s: got %q want %q", key, got, want)
	}
}
