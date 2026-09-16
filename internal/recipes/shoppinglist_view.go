package recipes

import (
	"context"
	"fmt"
	"html/template"
	"slices"
	"strconv"
	"strings"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/recipes/status"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
)

// shoppingRecipeView is a thin wrapper around ai.Recipe for the shopping list page.
//
// We keep ingredient expansion in Go instead of the template because the same derived
// list is used both for card rendering and for the combined shopping list below.
// The remaining extra fields are shopping-list-specific UI state that ai.Recipe
// should not own.
type shoppingRecipeView struct {
	ai.Recipe
	// Hash identifies the card. Generated recipes use their recipe hash for
	// links and HTMX endpoints; pending plan slots use a temporary ID.
	Hash string
	// ShoppingListHash identifies the surrounding /recipes?h=... page and is
	// used anywhere the card needs to refer back to the full list state.
	ShoppingListHash   string
	ServerSignedIn     bool
	DisplayIngredients []ai.Ingredient // merged food and wine
	PropertyDisplay    recipePropertyDisplay
	InstructionsHTML   []template.HTML
	Saved              bool
	Dismissed          bool
	HasImage           bool
	WineRecommendation *ai.WineSelection
	Ready              bool // enables saving, dismissing, and links to the recipe
}

// DOMID is a CSS-safe identifier; recipe URLs and cache keys keep their full hash.
func (v shoppingRecipeView) DOMID() string {
	return "shopping-recipe-" + strings.TrimRight(v.Hash, "=")
}

type shoppingProgress struct {
	StatusMessage string
	Slots         []status.Slot        // meal-plan order, with hashes for finished recipes
	Finished      map[string]ai.Recipe // generated recipes referenced by slots
	// Generating stays true until the final shopping list is cached; the slots
	// are also empty before planning finishes.
	Generating bool
	// Fragment renders only shopping_content for an HTMX outerHTML swap of
	// #shopping-content, including the final poll that removes polling controls.
	// Ordinary page requests render the full shoppinglist.html document.
	Fragment bool
}

type shoppingListPageView struct {
	templates.Page
	StatusMessage        string
	Generating           bool
	Location             locations.Location
	Date                 string
	DateDisplay          string
	MetaDescription      string
	Instructions         string
	PendingInstructions  string
	HelpMessage          string
	Hash                 string
	Recipes              []shoppingRecipeView
	ShoppingList         []shoppingListGroup
	HasSavedRecipes      bool
	HasMissingImages     bool
	ServerSignedIn       bool
	User                 *utypes.User
	AuthReturnTo         string
	UseTodaysIngredients bool
	AdminURL             string
}

type shoppingListViewInput struct {
	imagesOnly           bool
	params               *generatorParams
	list                 ai.ShoppingList
	wineRecommendations  map[string]*ai.WineSelection
	recipeImages         map[string]bool
	currentUser          *utypes.User
	hash                 string
	selection            recipeSelection
	helpMessage          string
	pendingInstructions  string
	progress             shoppingProgress
	useTodaysIngredients bool
}

func newShoppingListPageView(ctx context.Context, input shoppingListViewInput) (shoppingListPageView, error) {
	serverSignedIn := input.currentUser != nil
	instructions := strings.TrimSpace(input.params.Instructions)
	if instructions == "" && input.list.Plan != nil {
		instructions = input.list.Plan.ChefNoteSuggestion
	}
	recipeViews, err := shoppingRecipeViews(input.list.Recipes, input.progress, input.hash, input.selection, input.wineRecommendations, input.recipeImages, serverSignedIn)
	if err != nil {
		return shoppingListPageView{}, fmt.Errorf("recipe rendering error: %w", err)
	}
	combinedIngredients := make([]ai.Ingredient, 0)
	hasSavedRecipes := false
	for _, view := range recipeViews {
		if view.Saved {
			hasSavedRecipes = true
			combinedIngredients = append(combinedIngredients, view.DisplayIngredients...)
		}
	}

	data := shoppingListPageView{
		Page:                templates.NewPage(ctx),
		StatusMessage:       input.progress.StatusMessage,
		Generating:          input.progress.Generating,
		Location:            *input.params.Location,
		Date:                input.params.Date.Format("2006-01-02"),
		DateDisplay:         input.params.Date.Format("January 2, 2006"),
		MetaDescription:     shoppingListMetaDescription(input.list.Recipes, input.params.Location.Name, input.params.Date.Format("2006-01-02")),
		Instructions:        instructions,
		PendingInstructions: input.pendingInstructions,
		HelpMessage:         strings.TrimSpace(input.helpMessage),
		Hash:                input.hash,
		Recipes:             recipeViews,
		ShoppingList:        shoppingListForDisplay(combinedIngredients),
		HasSavedRecipes:     hasSavedRecipes,
		HasMissingImages: slices.ContainsFunc(recipeViews, func(view shoppingRecipeView) bool {
			return view.Ready && !view.Dismissed && !view.HasImage
		}),
		ServerSignedIn:       serverSignedIn,
		User:                 input.currentUser,
		AuthReturnTo:         "/recipes?h=" + input.hash,
		UseTodaysIngredients: input.useTodaysIngredients,
		AdminURL:             "/admin/mealplan/" + input.hash,
	}

	return data, nil
}

