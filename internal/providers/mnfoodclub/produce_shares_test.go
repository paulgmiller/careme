package mnfoodclub

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProduceShareIngredients(t *testing.T) {
	ingredients := produceShareIngredients()
	require.Len(t, ingredients, 33)
	byShare := map[string][]string{}
	ids := map[string]bool{}
	for _, ingredient := range ingredients {
		require.Len(t, ingredient.Categories, 2) // No substitutions listed for October 19–24.
		share := ingredient.Categories[1]
		assert.Equal(t, "Produce", ingredient.Categories[0])
		assert.Empty(t, ingredient.AisleNumber)
		assert.Equal(t, "mnfood.club", ingredient.Brand)
		assert.True(t, strings.HasPrefix(ingredient.Description, "Produce share "+strings.ToLower(share)+" "))
		byShare[share] = append(byShare[share], strings.TrimPrefix(ingredient.Description, "Produce share "+strings.ToLower(share)+" "))
		assert.False(t, ids[ingredient.ProductID], "duplicate ID %s", ingredient.ProductID)
		ids[ingredient.ProductID] = true
		assert.True(t, strings.HasPrefix(ingredient.ProductID, "mnfoodclub"))
		assert.Nil(t, ingredient.PriceRegular)
		assert.Nil(t, ingredient.PriceSale)
		require.NotNil(t, ingredient.Grade)
		assert.Equal(t, 10, ingredient.Grade.Score)
		assert.NotEmpty(t, ingredient.Grade.Reason)
	}
	assert.Equal(t, map[string][]string{
		"Seasonal":       {"Koginut Squash", "Broccolini", "Carrots", "Daikon Radish", "Kennebec Potatoes", "Red Keeper Onions", "Poblano Peppers"},
		"Small Seasonal": {"Stripetti or Spaghetti Squash", "Poblano Peppers", "Carrots", "Cilantro", "Red Keeper Onions", "Pie Apples (2nds)"},
		"Low Carb":       {"Mini Peppers", "Broccolini", "Zucchini", "Kale", "Red Cabbage"},
		"Staple":         {"Jalapeño Peppers", "Red Potatoes", "Kohlrabi", "Green Cabbage", "Yellow Onions", "Cucumbers"},
		"Fruit":          {"Mandarins", "Asian Pear", "Mango", "Cranberries", "Cantaloupe"},
		"Small Fruit":    {"Mandarins", "Asian Pear", "Mango", "Cantaloupe"},
	}, byShare)

	// A caller's edits must not affect another fetch's grades or categories.
	ingredients[0].Grade.Score = 0
	ingredients[0].Categories[0] = "Changed"
	fresh := produceShareIngredients()
	assert.Equal(t, 10, fresh[0].Grade.Score)
	assert.Equal(t, "Produce", fresh[0].Categories[0])
}
