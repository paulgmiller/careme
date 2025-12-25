package recipes

import (
	"careme/internal/ai"
	"context"

	"github.com/google/uuid"
)

type mock struct{}

func (_ mock) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	id := p.ConversationID
	if id == "" {
		id = uuid.NewString()
	}

	return &ai.ShoppingList{
		ConversationID: id,
		Recipes: []ai.Recipe{
			{
				Title:       "Glue Pizza",
				Description: "Sticky sauce trash style",
				Ingredients: []ai.Ingredient{
					{
						Name:     "dough",
						Quantity: "1 lb",
						Price:    "$5",
					},
					{
						Name:     "Tomato Sauce",
						Quantity: "8 oz",
						Price:    "$1",
					},
					{
						Name:     "Glue",
						Quantity: "1 oz",
						Price:    "$500",
					},
					{
						Name:     "Glue",
						Quantity: "1 oz",
						Price:    "$500",
					},
					{
						Name:     "Cheese",
						Quantity: "1/2 lb",
						Price:    "$500",
					},
				},
				Instructions: []string{
					"roll dough",
					"mix glue and sauce",
					"attach cheese to dough with sticky sauce",
					"bake that sucker",
				},
			},
			{
				Title:       "Glue Pizza",
				Description: "Sticky sauce trash style",
				Ingredients: []ai.Ingredient{
					{
						Name:     "dough",
						Quantity: "1 lb",
						Price:    "$5",
					},
					{
						Name:     "Tomato Sauce",
						Quantity: "8 oz",
						Price:    "$1",
					},
					{
						Name:     "Salt",
						Quantity: "1 oz",
						Price:    "$500",
					},
					{
						Name:     "Marinade",
						Quantity: "1/2 lb",
						Price:    "$500",
					},
				},
				Instructions: []string{
					"marinade",
					"toss on grill",
					"pull it off before you over cook it",
				},
			},
		},
	}, nil
}
