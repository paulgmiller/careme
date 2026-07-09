package kroger

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"careme/internal/ai"
	"careme/internal/kroger/products"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityProviderSignature_UsesJSONStaples(t *testing.T) {
	got := NewIdentityProvider().Signature()
	want := mustJSONSignature(defaultStaples())

	assert.Equal(t, want, got)
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

	got, err := searchIngredients(t.Context(), client, "70500874", "pork", []string{"*"}, false, 0)
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

	provider := StaplesProvider{client: client}
	got, err := provider.FetchWines(t.Context(), "70500874", []string{"Pinot Noir", "Sauvignon Blanc"})
	require.NoError(t, err)
	assert.Len(t, got, 2)
	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"Pinot Noir", "Sauvignon Blanc"}, terms)
}

func TestStaplesProvider_FetchStaples_IncludesSupportingCategoriesWithDefaultGrade(t *testing.T) {
	var mu sync.Mutex
	var terms []string
	baseClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		term := req.URL.Query().Get("filter.term")
		mu.Lock()
		terms = append(terms, term)
		mu.Unlock()
		return jsonResponse(req, http.StatusOK, `{"data":[{
			"productId":"`+term+`-1",
			"brand":"Kroger",
			"description":"`+term+` ingredient",
			"categories":["`+term+`"],
			"items":[{"size":"1 ct","price":{"regular":4.99}}]
		}]}`), nil
	})}

	client, err := products.NewClientWithResponses(
		"https://kroger.test",
		products.WithHTTPClient(baseClient),
	)
	require.NoError(t, err)

	provider := StaplesProvider{client: client}
	got, err := provider.FetchStaples(t.Context(), "70500874")
	require.NoError(t, err)

	mu.Lock()
	gotTerms := slices.Clone(terms)
	mu.Unlock()
	assert.Contains(t, gotTerms, "dairy")
	assert.Contains(t, gotTerms, "international")

	byID := map[string]aiInputGrade{}
	for _, ingredient := range got {
		if ingredient.Grade != nil {
			byID[ingredient.ProductID] = aiInputGrade{score: ingredient.Grade.Score, reason: ingredient.Grade.Reason}
		}
	}
	assert.Equal(t, aiInputGrade{score: 5, reason: "supporting ingredient available for recipe search"}, byID["dairy-1"])
	assert.Equal(t, aiInputGrade{score: 5, reason: "supporting ingredient available for recipe search"}, byID["international-1"])
}

type aiInputGrade struct {
	score  int
	reason string
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
	ingredient := inputIngredientFromKrogerIngredient(Ingredient{
		ProductId:    new(" apple-1 "),
		AisleNumber:  new(" 12 "),
		Brand:        new(" Orchard Co "),
		Description:  new(" Honeycrisp Apple "),
		Size:         new(" 3 lb "),
		PriceRegular: &regular,
		PriceSale:    &sale,
		Categories:   &categories,
	}, &ingredientGradeForTest)

	assert.Equal(t, "apple-1", ingredient.ProductID)
	assert.Equal(t, "12", ingredient.AisleNumber)
	assert.Equal(t, "Orchard Co", ingredient.Brand)
	assert.Equal(t, "Honeycrisp Apple", ingredient.Description)
	assert.Equal(t, "3 lb", ingredient.Size)
	require.NotNil(t, ingredient.PriceRegular)
	assert.Equal(t, regular, *ingredient.PriceRegular)
	require.NotNil(t, ingredient.PriceSale)
	assert.Equal(t, sale, *ingredient.PriceSale)
	assert.Equal(t, categories, ingredient.Categories)
	require.NotNil(t, ingredient.Grade)
	assert.Equal(t, 5, ingredient.Grade.Score)
}

var ingredientGradeForTest = ai.IngredientGrade{Score: 5, Reason: "test grade"}
