package recipes

import (
	"context"
	"fmt"
	"html/template"
	"slices"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/feedback"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
)

type recipeImageView struct {
	HasImage bool
	Poll     bool
	Hash     string
	// OutOfBand lets the shared panel template opt into the HTMX outerHTML swap
	// used by the save response without duplicating the panel markup.
	OutOfBand bool
}

type recipePageView struct {
	templates.Page
	Location                locations.Location
	Date                    string
	Recipe                  ai.Recipe
	InstructionsHTML        []template.HTML
	Saved                   bool
	DisplayIngredients      []ai.Ingredient
	PropertyDisplay         recipePropertyDisplay
	OriginHash              string
	ResponseID              string
	PromptCacheKey          string
	WineRecommendation      *ai.WineSelection
	Thread                  []RecipeThreadEntry
	Feedback                feedback.Feedback
	RecipeHash              string
	RecipeImage             recipeImageView
	ServerSignedIn          bool
	User                    *utypes.User
	AuthReturnTo            string
	RecipeCritiqueURL       string
	RecipeCritiqueScore     *int
	RecipeCritiqueNeedsCare bool
	MinimumRecipeScore      int
	AdminURL                string
}

type recipeViewInput struct {
	params             *generatorParams
	recipe             ai.Recipe
	saved              bool
	currentUser        *utypes.User
	recipeCritique     *ai.RecipeCritique
	hasRecipeImage     bool
	thread             []RecipeThreadEntry
	feedback           feedback.Feedback
	wineRecommendation *ai.WineSelection
}

func newRecipePageView(ctx context.Context, input recipeViewInput) (recipePageView, error) {
	recipe := input.recipe
	thread := input.thread

	thread = slices.Clone(thread)
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	recipeHash := recipe.ComputeHash()
	instructionsHTML, err := renderRecipeInstructions(recipe.Instructions)
	if err != nil {
		return recipePageView{}, fmt.Errorf("instruction rendering error: %w", err)
	}
	activeResponseID := recipe.ResponseID
	if threadResponseID := latestThreadResponseID(thread); threadResponseID != "" {
		activeResponseID = threadResponseID
	}
	serverSignedIn := input.currentUser != nil
	var critiqueScore *int
	var minimumRecipeScore int
	if input.recipeCritique != nil {
		critiqueScore = &input.recipeCritique.OverallScore
		minimumRecipeScore = critique.MinimumRecipeScoreForModel(input.recipeCritique.Model)
	}
	data := recipePageView{
		Page:                    templates.NewPage(ctx),
		Location:                *input.params.Location,
		Date:                    input.params.Date.Format("2006-01-02"),
		Recipe:                  recipe,
		InstructionsHTML:        instructionsHTML,
		Saved:                   input.saved,
		DisplayIngredients:      ingredientsForDisplay(recipe.Ingredients, input.wineRecommendation),
		PropertyDisplay:         newRecipePropertyDisplay(recipe),
		OriginHash:              recipe.OriginHash,
		ResponseID:              activeResponseID,
		PromptCacheKey:          recipe.PromptCacheKey,
		WineRecommendation:      input.wineRecommendation,
		Thread:                  thread,
		Feedback:                input.feedback,
		RecipeHash:              recipeHash,
		RecipeImage:             recipeImageData(recipeHash, input.hasRecipeImage, false, input.saved),
		ServerSignedIn:          serverSignedIn,
		User:                    input.currentUser,
		AuthReturnTo:            "/recipe/" + recipeHash,
		RecipeCritiqueURL:       "/critiques/" + recipeHash,
		RecipeCritiqueScore:     critiqueScore,
		RecipeCritiqueNeedsCare: critiqueScore != nil && *critiqueScore < minimumRecipeScore,
		MinimumRecipeScore:      minimumRecipeScore,
		AdminURL:                "/admin/prompt/recipe/" + recipeHash,
	}

	return data, nil
}

func recipeImageData(recipeHash string, hasImage bool, outOfBand, poll bool) recipeImageView {
	return recipeImageView{
		HasImage:  hasImage,
		Poll:      poll && !hasImage,
		Hash:      recipeHash,
		OutOfBand: outOfBand,
	}
}

type recipeThreadView struct {
	ResponseID     string
	PromptCacheKey string
	RecipeHash     string
	Thread         []RecipeThreadEntry
	ServerSignedIn bool
}

func newRecipeThreadView(thread []RecipeThreadEntry, signedIn bool, response ai.ResponseRef, recipeHash string) recipeThreadView {
	thread = slices.Clone(thread)
	slices.SortFunc(thread, func(i, j RecipeThreadEntry) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	data := recipeThreadView{
		ResponseID:     response.ID,
		PromptCacheKey: response.PromptCacheKey,
		RecipeHash:     recipeHash,
		Thread:         thread,
		ServerSignedIn: signedIn,
	}

	return data
}

type recipeSaveActionView struct {
	Recipe         ai.Recipe
	Saved          bool
	OriginHash     string
	RecipeHash     string
	ServerSignedIn bool
}

func newRecipeSaveActionView(recipe ai.Recipe, originHash string, saved bool) recipeSaveActionView {
	data := recipeSaveActionView{
		Recipe:         recipe,
		Saved:          saved,
		OriginHash:     originHash,
		RecipeHash:     recipe.ComputeHash(),
		ServerSignedIn: true,
	}
	return data
}
