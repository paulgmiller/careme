package kroger

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/ingredients/cachekey"
	"careme/internal/providers/kroger/products"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityProviderHashForStoreIncludesStapleDefinitions(t *testing.T) {
	t.Parallel()
	date := time.Date(2025, 9, 17, 9, 0, 0, 0, time.UTC)
	const locationID = "70500874"
	signature := mustJSONSignature(defaultStaples())
	want := cachekey.NewHash(locationID, date, "item-upc-v1/"+signature).Sum(nil)
	assert.Equal(t, want, NewIdentityProvider().HashForStore(locationID, date).Sum(nil))
}

func TestParseProductGetResponse_KeepsJSON400Body(t *testing.T) {
	rsp := httptest.NewRecorder()
	rsp.Header().Set("Content-Type", "application/json")
	rsp.WriteHeader(http.StatusBadRequest)
	_, _ = rsp.WriteString(`{"errors":{"timestamp":1776969026460,"code":"PRODUCT-2011","reason":"Field 'locationId' must have a length of 8 alphanumeric characters"}}`)

	parsed, err := products.ParseProductGetResponse(rsp.Result())
	require.NoError(t, err)
	require.NotNil(t, parsed.JSON400)
	assert.Contains(t, string(parsed.Body), "PRODUCT-2011")
	assert.Contains(t, string(parsed.Body), "length of 8")
}

func TestParseProductGetResponse_IgnoresUnusedPriceDateTimes(t *testing.T) {
	rsp := httptest.NewRecorder()
	rsp.Header().Set("Content-Type", "application/json")
	rsp.WriteHeader(http.StatusOK)
	_, _ = rsp.WriteString(`{
		"data": [{
			"items": [{
				"price": {
					"expirationDate": {"value": "9999-12-31T00:00:00Z", "timezone": "UTC"},
					"effectiveDate": {"value": "2026-04-29T03:59:59.999Z", "timezone": "UTC"}
				}
			}]
		}]
	}`)

	parsed, err := products.ParseProductGetResponse(rsp.Result())
	require.NoError(t, err)
	require.NotNil(t, parsed.JSON200)
	require.NotNil(t, parsed.JSON200.Data)
}

func TestParseProductGetResponse_IgnoresUnusedNutritionInformationArray(t *testing.T) {
	rsp := httptest.NewRecorder()
	rsp.Header().Set("Content-Type", "application/json")
	rsp.WriteHeader(http.StatusOK)
	_, _ = rsp.WriteString(`{
		"data": [{
			"nutritionInformation": [{
				"ingredientStatement": "beef"
			}]
		}]
	}`)

	parsed, err := products.ParseProductGetResponse(rsp.Result())
	require.NoError(t, err)
	require.NotNil(t, parsed.JSON200)
	require.NotNil(t, parsed.JSON200.Data)
}

func TestSearchIngredients_RetriesTransientProductFailures(t *testing.T) {
	var calls atomic.Int32
	baseClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call < 3 {
			return jsonResponse(req, http.StatusServiceUnavailable, `{"errors":{"code":"PRODUCT-4109-500","reason":"Service Unavailable"}}`), nil
		}
		return jsonResponse(req, http.StatusOK, `{"data":[]}`), nil
	})}

	client, err := products.NewClientWithResponses(
		"https://kroger.test",
		products.WithHTTPClient(withRetries(baseClient)),
	)
	require.NoError(t, err)

	got, err := searchIngredients(t.Context(), client, "70500874", "pork", false)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, int32(3), calls.Load())
}

func TestStaplesProvider_FetchWines_UsesEachStyleAsSearchTerm(t *testing.T) {
	var mu sync.Mutex
	var terms []string
	baseClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		terms = append(terms, req.URL.Query().Get("filter.term"))
		mu.Unlock()
		return jsonResponse(req, http.StatusOK, `{"data":[{
			"productId":"wine-1",
			"brand":"Kroger",
			"description":"Pinot Noir",
			"categories":["Wine"],
			"items":[{"size":"750mL","price":{"regular":12.99}}]
		}]}`), nil
	})}

	client, err := products.NewClientWithResponses(
		"https://kroger.test",
		products.WithHTTPClient(baseClient),
	)
	require.NoError(t, err)

	provider := staplesProvider{client: client}
	got, err := provider.FetchWines(t.Context(), "70500874", []string{"Pinot Noir", "Sauvignon Blanc"})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"Pinot Noir", "Sauvignon Blanc"}, terms)
}

