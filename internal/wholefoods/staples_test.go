package wholefoods

import (
	"context"
	"slices"
	"testing"
)

type stubCategoryClient struct {
	results map[string][]Product
	errs    map[string]error
	calls   []string
}

func (s *stubCategoryClient) Category(_ context.Context, queryterm, store string) (*CategoryResponse, error) {
	s.calls = append(s.calls, store+":"+queryterm)
	if err := s.errs[queryterm]; err != nil {
		return nil, err
	}
	return &CategoryResponse{Results: slices.Clone(s.results[queryterm])}, nil
}

func TestStaplesProvider_MapsProductsToIngredients(t *testing.T) {
	client := &stubCategoryClient{
		results: map[string][]Product{
			"vegetables": {
				{
					Name:         "Organic Asparagus",
					Slug:         "organic-asparagus",
					Brand:        "Whole Foods Market",
					Store:        10216,
					UOM:          "1 lb",
					RegularPrice: 5.99,
					SalePrice:    4.49,
				},
			},
		},
	}
	provider := NewStaplesProvider(client)

	got, err := provider.FetchStaples(t.Context(), "wholefoods_10216")
	if err != nil {
		t.Fatalf("FetchStaples returned error: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected ingredients, got none")
	}

	ingredient := got[0]
	if ingredient.Description == nil || *ingredient.Description != "Organic Asparagus" {
		t.Fatalf("unexpected description: %+v", ingredient.Description)
	}
	if ingredient.Brand == nil || *ingredient.Brand != "Whole Foods Market" {
		t.Fatalf("unexpected brand: %+v", ingredient.Brand)
	}
	if ingredient.Size == nil || *ingredient.Size != "1 lb" {
		t.Fatalf("unexpected size: %+v", ingredient.Size)
	}
	if ingredient.ProductId == nil || *ingredient.ProductId != "10216:organic-asparagus" {
		t.Fatalf("unexpected product id: %+v", ingredient.ProductId)
	}
	if ingredient.PriceRegular == nil || *ingredient.PriceRegular != float32(5.99) {
		t.Fatalf("unexpected regular price: %+v", ingredient.PriceRegular)
	}
	if ingredient.PriceSale == nil || *ingredient.PriceSale != float32(4.49) {
		t.Fatalf("unexpected sale price: %+v", ingredient.PriceSale)
	}
	if len(client.calls) != len(defaultStaples()) {
		t.Fatalf("expected %d category calls, got %d", len(defaultStaples()), len(client.calls))
	}
}

func TestStaplesProvider_InvalidLocationID(t *testing.T) {
	provider := NewStaplesProvider(&stubCategoryClient{})

	_, err := provider.FetchStaples(t.Context(), "10216")
	if err == nil {
		t.Fatal("expected invalid location error")
	}
	if got, want := err.Error(), `invalid whole foods location id "10216"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestStaplesProvider_GetIngredients_UsesSearchTerm(t *testing.T) {
	client := &stubCategoryClient{
		results: map[string][]Product{
			"pinot noir": {
				{Name: "Pinot Noir", Slug: "pinot-noir", Brand: "WFM", Store: 10216},
				{Name: "Rose", Slug: "rose", Brand: "WFM", Store: 10216},
			},
		},
	}
	provider := NewStaplesProvider(client)

	got, err := provider.GetIngredients(t.Context(), "wholefoods_10216", "pinot noir", 1)
	if err != nil {
		t.Fatalf("GetIngredients returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 ingredient after skip, got %d", len(got))
	}
	if got[0].Description == nil || *got[0].Description != "Rose" {
		t.Fatalf("unexpected ingredient description: %+v", got[0].Description)
	}
	if len(client.calls) != 1 || client.calls[0] != "10216:pinot noir" {
		t.Fatalf("unexpected category calls: %v", client.calls)
	}
}
