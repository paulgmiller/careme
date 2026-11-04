package localfoodmarketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicCatalog(t *testing.T) {
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		name := "storefront.html"
		if r.Method == http.MethodPost {
			assert.Equal(t, "/api/ProductApi/getProducts", r.URL.Path)
			assert.Equal(t, "application/json; charset=utf-8", r.Header.Get("Content-Type"))
			var params map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&params))
			assert.Equal(t, map[string]string{"callingPage": "Products", "catId": "", "customListId": "0", "listType": "ShowAll", "locId": "5", "subPeriodId": "4"}, params)
			name = "catalog.json"
		} else {
			assert.Equal(t, "/Products", r.URL.Path)
		}
		body, err := os.ReadFile("testdata/" + name)
		require.NoError(t, err)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})
	original := &http.Client{Transport: transport}
	c := NewClient("https://store.example", original)
	items, err := c.Fetch(t.Context(), Selection{CategoryIDs: []int{1230, 1232}})
	require.NoError(t, err)
	require.Len(t, items, 58)
	assert.Equal(t, 2, calls)
	assert.IsType(t, transport, original.Transport)
	var rendered bytes.Buffer
	require.NoError(t, ai.InputIngredientsToTSV(items, &rendered))
	assert.Contains(t, rendered.String(), "Albacore Tuna Loin")
	assert.Contains(t, rendered.String(), "Yukon Gold Potatoes")
	for _, item := range items {
		assert.NotEmpty(t, item.Size)
		assert.NotEmpty(t, item.VariantID)
		require.NotNil(t, item.PriceRegular)
		assert.NotEmpty(t, item.Brand)
	}
	expanded, err := c.Fetch(t.Context(), Selection{CategoryIDs: []int{1230, 1232, 1222, 1225}, SubcategoryIDs: []int{1289}})
	require.NoError(t, err)
	require.Len(t, expanded, 61)
	rendered.Reset()
	require.NoError(t, ai.InputIngredientsToTSV(expanded, &rendered))
	for _, name := range []string{"Asiago Fresco Cheese", "Bosc Pears", "Campanelle,16oz"} {
		assert.Contains(t, rendered.String(), name)
	}
	assert.NotContains(t, rendered.String(), "Alo Tama Bodi Soup")
}

func TestSubcategorySelection(t *testing.T) {
	data, err := os.ReadFile("testdata/catalog.json")
	require.NoError(t, err)
	var catalog struct {
		Featured []product `json:"featured"`
	}
	require.NoError(t, json.Unmarshal(data, &catalog))
	items, err := ingredients(catalog.Featured, Selection{SubcategoryIDs: []int{1289}})
	require.NoError(t, err)
	require.Len(t, items, 1, "only pasta, not other products in its parent category")
	assert.Contains(t, items[0].Categories, "Pasta")
	pasta := catalog.Featured[2]
	items, err = ingredients([]product{pasta, pasta}, Selection{CategoryIDs: []int{pasta.CategoryID}, SubcategoryIDs: []int{1289}})
	require.NoError(t, err)
	require.Len(t, items, 1, "overlapping category and subcategory selections must not duplicate products")
	items, err = ingredients(catalog.Featured[:2], Selection{CategoryIDs: []int{1222, 1225}, SubcategoryIDs: []int{1289}})
	require.ErrorContains(t, err, "missing subcategory 1289")
	assert.Nil(t, items, "missing pasta must fail the complete selection")
}

func TestCatalogFailures(t *testing.T) {
	for _, tc := range []struct {
		name, page, catalog, want string
		status                    int
	}{
		{name: "missing fields", page: "<html>Login</html>", want: "storefront missing"},
		{name: "HTTP failure", status: 403, want: "HTTP 403"},
		{name: "malformed JSON", catalog: "{", want: "parse LFM"},
		{name: "null JSON", catalog: "null", want: "missing category"},
		{name: "missing categories", catalog: `{"standard":[],"featured":[]}`, want: "missing category"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := os.ReadFile("testdata/storefront.html")
			require.NoError(t, err)
			if tc.page != "" {
				page = []byte(tc.page)
			}
			c := NewClient("https://store.example", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				body := string(page)
				if r.Method == http.MethodPost {
					body = tc.catalog
				}
				status := 200
				if tc.status != 0 {
					status = tc.status
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})})
			items, err := c.Fetch(t.Context(), Selection{CategoryIDs: []int{1230, 1232}})
			require.ErrorContains(t, err, tc.want)
			assert.Nil(t, items)
		})
	}
}

func TestSelectionAndPrices(t *testing.T) {
	const catalog = `[{"uniqueId":"meat_producer","productName":"Beef &amp; pork","producerName":"Farm","catId":1230,"parentCat":"Meats","mainCat":"Beef","units":[{"puId":1,"unitName":"sold out","remaining":0},{"puId":2,"unitName":"1 lb","remaining":2,"unitPrice":"$8.50","unitRegPrice":"$10.00","unitOnSale":true}]}]`
	var products []product
	require.NoError(t, json.Unmarshal([]byte(catalog), &products))
	products = append(products, products[0], product{CategoryID: 999})
	items, err := ingredients(products, Selection{CategoryIDs: []int{1230}})
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "2", items[0].VariantID)
	assert.Equal(t, "Beef & pork", items[0].Description)
	assert.Equal(t, "1 lb", items[0].Size)
	assert.Equal(t, float32(10), *items[0].PriceRegular)
	assert.Equal(t, float32(8.5), *items[0].PriceSale)
	for _, mutate := range []func(*product){
		func(p *product) { p.UniqueID = "" }, func(p *product) { p.Units = nil }, func(p *product) { p.HidePrices = true }, func(p *product) { p.Units[1].Price = "bad" },
	} {
		var rows []product
		require.NoError(t, json.Unmarshal([]byte(catalog), &rows))
		mutate(&rows[0])
		got, err := ingredients(rows, Selection{CategoryIDs: []int{1230}})
		require.Error(t, err)
		assert.Nil(t, got)
	}
	products[0].Unavailable = true
	got, err := ingredients(products[:1], Selection{CategoryIDs: []int{1230}})
	require.ErrorContains(t, err, "no available")
	assert.Nil(t, got)
}

func TestLiveLocalYokels(t *testing.T) {
	if os.Getenv("LOCALYOKELS_LIVE") != "1" {
		t.Skip("set LOCALYOKELS_LIVE=1 for public catalog smoke test")
	}
	items, err := NewClient("https://localyokels.localfoodmarketplace.com", http.DefaultClient).Fetch(t.Context(), Selection{CategoryIDs: []int{1230, 1232, 1222, 1225}, SubcategoryIDs: []int{1289}})
	require.NoError(t, err)
	require.NotEmpty(t, items)
	t.Logf("Fetched %d staple products", len(items))
}

func TestRetryAndCancellation(t *testing.T) {
	posts := 0
	c := NewClient("https://store.example", &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		name := "storefront.html"
		if r.Method == http.MethodPost {
			posts++
			var params map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&params))
			assert.Equal(t, "4", params["subPeriodId"], "POST body must survive retry")
			if posts == 1 {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("temporarily unavailable")), Header: make(http.Header)}, nil
			}
			name = "catalog.json"
		}
		body, err := os.ReadFile("testdata/" + name)
		require.NoError(t, err)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})})
	items, err := c.Fetch(t.Context(), Selection{CategoryIDs: []int{1230, 1232}})
	require.NoError(t, err)
	require.Len(t, items, 58)
	assert.Equal(t, 2, posts)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	items, err = c.Fetch(ctx, Selection{CategoryIDs: []int{1230, 1232}})
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, items)
}
