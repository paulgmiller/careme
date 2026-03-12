package albertsons

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExtractCuratedGridConfig(t *testing.T) {
	t.Parallel()

	page := []byte(`
<!doctype html>
<html><body>
<search-grid
	data-products-per-page="30"
	data-custom-control-id="GR-MeatF-fffc8662"
	data-dvid-config=" web-4.1">
</search-grid>
<script>
SWY.CONFIGSERVICE.initSearchConfig('{"apimSubscriptionKey":"subscription-key","apimPathwaySearchProductsEndpoint":"/abs/pub/xapi/wcax/pathway/search"}');
</script>
</body></html>`)

	cfg, err := ExtractCuratedGridConfig(page)
	if err != nil {
		t.Fatalf("ExtractCuratedGridConfig returned error: %v", err)
	}

	if cfg.WidgetID != "GR-MeatF-fffc8662" {
		t.Fatalf("unexpected widget id: %q", cfg.WidgetID)
	}
	if cfg.DVID != "web-4.1search" {
		t.Fatalf("unexpected dvid: %q", cfg.DVID)
	}
	if cfg.ProductsPerPage != 30 {
		t.Fatalf("unexpected products per page: %d", cfg.ProductsPerPage)
	}
	if cfg.SubscriptionKey != "subscription-key" {
		t.Fatalf("unexpected subscription key: %q", cfg.SubscriptionKey)
	}
}

func TestPathwayClientFetchCuratedProducts(t *testing.T) {
	t.Parallel()

	var starts []string
	httpClient := &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			switch r.URL.Path {
			case "/aisle-vs/meat-seafood/meat-favorites.html":
				return httpResponse(http.StatusOK, `
<!doctype html>
<html><body>
<search-grid
	data-products-per-page="30"
	data-custom-control-id="GR-MeatF-fffc8662"
	data-dvid-config=" web-4.1">
</search-grid>
<script>
SWY.CONFIGSERVICE.initSearchConfig('{"apimSubscriptionKey":"subscription-key","apimPathwaySearchProductsEndpoint":"/abs/pub/xapi/wcax/pathway/search"}');
</script>
</body></html>`), nil
			case "/abs/pub/xapi/wcax/pathway/search":
				if got := r.Header.Get("ocp-apim-subscription-key"); got != "subscription-key" {
					t.Fatalf("unexpected subscription key header: %q", got)
				}
				if got := r.URL.Query().Get("widget-id"); got != "GR-MeatF-fffc8662" {
					t.Fatalf("unexpected widget-id: %q", got)
				}
				if got := r.URL.Query().Get("dvid"); got != "web-4.1search" {
					t.Fatalf("unexpected dvid: %q", got)
				}

				start := r.URL.Query().Get("start")
				starts = append(starts, start)

				var docs []PathwayProduct
				switch start {
				case "0":
					docs = []PathwayProduct{{PID: "first"}, {PID: "second"}}
				case "2":
					docs = []PathwayProduct{{PID: "third"}}
				default:
					docs = nil
				}

				payload, err := json.Marshal(PathwaySearchResponse{
					AppCode: "ok",
					AppMsg:  "ok",
					Response: PathwaySearchResults{
						Docs:     docs,
						NumFound: 3,
					},
				})
				if err != nil {
					t.Fatalf("marshal response: %v", err)
				}
				return httpResponse(http.StatusOK, string(payload)), nil
			default:
				return httpResponse(http.StatusNotFound, "not found"), nil
			}
		}),
	}

	client := NewPathwayClientWithBaseURL("https://example.test", httpClient)
	products, err := client.FetchCuratedProducts(context.Background(), "https://example.test/aisle-vs/meat-seafood/meat-favorites.html", FetchCuratedProductsOptions{
		StoreID: "490",
		Rows:    2,
		Banner:  "safeway",
	})
	if err != nil {
		t.Fatalf("FetchCuratedProducts returned error: %v", err)
	}

	if len(products) != 3 {
		t.Fatalf("unexpected product count: got %d want 3", len(products))
	}
	if products[0].PID != "first" || products[2].PID != "third" {
		t.Fatalf("unexpected product order: %+v", products)
	}
	if len(starts) != 2 || starts[0] != "0" || starts[1] != "2" {
		t.Fatalf("unexpected pagination starts: %+v", starts)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
