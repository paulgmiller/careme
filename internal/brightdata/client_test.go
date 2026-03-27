package brightdata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewClient_RequiresAPIKey(t *testing.T) {
	t.Parallel()

	_, err := NewClient("", nil)
	if err == nil || !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestScrape_BuildsRequestAndReturnsRawBody(t *testing.T) {
	t.Parallel()

	type walmartInput struct {
		URL      string `json:"url"`
		ZipCode  string `json:"zip_code"`
		StoreID  string `json:"store_id"`
		MaxItems int    `json:"max_items,omitempty"`
	}

	var capturedReq *http.Request
	var capturedBody scrapePayload
	client := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		capturedReq = r
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, `[{"name":"Red Bull"},{"name":"OLIPOP"}]`), nil
	})

	resp, err := client.Scrape(context.Background(), "gd_m693oc1r1gebnayxq", []walmartInput{
		{
			URL:      "https://www.walmart.com/ip/5332753715",
			ZipCode:  "33177",
			StoreID:  "6397",
			MaxItems: 4,
		},
	}, ScrapeOptions{
		Notify:        false,
		IncludeErrors: true,
		Format:        FormatJSON,
	})
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/datasets/v3/scrape" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("dataset_id"); got != "gd_m693oc1r1gebnayxq" {
		t.Fatalf("unexpected dataset_id: %q", got)
	}
	if got := capturedReq.URL.Query().Get("notify"); got != "false" {
		t.Fatalf("unexpected notify value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("include_errors"); got != "true" {
		t.Fatalf("unexpected include_errors value: %q", got)
	}
	if got := capturedReq.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("unexpected authorization header: %q", got)
	}

	inputs, ok := capturedBody.Input.([]any)
	if !ok || len(inputs) != 1 {
		t.Fatalf("unexpected wrapped input payload: %#v", capturedBody.Input)
	}
	first, ok := inputs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first input type: %T", inputs[0])
	}
	if got := first["zip_code"]; got != "33177" {
		t.Fatalf("unexpected zip_code: %#v", got)
	}
	if got := string(resp.Body); got != `[{"name":"Red Bull"},{"name":"OLIPOP"}]` {
		t.Fatalf("unexpected body: %s", resp.Body)
	}
}

func TestScrape_ReturnsSnapshotIDOnAccepted(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusAccepted, `{"snapshot_id":"s_123","message":"still running"}`), nil
	})

	resp, err := client.Scrape(context.Background(), "gd_test", []map[string]string{{"url": "https://example.com"}}, ScrapeOptions{})
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	if resp.SnapshotID != "s_123" {
		t.Fatalf("unexpected snapshot id: %q", resp.SnapshotID)
	}
	if resp.Message != "still running" {
		t.Fatalf("unexpected message: %q", resp.Message)
	}
}

func TestTrigger_BuildsRawInputArray(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	var capturedBody []map[string]any
	client := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		capturedReq = r
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, `{"snapshot_id":"s_async"}`), nil
	})

	resp, err := client.Trigger(context.Background(), "gd_async", []map[string]string{
		{"url": "https://www.walmart.com/ip/5332753715", "zip_code": "33177", "store_id": "6397"},
	}, TriggerOptions{
		IncludeErrors: true,
		Format:        FormatNDJSON,
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/datasets/v3/trigger" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("format"); got != "ndjson" {
		t.Fatalf("unexpected format: %q", got)
	}
	if len(capturedBody) != 1 || capturedBody[0]["store_id"] != "6397" {
		t.Fatalf("unexpected trigger body: %#v", capturedBody)
	}
	if resp.SnapshotID != "s_async" {
		t.Fatalf("unexpected snapshot id: %q", resp.SnapshotID)
	}
}

func TestWaitAndDownload_PollsThenReturnsBody(t *testing.T) {
	t.Parallel()

	progressCalls := 0
	client := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/datasets/v3/progress/s_ready":
			progressCalls++
			if progressCalls == 1 {
				return jsonHTTPResponse(http.StatusOK, `{"snapshot_id":"s_ready","dataset_id":"gd_test","status":"running"}`), nil
			}
			return jsonHTTPResponse(http.StatusOK, `{"snapshot_id":"s_ready","dataset_id":"gd_test","status":"ready"}`), nil
		case "/datasets/v3/snapshot/s_ready":
			return jsonHTTPResponse(http.StatusOK, `[{"sku":"123"}]`), nil
		default:
			return jsonHTTPResponse(http.StatusNotFound, `{"error":"not found"}`), nil
		}
	})

	resp, err := client.WaitAndDownload(context.Background(), "s_ready", time.Millisecond, DownloadOptions{Format: FormatJSON})
	if err != nil {
		t.Fatalf("wait and download: %v", err)
	}

	if !resp.Ready {
		t.Fatal("expected ready response")
	}
	if progressCalls < 2 {
		t.Fatalf("expected at least two progress calls, got %d", progressCalls)
	}
	if got := string(resp.Body); got != `[{"sku":"123"}]` {
		t.Fatalf("unexpected body: %s", resp.Body)
	}
}

func TestDownloadSnapshot_NotReady(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		return jsonHTTPResponse(http.StatusAccepted, `{"status":"building","message":"Snapshot is building, try again in 10s"}`), nil
	})

	resp, err := client.DownloadSnapshot(context.Background(), "s_building", DownloadOptions{})
	if err != nil {
		t.Fatalf("download snapshot: %v", err)
	}
	if resp.Ready {
		t.Fatal("expected not-ready response")
	}
	if resp.Status != SnapshotStatusBuilding {
		t.Fatalf("unexpected status: %q", resp.Status)
	}
}

func TestDeliverToAzure_BuildsDeliveryRequest(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	var capturedBody map[string]any
	client := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		capturedReq = r
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return jsonHTTPResponse(http.StatusOK, `{"delivery_id":"d_123"}`), nil
	})

	resp, err := client.DeliverToAzure(context.Background(), "s_ready", AzureDeliveryOptions{
		Container:        "brightdata-results",
		Account:          "acct",
		Key:              "base64-key",
		Directory:        "walmart/2026-03-25",
		FilenameTemplate: "snapshot_{{id}}",
		Extension:        DeliveryExtensionJSON,
		Compress:         true,
		BatchSize:        1000,
	})
	if err != nil {
		t.Fatalf("deliver to azure: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/datasets/v3/deliver/s_ready" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	deliver, ok := capturedBody["deliver"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected deliver payload: %#v", capturedBody)
	}
	if got := deliver["type"]; got != "azure" {
		t.Fatalf("unexpected delivery type: %#v", got)
	}
	if got := deliver["container"]; got != "brightdata-results" {
		t.Fatalf("unexpected container: %#v", got)
	}
	creds, ok := deliver["credentials"].(map[string]any)
	if !ok || creds["account"] != "acct" || creds["key"] != "base64-key" {
		t.Fatalf("unexpected credentials: %#v", deliver["credentials"])
	}
	if resp.DeliveryID != "d_123" {
		t.Fatalf("unexpected delivery id: %q", resp.DeliveryID)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newTestClient(t *testing.T, fn roundTripFunc) *Client {
	t.Helper()

	client, err := NewClientWithBaseURL("https://api.example.test", "test-token", &http.Client{
		Transport: fn,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func jsonHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(body)),
	}
}
