package recipes

import (
	"careme/internal/ai"
	"context"
	"log/slog"
	"math/rand"
	"time"

	"github.com/google/uuid"
)

type mock struct{}

var mockRecipes = []ai.Recipe{
	{
		Title:       "Glue Pizza",
		Description: "Sticky sauce trash style",
		Ingredients: []ai.Ingredient{
			{Name: "dough", Quantity: "1 lb", Price: "$5"},
			{Name: "Tomato Sauce", Quantity: "8 oz", Price: "$1"},
			{Name: "Glue", Quantity: "1 oz", Price: "$500"},
			{Name: "Cheese", Quantity: "1/2 lb", Price: "$4"},
		},
		Instructions: []string{
			"roll dough",
			"mix glue and sauce",
			"attach cheese to dough with sticky sauce",
			"bake that sucker",
		},
	},
	{
		Title:       "Grilled Chicken Marinade",
		Description: "Simple marinated grilled chicken",
		Ingredients: []ai.Ingredient{
			{Name: "Chicken Breast", Quantity: "2 lbs", Price: "$8"},
			{Name: "Olive Oil", Quantity: "1/4 cup", Price: "$3"},
			{Name: "Salt", Quantity: "1 tsp", Price: "$1"},
			{Name: "Marinade", Quantity: "1/2 cup", Price: "$4"},
		},
		Instructions: []string{
			"marinade chicken for 2 hours",
			"toss on grill",
			"pull it off before you over cook it",
		},
	},
	{
		Title:       "Spaghetti Carbonara",
		Description: "Classic Italian pasta dish",
		Ingredients: []ai.Ingredient{
			{Name: "Spaghetti", Quantity: "1 lb", Price: "$2"},
			{Name: "Bacon", Quantity: "8 oz", Price: "$6"},
			{Name: "Eggs", Quantity: "4", Price: "$3"},
			{Name: "Parmesan Cheese", Quantity: "1 cup", Price: "$5"},
		},
		Instructions: []string{
			"cook spaghetti",
			"fry bacon until crispy",
			"mix eggs and parmesan",
			"combine all ingredients while pasta is hot",
		},
	},
	{
		Title:       "Beef Tacos",
		Description: "Seasoned ground beef tacos",
		Ingredients: []ai.Ingredient{
			{Name: "Ground Beef", Quantity: "1 lb", Price: "$7"},
			{Name: "Taco Shells", Quantity: "12", Price: "$3"},
			{Name: "Lettuce", Quantity: "1 head", Price: "$2"},
			{Name: "Cheese", Quantity: "8 oz", Price: "$4"},
			{Name: "Taco Seasoning", Quantity: "1 packet", Price: "$1"},
		},
		Instructions: []string{
			"brown ground beef",
			"add taco seasoning and water",
			"warm taco shells",
			"assemble tacos with toppings",
		},
	},
	{
		Title:       "Caesar Salad",
		Description: "Classic Caesar salad with homemade dressing",
		Ingredients: []ai.Ingredient{
			{Name: "Romaine Lettuce", Quantity: "2 heads", Price: "$4"},
			{Name: "Caesar Dressing", Quantity: "1 cup", Price: "$5"},
			{Name: "Croutons", Quantity: "2 cups", Price: "$3"},
			{Name: "Parmesan Cheese", Quantity: "1/2 cup", Price: "$4"},
		},
		Instructions: []string{
			"chop romaine lettuce",
			"toss with caesar dressing",
			"add croutons and parmesan",
			"serve immediately",
		},
	},
	{
		Title:       "Salmon Teriyaki",
		Description: "Pan-seared salmon with teriyaki glaze",
		Ingredients: []ai.Ingredient{
			{Name: "Salmon Fillets", Quantity: "2 lbs", Price: "$15"},
			{Name: "Teriyaki Sauce", Quantity: "1/2 cup", Price: "$4"},
			{Name: "Ginger", Quantity: "1 tbsp", Price: "$2"},
			{Name: "Green Onions", Quantity: "3", Price: "$1"},
		},
		Instructions: []string{
			"season salmon fillets",
			"sear in hot pan",
			"add teriyaki sauce and ginger",
			"garnish with green onions",
		},
	},
	{
		Title:       "Veggie Stir Fry",
		Description: "Colorful vegetable stir fry",
		Ingredients: []ai.Ingredient{
			{Name: "Mixed Vegetables", Quantity: "3 cups", Price: "$6"},
			{Name: "Soy Sauce", Quantity: "1/4 cup", Price: "$2"},
			{Name: "Garlic", Quantity: "3 cloves", Price: "$1"},
			{Name: "Rice", Quantity: "2 cups", Price: "$3"},
		},
		Instructions: []string{
			"cook rice according to package",
			"heat oil in wok",
			"stir fry vegetables with garlic",
			"add soy sauce and serve over rice",
		},
	},
	{
		Title:       "Mushroom Risotto",
		Description: "Creamy Italian rice dish",
		Ingredients: []ai.Ingredient{
			{Name: "Arborio Rice", Quantity: "1.5 cups", Price: "$4"},
			{Name: "Mushrooms", Quantity: "8 oz", Price: "$5"},
			{Name: "Chicken Broth", Quantity: "4 cups", Price: "$3"},
			{Name: "Parmesan Cheese", Quantity: "1 cup", Price: "$5"},
			{Name: "White Wine", Quantity: "1/2 cup", Price: "$8"},
		},
		Instructions: []string{
			"sauté mushrooms",
			"toast rice in butter",
			"add wine and broth gradually",
			"stir in parmesan until creamy",
		},
	},
	{
		Title:       "BBQ Pulled Pork",
		Description: "Slow-cooked pulled pork with BBQ sauce",
		Ingredients: []ai.Ingredient{
			{Name: "Pork Shoulder", Quantity: "3 lbs", Price: "$12"},
			{Name: "BBQ Sauce", Quantity: "2 cups", Price: "$5"},
			{Name: "Buns", Quantity: "8", Price: "$3"},
			{Name: "Coleslaw", Quantity: "1 lb", Price: "$4"},
		},
		Instructions: []string{
			"season pork shoulder",
			"slow cook for 8 hours",
			"shred meat and mix with BBQ sauce",
			"serve on buns with coleslaw",
		},
	},
	{
		Title:       "Greek Salad",
		Description: "Fresh Mediterranean salad",
		Ingredients: []ai.Ingredient{
			{Name: "Tomatoes", Quantity: "4", Price: "$3"},
			{Name: "Cucumber", Quantity: "2", Price: "$2"},
			{Name: "Feta Cheese", Quantity: "8 oz", Price: "$6"},
			{Name: "Olives", Quantity: "1 cup", Price: "$4"},
			{Name: "Olive Oil", Quantity: "1/4 cup", Price: "$3"},
		},
		Instructions: []string{
			"chop tomatoes and cucumber",
			"add feta cheese and olives",
			"dress with olive oil and lemon",
			"season with oregano",
		},
	},
	{
		Title:       "Chicken Curry",
		Description: "Spicy Indian curry with chicken",
		Ingredients: []ai.Ingredient{
			{Name: "Chicken Thighs", Quantity: "2 lbs", Price: "$9"},
			{Name: "Curry Paste", Quantity: "3 tbsp", Price: "$4"},
			{Name: "Coconut Milk", Quantity: "14 oz", Price: "$3"},
			{Name: "Rice", Quantity: "2 cups", Price: "$3"},
		},
		Instructions: []string{
			"brown chicken pieces",
			"add curry paste and cook",
			"pour in coconut milk and simmer",
			"serve over rice",
		},
	},
	{
		Title:       "Margherita Pizza",
		Description: "Classic Italian pizza",
		Ingredients: []ai.Ingredient{
			{Name: "Pizza Dough", Quantity: "1 lb", Price: "$3"},
			{Name: "Tomato Sauce", Quantity: "1 cup", Price: "$2"},
			{Name: "Fresh Mozzarella", Quantity: "8 oz", Price: "$6"},
			{Name: "Fresh Basil", Quantity: "1 bunch", Price: "$2"},
		},
		Instructions: []string{
			"stretch pizza dough",
			"spread tomato sauce",
			"add mozzarella slices",
			"bake and top with fresh basil",
		},
	},
	{
		Title:       "Beef Stew",
		Description: "Hearty slow-cooked beef stew",
		Ingredients: []ai.Ingredient{
			{Name: "Beef Chuck", Quantity: "2 lbs", Price: "$12"},
			{Name: "Potatoes", Quantity: "4", Price: "$3"},
			{Name: "Carrots", Quantity: "4", Price: "$2"},
			{Name: "Beef Broth", Quantity: "4 cups", Price: "$3"},
			{Name: "Onions", Quantity: "2", Price: "$2"},
		},
		Instructions: []string{
			"brown beef chunks",
			"add vegetables and broth",
			"simmer for 3 hours",
			"season and serve hot",
		},
	},
	{
		Title:       "Shrimp Scampi",
		Description: "Garlic butter shrimp over pasta",
		Ingredients: []ai.Ingredient{
			{Name: "Shrimp", Quantity: "1 lb", Price: "$12"},
			{Name: "Linguine", Quantity: "1 lb", Price: "$2"},
			{Name: "Garlic", Quantity: "6 cloves", Price: "$1"},
			{Name: "Butter", Quantity: "1/2 cup", Price: "$3"},
			{Name: "White Wine", Quantity: "1/2 cup", Price: "$8"},
		},
		Instructions: []string{
			"cook linguine",
			"sauté garlic in butter",
			"add shrimp and wine",
			"toss with pasta",
		},
	},
	{
		Title:       "Vegetable Soup",
		Description: "Healthy mixed vegetable soup",
		Ingredients: []ai.Ingredient{
			{Name: "Mixed Vegetables", Quantity: "4 cups", Price: "$6"},
			{Name: "Vegetable Broth", Quantity: "6 cups", Price: "$4"},
			{Name: "Tomatoes", Quantity: "2", Price: "$2"},
			{Name: "Beans", Quantity: "1 can", Price: "$2"},
		},
		Instructions: []string{
			"chop all vegetables",
			"bring broth to boil",
			"add vegetables and simmer",
			"season to taste",
		},
	},
	{
		Title:       "Fish Tacos",
		Description: "Crispy fish tacos with slaw",
		Ingredients: []ai.Ingredient{
			{Name: "White Fish", Quantity: "1.5 lbs", Price: "$10"},
			{Name: "Tortillas", Quantity: "12", Price: "$3"},
			{Name: "Cabbage", Quantity: "1 head", Price: "$2"},
			{Name: "Lime", Quantity: "3", Price: "$1"},
			{Name: "Chipotle Mayo", Quantity: "1/2 cup", Price: "$3"},
		},
		Instructions: []string{
			"bread and fry fish",
			"make cabbage slaw with lime",
			"warm tortillas",
			"assemble with chipotle mayo",
		},
	},
	{
		Title:       "Lasagna",
		Description: "Layered Italian pasta bake",
		Ingredients: []ai.Ingredient{
			{Name: "Lasagna Noodles", Quantity: "1 lb", Price: "$3"},
			{Name: "Ground Beef", Quantity: "1 lb", Price: "$7"},
			{Name: "Ricotta Cheese", Quantity: "15 oz", Price: "$5"},
			{Name: "Mozzarella Cheese", Quantity: "2 cups", Price: "$6"},
			{Name: "Marinara Sauce", Quantity: "4 cups", Price: "$4"},
		},
		Instructions: []string{
			"cook lasagna noodles",
			"brown ground beef with sauce",
			"layer noodles, ricotta, beef sauce, mozzarella",
			"bake until bubbly",
		},
	},
	{
		Title:       "Pad Thai",
		Description: "Thai stir-fried noodles",
		Ingredients: []ai.Ingredient{
			{Name: "Rice Noodles", Quantity: "8 oz", Price: "$4"},
			{Name: "Shrimp or Chicken", Quantity: "1 lb", Price: "$10"},
			{Name: "Peanuts", Quantity: "1/2 cup", Price: "$3"},
			{Name: "Bean Sprouts", Quantity: "2 cups", Price: "$2"},
			{Name: "Pad Thai Sauce", Quantity: "1/2 cup", Price: "$5"},
		},
		Instructions: []string{
			"soak rice noodles",
			"stir fry protein",
			"add noodles and sauce",
			"garnish with peanuts and sprouts",
		},
	},
	{
		Title:       "Chili Con Carne",
		Description: "Spicy beef and bean chili",
		Ingredients: []ai.Ingredient{
			{Name: "Ground Beef", Quantity: "2 lbs", Price: "$14"},
			{Name: "Kidney Beans", Quantity: "2 cans", Price: "$3"},
			{Name: "Tomatoes", Quantity: "28 oz can", Price: "$3"},
			{Name: "Chili Powder", Quantity: "3 tbsp", Price: "$2"},
			{Name: "Onions", Quantity: "2", Price: "$2"},
		},
		Instructions: []string{
			"brown beef with onions",
			"add beans and tomatoes",
			"season with chili powder",
			"simmer for 1 hour",
		},
	},
	{
		Title:       "Chicken Parmesan",
		Description: "Breaded chicken with marinara and cheese",
		Ingredients: []ai.Ingredient{
			{Name: "Chicken Breast", Quantity: "4 pieces", Price: "$8"},
			{Name: "Bread Crumbs", Quantity: "2 cups", Price: "$3"},
			{Name: "Marinara Sauce", Quantity: "2 cups", Price: "$3"},
			{Name: "Mozzarella Cheese", Quantity: "1 cup", Price: "$4"},
			{Name: "Parmesan Cheese", Quantity: "1/2 cup", Price: "$4"},
		},
		Instructions: []string{
			"bread chicken breasts",
			"fry until golden",
			"top with marinara and cheese",
			"bake until cheese melts",
		},
	},
}

