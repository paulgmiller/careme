package recipes

import (
	"context"
	"html/template"
	"io"
	"net/http"
	"slices"
	"strings"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/recipes/feedback"
	"careme/internal/seasons"
	"careme/internal/templates"
)

type recipeImageView struct {
	HasImage bool
	Hash     string
	Version  string
}

// shoppingRecipeView is a thin wrapper around ai.Recipe for the shopping list page.
//
// We keep ingredient expansion in Go instead of the template because the same derived
// list is used both for card rendering and for the combined shopping list below.
// The remaining extra fields are shopping-list-specific UI state that ai.Recipe
// should not own.
type shoppingRecipeView struct {
	ai.Recipe
	// Hash identifies the individual recipe card and backs recipe-scoped
	// links and HTMX endpoints like /recipe/{hash}/save or /recipe/{hash}/wine.
	Hash string
	// ShoppingListHash identifies the surrounding /recipes?h=... page and is
	// used anywhere the card needs to refer back to the full list state.
	ShoppingListHash   string
	ServerSignedIn     bool
	DisplayIngredients []ai.Ingredient // merged food and wine
	Dismissed          bool            // saved already in recipe
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
func FormatShoppingListHTML(ctx context.Context, p *generatorParams, l ai.ShoppingList, signedIn bool, writer http.ResponseWriter) {
	FormatShoppingListHTMLForHash(ctx, p, l, nil, signedIn, p.Hash(), writer)
}

// FormatShoppingListHTMLForHash renders the multi-recipe shopping list view for a specific hash.
func FormatShoppingListHTMLForHash(ctx context.Context, p *generatorParams, l ai.ShoppingList, wineRecommendations map[string]*ai.WineSelection, signedIn bool, hash string, writer http.ResponseWriter) {
	dismissedHashes := make(map[string]bool, len(p.Dismissed))
	for _, recipe := range p.Dismissed {
		dismissedHashes[recipe.ComputeHash()] = true
	}
	recipeViews := make([]shoppingRecipeView, 0, len(l.Recipes))
	combinedIngredients := make([]ai.Ingredient, 0)
	for _, recipe := range l.Recipes {
		recipeHash := recipe.ComputeHash()
		wineRecommendation := wineRecommendations[recipeHash]
		displayIngredients := ingredientsForDisplay(recipe.Ingredients, wineRecommendation)
		wineActionID, wineButtonID := shoppingWineDOMIDs(recipeHash)
		wineDetailID, wineDetailButtonID := shoppingWineDetailDOMIDs(recipeHash)
		recipeViews = append(recipeViews, shoppingRecipeView{
			Recipe:             recipe,
			Hash:               recipeHash,
			ShoppingListHash:   hash,
			ServerSignedIn:     signedIn,
			DisplayIngredients: displayIngredients,
			Dismissed:          dismissedHashes[recipeHash],
			Wine: shoppingRecipeWineView{
				ActionID:       wineActionID,
				ActionButtonID: wineButtonID,
				PreviewID:      shoppingWinePreviewDOMID(recipeHash),
				DetailID:       wineDetailID,
				DetailButtonID: wineDetailButtonID,
				Preview:        winePreviewPicks(wineRecommendation),
				Recommendation: wineRecommendation,
			},
		})
		combinedIngredients = append(combinedIngredients, displayIngredients...)
	}
	shoppingList := shoppingListForDisplay(combinedIngredients)
	data := struct {
		Location        locations.Location
		Date            string
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Instructions    string
		Hash            string
		Recipes         []shoppingRecipeView
		ShoppingList    []ai.Ingredient
		HasSavedRecipes bool
		ConversationID  string
		Style           seasons.Style
		ServerSignedIn  bool
	}{
		Location:        *p.Location,
		Date:            p.Date.Format("2006-01-02"),
		ClarityScript:   templates.ClarityScript(ctx),
		GoogleTagScript: templates.GoogleTagScript(),
		Instructions:    p.Instructions,
		Hash:            hash,
		Recipes:         recipeViews,
		ShoppingList:    shoppingList,
		HasSavedRecipes: len(p.Saved) > 0,
		ConversationID:  l.ConversationID,
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  signedIn,
	}

	if err := templates.ShoppingList.Execute(writer, data); err != nil {
		http.Error(writer, "shopping list template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatRecipeHTML renders a single recipe view with a browser session id for analytics.
func FormatRecipeHTML(ctx context.Context, p *generatorParams, recipe ai.Recipe, signedIn bool, hasRecipeImage bool, thread []RecipeThreadEntry, fb feedback.Feedback, wineRecommendation *ai.WineSelection, writer http.ResponseWriter) {
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	recipeHash := recipe.ComputeHash()
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
		Feedback           feedback.Feedback
		RecipeHash         string
		RecipeImage        recipeImageView
		Style              seasons.Style
		ServerSignedIn     bool
	}{
		Location:           *p.Location,
		Date:               p.Date.Format("2006-01-02"),
		ClarityScript:      templates.ClarityScript(ctx),
		GoogleTagScript:    templates.GoogleTagScript(),
		Recipe:             recipe,
		DisplayIngredients: ingredientsForDisplay(recipe.Ingredients, wineRecommendation),
		OriginHash:         recipe.OriginHash,
		ConversationID:     p.ConversationID,
		WineRecommendation: wineRecommendation,
		Thread:             thread,
		Feedback:           fb,
		RecipeHash:         recipeHash,
		RecipeImage:        recipeImageData(recipeHash, hasRecipeImage),
		Style:              seasons.GetCurrentStyle(),
		ServerSignedIn:     signedIn,
	}

	if err := templates.Recipe.Execute(writer, data); err != nil {
		http.Error(writer, "recipe template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func recipeImageData(recipeHash string, hasImage bool) recipeImageView {
	return recipeImageView{
		HasImage: hasImage,
		Hash:     recipeHash,
		Version:  ai.RecipeImageSignature(),
	}
}

func FormatRecipeImageActionHTML(recipeHash string, signedIn bool, hasRecipeImage bool, writer http.ResponseWriter) {
	data := struct {
		RecipeHash     string
		RecipeImage    recipeImageView
		ServerSignedIn bool
	}{
		RecipeHash:     recipeHash,
		RecipeImage:    recipeImageData(recipeHash, hasRecipeImage),
		ServerSignedIn: signedIn,
	}

	if err := templates.Recipe.ExecuteTemplate(writer, "recipe_image_action", data); err != nil {
		http.Error(writer, "recipe image action template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func FormatRecipeImageActionResponseHTML(recipeHash string, signedIn bool, hasRecipeImage bool, writer http.ResponseWriter) {
	data := struct {
		RecipeHash     string
		RecipeImage    recipeImageView
		ServerSignedIn bool
	}{
		RecipeHash:     recipeHash,
		RecipeImage:    recipeImageData(recipeHash, hasRecipeImage),
		ServerSignedIn: signedIn,
	}

	if err := templates.Recipe.ExecuteTemplate(writer, "recipe_image_action_response", data); err != nil {
		http.Error(writer, "recipe image response template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatRecipeThreadHTML renders the question thread fragment for HTMX swaps.
func FormatRecipeThreadHTML(thread []RecipeThreadEntry, signedIn bool, conversationID string, writer http.ResponseWriter) {
	// memory waste because we alwways resort?
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

// FormatShoppingRecipeWineHTML renders the signed-in shopping list wine fragment for HTMX swaps.
func FormatShoppingRecipeWineHTML(recipeHash, slot string, selection *ai.WineSelection, writer http.ResponseWriter) {
	wineActionID, wineButtonID := shoppingWineDOMIDs(recipeHash)
	winePreviewID := shoppingWinePreviewDOMID(recipeHash)
	wineDetailID, wineDetailButtonID := shoppingWineDetailDOMIDs(recipeHash)
	data := struct {
		// Hash is used for recipe-scoped DOM IDs and /recipe/{hash}/wine endpoints.
		Hash           string
		ServerSignedIn bool
		Wine           shoppingRecipeWineView
	}{
		Hash:           recipeHash,
		ServerSignedIn: true,
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

func RenderShoppingFinalizeControlsHTML(hash string, hasSavedRecipes bool, writer io.Writer) error {
	data := struct {
		Hash            string
		HasSavedRecipes bool
	}{
		Hash:            hash,
		HasSavedRecipes: hasSavedRecipes,
	}

	return templates.ShoppingList.ExecuteTemplate(writer, "shopping_finalize_controls_response", data)
}

// drops clarity, instructions and most of shoppinglist
func FormatMail(p *generatorParams, l ai.ShoppingList, publicOrigin string, writer io.Writer) error {
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
		Domain:   publicOrigin,
		Style:    seasons.GetCurrentStyle(),
	}

	return templates.Mail.Execute(writer, data)
}

func shoppingListForDisplay(ingredients []ai.Ingredient) []ai.Ingredient {
	if len(ingredients) == 0 {
		return nil
	}
	items := make(map[string]*ai.Ingredient)
	order := make([]string, 0)

	for _, ingredient := range ingredients {
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

	combined := make([]ai.Ingredient, 0, len(order))
	for _, name := range order {
		combined = append(combined, *items[name])
	}
	return combined
}

func ingredientsForDisplay(base []ai.Ingredient, wineRecommendation *ai.WineSelection) []ai.Ingredient {
	display := make([]ai.Ingredient, 0, len(base))
	display = append(display, base...)
	if wineRecommendation == nil || len(wineRecommendation.Wines) == 0 {
		return display
	}
	display = append(display, wineRecommendation.Wines[0]) // Need a way to let the user pick among wines.
	return display
}

// shoppingWineDOMIDs and friends live in Go rather than the template because the IDs
// have to match across two render paths:
// - the full shopping list page render
// - the HTMX wine fragment responses that swap those regions later
//
// Keeping the ID construction here gives us one source of truth for:
// - how recipe hashes are normalized into valid DOM ids
// - which ids belong to the action, preview, and details regions
// - tests that assert HTMX responses target the same elements as the full page
func shoppingWineDOMIDs(hash string) (containerID string, buttonID string) {
	safeHash := shoppingWineSafeHash(hash)
	return "shopping-wine-" + safeHash, "shopping-wine-picker-" + safeHash
}

func shoppingWinePreviewDOMID(hash string) string {
	safeHash := shoppingWineSafeHash(hash)
	return "shopping-wine-preview-" + safeHash
}

func shoppingWineDetailDOMIDs(hash string) (containerID string, buttonID string) {
	safeHash := shoppingWineSafeHash(hash)
	return "shopping-wine-details-" + safeHash, "shopping-wine-details-picker-" + safeHash
}

func shoppingWineSafeHash(hash string) string {
	return strings.TrimRight(strings.TrimSpace(hash), "=")
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
