package recipes

import (
	"cmp"
	"context"
	"html/template"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/recipes/feedback"
	"careme/internal/seasons"
	"careme/internal/templates"

	"github.com/samber/lo"
)

type recipeImageView struct {
	HasImage bool
	Hash     string
	// OutOfBand lets the shared panel template opt into the HTMX outerHTML swap
	// used by the image-generation response without duplicating the panel markup.
	OutOfBand bool
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
	WineRecommendation *ai.WineSelection
}

// FormatShoppingListHTMLForHash renders the multi-recipe shopping list view for a specific hash.
// should shove wine recs into recipe instead of having them seperate.
func FormatShoppingListHTMLForHash(ctx context.Context, p *generatorParams, l ai.ShoppingList,
	wineRecommendations map[string]*ai.WineSelection, signedIn bool, hash string, inputs []ai.InputIngredient, writer http.ResponseWriter,
) {
	dismissedHashes := lo.SliceToMap(p.Dismissed, func(r ai.Recipe) (string, bool) {
		return r.ComputeHash(), true
	})
	recipeViews := make([]shoppingRecipeView, 0, len(l.Recipes))
	combinedIngredients := make([]ai.Ingredient, 0)
	hasSavedRecipes := false
	for _, recipe := range l.Recipes {
		recipeHash := recipe.ComputeHash()
		wineRecommendation := wineRecommendations[recipeHash]
		displayIngredients := ingredientsForDisplay(recipe.Ingredients, wineRecommendation)
		recipeViews = append(recipeViews, shoppingRecipeView{
			Recipe:             recipe,
			Hash:               recipeHash,
			ShoppingListHash:   hash,
			ServerSignedIn:     signedIn,
			DisplayIngredients: displayIngredients,
			Dismissed:          dismissedHashes[recipeHash],
			WineRecommendation: wineRecommendation,
		})
		if recipe.Saved {
			hasSavedRecipes = true
			combinedIngredients = append(combinedIngredients, displayIngredients...)
		}
	}
	shoppingList := shoppingListForDisplay(combinedIngredients, inputs)
	data := struct {
		Location        locations.Location
		Date            string
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Instructions    string
		Hash            string
		Recipes         []shoppingRecipeView
		ShoppingList    []*ai.Ingredient
		HasSavedRecipes bool
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
		HasSavedRecipes: hasSavedRecipes,
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  signedIn,
	}

	setTextContent(writer)
	if err := templates.ShoppingList.Execute(writer, data); err != nil {
		http.Error(writer, "shopping list template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// FormatRecipeHTML renders a single recipe view with a browser session id for analytics.
func FormatRecipeHTML(ctx context.Context, p *generatorParams, recipe ai.Recipe, signedIn bool,
	critiqueScore *int, hasRecipeImage bool, thread []RecipeThreadEntry,
	fb feedback.Feedback, wineRecommendation *ai.WineSelection, writer http.ResponseWriter,
) {
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	recipeHash := recipe.ComputeHash()
	activeResponseID := recipe.ResponseID
	if threadResponseID := latestThreadResponseID(thread); threadResponseID != "" {
		activeResponseID = threadResponseID
	}
	data := struct {
		Location            locations.Location
		Date                string
		ClarityScript       template.HTML
		GoogleTagScript     template.HTML
		Recipe              ai.Recipe
		DisplayIngredients  []ai.Ingredient
		OriginHash          string
		ResponseID          string
		WineRecommendation  *ai.WineSelection
		Thread              []RecipeThreadEntry
		Feedback            feedback.Feedback
		RecipeHash          string
		RecipeImage         recipeImageView
		Style               seasons.Style
		ServerSignedIn      bool
		RecipeCritiqueURL   string
		RecipeCritiqueScore *int
	}{
		Location:            *p.Location,
		Date:                p.Date.Format("2006-01-02"),
		ClarityScript:       templates.ClarityScript(ctx),
		GoogleTagScript:     templates.GoogleTagScript(),
		Recipe:              recipe,
		DisplayIngredients:  ingredientsForDisplay(recipe.Ingredients, wineRecommendation),
		OriginHash:          recipe.OriginHash,
		ResponseID:          activeResponseID,
		WineRecommendation:  wineRecommendation,
		Thread:              thread,
		Feedback:            fb,
		RecipeHash:          recipeHash,
		RecipeImage:         recipeImageData(recipeHash, hasRecipeImage, false),
		Style:               seasons.GetCurrentStyle(),
		ServerSignedIn:      signedIn,
		RecipeCritiqueURL:   "/critiques/" + recipeHash,
		RecipeCritiqueScore: critiqueScore,
	}

	setTextContent(writer)
	if err := templates.Recipe.Execute(writer, data); err != nil {
		http.Error(writer, "recipe template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func recipeImageData(recipeHash string, hasImage bool, outOfBand bool) recipeImageView {
	return recipeImageView{
		HasImage:  hasImage,
		Hash:      recipeHash,
		OutOfBand: outOfBand,
	}
}

// FormatRecipeThreadHTML renders the question thread fragment for HTMX swaps.
func FormatRecipeThreadHTML(thread []RecipeThreadEntry, signedIn bool, responseID string, writer http.ResponseWriter) {
	// memory waste because we alwways resort?
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	data := struct {
		ResponseID     string
		Thread         []RecipeThreadEntry
		ServerSignedIn bool
	}{
		ResponseID:     responseID,
		Thread:         thread,
		ServerSignedIn: signedIn,
	}

	setTextContent(writer)
	if err := templates.Recipe.ExecuteTemplate(writer, "recipe_thread", data); err != nil {
		http.Error(writer, "recipe thread template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func RenderShoppingFinalizeControlsHTML(hash string, writer io.Writer) error {
	data := struct {
		Hash            string
		HasSavedRecipes bool
	}{
		Hash:            hash,
		HasSavedRecipes: true,
	}

	return templates.ShoppingList.ExecuteTemplate(writer, "shopping_finalize_controls_response", data)
}

// called from shoppping list and will either mimimize dimissed or bring back in all on undo.
func RenderShoppingRecipeCardHTML(recipe ai.Recipe, shoppingListHash string, wineRecommendation *ai.WineSelection, writer io.Writer) error {
	data := shoppingRecipeView{
		Recipe:             recipe,
		Hash:               recipe.ComputeHash(),
		ShoppingListHash:   shoppingListHash,
		ServerSignedIn:     true, // have to be signed in to toggle
		DisplayIngredients: ingredientsForDisplay(recipe.Ingredients, wineRecommendation),
		Dismissed:          !recipe.Saved, // no inbetween state left.
		WineRecommendation: wineRecommendation,
	}
	return templates.ShoppingList.ExecuteTemplate(writer, "shopping_recipe_card", data)
}

// called from single recipe page just swaps save dimiss
func RenderRecipeSaveActionHTML(recipe ai.Recipe, originHash string, writer io.Writer) error {
	data := struct {
		Recipe         ai.Recipe
		OriginHash     string
		RecipeHash     string
		ServerSignedIn bool
	}{
		Recipe:         recipe,
		OriginHash:     originHash,
		RecipeHash:     recipe.ComputeHash(),
		ServerSignedIn: true,
	}
	return templates.Recipe.ExecuteTemplate(writer, "recipe_save_action", data)
}

func latestThreadResponseID(thread []RecipeThreadEntry) string {
	if len(thread) == 0 {
		return ""
	}
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	for _, entry := range thread {
		if responseID := strings.TrimSpace(entry.ResponseID); responseID != "" {
			return responseID
		}
	}
	return ""
}

// drops clarity, instructions and most of shoppinglist
func FormatMail(p *generatorParams, l ai.ShoppingList, publicOrigin string, unsubscribeURL string, writer io.Writer) error {
	data := struct {
		Location       locations.Location
		Date           string
		Hash           string
		Recipes        []ai.Recipe
		Domain         string
		UnsubscribeURL string
		Style          seasons.Style
	}{
		Location:       *p.Location,
		Date:           p.Date.Format("2006-01-02"),
		Hash:           p.Hash(),
		Recipes:        l.Recipes,
		Domain:         publicOrigin,
		UnsubscribeURL: unsubscribeURL,
		Style:          seasons.GetCurrentStyle(),
	}

	return templates.Mail.Execute(writer, data)
}

func shoppingListForDisplay(ingredients []ai.Ingredient, inputs []ai.InputIngredient) []*ai.Ingredient {
	items := make(map[string]*ai.Ingredient)
	var combined []*ai.Ingredient // maintain original ordering after deduping

	for _, ingredient := range ingredients {
		name := normalizeShoppingListName(ingredient.Name)
		if name == "" {
			continue
		}
		existing, ok := items[name]
		if !ok {
			item := &ai.Ingredient{
				Name:     ingredient.Name, // show non normalized
				Quantity: strings.TrimSpace(ingredient.Quantity),
			}
			items[name] = item
			combined = append(combined, item)

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

	// kind of big to do each time but we're fast right?
	// better to do product here
	aisles := lo.SliceToMap(inputs, func(ing ai.InputIngredient) (string, string) {
		return normalizeShoppingListName(ing.Description), strings.TrimSpace(ing.AisleNumber)
	})

	slices.SortStableFunc(combined, func(a, b *ai.Ingredient) int {
		return compareShoppingAisles(aisles[normalizeShoppingListName(a.Name)], aisles[normalizeShoppingListName(b.Name)])
	})

	return combined
}

// should only be used for internal matching otherwise violate some kroger must show names unaltered agreement
func normalizeShoppingListName(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, name)
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func compareShoppingAisles(a, b string) int {
	if a == "" && b == "" {
		return 0
	}
	if a == "" {
		return 1
	}
	if b == "" {
		return -1
	}
	aint, aerr := strconv.Atoi(a)
	bint, berr := strconv.Atoi(b)
	if aerr == nil && berr == nil {
		return cmp.Compare(aint, bint)
	}
	return cmp.Compare(a, b)
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
