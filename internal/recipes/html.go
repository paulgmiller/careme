package recipes

import (
	"cmp"
	"context"
	"fmt"
	"html/template"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"careme/internal/ai"
	"careme/internal/httpx"
	"careme/internal/locations"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/feedback"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
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
	Saved              bool
	Dismissed          bool
	HasImage           bool
	WineRecommendation *ai.WineSelection
}

type shoppingListGroup struct {
	Aisle string
	Items []*ai.Ingredient
}

type shoppingListPage struct {
	Params              *generatorParams
	ShoppingList        ai.ShoppingList
	WineRecommendations map[string]*ai.WineSelection
	RecipeImages        map[string]bool
	User                *utypes.User
	Hash                string
	Selection           recipeSelection
	HelpMessage         string
	PendingInstructions string
}

func renderShoppingListHTML(ctx context.Context, page shoppingListPage, writer http.ResponseWriter) {
	serverSignedIn := page.User != nil
	instructions := strings.TrimSpace(page.Params.Instructions)
	if instructions == "" && page.ShoppingList.Plan != nil {
		instructions = page.ShoppingList.Plan.ChefNoteSuggestion
	}
	recipeViews := make([]shoppingRecipeView, 0, len(page.ShoppingList.Recipes))
	combinedIngredients := make([]ai.Ingredient, 0)
	hasSavedRecipes := false
	for _, recipe := range page.ShoppingList.Recipes {
		recipeHash := recipe.ComputeHash()
		wineRecommendation := page.WineRecommendations[recipeHash]
		displayIngredients := ingredientsForDisplay(recipe.Ingredients, wineRecommendation)
		saved := page.Selection.IsSaved(recipeHash)
		recipeViews = append(recipeViews, shoppingRecipeView{
			Recipe:             recipe,
			Hash:               recipeHash,
			ShoppingListHash:   page.Hash,
			ServerSignedIn:     serverSignedIn,
			DisplayIngredients: displayIngredients,
			Saved:              saved,
			Dismissed:          page.Selection.IsDismissed(recipeHash),
			HasImage:           page.RecipeImages[recipeHash],
			WineRecommendation: wineRecommendation,
		})
		if saved {
			hasSavedRecipes = true
			combinedIngredients = append(combinedIngredients, displayIngredients...)
		}
	}
	data := struct {
		Location             locations.Location
		Date                 string
		DateDisplay          string
		MetaDescription      string
		ClarityScript        template.HTML
		GoogleTagScript      template.HTML
		Instructions         string
		PendingInstructions  string
		HelpMessage          string
		Hash                 string
		Recipes              []shoppingRecipeView
		ShoppingList         []shoppingListGroup
		HasSavedRecipes      bool
		Style                seasons.Style
		ServerSignedIn       bool
		User                 *utypes.User
		AuthReturnTo         string
		UseTodaysIngredients bool
		AdminURL             string
	}{
		Location:             *page.Params.Location,
		Date:                 page.Params.Date.Format("2006-01-02"),
		DateDisplay:          page.Params.Date.Format("January 2, 2006"),
		MetaDescription:      shoppingListMetaDescription(page.ShoppingList.Recipes, page.Params.Location.Name, page.Params.Date.Format("2006-01-02")),
		ClarityScript:        templates.ClarityScript(ctx),
		GoogleTagScript:      templates.GoogleTagScript(),
		Instructions:         instructions,
		PendingInstructions:  page.PendingInstructions,
		HelpMessage:          strings.TrimSpace(page.HelpMessage),
		Hash:                 page.Hash,
		Recipes:              recipeViews,
		ShoppingList:         shoppingListForDisplay(combinedIngredients),
		HasSavedRecipes:      hasSavedRecipes,
		Style:                seasons.GetCurrentStyle(),
		ServerSignedIn:       serverSignedIn,
		User:                 page.User,
		AuthReturnTo:         "/recipes?h=" + page.Hash,
		UseTodaysIngredients: shoppingListIsOlderThanFreshIngredientsWindow(ctx, page.Params),
		AdminURL:             "/admin/mealplan/" + page.Hash,
	}

	httpx.SetHTMLContentType(writer)
	if err := templates.ShoppingList.Execute(writer, data); err != nil {
		http.Error(writer, "shopping list template error: "+err.Error(), http.StatusInternalServerError)
	}
}

func shoppingListIsOlderThanFreshIngredientsWindow(ctx context.Context, p *generatorParams) bool {
	today, err := StoreToDate(ctx, nowFn(), p.Location)
	if err != nil {
		return false
	}
	return today.Sub(p.Date) > 24*time.Hour
}

