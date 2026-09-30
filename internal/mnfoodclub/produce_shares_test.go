package mnfoodclub

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProduceShareIngredients(t *testing.T) {
	ingredients := produceShareIngredients()
	require.Len(t, ingredients, 36)
	byShare := map[string][]string{}
	ids := map[string]bool{}
	for _, ingredient := range ingredients {
		require.GreaterOrEqual(t, len(ingredient.Categories), 2)
		share := ingredient.Categories[1]
		assert.Equal(t, "Produce", ingredient.Categories[0])
		assert.Equal(t, "Produce", ingredient.AisleNumber)
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
		"Seasonal":       {"Garlic", "Carrots", "Yellow Candy Onions", "Anaheim Peppers", "French Breakfast Radish", "Arugula", "Red Cabbage", "Yukon Gold Potatoes", "Cremini Mushrooms"},
		"Small Seasonal": {"Garlic", "Carrots", "Yellow Candy Onions", "Anaheim Peppers", "Koginut Squash", "Arugula", "French Breakfast Radish", "Collard Greens (substitution)"},
		"Low Carb":       {"Celeriac", "Mini Cucumbers", "Heirloom Tomatoes", "Leeks", "Spaghetti Squash"},
		"Staple":         {"Dino Kale", "Gold Beets", "Watermelon Radishes", "Parsnips", "Red Cipollini Onions"},
		"Fruit":          {"Table Apples (1sts)", "Bartlett Pears", "Pomegranate", "Meyer Lemons", "Grapefruit"},
		"Small Fruit":    {"Table Apples (1sts)", "Bartlett Pears", "Pomegranate", "Grapefruit"},
	}, byShare)
	assert.Equal(t, []string{"Produce", "Small Seasonal", "Substitution"}, ingredients[16].Categories)

	// A caller's edits must not affect another fetch's grades or categories.
	ingredients[0].Grade.Score = 0
	ingredients[0].Categories[0] = "Changed"
	fresh := produceShareIngredients()
	assert.Equal(t, 10, fresh[0].Grade.Score)
	assert.Equal(t, "Produce", fresh[0].Categories[0])
}
