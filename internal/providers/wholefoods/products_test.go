package wholefoods

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProductSearch_BuildsRequestAndDecodesResponse(t *testing.T) {
	t.Parallel()

	fixture := []byte(`{
		"mainResultSet": {
			"searchResults": [
				{
					"asin": "B01N008341",
					"injectionSource": "keywords,phrasedoc,knn,behavioral",
					"isAdultProduct": false,
					"productGroup": "alcoholic_beverage_display_on_website",
					"amazonsChoiceExactLabel": true
				},
				{
					"asin": "B06XH151S6",
					"injectionSource": "keywords,phrasedoc,knn,behavioral",
					"isAdultProduct": false,
					"productGroup": "alcoholic_beverage_display_on_website",
					"amazonsChoiceExactLabel": true
				}
			],
			"approximateTotalResultCount": 59,
			"availableTotalResultCount": 59,
			"totalResultCountPreVE": 71,
			"keywords": "merlot",
			"augmentModifications": [
				{
					"action": "add",
					"type": "qlb-relevance-lookup",
					"source": "A9",
					"metadata": {
						"add-segment": "QSModifyTransformer:qlb-relevance-lookup"
					}
				}
			]
		}
	}`)

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	resp, err := client.productSearch(context.Background(), productSearchRequest{
		Text:                      " merlot ",
		OfferListingDiscriminator: " A04C ",
		Categories:                []string{"18473610011"},
	})
	if err != nil {
		t.Fatalf("ProductSearch returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/api/wwos/rsi/search" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("text"); got != "merlot" {
		t.Fatalf("unexpected text query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("old"); got != "A04C" {
		t.Fatalf("unexpected old query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("offset"); got != "0" {
		t.Fatalf("unexpected offset query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("size"); got != "30" {
		t.Fatalf("unexpected size query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("sort"); got != "relevanceblender" {
		t.Fatalf("unexpected sort query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("programType"); got != "GROCERY" {
		t.Fatalf("unexpected programType query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("filters"); got != "" {
		t.Fatalf("unexpected filters query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("categories"); got != "18473610011" {
		t.Fatalf("unexpected categories query value: %q", got)
	}

	if got := resp.MainResultSet.Keywords; got != "merlot" {
		t.Fatalf("unexpected keywords: %q", got)
	}
	if got := resp.MainResultSet.ApproximateTotalResultCount; got != 59 {
		t.Fatalf("unexpected approximate total: %d", got)
	}
	if got := len(resp.MainResultSet.SearchResults); got != 2 {
		t.Fatalf("unexpected result count: %d", got)
	}
	if got := resp.MainResultSet.SearchResults[0].ASIN; got != "B01N008341" {
		t.Fatalf("unexpected first asin: %q", got)
	}
	if got := resp.MainResultSet.AugmentModifications[0].Metadata["add-segment"]; got != "QSModifyTransformer:qlb-relevance-lookup" {
		t.Fatalf("unexpected augment metadata: %q", got)
	}
}

func TestProductSearch_RequiresTextAndOfferListingDiscriminator(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)

	_, err := client.productSearch(context.Background(), productSearchRequest{
		OfferListingDiscriminator: "A04C",
	})
	if err == nil || !strings.Contains(err.Error(), "text is required") {
		t.Fatalf("unexpected text error: %v", err)
	}

	_, err = client.productSearch(context.Background(), productSearchRequest{
		Text: "merlot",
	})
	if err == nil || !strings.Contains(err.Error(), "offer listing discriminator is required") {
		t.Fatalf("unexpected discriminator error: %v", err)
	}
}

func TestProductHydration_BuildsRequestAndDecodesResponse(t *testing.T) {
	t.Parallel()

	fixture := []byte(`[
		{
			"brandName": "Justin",
			"name": "Justin Cabernet Sauvignon 2013, 750Ml",
			"asin": "B06WVGV73Z",
			"programType": "GROCERY",
			"description": "With aromas of black fruit and spice.",
			"about": [
				"Cabernet Sauvignon, Paso Robles, California"
			],
			"productImages": [
				"https://m.media-amazon.com/images/I/41+VJwn0AeL.jpg"
			],
			"availability": "IN_STOCK",
			"pdpType": "STANDARD",
			"offerDetails": {
				"price": {
					"currencyCode": "USD",
					"priceAmount": 31.99,
					"basisPriceAmount": null,
					"savings": {
						"currencyCode": null,
						"savingsAmount": null,
						"percentSavings": null
					},
					"primeBenefit": {
						"isApplied": null,
						"text": null,
						"currencyCode": null,
						"priceAmount": null,
						"savingsAmount": null
					}
				},
				"offerListingId": "listing-1",
				"maxOrderQuantity": 33,
				"isMaxQuantityRestricted": true
			},
			"variableUnitOfMeasure": {
				"pricingUom": {
					"dimension": "COUNT",
					"unit": "UNITS"
				},
				"sellingUom": {
					"dimension": "COUNT",
					"unit": "UNITS"
				},
				"selectorItemList": [
					{
						"selectorPrice": {
							"baseUnit": null,
							"currencyCode": "USD",
							"priceAmount": 31.99
						},
						"selectorSellingQuantityString": "1",
						"selectorSellingQuantityValue": 1
					}
				]
			},
			"ctaTag": "UNRECOGNIZED_SIGN_IN",
			"deliveryPromiseHtml": "<div>$9.95 delivery</div>",
			"dietTypes": [],
			"category": {
				"productType": "WINE",
				"glProductGroupSymbol": "gl_wine",
				"displayName": "Alcoholic Beverage"
			}
		},
		{
			"brandName": "OREGON TRAILS WINE COMPANY",
			"name": "Willamette Valley Pinot Noir",
			"asin": "B07G4TKBFP",
			"programType": "GROCERY",
			"description": "",
			"about": [],
			"productImages": [
				"https://m.media-amazon.com/images/I/71BBLB8KFBL.jpg"
			],
			"availability": "IN_STOCK",
			"pdpType": "STANDARD",
			"offerDetails": null,
			"variableUnitOfMeasure": null,
			"dietTypes": [],
			"category": {
				"productType": "WINE",
				"glProductGroupSymbol": "gl_wine",
				"displayName": "Alcoholic Beverage"
			}
		}
	]`)

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	resp, err := client.productHydration(context.Background(), productHydrationRequest{
		OfferListingDiscriminator: " A04C ",
		ASINs:                     []string{"B06WVGV73Z", " B07G4TKBFP "},
	})
	if err != nil {
		t.Fatalf("ProductHydration returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/api/wwos/products" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("offerListingDiscriminator"); got != "A04C" {
		t.Fatalf("unexpected discriminator query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("programType"); got != "GROCERY" {
		t.Fatalf("unexpected programType query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("asins"); got != "B06WVGV73Z,B07G4TKBFP" {
		t.Fatalf("unexpected asins query value: %q", got)
	}

	if got := len(resp); got != 2 {
		t.Fatalf("unexpected response count: %d", got)
	}
	if got := resp[0].ASIN; got != "B06WVGV73Z" {
		t.Fatalf("unexpected first asin: %q", got)
	}
	if resp[0].OfferDetails == nil {
		t.Fatal("expected offer details to be decoded")
	}
	if got := resp[0].OfferDetails.Price.PriceAmount; got != 31.99 {
		t.Fatalf("unexpected first price amount: %v", got)
	}
	if resp[0].VariableUnitOfMeasure == nil {
		t.Fatal("expected variable unit of measure to be decoded")
	}
	if got := resp[0].VariableUnitOfMeasure.SelectorItemList[0].SelectorSellingQuantityValue; got != 1 {
		t.Fatalf("unexpected selector quantity: %d", got)
	}
	if got := resp[1].Category.GLProductGroupSymbol; got != "gl_wine" {
		t.Fatalf("unexpected category symbol: %q", got)
	}
	if resp[1].OfferDetails != nil {
		t.Fatal("expected nil offer details for second product")
	}
}

func TestProductHydration_RequiresOfferListingDiscriminatorAndASINs(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)

	_, err := client.productHydration(context.Background(), productHydrationRequest{
		ASINs: []string{"B06WVGV73Z"},
	})
	if err == nil || !strings.Contains(err.Error(), "offer listing discriminator is required") {
		t.Fatalf("unexpected discriminator error: %v", err)
	}

	_, err = client.productHydration(context.Background(), productHydrationRequest{
		OfferListingDiscriminator: "A04C",
	})
	if err == nil || !strings.Contains(err.Error(), "at least one ASIN is required") {
		t.Fatalf("unexpected asin error: %v", err)
	}
}