// shoppingRecipeViews renders slots first, followed by the list's recipes.
// During generation the list contains only carried-forward saved recipes;
// completed lists contain every recipe and have no slots.
func shoppingRecipeViews(recipes []ai.Recipe, progress shoppingProgress, listHash string, selection recipeSelection,
	wines map[string]*ai.WineSelection, images map[string]bool, signedIn bool,
) ([]shoppingRecipeView, error) {
	views := make([]shoppingRecipeView, 0, len(progress.Slots)+len(recipes))
	appendRecipe := func(recipe ai.Recipe, ready bool) error {
		hash := recipe.ComputeHash()
		view, err := newShoppingRecipeView(recipe, shoppingRecipeInput{
			ShoppingListHash:   listHash,
			ServerSignedIn:     signedIn,
			Saved:              selection.IsSaved(hash),
			Dismissed:          selection.IsDismissed(hash),
			HasImage:           images[hash],
			WineRecommendation: wines[hash],
			Ready:              ready,
		})
		if err != nil {
			return fmt.Errorf("render recipe %s: %w", hash, err)
		}
		views = append(views, view)
		return nil
	}
	for index, slot := range progress.Slots {
		if slot.RecipeHash == "" {
			views = append(views, shoppingRecipeView{
				Recipe: ai.Recipe{
					Title:       slot.Plan.Cuisine + "  " + slot.Plan.DishFormat,
					Description: "using " + slot.Plan.AnchorIngredient + " and " + slot.Plan.SideVegetable,
				},
				Hash: "pending-" + strconv.Itoa(index),
			})
		} else if err := appendRecipe(progress.Finished[slot.RecipeHash], slot.Reviewed); err != nil {
			return nil, err
		}
	}
	for _, recipe := range recipes {
		if err := appendRecipe(recipe, true); err != nil {
			return nil, err
		}
	}
	return views, nil
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

type shoppingFinalizeView struct {
	Hash            string
	HasSavedRecipes bool
}

func newShoppingFinalizeView(hash string) shoppingFinalizeView {
	data := shoppingFinalizeView{
		Hash:            hash,
		HasSavedRecipes: true,
	}

	return data
}

// shoppingRecipeInput is the card state supplied by the controller or list builder.
type shoppingRecipeInput struct {
	ShoppingListHash   string
	ServerSignedIn     bool
	Saved              bool
	Dismissed          bool
	HasImage           bool
	WineRecommendation *ai.WineSelection
	Ready              bool
}

func newShoppingRecipeView(recipe ai.Recipe, state shoppingRecipeInput) (shoppingRecipeView, error) {
	instructionsHTML, err := renderRecipeInstructions(recipe.Instructions)
	if err != nil {
		return shoppingRecipeView{}, fmt.Errorf("render instructions: %w", err)
	}
	data := shoppingRecipeView{
		Recipe:             recipe,
		Hash:               recipe.ComputeHash(),
		ShoppingListHash:   state.ShoppingListHash,
		ServerSignedIn:     state.ServerSignedIn,
		DisplayIngredients: ingredientsForDisplay(recipe.Ingredients, state.WineRecommendation),
		PropertyDisplay:    newRecipePropertyDisplay(recipe),
		InstructionsHTML:   instructionsHTML,
		Saved:              state.Saved,
		Dismissed:          state.Dismissed,
		HasImage:           state.HasImage,
		WineRecommendation: state.WineRecommendation,
		Ready:              state.Ready,
	}
	return data, nil
}
