package wholefoods

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"careme/internal/brightdata"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCategory_BuildsRequestAndDecodesFixture(t *testing.T) {
	t.Parallel()

	fixture := loadFixture(t, "beef.json")

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	resp, err := client.Category(context.Background(), "beef", "10216")
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/api/products/category/beef" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("store"); got != "10216" {
		t.Fatalf("unexpected store query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("limit"); got != "60" {
		t.Fatalf("unexpected limit query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("offset"); got != "0" {
		t.Fatalf("unexpected offset query value: %q", got)
	}
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected Accept header: %q", got)
	}

	if len(resp) != 18 {
		t.Fatalf("unexpected results count: %d", len(resp))
	}
	if got := resp[0].Name; got != "Organic Ground Beef 93% Lean/7% Fat, 16 OZ" {
		t.Fatalf("unexpected first result name: %q", got)
	}
	if got := resp[14].SalePrice; got != 12.44 {
		t.Fatalf("unexpected sale price: %v", got)
	}
}

func TestCategory_RequiresQuerytermAndStore(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)

	_, err := client.Category(context.Background(), "", "10216")
	if err == nil || !strings.Contains(err.Error(), "queryterm is required") {
		t.Fatalf("unexpected queryterm error: %v", err)
	}

	_, err = client.Category(context.Background(), "beef", "")
	if err == nil || !strings.Contains(err.Error(), "store is required") {
		t.Fatalf("unexpected store error: %v", err)
	}
}

func TestCategory_UsesSeparateProxySessionPerCategory(t *testing.T) {
	t.Parallel()

	var sessions []string
	client := NewClientWithBaseURL("https://example.com", &http.Client{
		Transport: wholeFoodsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			sessionID, ok := brightdata.ProxySessionIDFromContext(r.Context())
			if !ok {
				t.Fatal("expected Bright Data proxy session in request context")
			}
			sessions = append(sessions, sessionID)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       ioNopCloserString(`{"results":[]}`),
				Request:    r,
			}, nil
		}),
	})

	if _, err := client.Category(context.Background(), "beef", "10216"); err != nil {
		t.Fatalf("first Category() error = %v", err)
	}
	if _, err := client.Category(context.Background(), "rice-grains", "10216"); err != nil {
		t.Fatalf("second Category() error = %v", err)
	}
	if got, want := len(sessions), 2; got != want {
		t.Fatalf("unexpected session count: got %d want %d", got, want)
	}
	if sessions[0] == sessions[1] {
		t.Fatalf("expected separate category sessions, both were %q", sessions[0])
	}
}

func TestCategory_StatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	_, err := client.Category(context.Background(), "beef", "10216")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCategory_RetriesTransient5xx(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := NewClientWithBaseURL("https://example.com", &http.Client{
		Transport: wholeFoodsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			if attempts < 3 {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Header:     make(http.Header),
					Body:       ioNopCloserString("temporary failure"),
					Request:    r,
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    ioNopCloserString(`{"results":[{"name":"Retry Beef","slug":"retry-beef","brand":"Whole Foods","store":10216}],"meta":{"total":{"value":1,"relation":"eq"},"state":{"refinements":[],"sort":""}}}`),
				Request: r,
			}, nil
		}),
	})

	resp, err := client.Category(context.Background(), "beef", "10216")
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if got, want := attempts, 3; got != want {
		t.Fatalf("unexpected attempts: got %d want %d", got, want)
	}
	if got, want := len(resp), 1; got != want {
		t.Fatalf("unexpected result count: got %d want %d", got, want)
	}
	if got := resp[0].Name; got != "Retry Beef" {
		t.Fatalf("unexpected product name: %q", got)
	}
}

func TestCategory_RetriesRequestAttemptTimeoutWithinCategoryBudget(t *testing.T) {
	t.Parallel()

	attempts := 0
	client := NewClientWithBaseURL("https://example.com", &http.Client{
		Timeout: time.Millisecond,
		Transport: wholeFoodsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    ioNopCloserString(`{"results":[{"name":"Timeout Retry Beef","slug":"timeout-retry-beef","brand":"Whole Foods","store":10216}],"meta":{"total":{"value":1,"relation":"eq"},"state":{"refinements":[],"sort":""}}}`),
				Request: r,
			}, nil
		}),
	})

	resp, err := client.Category(context.Background(), "beef", "10216")
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if got, want := attempts, 2; got != want {
		t.Fatalf("unexpected attempts: got %d want %d", got, want)
	}
	if got, want := len(resp), 1; got != want {
		t.Fatalf("unexpected result count: got %d want %d", got, want)
	}
	if got := resp[0].Name; got != "Timeout Retry Beef" {
		t.Fatalf("unexpected product name: %q", got)
	}
}

