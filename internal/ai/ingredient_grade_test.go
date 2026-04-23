package ai

import (
	"strings"
	"testing"

	"careme/internal/kroger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotFromKrogerIngredientNormalizesFields(t *testing.T) {
	snapshot := SnapshotFromKrogerIngredient(kroger.Ingredient{
		ProductId:    strPtr(" 123 "),
		AisleNumber:  strPtr(" A5 "),
		Brand:        strPtr(" Farm Stand "),
		Description:  strPtr("  Baby Spinach  "),
		Size:         strPtr(" 5 oz "),
		PriceRegular: float32Ptr(4.99),
		PriceSale:    float32Ptr(3.49),
		Categories:   &[]string{" greens ", "Produce", "produce", ""},
	})

	assert.Equal(t, "123", snapshot.ProductID)
	assert.Equal(t, "A5", snapshot.AisleNumber)
	assert.Equal(t, "Farm Stand", snapshot.Brand)
	assert.Equal(t, "Baby Spinach", snapshot.Description)
	assert.Equal(t, "5 oz", snapshot.Size)
	assert.Equal(t, "4.99", snapshot.PriceRegular)
	assert.Equal(t, "3.49", snapshot.PriceSale)
	assert.Equal(t, []string{"greens", "Produce"}, snapshot.Categories)
}

func TestIngredientSnapshotHashStableAcrossCategoryOrder(t *testing.T) {
	left := IngredientSnapshot{
		ProductID:   "123",
		Description: "Baby Spinach",
		Categories:  []string{"Produce", "Greens"},
	}
	right := IngredientSnapshot{
		ProductID:   "123",
		Description: "Baby Spinach",
		Categories:  []string{"Greens", "Produce"},
	}

	assert.Equal(t, left.Hash(), right.Hash())
}

func TestBuildIngredientGradePrompt(t *testing.T) {
	snapshot := IngredientSnapshot{
		Description: "Asparagus",
		ProductID:   "foobar",
		Categories:  []string{"Produce"},
	}
	prompt, err := buildIngredientGradePrompt([]IngredientSnapshot{snapshot})
	require.NoError(t, err)
	assert.Contains(t, prompt, "preserving each product_id")
	assert.Contains(t, prompt, `"product_id": "`)
	assert.Contains(t, prompt, "Return JSON only matching the provided schema.")
	assert.Contains(t, prompt, `"description": "Asparagus"`)
	assert.Contains(t, prompt, `"categories": [`)
}

func TestParseIngredientGrades(t *testing.T) {
	items := []IngredientSnapshot{{
		Description: "Asparagus",
		ProductID:   "ingredient-1",
	}}
	grades, err := parseIngredientGrades(`{"grades":[{"product_id":"ingredient-1","score":8,"reason":"Fresh produce with broad weeknight use."}]}`, items)
	require.NoError(t, err)
	require.Len(t, grades, 1)
	grade := grades[0]
	assert.Equal(t, 8, grade.Score)
	assert.Equal(t, "Fresh produce with broad weeknight use.", grade.Reason)
	assert.Equal(t, "Asparagus", grade.Ingredient.Description)
}

func TestParseIngredientGradesRejectsInvalidResponses(t *testing.T) {
	items := []IngredientSnapshot{{ProductID: "ingredient-1"}}
	_, err := parseIngredientGrades(`{"grades":[{"product_id":"ingredient-1","score":11,"reason":"too high"}]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 0 and 10")

	_, err = parseIngredientGrades(`{"grades":[{"product_id":"ingredient-1","score":3,"reason":"   "}]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason is required")
	_, err = parseIngredientGrades(`{"grades":[]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count mismatch")
}

func TestParseIngredientGradesMatchesByIngredientIDInsteadOfOrder(t *testing.T) {
	items := []IngredientSnapshot{
		{Description: "Potato Chips", ProductID: "b"},
		{Description: "Asparagus", ProductID: "a"},
	}

	grades, err := parseIngredientGrades(`{"grades":[{"product_id":"b","score":2,"reason":"Snack food."},{"product_id":"a","score":9,"reason":"Fresh vegetable."}]}`, items)
	require.NoError(t, err)
	require.Len(t, grades, 2)
	assert.Equal(t, "Potato Chips", grades[0].Ingredient.Description)
	assert.Equal(t, 2, grades[0].Score)
	assert.Equal(t, "Asparagus", grades[1].Ingredient.Description)
	assert.Equal(t, 9, grades[1].Score)
}

func TestIngredientGradeSchemaOmitsOperationalFields(t *testing.T) {
	schema := ingredientGradeJSONSchema()
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	_, hasModel := properties["model"]
	_, hasGradedAt := properties["graded_at"]
	assert.False(t, hasModel)
	assert.False(t, hasGradedAt)
}

func TestNormalizeCategoriesRemovesBlanksAndDuplicates(t *testing.T) {
	got := normalizeCategories([]string{" Produce ", "", "greens", "produce", "Greens"})
	assert.Equal(t, []string{"greens", "Produce"}, got)
	assert.True(t, strings.Compare(strings.ToLower(got[0]), strings.ToLower(got[1])) < 0)
}
