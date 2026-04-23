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
	prompt, err := buildIngredientGradePrompt(IngredientSnapshot{
		Description: "Asparagus",
		Categories:  []string{"Produce"},
	})
	require.NoError(t, err)
	assert.Contains(t, prompt, "Return JSON only matching the provided schema.")
	assert.Contains(t, prompt, `"description": "Asparagus"`)
	assert.Contains(t, prompt, `"categories": [`)
}

func TestParseIngredientGrade(t *testing.T) {
	grade, err := parseIngredientGrade(`{"score":8,"reason":"Fresh produce with broad weeknight use."}`, IngredientSnapshot{
		Description: "Asparagus",
	})
	require.NoError(t, err)
	assert.Equal(t, ingredientGradeSchemaV1, grade.SchemaVersion)
	assert.Equal(t, 8, grade.Score)
	assert.Equal(t, IngredientDecisionKeep, grade.Decision)
	assert.Equal(t, "Fresh produce with broad weeknight use.", grade.Reason)
	assert.Equal(t, "Asparagus", grade.Ingredient.Description)
}

func TestParseIngredientGradeRejectsInvalidResponses(t *testing.T) {
	_, err := parseIngredientGrade(`{"score":11,"reason":"too high"}`, IngredientSnapshot{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "between 0 and 10")

	_, err = parseIngredientGrade(`{"score":3,"reason":"   "}`, IngredientSnapshot{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason is required")
}

func TestDecisionFromScoreBands(t *testing.T) {
	assert.Equal(t, IngredientDecisionKeep, decisionFromScore(7))
	assert.Equal(t, IngredientDecisionMaybe, decisionFromScore(4))
	assert.Equal(t, IngredientDecisionDrop, decisionFromScore(3))
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