func TestCategory_RetriesLatePageAttemptTimeoutWithinCategoryBudget(t *testing.T) {
	t.Parallel()

	attemptsByOffset := map[string]int{}
	var proxySessions []string
	client := NewClientWithBaseURL("https://example.com", wholeFoodsProxyClient(t, &http.Client{
		Transport: wholeFoodsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			sessionID, ok := brightdata.ProxySessionIDFromContext(r.Context())
			if !ok {
				t.Fatal("expected Bright Data proxy session in request context")
			}
			proxySessions = append(proxySessions, sessionID)

			offset := r.URL.Query().Get("offset")
			attemptsByOffset[offset]++
			if offset == strconv.Itoa(defaultCategoryLimit*2) && attemptsByOffset[offset] == 1 {
				return nil, context.DeadlineExceeded
			}

			pageSize := defaultCategoryLimit
			if offset == strconv.Itoa(defaultCategoryLimit*3) {
				pageSize = 1
			}

			results := make([]product, 0, pageSize)
			for i := 0; i < pageSize; i++ {
				results = append(results, product{
					Name:  fmt.Sprintf("Product %s-%d", offset, i),
					Slug:  fmt.Sprintf("product-%s-%d", offset, i),
					Brand: "Whole Foods Market",
					Store: 10216,
				})
			}

			payload, err := json.Marshal(categoryResponse{Results: results})
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
				},
				Body:    io.NopCloser(strings.NewReader(string(payload))),
				Request: r,
			}, nil
		}),
	}))

	resp, err := client.Category(context.Background(), "fresh-vegetables", "10153")
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if got, want := attemptsByOffset[strconv.Itoa(defaultCategoryLimit*2)], 2; got != want {
		t.Fatalf("unexpected late page attempts: got %d want %d", got, want)
	}
	if got, want := len(resp), defaultCategoryLimit*3+1; got != want {
		t.Fatalf("unexpected result count: got %d want %d", got, want)
	}
	require.Len(t, proxySessions, 5)
	assert.Equal(t, proxySessions[0], proxySessions[1])
	assert.Equal(t, proxySessions[0], proxySessions[2])
	assert.NotEqual(t, proxySessions[2], proxySessions[3], "timeout retry should rotate the session")
	assert.Equal(t, proxySessions[3], proxySessions[4], "successful pagination should retain the replacement session")
}

func TestCategory_RotatesSessionsThroughBrightDataTimeouts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		timeouts int
		attempts int
	}{
		{name: "recovers", timeouts: 1, attempts: 2},
		{name: "exhausted", timeouts: 3, attempts: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Exercise the nested retry stack used in production. The inner
			// Bright Data client receives an expired per-attempt context.
			proxyClient, err := brightdata.NewProxyAwareHTTPClient(brightdata.ProxyConfig{})
			require.NoError(t, err)
			proxyClient.Timeout = time.Millisecond
			var sessions []string
			proxyClient.Transport.(*retryablehttp.RoundTripper).Client.HTTPClient.Transport = wholeFoodsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				session, ok := brightdata.ProxySessionIDFromContext(r.Context())
				require.True(t, ok)
				sessions = append(sessions, session)
				if len(sessions) <= tc.timeouts {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: ioNopCloserString(`{"results":[]}`), Request: r}, nil
			})
			_, err = NewClientWithBaseURL("https://example.com", proxyClient).Category(t.Context(), "shellfish", "10103")
			if tc.timeouts == 3 {
				require.ErrorContains(t, err, "wholefoods request failed after 3 attempt(s)")
				assert.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, sessions, tc.attempts)
			assert.Len(t, slices.Compact(slices.Clone(sessions)), tc.attempts, "every timeout retry should use a new session")
		})
	}
}

