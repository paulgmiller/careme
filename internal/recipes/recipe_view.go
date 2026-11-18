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
	Hash      string
	HasImage  bool
	Thumbnail bool
}

type recipePageView struct {
	templates.Page
	Location                locations.Location
	Date                    string
	Recipe                  ai.Recipe
	Steps                   []recipeStepView
	Saved                   bool
	DisplayIngredients      []ai.Ingredient
	PropertyDisplay         recipePropertyDisplay
	OriginHash              string
	ResponseID              string
	PromptCacheKey          string
	WineRecommendation      *ai.WineSelection
	Thread                  []recipeThreadEntryView
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

type recipeStepView struct {
	Number         int
	HTML           template.HTML
	HasImage       bool
	Hash           string
	ServerSignedIn bool
}

func newRecipePageView(ctx context.Context, input recipeViewInput) (recipePageView, error) {
	recipe := input.recipe
	thread := input.thread

	threadView, err := newRecipeThreadEntries(thread)
	if err != nil {
		return recipePageView{}, err
	}
	recipeHash := recipe.ComputeHash()
	instructionsHTML, err := renderRecipeInstructions(recipe.Instructions)
	if err != nil {
		return recipePageView{}, fmt.Errorf("instruction rendering error: %w", err)
	}
	steps := make([]recipeStepView, len(instructionsHTML))
	for index, html := range instructionsHTML {
		steps[index] = recipeStepView{Number: index + 1, HTML: html, Hash: recipeHash, ServerSignedIn: input.currentUser != nil}
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
		Steps:                   steps,
		Saved:                   input.saved,
		DisplayIngredients:      ingredientsForDisplay(recipe.Ingredients, input.wineRecommendation),
		PropertyDisplay:         newRecipePropertyDisplay(recipe),
		OriginHash:              recipe.OriginHash,
		ResponseID:              activeResponseID,
		PromptCacheKey:          recipe.PromptCacheKey,
		WineRecommendation:      input.wineRecommendation,
		Thread:                  threadView,
		Feedback:                input.feedback,
		RecipeHash:              recipeHash,
		RecipeImage:             recipeImageView{Hash: recipeHash, HasImage: input.hasRecipeImage},
		ServerSignedIn:          serverSignedIn,
		User:                    input.currentUser,
		AuthReturnTo:            "/recipe/" + recipeHash,
		RecipeCritiqueURL:       "/critiques/" + recipeHash,
		RecipeCritiqueScore:     critiqueScore,
		RecipeCritiqueNeedsCare: critiqueScore != nil && *critiqueScore < minimumRecipeScore,
		MinimumRecipeScore:      minimumRecipeScore,
		AdminURL:                "/admin/prompt/recipe/" + recipeHash,
	}

	data.Title = recipe.Title
	data.Description = fmt.Sprintf("%s Recipe for %s on %s.", recipe.Description, data.Location.Name, data.Date)
	if recipe.Title != "" {
		imagePath := "/favicon.ico"
		if input.hasRecipeImage {
			imagePath = "/recipe/" + recipeHash + "/image"
		}
		data.Social = &templates.SocialPreview{
			Title:       recipe.Title,
			Description: recipe.Description,
			ImagePath:   imagePath,
		}
	}

	return data, nil
}

type recipeThreadView struct {
	ResponseID     string
	PromptCacheKey string
	RecipeHash     string
	Thread         []recipeThreadEntryView
	ServerSignedIn bool
}

func newRecipeThreadView(thread []RecipeThreadEntry, signedIn bool, response ai.ResponseRef, recipeHash string) (recipeThreadView, error) {
	threadView, err := newRecipeThreadEntries(thread)
	if err != nil {
		return recipeThreadView{}, err
	}
	data := recipeThreadView{
		ResponseID:     response.ID,
		PromptCacheKey: response.PromptCacheKey,
		RecipeHash:     recipeHash,
		Thread:         threadView,
		ServerSignedIn: signedIn,
	}

	return data, nil
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

type recipeThreadEntryView struct {
	RecipeThreadEntry
	AnswerHTML template.HTML
}

func newRecipeThreadEntries(thread []RecipeThreadEntry) ([]recipeThreadEntryView, error) {
	entries := make([]recipeThreadEntryView, len(thread))
	for index, entry := range thread {
		html, err := renderRecipeMarkdown(entry.Answer)
		if err != nil {
			return nil, fmt.Errorf("render question answer %d: %w", index+1, err)
		}
		entries[index] = recipeThreadEntryView{RecipeThreadEntry: entry, AnswerHTML: html}
	}
	slices.SortFunc(entries, func(i, j recipeThreadEntryView) int {
		return j.CreatedAt.Compare(i.CreatedAt)
	})
	return entries, nil
}
