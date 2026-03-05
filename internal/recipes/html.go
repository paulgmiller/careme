package recipes

import (
	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"html/template"
	"io"
	"net/http"
	"slices"
	"strings"
)

// shoppingRecipeView is a thin wrapper around ai.Recipe for the shopping list page.
//
// We keep ingredient expansion in Go instead of the template because the same derived
// list is used both for card rendering and for the combined shopping list below.
// The remaining extra fields are shopping-list-specific UI state that ai.Recipe
// should not own.
type shoppingRecipeView struct {
	Recipe             ai.Recipe
	Hash               string
	DisplayIngredients []ai.Ingredient
	Dismissed          bool
	Wine               shoppingRecipeWineView
}

// shoppingRecipeWineView holds the template-only state for the shopping list wine UI.
// The shopping card has three independently swappable regions, so the IDs are grouped
// here instead of being spread across the parent recipe view.
type shoppingRecipeWineView struct {
	ActionID       string
	ActionButtonID string
	PreviewID      string
	DetailID       string
	DetailButtonID string
	Preview        []ai.Ingredient
	Recommendation *ai.WineSelection
}

// FormatShoppingListHTML renders the multi-recipe shopping list view.
func FormatShoppingListHTML(p *generatorParams, l ai.ShoppingList, signedIn bool, writer http.ResponseWriter) {
	FormatShoppingListHTMLForHash(p, l, nil, signedIn, p.Hash(), writer)
}