func TestCategory_RetrySessionPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		status     int
		err        error
		cancel     bool
		attempts   int
		wantRotate bool
	}{
		{name: "timeout", err: context.DeadlineExceeded, attempts: 2, wantRotate: true},
		{name: "server error", status: http.StatusBadGateway, attempts: 2},
		{name: "not found", status: http.StatusNotFound, attempts: 1},
		{name: "canceled", err: context.Canceled, cancel: true, attempts: 1},
		{name: "network error", err: errors.New("connection reset"), attempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var sessions []string
			client := NewClientWithBaseURL("https://example.com", wholeFoodsProxyClient(t, &http.Client{
				Transport: wholeFoodsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
					session, ok := brightdata.ProxySessionIDFromContext(r.Context())
					require.True(t, ok)
					sessions = append(sessions, session)
					if len(sessions) == 1 {
						if tc.cancel {
							cancel()
						}
						if tc.err != nil {
							return nil, tc.err
						}
						return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: ioNopCloserString(`{}`), Request: r}, nil
					}
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: ioNopCloserString(`{"results":[]}`), Request: r}, nil
				}),
			}))
			_, err := client.Category(ctx, "shellfish", "10103")
			if tc.attempts == 2 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				if tc.err != nil {
					assert.ErrorIs(t, err, tc.err)
				}
			}
			require.Len(t, sessions, tc.attempts)
			if tc.attempts == 2 {
				assert.Equal(t, tc.wantRotate, sessions[0] != sessions[1])
			}
		})
	}
}

func TestCategory_LogsRetryAttemptsWithCategoryStoreAndOffset(t *testing.T) {
	var logBuf bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
	})

	attemptsByOffset := map[string]int{}
	client := NewClientWithBaseURL("https://example.com", wholeFoodsProxyClient(t, &http.Client{
		Transport: wholeFoodsRoundTripFunc(func(r *http.Request) (*http.Response, error) {
			offset := r.URL.Query().Get("offset")
			attemptsByOffset[offset]++
			if offset == strconv.Itoa(defaultCategoryLimit*2) && attemptsByOffset[offset] == 1 {
				return nil, context.DeadlineExceeded
			}

			pageSize := defaultCategoryLimit
			if offset == strconv.Itoa(defaultCategoryLimit*2) {
				pageSize = 1
			}

			results := make([]product, 0, pageSize)
			for i := 0; i < pageSize; i++ {
				results = append(results, product{
					Name:  fmt.Sprintf("Product %s-%d", offset, i),
					Slug:  fmt.Sprintf("product-%s-%d", offset, i),
					Brand: "Whole Foods Market",
					Store: 10216,
				})
			}

			payload, err := json.Marshal(categoryResponse{Results: results})
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(string(payload))),
				Request:    r,
			}, nil
		}),
	}))

	_, err := client.Category(context.Background(), "fresh-vegetables", "10153")
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}

	logs := logBuf.String()
	for _, want := range []string{
		"Rotating Bright Data proxy session",
		"reason=timeout",
		"Retrying HTTP request",
		"source=wholefoods",
		`url="https://example.com/api/products/category/fresh-vegetables?limit=60&offset=120&store=10153"`,
		"attempt=2",
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("retry log missing %q in logs:\n%s", want, logs)
		}
	}
}

func TestCategory_PaginatesUntilShortPage(t *testing.T) {
	t.Parallel()

	var offsets []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Fatalf("parse offset: %v", err)
		}
		offsets = append(offsets, offset)

		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil {
			t.Fatalf("parse limit: %v", err)
		}
		if limit != defaultCategoryLimit {
			t.Fatalf("unexpected limit: %d", limit)
		}

		pageSize := limit
		if offset >= limit*2 {
			pageSize = 5
		}

		results := make([]product, 0, pageSize)
		for i := 0; i < pageSize; i++ {
			n := offset + i
			results = append(results, product{
				Name:  fmt.Sprintf("Product %d", n),
				Slug:  fmt.Sprintf("product-%d", n),
				Brand: "Whole Foods Market",
				Store: 10216,
			})
		}

		resp := categoryResponse{
			Breadcrumb: []breadcrumb{{Label: "Meat", Slug: "meat"}, {Label: "Fish", Slug: "fish"}},
			Meta: meta{
				Total: total{Value: limit*2 + 5, Relation: "eq"},
			},
			Results: results,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	resp, err := client.Category(context.Background(), "fish", "10216")
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}

	if got, want := offsets, []int{0, defaultCategoryLimit, defaultCategoryLimit * 2}; !slices.Equal(got, want) {
		t.Fatalf("unexpected offsets: got %v want %v", got, want)
	}
	if got, want := len(resp), defaultCategoryLimit*2+5; got != want {
		t.Fatalf("unexpected result count: got %d want %d", got, want)
	}
	if got := resp[0].Slug; got != "product-0" {
		t.Fatalf("unexpected first slug: %q", got)
	}
	if got := resp[len(resp)-1].Slug; got != "product-124" {
		t.Fatalf("unexpected last slug: %q", got)
	}
}

