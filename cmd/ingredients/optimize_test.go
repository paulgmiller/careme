package main

import (
	"testing"

	"careme/internal/ai"
	"careme/internal/providers/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoreSearchSetDeduplicatesProducts(t *testing.T) {
	catalogs := []studyCatalog{
		{Requests: 2, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "apple"}, {ProductID: "chips"}}},
		{Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "pear"}}},
	}
	score := scoreSearchSet(catalogs, map[string]int{"apple": 10, "pear": 9}, []int{0, 1})
	assert.Equal(t, 3, score.Requests)
	assert.Equal(t, 3, score.Products)
	assert.Equal(t, 2, score.UsefulProducts)
	assert.Equal(t, 19, score.GradePoints)
	assert.Equal(t, 0, score.ProduceScore) // Match the app's integer /100 scale.
}

func TestStudyFrontierPrefersCoveragePerAPIRequest(t *testing.T) {
	catalogs := []studyCatalog{
		{Search: kroger.StaplesSearch{Term: "expensive"}, Requests: 6, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "pear"}}},
		{Search: kroger.StaplesSearch{Term: "cheap"}, Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "pear"}}},
		{Search: kroger.StaplesSearch{Term: "new"}, Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "fish"}}},
		{Search: kroger.StaplesSearch{Term: "junk"}, Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "chips"}}},
	}
	report := evaluateStudy("store", catalogs, map[string]int{"apple": 10, "pear": 9, "fish": 8}, 1)
	assert.Equal(t, 6, report.Baseline.Requests)
	assert.Equal(t, 19, report.Baseline.GradePoints)
	require.Len(t, report.Frontier, 2)
	assert.Equal(t, []int{1}, report.Frontier[0].Searches)
	assert.Equal(t, []int{1, 2}, report.Frontier[1].Searches)
	assert.Equal(t, 27, report.Frontier[1].GradePoints)
	assert.Equal(t, 2, report.Frontier[1].Requests)
}

func TestStudyRemovesQueriesMadeRedundant(t *testing.T) {
	catalogs := []studyCatalog{
		{Requests: 1, Ingredients: []ai.InputIngredient{{ProductID: "apple"}}},
		{Requests: 3, Ingredients: []ai.InputIngredient{{ProductID: "apple"}, {ProductID: "pear"}, {ProductID: "fish"}}},
	}
	report := evaluateStudy("store", catalogs, map[string]int{"apple": 10, "pear": 9, "fish": 8}, 1)
	require.Len(t, report.Frontier, 2)
	assert.Equal(t, []int{0}, report.Frontier[0].Searches)
	assert.Equal(t, []int{1}, report.Frontier[1].Searches)
	assert.Equal(t, 3, report.Frontier[1].Requests)
}

func TestStudyBaselineDoesNotInheritChangedDefaultFields(t *testing.T) {
	directory := t.TempDir()
	// Default slot 2 now admits frozen meat; the saved original beef query did not.
	saved := []kroger.StaplesSearch{
		{Term: "fresh vegatable", Brands: []string{"*"}},
		{Term: "fresh produce", Brands: []string{"*"}},
		{Term: "beef", Brands: []string{"Kroger"}},
	}
	require.NoError(t, writeStudyJSON(directory+"/baseline-70500874.json", saved))
	baseline, err := studyBaseline(directory, "70500874")
	require.NoError(t, err)
	assert.Equal(t, saved, baseline)
}