func (m mock) GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error) {
	id := p.ConversationID
	if id == "" {
		id = uuid.NewString()
	}
	// fake like we're taking time to call an LLM so we get the spinner.
	time.Sleep(100 * time.Millisecond)

	// Select 3 random recipes from the pool of 20
	// Create a new random generator with current time as seed
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	toGenerate := 3 - len(p.Saved)
	var selectedRecipes []ai.Recipe
	seen := map[string]bool{}
	for _, s := range append(p.Saved, p.Dismissed...) {
		seen[s.ComputeHash()] = true
	}
	indices := rng.Perm(len(mockRecipes)) // just shuffle?
	for _, idx := range indices {
		if toGenerate <= 0 {
			break
		}
		mr := mockRecipes[idx]
		if _, found := seen[mr.ComputeHash()]; !found {

			slog.InfoContext(ctx, "adding", "title", mr.Title)
			selectedRecipes = append(selectedRecipes, mr)
			toGenerate--
		}
	}
	// not presisting dimissed as
	for _, s := range p.Saved {
		slog.InfoContext(ctx, "keeping", "title", s.Title)
		s.Saved = true
		selectedRecipes = append(selectedRecipes, s)
	}

	return &ai.ShoppingList{
		ConversationID: id,
		Recipes:        selectedRecipes,
	}, nil
}