// FormatShoppingListHTMLForHash renders the multi-recipe shopping list view for a specific hash.
func FormatShoppingListHTMLForHash(p *generatorParams, l ai.ShoppingList, wineRecommendations map[string]*ai.WineSelection, signedIn bool, hash string, writer http.ResponseWriter) {
	dismissedHashes := make(map[string]bool, len(p.Dismissed))
	for _, recipe := range p.Dismissed {
		dismissedHashes[recipe.ComputeHash()] = true
	}
	recipeViews := make([]shoppingRecipeView, 0, len(l.Recipes))
	recipesForCombinedList := make([]ai.Recipe, 0, len(l.Recipes))
	for _, recipe := range l.Recipes {
		recipeHash := recipe.ComputeHash()
		wineRecommendation := wineRecommendations[recipeHash]
		displayIngredients := ingredientsForDisplay(recipe.Ingredients, wineRecommendation)
		wineActionID, wineButtonID := shoppingWineDOMIDs(recipeHash)
		winePreviewID := shoppingWinePreviewDOMID(recipeHash)
		wineDetailID, wineDetailButtonID := shoppingWineDetailDOMIDs(recipeHash)
		recipeViews = append(recipeViews, shoppingRecipeView{
			Recipe:             recipe,
			Hash:               recipeHash,
			DisplayIngredients: displayIngredients,
			Dismissed:          dismissedHashes[recipeHash],
			Wine: shoppingRecipeWineView{
				ActionID:       wineActionID,
				ActionButtonID: wineButtonID,
				PreviewID:      winePreviewID,
				DetailID:       wineDetailID,
				DetailButtonID: wineDetailButtonID,
				Preview:        winePreviewPicks(wineRecommendation),
				Recommendation: wineRecommendation,
			},
		})
		recipesForCombinedList = append(recipesForCombinedList, ai.Recipe{Ingredients: displayIngredients})
	}
	data := struct {
		Location        locations.Location
		Date            string
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Instructions    string
		Hash            string
		Recipes         []shoppingRecipeView
		ShoppingList    []ai.Ingredient
		ConversationID  string
		Style           seasons.Style
		ServerSignedIn  bool
	}{
		Location:        *p.Location,
		Date:            p.Date.Format("2006-01-02"),
		ClarityScript:   templates.ClarityScript(),
		GoogleTagScript: templates.GoogleTagScript(),
		Instructions:    p.Instructions,
		Hash:            hash,
		Recipes:         recipeViews,
		ShoppingList:    shoppingListForDisplay(recipesForCombinedList),
		ConversationID:  l.ConversationID,
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  signedIn,
	}

	if err := templates.ShoppingList.Execute(writer, data); err != nil {
		http.Error(writer, "shopping list template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatRecipeHTML renders a single recipe view.
func FormatRecipeHTML(p *generatorParams, recipe ai.Recipe, signedIn bool, thread []RecipeThreadEntry, feedback RecipeFeedback, wineRecommendation *ai.WineSelection, writer http.ResponseWriter) {
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	data := struct {
		Location           locations.Location
		Date               string
		ClarityScript      template.HTML
		GoogleTagScript    template.HTML
		Recipe             ai.Recipe
		DisplayIngredients []ai.Ingredient
		OriginHash         string
		ConversationID     string
		WineRecommendation *ai.WineSelection
		Thread             []RecipeThreadEntry
		Feedback           RecipeFeedback
		RecipeHash         string
		Style              seasons.Style
		ServerSignedIn     bool
	}{
		Location:           *p.Location,
		Date:               p.Date.Format("2006-01-02"),
		ClarityScript:      templates.ClarityScript(),
		GoogleTagScript:    templates.GoogleTagScript(),
		Recipe:             recipe,
		DisplayIngredients: ingredientsForDisplay(recipe.Ingredients, wineRecommendation),
		OriginHash:         recipe.OriginHash,
		ConversationID:     p.ConversationID,
		WineRecommendation: wineRecommendation,
		Thread:             thread,
		Feedback:           feedback,
		RecipeHash:         recipe.ComputeHash(),
		Style:              seasons.GetCurrentStyle(),
		ServerSignedIn:     signedIn,
	}

	if err := templates.Recipe.Execute(writer, data); err != nil {
		http.Error(writer, "recipe template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatRecipeThreadHTML renders the question thread fragment for HTMX swaps.
func FormatRecipeThreadHTML(thread []RecipeThreadEntry, signedIn bool, conversationID string, writer http.ResponseWriter) {
	//memory waste because we alwways resort?
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	data := struct {
		ConversationID string
		Thread         []RecipeThreadEntry
		ServerSignedIn bool
	}{
		ConversationID: conversationID,
		Thread:         thread,
		ServerSignedIn: signedIn,
	}

	if err := templates.Recipe.ExecuteTemplate(writer, "recipe_thread", data); err != nil {
		http.Error(writer, "recipe thread template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatRecipeWineHTML renders the wine recommendation fragment for HTMX swaps.
func FormatRecipeWineHTML(recipeHash string, selection *ai.WineSelection, writer http.ResponseWriter) {
	data := struct {
		RecipeHash         string
		WineRecommendation *ai.WineSelection
	}{
		RecipeHash:         recipeHash,
		WineRecommendation: selection,
	}

	if err := templates.Recipe.ExecuteTemplate(writer, "recipe_wine", data); err != nil {
		http.Error(writer, "recipe wine template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatShoppingRecipeWineHTML renders the shopping list wine recommendation fragment for HTMX swaps.
func FormatShoppingRecipeWineHTML(recipeHash, slot string, selection *ai.WineSelection, writer http.ResponseWriter) {
	wineActionID, wineButtonID := shoppingWineDOMIDs(recipeHash)
	winePreviewID := shoppingWinePreviewDOMID(recipeHash)
	wineDetailID, wineDetailButtonID := shoppingWineDetailDOMIDs(recipeHash)
	data := struct {
		Hash string
		Wine shoppingRecipeWineView
	}{
		Hash: recipeHash,
		Wine: shoppingRecipeWineView{
			ActionID:       wineActionID,
			ActionButtonID: wineButtonID,
			PreviewID:      winePreviewID,
			DetailID:       wineDetailID,
			DetailButtonID: wineDetailButtonID,
			Preview:        winePreviewPicks(selection),
			Recommendation: selection,
		},
	}

	templateName := "shopping_recipe_wine_action_response"
	if strings.EqualFold(strings.TrimSpace(slot), "details") {
		templateName = "shopping_recipe_wine_details_response"
	}

	if err := templates.ShoppingList.ExecuteTemplate(writer, templateName, data); err != nil {
		http.Error(writer, "shopping list wine template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// drops clarity, instructions and most of shoppinglist
func FormatMail(p *generatorParams, l ai.ShoppingList, writer io.Writer) error {
	// TODO just put params into shopping list and pass that up?

	data := struct {
		Location locations.Location
		Date     string
		Hash     string
		Recipes  []ai.Recipe
		Domain   string
		Style    seasons.Style
	}{
		Location: *p.Location,
		Date:     p.Date.Format("2006-01-02"),
		Hash:     p.Hash(),
		Recipes:  l.Recipes,
		Domain:   "https://careme.cooking",
		Style:    seasons.GetCurrentStyle(),
	}

	return templates.Mail.Execute(writer, data)
}

func shoppingListForDisplay(recipes []ai.Recipe) []ai.Ingredient {
	if len(recipes) <= 1 {
		return nil
	}
	items := make(map[string]*ai.Ingredient)
	order := make([]string, 0)

	for _, recipe := range recipes {
		for _, ingredient := range recipe.Ingredients {
			name := strings.ToLower(strings.TrimSpace(ingredient.Name))
			if name == "" {
				continue
			}
			existing, ok := items[name]
			if !ok {
				items[name] = &ai.Ingredient{
					Name:     ingredient.Name,
					Quantity: strings.TrimSpace(ingredient.Quantity),
				}
				order = append(order, name)
				continue
			}
			qty := strings.TrimSpace(ingredient.Quantity)
			if qty == "" {
				continue
			}
			if existing.Quantity == "" {
				existing.Quantity = qty
				continue
			}
			existing.Quantity = existing.Quantity + ", " + qty
		}
	}

	combined := make([]ai.Ingredient, 0, len(order))
	for _, name := range order {
		combined = append(combined, *items[name])
	}
	return combined
}

func ingredientsForDisplay(base []ai.Ingredient, wineRecommendation *ai.WineSelection) []ai.Ingredient {
	display := make([]ai.Ingredient, 0, len(base))
	display = append(display, base...)
	if wineRecommendation == nil {
		return display
	}
	for _, wine := range wineRecommendation.Wines {
		name := strings.TrimSpace(wine.Name)
		if name == "" {
			continue
		}
		display = append(display, ai.Ingredient{
			Name:     name,
			Quantity: strings.TrimSpace(wine.Quantity),
			Price:    strings.TrimSpace(wine.Price),
		})
		break //just geting the first one. Need a way to let user pick.
	}
	return display
}

func shoppingWineDOMIDs(hash string) (containerID string, buttonID string) {
	safeHash := strings.TrimRight(strings.TrimSpace(hash), "=")
	return "shopping-wine-" + safeHash, "shopping-wine-picker-" + safeHash
}

func shoppingWinePreviewDOMID(hash string) string {
	safeHash := strings.TrimRight(strings.TrimSpace(hash), "=")
	return "shopping-wine-preview-" + safeHash
}

func shoppingWineDetailDOMIDs(hash string) (containerID string, buttonID string) {
	safeHash := strings.TrimRight(strings.TrimSpace(hash), "=")
	return "shopping-wine-details-" + safeHash, "shopping-wine-details-picker-" + safeHash
}

func winePreviewPicks(selection *ai.WineSelection) []ai.Ingredient {
	if selection == nil || len(selection.Wines) == 0 {
		return nil
	}
	if len(selection.Wines) <= 2 {
		return selection.Wines
	}
	return selection.Wines[:2]
}