func shoppingListMetaDescription(recipes []ai.Recipe, locationName, date string) string {
	titles := make([]string, 0, len(recipes))
	for _, recipe := range recipes {
		title := strings.TrimSpace(recipe.Title)
		if title != "" {
			titles = append(titles, title)
		}
	}
	if len(titles) == 0 {
		return fmt.Sprintf("Recipes for %s on %s.", locationName, date)
	}
	return fmt.Sprintf("Recipes for %s on %s: %s.", locationName, date, strings.Join(titles, ", "))
}

type recipePage struct {
	Params             *generatorParams
	Recipe             ai.Recipe
	Saved              bool
	User               *utypes.User
	CritiqueScore      *int
	HasImage           bool
	Thread             []RecipeThreadEntry
	Feedback           feedback.Feedback
	WineRecommendation *ai.WineSelection
}

func renderRecipeHTML(ctx context.Context, page recipePage, writer http.ResponseWriter) {
	slices.SortFunc(page.Thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	recipeHash := page.Recipe.ComputeHash()
	activeResponseID := page.Recipe.ResponseID
	if threadResponseID := latestThreadResponseID(page.Thread); threadResponseID != "" {
		activeResponseID = threadResponseID
	}
	serverSignedIn := page.User != nil
	data := struct {
		Location                locations.Location
		Date                    string
		ClarityScript           template.HTML
		GoogleTagScript         template.HTML
		Recipe                  ai.Recipe
		Saved                   bool
		DisplayIngredients      []ai.Ingredient
		OriginHash              string
		ResponseID              string
		WineRecommendation      *ai.WineSelection
		Thread                  []RecipeThreadEntry
		Feedback                feedback.Feedback
		RecipeHash              string
		RecipeImage             recipeImageView
		Style                   seasons.Style
		ServerSignedIn          bool
		User                    *utypes.User
		AuthReturnTo            string
		RecipeCritiqueURL       string
		RecipeCritiqueScore     *int
		RecipeCritiqueNeedsCare bool
		MinimumRecipeScore      int
		AdminURL                string
	}{
		Location:                *page.Params.Location,
		Date:                    page.Params.Date.Format("2006-01-02"),
		ClarityScript:           templates.ClarityScript(ctx),
		GoogleTagScript:         templates.GoogleTagScript(),
		Recipe:                  page.Recipe,
		Saved:                   page.Saved,
		DisplayIngredients:      ingredientsForDisplay(page.Recipe.Ingredients, page.WineRecommendation),
		OriginHash:              page.Recipe.OriginHash,
		ResponseID:              activeResponseID,
		WineRecommendation:      page.WineRecommendation,
		Thread:                  page.Thread,
		Feedback:                page.Feedback,
		RecipeHash:              recipeHash,
		RecipeImage:             recipeImageData(recipeHash, page.HasImage, false),
		Style:                   seasons.GetCurrentStyle(),
		ServerSignedIn:          serverSignedIn,
		User:                    page.User,
		AuthReturnTo:            "/recipe/" + recipeHash,
		RecipeCritiqueURL:       "/critiques/" + recipeHash,
		RecipeCritiqueScore:     page.CritiqueScore,
		RecipeCritiqueNeedsCare: page.CritiqueScore != nil && *page.CritiqueScore < critique.MinimumRecipeScore,
		MinimumRecipeScore:      critique.MinimumRecipeScore,
		AdminURL:                "/admin/prompt/recipe/" + recipeHash,
	}

	httpx.SetHTMLContentType(writer)
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
func FormatRecipeThreadHTML(thread []RecipeThreadEntry, signedIn bool, responseID, recipeHash string, writer http.ResponseWriter) {
	// memory waste because we alwways resort?
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	data := struct {
		ResponseID     string
		RecipeHash     string
		Thread         []RecipeThreadEntry
		ServerSignedIn bool
	}{
		ResponseID:     responseID,
		RecipeHash:     recipeHash,
		Thread:         thread,
		ServerSignedIn: signedIn,
	}

	httpx.SetHTMLContentType(writer)
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
func RenderShoppingRecipeCardHTML(recipe ai.Recipe, saved bool, shoppingListHash string, wineRecommendation *ai.WineSelection, hasImage bool, writer io.Writer) error {
	data := shoppingRecipeView{
		Recipe:             recipe,
		Hash:               recipe.ComputeHash(),
		ShoppingListHash:   shoppingListHash,
		ServerSignedIn:     true, // have to be signed in to toggle
		DisplayIngredients: ingredientsForDisplay(recipe.Ingredients, wineRecommendation),
		Saved:              saved,
		Dismissed:          !saved,
		HasImage:           hasImage,
		WineRecommendation: wineRecommendation,
	}
	return templates.ShoppingList.ExecuteTemplate(writer, "shopping_recipe_card", data)
}

// called from single recipe page just swaps save dimiss
func RenderRecipeSaveActionHTML(recipe ai.Recipe, originHash string, saved bool, writer io.Writer) error {
	data := struct {
		Recipe         ai.Recipe
		Saved          bool
		OriginHash     string
		RecipeHash     string
		ServerSignedIn bool
	}{
		Recipe:         recipe,
		Saved:          saved,
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

func shoppingListForDisplay(ingredients []ai.Ingredient) []shoppingListGroup {
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
				ProductID:   strings.TrimSpace(ingredient.ProductID),
				AisleNumber: strings.TrimSpace(ingredient.AisleNumber),
				Name:        ingredient.Name, // show non normalized
				Quantity:    strings.TrimSpace(ingredient.Quantity),
				Price:       strings.TrimSpace(ingredient.Price),
			}
			items[name] = item
			combined = append(combined, item)

			continue
		}
		existing.Quantity = mergeShoppingQuantities(existing.Quantity, ingredient.Quantity)
	}

	slices.SortStableFunc(combined, func(a, b *ai.Ingredient) int {
		return compareShoppingAisles(strings.TrimSpace(a.AisleNumber), strings.TrimSpace(b.AisleNumber))
	})

	var groups []shoppingListGroup
	for _, item := range combined {
		aisle := strings.TrimSpace(item.AisleNumber)
		if len(groups) == 0 || groups[len(groups)-1].Aisle != shoppingAisleHeading(aisle) {
			groups = append(groups, shoppingListGroup{
				Aisle: shoppingAisleHeading(aisle),
			})
		}
		groups[len(groups)-1].Items = append(groups[len(groups)-1].Items, item)
	}
	return groups
}

var shoppingQtyWithSuffixPattern = regexp.MustCompile(`^\s*(\d+(?:\.\d+)?)\s+(.+?)\s*$`)

func mergeShoppingQuantities(existing string, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	switch {
	case incoming == "":
		return existing
	case existing == "":
		return incoming
	}

	existingNumber, existingSuffix, okExisting := parseShoppingQuantity(existing)
	incomingNumber, incomingSuffix, okIncoming := parseShoppingQuantity(incoming)
	if okExisting && okIncoming && normalizeShoppingQuantitySuffix(existingSuffix) == normalizeShoppingQuantitySuffix(incomingSuffix) {
		return formatShoppingQuantity(existingNumber+incomingNumber, existingSuffix)
	}
	return existing + ", " + incoming
}

func parseShoppingQuantity(raw string) (float64, string, bool) {
	match := shoppingQtyWithSuffixPattern.FindStringSubmatch(beforeFirstComma(raw))
	if len(match) != 3 {
		return 0, "", false
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, "", false
	}
	suffix := strings.TrimSpace(match[2])
	if suffix == "" {
		return 0, "", false
	}
	return value, suffix, true
}

func beforeFirstComma(value string) string {
	if index := strings.Index(value, ","); index >= 0 {
		return strings.TrimSpace(value[:index])
	}
	return strings.TrimSpace(value)
}

func normalizeShoppingQuantitySuffix(suffix string) string {
	return strings.ToLower(strings.Join(strings.Fields(suffix), " "))
}

func formatShoppingQuantity(value float64, suffix string) string {
	if math.Abs(value-math.Round(value)) < 1e-9 {
		return fmt.Sprintf("%d %s", int64(math.Round(value)), suffix)
	}
	return fmt.Sprintf("%s %s", strconv.FormatFloat(value, 'f', -1, 64), suffix)
}

func shoppingAisleHeading(aisle string) string {
	aisle = strings.TrimSpace(aisle)
	if aisle == "" {
		return "Other items"
	}
	if _, err := strconv.Atoi(aisle); err == nil {
		return "Aisle " + aisle
	}
	if label, ok := knownShoppingAisleLabels[aisle]; ok {
		return label
	}
	// Some providers give category slugs instead of display aisle names.
	// Convert values like "fresh-vegetables" into a readable heading.
	parts := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(aisle))
	for i, part := range parts {
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

var knownShoppingAisleLabels = map[string]string{
	"dairy-eggs":  "Dairy & eggs",
	"fresh-herbs": "Fresh herbs",
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