func TestStoreSummary_BuildsRequestAndDecodesResponse(t *testing.T) {
	t.Parallel()

	fixture := []byte(`{"storeId":10216,"token":"westlake","displayName":"Westlake","status":"Open","phone":"(206) 621-9700","storePrimeEligibility":true,"storeOperationalGuidance":"","bu":10216,"folder":"westlake","openedAt":"2006-11-08T12:00:00Z","links":{"Details":"/stores/westlake","Directions":"https://www.google.com/maps/dir/?api=1&destination=47.618249,-122.337898","Sales":"/sales-flyer?store-id=10216","PrimeNowPickUpAndDelivery":"https://www.wholefoods.com/grocery?ref_=US_TRF_ALL_UFG_WFM_REFER_0417726","MapUrlDesktop":"https://maps.googleapis.com/maps/api/staticmap?zoom=16&size=780x543","MapUrlTablet":"https://maps.googleapis.com/maps/api/staticmap?zoom=16&size=780x458","MapUrlMobile":"https://maps.googleapis.com/maps/api/staticmap?zoom=17&size=780x228"},"primaryLocation":{"address":{"STREET_ADDRESS_LINE1":"2210 Westlake Ave","CITY":"Seattle","STATE":"WA","POSTAL_CODE":"98121","ZIP_CODE":"98121","COUNTRY":"United States of America"},"latitude":47.618249,"longitude":-122.337898},"hours":{"Open":"8 am – 10 pm today","Sat":"8 am – 10 pm","Sun":"8 am – 10 pm","Mon":"8 am – 10 pm","Tue":"8 am – 10 pm","Wed":"8 am – 10 pm","Thu":"8 am – 10 pm"},"holidays":{}}`)

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	resp, err := client.StoreSummary(context.Background(), "10216")
	if err != nil {
		t.Fatalf("StoreSummary returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/api/stores/10216/summary" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected Accept header: %q", got)
	}

	if got := resp.StoreID; got != 10216 {
		t.Fatalf("unexpected store id: %d", got)
	}
	if got := resp.DisplayName; got != "Westlake" {
		t.Fatalf("unexpected display name: %q", got)
	}
	if got := resp.PrimaryLocation.Address.City; got != "Seattle" {
		t.Fatalf("unexpected city: %q", got)
	}
	if got := resp.PrimaryLocation.Latitude; got != 47.618249 {
		t.Fatalf("unexpected latitude: %v", got)
	}
	if got := resp.Hours["Open"]; got != "8 am – 10 pm today" {
		t.Fatalf("unexpected open hours: %q", got)
	}
}

func TestStoreSummary_RequiresStore(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)

	_, err := client.StoreSummary(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "store is required") {
		t.Fatalf("unexpected store error: %v", err)
	}
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

type wholeFoodsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f wholeFoodsRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func ioNopCloserString(body string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(body))
}

// Disable inner retries here so these tests exercise the provider's outer retry
// policy while retaining Bright Data's session handling.
func wholeFoodsProxyClient(t *testing.T, baseClient *http.Client) *http.Client {
	t.Helper()
	client, err := brightdata.NewProxyAwareHTTPClient(brightdata.ProxyConfig{})
	require.NoError(t, err)
	transport := client.Transport.(*retryablehttp.RoundTripper)
	transport.Client.HTTPClient = baseClient
	transport.Client.RetryMax = 0
	transport.Client.ErrorHandler = retryablehttp.PassthroughErrorHandler
	return client
}
