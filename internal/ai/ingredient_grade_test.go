package ai

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeInputIngredientNormalizesFieldsAndSetsID(t *testing.T) {
	ingredient := NormalizeInputIngredient(InputIngredient{
		ProductID:    " 123 ",
		AisleNumber:  " A5 ",
		Brand:        " Farm Stand ",
		Description:  "  Baby Spinach  ",
		Size:         " 5 oz ",
		PriceRegular: float32Ptr(4.99),
		PriceSale:    float32Ptr(3.49),
		Categories:   []string{" greens ", "Produce", "produce", ""},
	})

	assert.Equal(t, "123", ingredient.ProductID)
	assert.Equal(t, "A5", ingredient.AisleNumber)
	assert.Equal(t, "Farm Stand", ingredient.Brand)
	assert.Equal(t, "Baby Spinach", ingredient.Description)
	assert.Equal(t, "5 oz", ingredient.Size)
	require.NotNil(t, ingredient.PriceRegular)
	require.NotNil(t, ingredient.PriceSale)
	assert.Equal(t, float32(4.99), *ingredient.PriceRegular)
	assert.Equal(t, float32(3.49), *ingredient.PriceSale)
	assert.Equal(t, []string{"greens", "Produce"}, ingredient.Categories)
}

func float32Ptr(v float32) *float32 {
	return &v
}

func TestInputIngredientHashStableAcrossCategoryOrder(t *testing.T) {
	left := NormalizeInputIngredient(InputIngredient{
		ProductID:   "123",
		Description: "Baby Spinach",
		Categories:  []string{"Produce", "Greens"},
	})
	right := NormalizeInputIngredient(InputIngredient{
		ProductID:   "123",
		Description: "Baby Spinach",
		Categories:  []string{"Greens", "Produce"},
	})

	assert.Equal(t, left.Hash(), right.Hash())
}

func TestIngredientGradeCacheVersionChangesWhenPromptOrModelChanges(t *testing.T) {
	base := ingredientGradeCacheVersion("gpt-5-mini", "prompt a", ingredientGradeSchemaV1)
	same := ingredientGradeCacheVersion(" gpt-5-mini ", "prompt a", ingredientGradeSchemaV1)
	differentModel := ingredientGradeCacheVersion("gpt-5-nano", "prompt a", ingredientGradeSchemaV1)
	differentPrompt := ingredientGradeCacheVersion("gpt-5-mini", "prompt b", ingredientGradeSchemaV1)

	assert.Equal(t, base, same)
	assert.NotEqual(t, base, differentModel)
	assert.NotEqual(t, base, differentPrompt)
}

func TestBuildIngredientGradePrompt(t *testing.T) {
	ingredient := NormalizeInputIngredient(InputIngredient{
		Description: "Asparagus",
		ProductID:   "foobar",
		Categories:  []string{"Produce"},
	})
	prompt, err := buildIngredientGradePrompt([]InputIngredient{ingredient})
	require.NoError(t, err)
	assert.Contains(t, prompt, "preserving each id")
	assert.Contains(t, prompt, `"id": "foobar"`)
	assert.Contains(t, prompt, "Return JSON only matching the provided schema.")
	assert.Contains(t, prompt, `"description": "Asparagus"`)
}

func TestParseIngredientGrades(t *testing.T) {
	items := []InputIngredient{NormalizeInputIngredient(InputIngredient{
		Description: "Asparagus",
		ProductID:   "ingredient-1",
	})}
	graded, err := parseIngredientGrades(`{"grades":[{"id":"`+items[0].ProductID+`","score":8,"reason":"Fresh produce with broad weeknight use."}]}`, items)
	require.NoError(t, err)
	require.Len(t, graded, 1)
	require.NotNil(t, graded[0].Grade)
	assert.Equal(t, 8, graded[0].Grade.Score)
	assert.Equal(t, "Fresh produce with broad weeknight use.", graded[0].Grade.Reason)
	assert.Equal(t, "Asparagus", graded[0].Description)
}

func TestParseIngredientGradesRejectsInvalidResponses(t *testing.T) {
	items := []InputIngredient{NormalizeInputIngredient(InputIngredient{ProductID: "ingredient-1"})}
	_, err := parseIngredientGrades(`{"grades":[{"id":"`+items[0].ProductID+`","score":11,"reason":"too high"}]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 0 and 10")

	_, err = parseIngredientGrades(`{"grades":[{"id":"`+items[0].ProductID+`","score":3,"reason":"   "}]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason is required")

	_, err = parseIngredientGrades(`{"grades":[]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count mismatch")
}

func TestParseIngredientGradesMatchesByIDInsteadOfOrder(t *testing.T) {
	items := []InputIngredient{
		NormalizeInputIngredient(InputIngredient{Description: "Potato Chips", ProductID: "b"}),
		NormalizeInputIngredient(InputIngredient{Description: "Asparagus", ProductID: "a"}),
	}

	body := `{"grades":[{"id":"` + items[1].ProductID + `","score":9,"reason":"Fresh vegetable."},{"id":"` + items[0].ProductID + `","score":2,"reason":"Snack food."}]}`
	graded, err := parseIngredientGrades(body, items)
	require.NoError(t, err)
	require.Len(t, graded, 2)

	byID := make(map[string]InputIngredient, len(graded))
	for _, ingredient := range graded {
		require.NotNil(t, ingredient.Grade)
		byID[ingredient.ProductID] = ingredient
	}

	require.Contains(t, byID, "b")
	assert.Equal(t, "Potato Chips", byID["b"].Description)
	assert.Equal(t, 2, byID["b"].Grade.Score)

	require.Contains(t, byID, "a")
	assert.Equal(t, "Asparagus", byID["a"].Description)
	assert.Equal(t, 9, byID["a"].Grade.Score)
}

func TestParseIngredientGradesRejectsDuplicateInputProductIDs(t *testing.T) {
	items := []InputIngredient{
		NormalizeInputIngredient(InputIngredient{Description: "Asparagus", ProductID: "ingredient-1"}),
		NormalizeInputIngredient(InputIngredient{Description: "Broccoli", ProductID: "ingredient-1"}),
	}

	_, err := parseIngredientGrades(`{"grades":[{"id":"ingredient-1","score":8,"reason":"Fresh vegetable."},{"id":"ingredient-1","score":7,"reason":"Another vegetable."}]}`, items)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicated input product_id")
}

func TestIngredientGradeSchemaOmitsOperationalFields(t *testing.T) {
	schema := ingredientGradeJSONSchema()
	properties, ok := schema["properties"].(map[string]any)
	require.True(t, ok)

	_, hasSchemaVersion := properties["schema_version"]
	assert.False(t, hasSchemaVersion)
}

func TestNormalizeCategoriesRemovesBlanksAndDuplicates(t *testing.T) {
	got := normalizeCategories([]string{" Produce ", "", "greens", "produce", "Greens"})
	assert.Equal(t, []string{"greens", "Produce"}, got)
	assert.True(t, strings.Compare(strings.ToLower(got[0]), strings.ToLower(got[1])) < 0)
}