func TestStaplesProvider_FetchPantry_UsesPantrySearchTerms(t *testing.T) {
	var mu sync.Mutex
	var terms []string
	baseClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		terms = append(terms, req.URL.Query().Get("filter.term"))
		mu.Unlock()
		return jsonResponse(req, http.StatusOK, `{"data":[{"productId":"shared","brand":"Kroger","description":"Seasoning","categories":["Baking"],"items":[{"size":"1 oz","price":{"regular":2}}]}]}`), nil
	})}
	client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(baseClient))
	require.NoError(t, err)

	got, err := (staplesProvider{client: client}).FetchPantry(t.Context(), "70500874")
	require.NoError(t, err)
	require.Len(t, got, 3)
	for i, term := range PantryCategories() {
		assert.ElementsMatch(t, []string{"Baking", term}, got[i].Categories)
	}
	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"spices", "dairy", "international"}, terms)
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(req *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Header:     http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"0"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestProductSearchErrorPayloadPrefersRawBody(t *testing.T) {
	resp := &products.ProductGetResponse{
		Body: []byte(`{"errors":{"code":"PRODUCT-2011","reason":"Field 'locationId' must have a length of 8 alphanumeric characters"}}`),
	}

	payload := productSearchErrorPayload(resp)
	require.NotNil(t, payload)
	assert.Contains(t, krogerError(http.StatusBadRequest, payload).Error(), "PRODUCT-2011")
}

func TestInputIngredientFromKrogerIngredientMapsFields(t *testing.T) {
	regular := float32(4.99)
	sale := float32(3.49)
	categories := []string{"Produce", "Fresh Fruit"}
	ingredient := inputIngredientFromKrogerIngredient(ingredient{
		ProductId:    new(" apple-1 "),
		ItemId:       new(" apple-upc "),
		AisleNumber:  new(" 12 "),
		Brand:        new(" Orchard Co "),
		Description:  new(" Honeycrisp Apple "),
		Size:         new(" 3 lb "),
		PriceRegular: &regular,
		PriceSale:    &sale,
		Categories:   &categories,
	}, 0)

	assert.Equal(t, "apple-1", ingredient.ProductID)
	assert.Equal(t, "apple-upc", ingredient.VariantID)
	assert.Equal(t, "12", ingredient.AisleNumber)
	assert.Equal(t, "Orchard Co", ingredient.Brand)
	assert.Equal(t, "Honeycrisp Apple", ingredient.Description)
	assert.Equal(t, "3 lb", ingredient.Size)
	require.NotNil(t, ingredient.PriceRegular)
	assert.Equal(t, regular, *ingredient.PriceRegular)
	require.NotNil(t, ingredient.PriceSale)
	assert.Equal(t, sale, *ingredient.PriceSale)
	assert.Equal(t, categories, ingredient.Categories)
}

func TestSearchIngredientsSelectsItemByInventory(t *testing.T) {
	item := func(id, stock string, priced bool) products.ProductsProductItemModel {
		value := products.ProductsProductItemModel{ItemId: new(id), Size: new(id + " size")}
		if stock != "" {
			value.Inventory = &products.ProductsProductItemInventoryModel{StockLevel: new(products.ProductsProductItemInventoryModelStockLevel(stock))}
		}
		if priced {
			value.Price = &products.ProductsProductItemPriceModel{Regular: new(float32(4)), Promo: new(float32(3))}
		}
		return value
	}
	for _, tc := range []struct {
		name  string
		items []products.ProductsProductItemModel
		want  string
	}{
		{"high beats low", []products.ProductsProductItemModel{item("low", "LOW", true), item("high", "HIGH", true)}, "high"},
		{"high remains ahead of low", []products.ProductsProductItemModel{item("high", "HIGH", true), item("low", "LOW", true)}, "high"},
		{"low beats unknown", []products.ProductsProductItemModel{item("unknown", "", true), item("low", "LOW", true)}, "low"},
		{"high ties keep first", []products.ProductsProductItemModel{item("first", "HIGH", true), item("second", "HIGH", true)}, "first"},
		{"unknown ties keep first", []products.ProductsProductItemModel{item("first", "", true), item("second", "", true)}, "first"},
		{"unrecognized stock is unknown", []products.ProductsProductItemModel{item("unknown", "FUTURE_VALUE", true), item("low", "LOW", true)}, "low"},
		{"unpriced high is excluded", []products.ProductsProductItemModel{item("high", "HIGH", false), item("low", "LOW", true)}, "low"},
		{"out of stock is excluded", []products.ProductsProductItemModel{item("out", "TEMPORARILY_OUT_OF_STOCK", true), item("unknown", "", true)}, "unknown"},
		{"none eligible", []products.ProductsProductItemModel{item("out", "TEMPORARILY_OUT_OF_STOCK", true), item("unpriced", "HIGH", false)}, ""},
		{"empty items", []products.ProductsProductItemModel{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"data": []any{map[string]any{"productId": "food", "description": "Food", "categories": []string{"Produce"}, "items": tc.items}}})
			require.NoError(t, err)
			client, err := products.NewClientWithResponses(krogerAPIOrigin, products.WithHTTPClient(&http.Client{Transport: cartRoundTrip(func(r *http.Request) (*http.Response, error) {
				assert.Equal(t, "70500874", r.URL.Query().Get("filter.locationId"))
				return cartResponse(http.StatusOK, string(body)), nil
			})}))
			require.NoError(t, err)
			got, err := searchIngredients(t.Context(), client, "70500874", "food", false)
			require.NoError(t, err)
			if tc.want == "" {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			selected := inputIngredientFromKrogerIngredient(got[0], 0)
			assert.Equal(t, "food", selected.ProductID)
			assert.Equal(t, tc.want, selected.VariantID)
			assert.Equal(t, tc.want+" size", selected.Size)
			assert.Equal(t, float32(4), *selected.PriceRegular)
			assert.Equal(t, float32(3), *selected.PriceSale)
		})
	}
}

