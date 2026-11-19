package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"careme/internal/ai"
	"careme/internal/providers/kroger/products"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inspectionSearchCall struct {
	location string
	term     string
}

type inspectionProviderFake struct {
	products     []products.ProductsProductModel
	ingredients  []ai.InputIngredient
	searchErr    error
	staplesErr   error
	searches     []inspectionSearchCall
	staplesCalls int
}

func (p *inspectionProviderFake) SearchProducts(_ context.Context, location, term string) ([]products.ProductsProductModel, error) {
	p.searches = append(p.searches, inspectionSearchCall{location: location, term: term})
	return p.products, p.searchErr
}

func (p *inspectionProviderFake) FetchStaples(context.Context, string) ([]ai.InputIngredient, error) {
	p.staplesCalls++
	return p.ingredients, p.staplesErr
}

func TestInspectStaplesPreservesRawVariantsAndCatalogDuplicates(t *testing.T) {
	var raw []products.ProductsProductModel
	require.NoError(t, json.Unmarshal([]byte(`[
  {"productId":"sausage","description":"Mild Italian sausage","items":[{"itemId":"low","size":"1 lb"},{"itemId":"high","size":"2 lb"},{"itemId":"out","size":"3 lb"}]},
  {"productId":"country","description":"Country sausage","items":[{"itemId":"country-upc"}]}
 ]`), &raw))
	selected := ai.InputIngredient{ProductID: "sausage", VariantID: "high", Description: "Mild Italian sausage"}
	provider := &inspectionProviderFake{products: raw, ingredients: []ai.InputIngredient{selected, {ProductID: "unrelated", Description: "Apple"}, selected}}
	report, err := inspectStaples(t.Context(), provider, "70500874", []string{"sausage", "mild italian"})
	require.NoError(t, err)
	assert.Equal(t, "70500874", report.LocationID)
	require.Len(t, report.Searches, 2)
	assert.Equal(t, raw, report.Searches[0].Products)
	assert.Equal(t, raw, report.Searches[1].Products)
	assert.Len(t, *report.Searches[0].Products[0].Items, 3)
	assert.Equal(t, []ai.InputIngredient{selected, selected}, report.Staples)
	assert.Equal(t, []inspectionSearchCall{
		{location: "70500874", term: "sausage"},
		{location: "70500874", term: "mild italian"},
	}, provider.searches)
	assert.Equal(t, 1, provider.staplesCalls)
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	assert.Len(t, fields, 3)
	for _, key := range []string{"locationId", "searches", "matchingStaplesBeforeDeduplication"} {
		assert.Contains(t, fields, key)
	}
}

func TestInspectStaplesPropagatesFailuresWithoutPartialReport(t *testing.T) {
	failure := errors.New("catalog unavailable")
	for _, searchFails := range []bool{true, false} {
		provider := &inspectionProviderFake{products: []products.ProductsProductModel{}, ingredients: []ai.InputIngredient{}}
		if searchFails {
			provider.searchErr = failure
		} else {
			provider.staplesErr = failure
		}
		report, err := inspectStaples(t.Context(), provider, "70500874", []string{"sausage"})
		require.ErrorIs(t, err, failure)
		assert.Empty(t, report)
		if searchFails {
			assert.Zero(t, provider.staplesCalls)
		} else {
			assert.Equal(t, 1, provider.staplesCalls)
			assert.ErrorContains(t, err, "fetch staples for comparison")
		}
	}
}
