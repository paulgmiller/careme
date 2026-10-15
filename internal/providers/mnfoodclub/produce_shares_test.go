package mnfoodclub

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProduceShareIngredients(t *testing.T) {
	ingredients := produceShareIngredients()
	require.Len(t, ingredients, 32)
	byShare := map[string][]string{}
	ids := map[string]bool{}
	for _, ingredient := range ingredients {
		require.GreaterOrEqual(t, len(ingredient.Categories), 2)
		share := ingredient.Categories[1]
		assert.Equal(t, "Produce", ingredient.Categories[0])
		assert.Empty(t, ingredient.AisleNumber)
		assert.Equal(t, "mnfood.club", ingredient.Brand)
		assert.True(t, strings.HasPrefix(ingredient.Description, "Produce share "+strings.ToLower(share)+" "))
		byShare[share] = append(byShare[share], strings.TrimPrefix(ingredient.Description, "Produce share "+strings.ToLower(share)+" "))
		assert.False(t, ids[ingredient.ProductID], "duplicate ID %s", ingredient.ProductID)
		ids[ingredient.ProductID] = true
		assert.True(t, strings.HasPrefix(ingredient.ProductID, LocationIDPrefix))
		assert.Nil(t, ingredient.PriceRegular)
		assert.Nil(t, ingredient.PriceSale)
		require.NotNil(t, ingredient.Grade)
		assert.Equal(t, 10, ingredient.Grade.Score)
		assert.NotEmpty(t, ingredient.Grade.Reason)
	}
	assert.Equal(t, map[string][]string{
		"Seasonal":       {"Rutabaga", "Leeks", "Flat Leaf Parsley", "Watermelon Radish", "Parsnips", "Pie Apples (2nds)"},
		"Small Seasonal": {"Butternut Squash", "Yellow Onions", "Flat Leaf Parsley", "Garlic Chives", "Parsnips", "Table Apples (1sts)"},
		"Low Carb":       {"Kohlrabi", "Asparagus", "Yellow Candy Onions", "Avocado", "Carmen Peppers", "Cauliflower (substitution)"},
		"Staple":         {"Broccoli", "Russet Potatoes", "Red Beets", "Red Onions", "Koginut Squash"},
		"Fruit":          {"Table Apples (1sts)", "Kiwi", "Bosc Pears", "Pineapple", "Red Grapes"},
		"Small Fruit":    {"Table Apples (1sts)", "Kiwi", "Bosc Pears", "Red Grapes"},
	}, byShare)
	assert.Equal(t, []string{"Produce", "Low Carb", "Substitution"}, ingredients[17].Categories)

	// A caller's edits must not affect another fetch's grades or categories.
	ingredients[0].Grade.Score = 0
	ingredients[0].Categories[0] = "Changed"
	fresh := produceShareIngredients()
	assert.Equal(t, 10, fresh[0].Grade.Score)
	assert.Equal(t, "Produce", fresh[0].Categories[0])
}