func TestStaplesKeepsStarchesAndAllowsFrozenShellfishOnly(t *testing.T) {
	var mu sync.Mutex
	var terms []string
	client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		term := req.URL.Query().Get("filter.term")
		mu.Lock()
		terms = append(terms, term)
		mu.Unlock()
		assert.NotContains(t, req.URL.Query(), "filter.brand")
		var body string
		switch term {
		case "Meat & Seafood":
			body = `{"data":[
   {"productId":"italian","brand":"Isernio's","description":"Mild Italian Sausage","categories":["Meat & Seafood"],"items":[{"itemId":"italian-upc","price":{"regular":5}}]},
   {"productId":"salmon","description":"Frozen salmon fillets","categories":["Meat & Seafood"],"items":[{"itemId":"salmon-upc","price":{"regular":8}}]}
  ]}`
		case "shellfish":
			body = `{"data":[{"productId":"shrimp","description":"Frozen raw shrimp","categories":["Frozen"],"items":[{"itemId":"shrimp-upc","price":{"regular":8}}]}]}`
		case "grains":
			body = `{"data":[{"productId":"rice","description":"Brown rice","categories":["Grains"],"items":[{"itemId":"rice-upc","price":{"regular":3}}]}]}`
		case "pasta":
			body = `{"data":[{"productId":"pasta","description":"Spaghetti","categories":["Pasta"],"items":[{"itemId":"pasta-upc","price":{"regular":2}}]}]}`
		default:
			body = `{"data":[]}`
		}
		return jsonResponse(req, http.StatusOK, body), nil
	})}))
	require.NoError(t, err)
	rows, err := (staplesProvider{client: client}).FetchStaples(t.Context(), "70500874")
	require.NoError(t, err)
	var ids []string
	for _, row := range rows {
		ids = append(ids, row.ProductID)
		assert.Equal(t, row.ProductID+"-upc", row.VariantID)
	}
	assert.ElementsMatch(t, []string{"italian", "shrimp", "rice", "pasta"}, ids)
	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"fresh vegetables", "fresh produce", "beef", "chicken", "fish", "pork", "shellfish", "lamb", "grains", "pasta", "Meat & Seafood", "Dairy & Eggs"}, terms)
	assert.Contains(t, terms, "grains")
	assert.Contains(t, terms, "pasta")
}

