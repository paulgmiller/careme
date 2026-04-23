package ai

import (
	"strings"
	"testing"
	"time"

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
		PriceRegular: " 4.99 ",
		PriceSale:    " 3.49 ",
		Categories:   []string{" greens ", "Produce", "produce", ""},
	})

	assert.Equal(t, "123", ingredient.ProductID)
	assert.Equal(t, "A5", ingredient.AisleNumber)
	assert.Equal(t, "Farm Stand", ingredient.Brand)
	assert.Equal(t, "Baby Spinach", ingredient.Description)
	assert.Equal(t, "5 oz", ingredient.Size)
	assert.Equal(t, "4.99", ingredient.PriceRegular)
	assert.Equal(t, "3.49", ingredient.PriceSale)
	assert.Equal(t, []string{"greens", "Produce"}, ingredient.Categories)
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

func TestBuildIngredientGradePrompt(t *testing.T) {
	ingredient := NormalizeInputIngredient(InputIngredient{
		Description: "Asparagus",
		ProductID:   "foobar",
		Categories:  []string{"Produce"},
	})
	prompt, err := buildIngredientGradePrompt([]InputIngredient{ingredient})
	require.NoError(t, err)
	assert.Contains(t, prompt, "preserving each product_id")
	assert.Contains(t, prompt, `"product_id": "foobar"`)
	assert.Contains(t, prompt, "Return JSON only matching the provided schema.")
	assert.Contains(t, prompt, `"description": "Asparagus"`)
}

func TestParseIngredientGrades(t *testing.T) {
	items := []InputIngredient{NormalizeInputIngredient(InputIngredient{
		Description: "Asparagus",
		ProductID:   "ingredient-1",
	})}
	gradedAt := time.Date(2026, 4, 23, 12, 0, 0, 0, time.UTC)
	graded, err := parseIngredientGrades(`{"grades":[{"product_id":"`+items[0].ProductID+`","score":8,"reason":"Fresh produce with broad weeknight use."}]}`, items, "gpt-test", gradedAt)
	require.NoError(t, err)
	require.Len(t, graded, 1)
	require.NotNil(t, graded[0].Grade)
	assert.Equal(t, 8, graded[0].Grade.Score)
	assert.Equal(t, "Fresh produce with broad weeknight use.", graded[0].Grade.Reason)
	assert.Equal(t, "gpt-test", graded[0].Grade.Model)
	assert.True(t, gradedAt.Equal(graded[0].Grade.GradedAt))
	assert.Equal(t, "Asparagus", graded[0].Description)
}

func TestParseIngredientGradesRejectsInvalidResponses(t *testing.T) {
	items := []InputIngredient{NormalizeInputIngredient(InputIngredient{ProductID: "ingredient-1"})}
	_, err := parseIngredientGrades(`{"grades":[{"product_id":"`+items[0].ProductID+`","score":11,"reason":"too high"}]}`, items, "", time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 0 and 10")

	_, err = parseIngredientGrades(`{"grades":[{"product_id":"`+items[0].ProductID+`","score":3,"reason":"   "}]}`, items, "", time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason is required")

	_, err = parseIngredientGrades(`{"grades":[]}`, items, "", time.Time{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "count mismatch")
}

func TestParseIngredientGradesMatchesByIDInsteadOfOrder(t *testing.T) {
	items := []InputIngredient{
		NormalizeInputIngredient(InputIngredient{Description: "Potato Chips", ProductID: "b"}),
		NormalizeInputIngredient(InputIngredient{Description: "Asparagus", ProductID: "a"}),
	}

	body := `{"grades":[{"product_id":"` + items[1].ProductID + `","score":9,"reason":"Fresh vegetable."},{"product_id":"` + items[0].ProductID + `","score":2,"reason":"Snack food."}]}`
	graded, err := parseIngredientGrades(body, items, "", time.Time{})
	require.NoError(t, err)
	require.Len(t, graded, 2)
	require.NotNil(t, graded[0].Grade)
	require.NotNil(t, graded[1].Grade)
	assert.Equal(t, "Potato Chips", graded[0].Description)
	assert.Equal(t, 2, graded[0].Grade.Score)
	assert.Equal(t, "Asparagus", graded[1].Description)
	assert.Equal(t, 9, graded[1].Grade.Score)
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