func TestCatalogFetchesRejectLaterPageFailures(t *testing.T) {
	var page []products.ProductsProductModel
	for range 50 {
		page = append(page, products.ProductsProductModel{ProductId: new("food"), Description: new("Food"), Categories: new([]string{"Produce"}), Items: new([]products.ProductsProductItemModel{{ItemId: new("food-upc"), Price: &products.ProductsProductItemPriceModel{Regular: new(float32(2))}}})})
	}
	body, err := json.Marshal(map[string]any{"data": page})
	require.NoError(t, err)
	for _, tc := range []struct{ name, term string }{{"staples", "fresh vegetables"}, {"pantry", "spices"}, {"wines", "red"}} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				assert.Equal(t, "70500874", req.URL.Query().Get("filter.locationId"))
				if req.URL.Query().Get("filter.term") != tc.term {
					return jsonResponse(req, http.StatusOK, `{"data":[]}`), nil
				}
				if req.URL.Query().Get("filter.start") == "0" {
					return jsonResponse(req, http.StatusOK, string(body)), nil
				}
				assert.Equal(t, "50", req.URL.Query().Get("filter.start"))
				return jsonResponse(req, http.StatusBadRequest, `{"errors":{"reason":"page failed"}}`), nil
			})}))
			require.NoError(t, err)
			provider := staplesProvider{client: client}
			var rows []ai.InputIngredient
			switch tc.name {
			case "staples":
				rows, err = provider.FetchStaples(t.Context(), "70500874")
			case "pantry":
				rows, err = provider.FetchPantry(t.Context(), "70500874")
			case "wines":
				rows, err = provider.FetchWines(t.Context(), "70500874", []string{"red"})
			}
			require.ErrorContains(t, err, "offset 50")
			assert.ErrorContains(t, err, "page failed")
			assert.Empty(t, rows)
		})
	}
}

func TestSearchProductsPagination(t *testing.T) {
	for _, tc := range []struct {
		name    string
		second  string
		status  int
		wantErr string
	}{
		{"success", `{"data":[{"productId":"last"}]}`, http.StatusOK, ""},
		{"later failure", `{"errors":{"reason":"failed"}}`, http.StatusBadRequest, "offset 50"},
		{"missing data", `{}`, http.StatusOK, "missing product data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				assert.NotContains(t, req.URL.Query(), "filter.brand")
				if calls == 1 {
					assert.Equal(t, "0", req.URL.Query().Get("filter.start"))
					body := `{"data":[`
					for i := 0; i < 50; i++ {
						if i > 0 {
							body += ","
						}
						body += fmt.Sprintf(`{"productId":"%d"}`, i)
					}
					return jsonResponse(req, http.StatusOK, body+`]}`), nil
				}
				assert.Equal(t, "50", req.URL.Query().Get("filter.start"))
				return jsonResponse(req, tc.status, tc.second), nil
			})}))
			require.NoError(t, err)
			rows, err := (staplesProvider{client: client}).SearchProducts(t.Context(), "store", "pork")
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, rows)
			} else {
				require.NoError(t, err)
				require.Len(t, rows, 51)
				assert.Equal(t, "last", *rows[50].ProductId)
			}
			assert.Equal(t, 2, calls)
		})
	}
}

func TestFrozenProductDetection(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		categories        []string
		want              bool
	}{
		{"category", "Salmon fillets", []string{"Frozen"}, true},
		{"missing category", "Kroger Wild Caught Cod Fillets Frozen BIG DEAL!", []string{"Meat & Seafood"}, true},
		{"frozen prefix", "Frozen salmon fillets", []string{"Meat & Seafood"}, true},
		{"counter thawed", "Sockeye salmon fillet (Previously Frozen)", []string{"Meat & Seafood"}, false},
		{"never frozen", "Fresh Atlantic salmon (never frozen sustainably sourced)", []string{"Meat & Seafood"}, false},
		{"hyphenated never frozen", "Never-frozen salmon", []string{"Meat & Seafood"}, false},
		{"fresh", "Fresh Pacific cod fillet", []string{"Meat & Seafood"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isFrozenProduct(products.ProductsProductModel{Description: new(tc.description), Categories: &tc.categories}))
		})
	}
}
