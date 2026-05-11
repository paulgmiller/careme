package recipes

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"
	"careme/internal/recipes/critique"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type captureWineQuestionAIClient struct {
	question  string
	answer    string
	recipe    ai.Recipe
	selection *ai.WineSelection
}

type captureRegenerateAIClient struct {
	instructions []string
	responseID   string
	recipe       *ai.Recipe
}

type captureGenerateAIClient struct {
	shoppingList *ai.ShoppingList
	ingredients  []ai.InputIngredient
	instructions [][]string
	lastRecipes  []string
	mu           sync.Mutex
}

type sequenceAIClient struct {
	mu                     sync.Mutex
	generateCalls          int
	generateInstructions   [][]string
	regenerateCalls        int
	regenerateInstructions [][]string
	regenerateResponseIDs  []string
	generateResponses      []*ai.ShoppingList
	plannedRecipes         []ai.Recipe
	regenerateResponses    []*ai.Recipe
}

type captureCritiqueService struct {
	mu      sync.Mutex
	err     error
	recipes []ai.Recipe
	fn      func(ai.Recipe) (*ai.RecipeCritique, error)
}

type captureWineStaplesProvider struct {
	mu        sync.Mutex
	searches  []string
	responses map[string][]ai.InputIngredient
}

type panicStaplesService struct{}

type noopRecipeSaver struct{}

type captureRecipeSaver struct {
	mu      sync.Mutex
	recipes []ai.Recipe
}

func newTestGenerator(
	t *testing.T,
	aiClient aiClient,
	critiquer critique.Service,
	staples staplesService,
	statuses statusWriter,
	saver recipeSaver,
) *generatorService {
	t.Helper()
	if critiquer == nil {
		critiquer = &captureCritiqueService{}
	}
	if staples == nil {
		staples = panicStaplesService{}
	}
	if statuses == nil {
		statuses = noopstatuswriter{}
	}
	if saver == nil {
		saver = noopRecipeSaver{}
	}
	g, err := NewGenerator(aiClient, critiquer, staples, statuses, saver)
	require.NoError(t, err)
	return g
}

func (c *captureWineQuestionAIClient) CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.MenuPlan, error) {
	panic("unexpected call to CreateMenuPlan")
}

func (c *captureWineQuestionAIClient) GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, plan ai.RecipePlan) (*ai.Recipe, error) {
	panic("unexpected call to GenerateRecipe")
}

func (c *captureWineQuestionAIClient) Regenerate(ctx context.Context, newinstructions []string, previousResponseID string) (*ai.Recipe, error) {
	panic("unexpected call to Regenerate")
}

func (c *captureWineQuestionAIClient) AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error) {
	c.question = question
	return &ai.QuestionResponse{Answer: c.answer, ResponseID: "resp-question"}, nil
}

func (c *captureWineQuestionAIClient) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *captureWineQuestionAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []ai.InputIngredient) (*ai.WineSelection, error) {
	c.recipe = recipe
	if c.selection != nil {
		return c.selection, nil
	}
	return &ai.WineSelection{
		Wines:      []ai.Ingredient{},
		Commentary: c.answer,
	}, nil
}

func (c *captureWineQuestionAIClient) Ready(ctx context.Context) error {
	return nil
}

func (c *captureRegenerateAIClient) CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.MenuPlan, error) {
	panic("unexpected call to CreateMenuPlan")
}

func (c *captureRegenerateAIClient) GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, plan ai.RecipePlan) (*ai.Recipe, error) {
	panic("unexpected call to GenerateRecipe")
}

func (c *captureRegenerateAIClient) Regenerate(ctx context.Context, newinstructions []string, previousResponseID string) (*ai.Recipe, error) {
	c.instructions = append([]string(nil), newinstructions...)
	c.responseID = previousResponseID
	if c.recipe != nil {
		return c.recipe, nil
	}
	return &ai.Recipe{}, nil
}

func (c *captureRegenerateAIClient) AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error) {
	panic("unexpected call to AskQuestion")
}

func (c *captureRegenerateAIClient) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *captureRegenerateAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []ai.InputIngredient) (*ai.WineSelection, error) {
	panic("unexpected call to PickWine")
}

func (c *captureRegenerateAIClient) Ready(ctx context.Context) error {
	return nil
}

func (c *captureGenerateAIClient) CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.MenuPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ingredients = append([]ai.InputIngredient(nil), ingredients...)
	c.instructions = append(c.instructions, append([]string(nil), instructions...))
	c.lastRecipes = append([]string(nil), lastRecipes...)
	if c.shoppingList != nil {
		return menuPlanForRecipes(c.shoppingList.Recipes), nil
	}
	return &ai.MenuPlan{}, nil
}

func (c *captureGenerateAIClient) GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, plan ai.RecipePlan) (*ai.Recipe, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.shoppingList == nil {
		return &ai.Recipe{}, nil
	}
	for _, recipe := range c.shoppingList.Recipes {
		if recipe.Title == plan.AnchorIngredient {
			return &recipe, nil
		}
	}
	return &ai.Recipe{}, nil
}

func (c *captureGenerateAIClient) Regenerate(ctx context.Context, newinstructions []string, previousResponseID string) (*ai.Recipe, error) {
	panic("unexpected call to Regenerate")
}

func (c *captureGenerateAIClient) AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error) {
	panic("unexpected call to AskQuestion")
}

func (c *captureGenerateAIClient) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *captureGenerateAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []ai.InputIngredient) (*ai.WineSelection, error) {
	panic("unexpected call to PickWine")
}

func (c *captureGenerateAIClient) Ready(ctx context.Context) error {
	return nil
}

func (c *sequenceAIClient) CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.MenuPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.generateCalls++
	c.generateInstructions = append(c.generateInstructions, append([]string(nil), instructions...))
	if len(c.generateResponses) == 0 {
		c.plannedRecipes = nil
		return &ai.MenuPlan{}, nil
	}
	resp := c.generateResponses[0]
	c.generateResponses = c.generateResponses[1:]
	c.plannedRecipes = slices.Clone(resp.Recipes)
	return menuPlanForRecipes(resp.Recipes), nil
}

func (c *sequenceAIClient) GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, plan ai.RecipePlan) (*ai.Recipe, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.generateInstructions = append(c.generateInstructions, append([]string(nil), instructions...))
	for _, recipe := range c.plannedRecipes {
		if recipe.Title == plan.AnchorIngredient {
			return &recipe, nil
		}
	}
	return &ai.Recipe{}, nil
}

func (c *sequenceAIClient) Regenerate(ctx context.Context, newinstructions []string, previousResponseID string) (*ai.Recipe, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.regenerateCalls++
	c.regenerateInstructions = append(c.regenerateInstructions, append([]string(nil), newinstructions...))
	c.regenerateResponseIDs = append(c.regenerateResponseIDs, previousResponseID)
	if len(c.regenerateResponses) == 0 {
		return &ai.Recipe{}, nil
	}
	resp := c.regenerateResponse(previousResponseID)
	return resp, nil
}

func (c *sequenceAIClient) regenerateResponse(previousResponseID string) *ai.Recipe {
	for i, resp := range c.regenerateResponses {
		if regenerateResponseMatches(*resp, previousResponseID) {
			c.regenerateResponses = append(c.regenerateResponses[:i], c.regenerateResponses[i+1:]...)
			return resp
		}
	}
	resp := c.regenerateResponses[0]
	c.regenerateResponses = c.regenerateResponses[1:]
	return resp
}

func regenerateResponseMatches(recipe ai.Recipe, previousResponseID string) bool {
	previousResponseID = strings.ToLower(previousResponseID)
	haystack := strings.ToLower(recipe.ResponseID + " " + recipe.Title)
	for _, token := range strings.FieldsFunc(previousResponseID, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	}) {
		if len(token) > 2 && token != "resp" && strings.Contains(haystack, token) {
			return true
		}
	}
	return false
}

func menuPlanForRecipes(recipes []ai.Recipe) *ai.MenuPlan {
	plans := make([]ai.RecipePlan, 0, len(recipes))
	for _, recipe := range recipes {
		plans = append(plans, ai.RecipePlan{
			Cuisine:          "test",
			AnchorIngredient: recipe.Title,
			Technique:        "test",
		})
	}
	return &ai.MenuPlan{Plans: plans}
}

func (c *sequenceAIClient) AskQuestion(ctx context.Context, question string, previousResponseID string) (*ai.QuestionResponse, error) {
	panic("unexpected call to AskQuestion")
}

func (c *sequenceAIClient) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *sequenceAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []ai.InputIngredient) (*ai.WineSelection, error) {
	panic("unexpected call to PickWine")
}

func (c *sequenceAIClient) Ready(ctx context.Context) error {
	return nil
}

func (c *captureCritiqueService) CritiqueRecipe(_ context.Context, recipe ai.Recipe) <-chan critique.Result {
	results := make(chan critique.Result, 1)
	c.mu.Lock()
	c.recipes = append(c.recipes, recipe)
	c.mu.Unlock()

	crit, err := c.critiqueFor(recipe)
	results <- critique.Result{
		Recipe:   &recipe,
		Critique: crit,
		Err:      err,
	}
	close(results)
	return results
}

func (c *captureCritiqueService) critiqueFor(recipe ai.Recipe) (*ai.RecipeCritique, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.fn != nil {
		return c.fn(recipe)
	}
	return &ai.RecipeCritique{
		SchemaVersion:  "recipe-critique-v1",
		OverallScore:   10,
		Summary:        "Solid draft.",
		Strengths:      []string{"clear direction"},
		Issues:         []ai.RecipeCritiqueIssue{{Severity: "medium", Category: "timing", Detail: "Timing could be tighter."}},
		SuggestedFixes: []string{"tighten the timing"},
	}, nil
}

func (s *captureWineStaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	panic("unexpected call to FetchStaples")
}

func (s *captureWineStaplesProvider) GetIngredients(_ context.Context, _ string, searchTerm string, _ int) ([]ai.InputIngredient, error) {
	s.mu.Lock()
	s.searches = append(s.searches, searchTerm)
	s.mu.Unlock()
	return slices.Clone(s.responses[searchTerm]), nil
}

func (s *captureWineStaplesProvider) searchTerms() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.searches)
}

func (panicStaplesService) FetchStaples(context.Context, *GeneratorParams) ([]ai.InputIngredient, error) {
	panic("unexpected call to FetchStaples")
}

func (panicStaplesService) GetIngredients(context.Context, string, string, int, time.Time) ([]ai.InputIngredient, error) {
	panic("unexpected call to GetIngredients")
}

func (noopRecipeSaver) SaveRecipe(context.Context, ai.Recipe) error {
	return nil
}

func (s *captureRecipeSaver) SaveRecipe(_ context.Context, recipe ai.Recipe) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recipes = append(s.recipes, recipe)
	return nil
}

func (s *captureRecipeSaver) titles() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	titles := make([]string, 0, len(s.recipes))
	for _, recipe := range s.recipes {
		titles = append(titles, recipe.Title)
	}
	return titles
}

func TestWineIngredientsCacheKey_UsesStyleDateAndLocation(t *testing.T) {
	got := wineIngredientsCacheKey(" Pinot Noir ", "70500874", time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC))
	want := "wines/0XY3COdxwHk"
	if got != want {
		t.Fatalf("unexpected cache key: got %q want %q", got, want)
	}
}

func TestPickAWine_UsesCachedIngredientsForStyleDateAndLocation(t *testing.T) {
	const (
		location = "70500874"
		style    = "Pinot Noir"
	)
	cacheDate := time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC)

	cacheStore := cache.NewFileCache(t.TempDir())
	rio := IO(cacheStore)
	cached := []ai.InputIngredient{
		{
			ProductID:   "cached-pinot-noir",
			Description: "Cached Pinot Noir",
			Size:        "750mL",
		},
	}
	if err := rio.SaveIngredients(t.Context(), wineIngredientsCacheKey(style, location, cacheDate), cached); err != nil {
		t.Fatalf("failed to seed wine ingredients cache: %v", err)
	}

	aiStub := &captureWineQuestionAIClient{
		answer: "Great with your dish.",
		selection: &ai.WineSelection{
			Wines:      []ai.Ingredient{{Name: "Cached Pinot Noir", Quantity: "750mL"}},
			Commentary: "Great with your dish.",
		},
	}
	g := newTestGenerator(t, aiStub, nil, &cachedStaplesService{cache: rio, provider: &captureWineStaplesProvider{}}, nil, nil)

	got, err := g.PickAWine(t.Context(), location, ai.Recipe{
		Title:      "Roast Chicken",
		WineStyles: []string{style},
	}, cacheDate)
	if err != nil {
		t.Fatalf("PickAWine returned error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil wine selection")
		return
	}
	if got.Commentary != aiStub.answer {
		t.Fatalf("unexpected commentary: got %q want %q", got.Commentary, aiStub.answer)
	}
	if got.Wines == nil || len(got.Wines) != 1 || got.Wines[0].Name != "Cached Pinot Noir" {
		t.Fatalf("unexpected wine selection payload: %+v", got.Wines)
	}
	if aiStub.recipe.Title != "Roast Chicken" {
		t.Fatalf("expected recipe title %q, got %q", "Roast Chicken", aiStub.recipe.Title)
	}
}

func TestPickAWine_WholeFoodsUsesHardcodedWineCategories(t *testing.T) {
	aiStub := &captureWineQuestionAIClient{
		answer: "Try one of these bottles.",
		selection: &ai.WineSelection{
			Wines: []ai.Ingredient{
				{Name: "Whole Foods Red"},
				{Name: "Whole Foods White"},
				{Name: "Whole Foods Bubbly"},
			},
			Commentary: "Try one of these bottles.",
		},
	}
	staplesStub := &captureWineStaplesProvider{
		responses: map[string][]ai.InputIngredient{
			"red-wine":   {{ProductID: "wholefoods-red", Description: "Whole Foods Red"}},
			"white-wine": {{ProductID: "wholefoods-white", Description: "Whole Foods White"}},
			"sparkling":  {{ProductID: "wholefoods-bubbly", Description: "Whole Foods Bubbly"}},
		},
	}
	rio := IO(cache.NewFileCache(t.TempDir()))
	g := newTestGenerator(t, aiStub, nil, &cachedStaplesService{cache: rio, provider: staplesStub}, nil, nil)

	got, err := g.PickAWine(t.Context(), "wholefoods_10216", ai.Recipe{
		Title:      "Salmon",
		WineStyles: []string{"Pinot Noir"},
	}, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("PickAWine returned error: %v", err)
	}

	searches := staplesStub.searchTerms()
	slices.Sort(searches)
	wantSearches := []string{"red-wine", "sparkling", "white-wine"}
	if !slices.Equal(searches, wantSearches) {
		t.Fatalf("unexpected whole foods wine searches: got %v want %v", searches, wantSearches)
	}
	if got == nil || len(got.Wines) != 3 {
		t.Fatalf("unexpected wine selection: %+v", got)
	}
	if aiStub.recipe.Title != "Salmon" {
		t.Fatalf("expected recipe title %q, got %q", "Salmon", aiStub.recipe.Title)
	}
}

func TestGenerateRecipes_RegenerateIncludesOnlyNewlySavedRecipesInAvoidInstruction(t *testing.T) {
	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	newlySaved := ai.Recipe{Title: "Newly Saved", Description: "Saved now"}
	dismissed := ai.Recipe{Title: "Dismissed Recipe", Description: "Passed on", ResponseID: "resp-123"}
	newResult := ai.Recipe{Title: "Brand New Dinner", Description: "Fresh idea", ResponseID: "resp-new"}

	aiStub := &captureRegenerateAIClient{
		recipe: &newResult,
	}
	g := newTestGenerator(t, aiStub, nil, nil, noopstatuswriter{}, nil)

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.Instructions = "make it vegetarian"
	params.Saved = []ai.Recipe{alreadySaved, newlySaved}
	params.Dismissed = []ai.Recipe{dismissed}
	params.PriorSavedHashes = []string{alreadySaved.ComputeHash()}

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}

	wantInstructions := []string{
		"make it vegetarian",
		"Enjoyed and saved so don't repeat: Newly Saved",
		"Passed on Dismissed Recipe",
	}
	if !slices.Equal(aiStub.instructions, wantInstructions) {
		t.Fatalf("unexpected regenerate instructions: got %v want %v", aiStub.instructions, wantInstructions)
	}
	if aiStub.responseID != "resp-123" {
		t.Fatalf("expected response ID %q, got %q", "resp-123", aiStub.responseID)
	}
	if got == nil || len(got.Recipes) != 3 {
		t.Fatalf("expected regenerated list plus saved recipes, got %+v", got)
	}
	if got.Recipes[0].Title != "Brand New Dinner" || got.Recipes[1].Title != "Already Saved" || got.Recipes[2].Title != "Newly Saved" {
		t.Fatalf("unexpected recipe order after regenerate: %+v", got.Recipes)
	}
}

func TestGenerateRecipes_RegenerateWithOnlySavedRecipesPreservesSavedRecipes(t *testing.T) {
	saved := ai.Recipe{Title: "Saved Dinner", Description: "Keep this one"}
	g := newTestGenerator(t, &captureGenerateAIClient{}, nil, nil, noopstatuswriter{}, nil)

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.Instructions = "make the next round brighter"
	params.Saved = []ai.Recipe{saved}

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 1 || got.Recipes[0].ComputeHash() != saved.ComputeHash() {
		t.Fatalf("expected saved recipe to be preserved, got %+v", got)
	}
}

func TestGenerateRecipes_CritiquesGeneratedRecipes(t *testing.T) {
	generated := []ai.Recipe{
		{Title: "Roast Chicken", Description: "Crisp and simple", Instructions: []string{"Roast the chicken."}, ResponseID: "resp-chicken"},
		{Title: "Pasta Primavera", Description: "Vegetable pasta", Instructions: []string{"Boil pasta.", "Toss with vegetables."}, ResponseID: "resp-pasta"},
	}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &captureGenerateAIClient{
		shoppingList: &ai.ShoppingList{
			Recipes: generated,
		},
	}
	critiquer := &captureCritiqueService{}
	saver := &captureRecipeSaver{}
	g := newTestGenerator(t, aiStub, critiquer, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, noopstatuswriter{}, saver)

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != len(generated) {
		t.Fatalf("expected generated recipes, got %+v", got)
	}
	if len(critiquer.recipes) != len(generated) {
		t.Fatalf("expected %d critiques, got %d", len(generated), len(critiquer.recipes))
	}
	critiquedByTitle := map[string]ai.Recipe{}
	for _, recipe := range critiquer.recipes {
		critiquedByTitle[recipe.Title] = recipe
	}
	for _, want := range generated {
		recipe, ok := critiquedByTitle[want.Title]
		if !ok {
			t.Fatalf("expected recipe %q to be critiqued, got %+v", want.Title, critiquer.recipes)
		}
		if recipe.OriginHash != params.Hash() {
			t.Fatalf("expected critiqued recipe to include origin hash %q, got %+v", params.Hash(), recipe)
		}
	}
}

type noopstatuswriter struct{}

func (noopstatuswriter) SaveGenerationStatus(_ context.Context, _, _ string) error { return nil }

func TestGenerateRecipes_RegenerateCritiquesOnlyFreshRecipes(t *testing.T) {
	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	dismissed := ai.Recipe{Title: "Dismissed Dinner", Description: "Passed on", ResponseID: "resp-123"}
	newResult := ai.Recipe{Title: "Brand New Dinner", Description: "Fresh idea", ResponseID: "resp-new"}

	critiquer := &captureCritiqueService{}
	g := newTestGenerator(t, &captureRegenerateAIClient{recipe: &newResult}, critiquer, nil, noopstatuswriter{}, nil)

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.Saved = []ai.Recipe{alreadySaved}
	params.Dismissed = []ai.Recipe{dismissed}

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 2 {
		t.Fatalf("expected regenerated list plus saved recipes, got %+v", got)
	}
	if len(critiquer.recipes) != 1 || critiquer.recipes[0].Title != "Brand New Dinner" {
		t.Fatalf("expected only the newly generated recipe to be critiqued, got %+v", critiquer.recipes)
	}
}

func TestGenerateRecipes_RetriesLowScoringGeneratedRecipesOnce(t *testing.T) {
	initial := ai.Recipe{Title: "Weak Dinner", Description: "Needs work", ResponseID: "resp-initial"}
	retried := ai.Recipe{Title: "Better Dinner", Description: "Improved", ResponseID: "resp-retried"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			Recipes: []ai.Recipe{initial},
		}},
		regenerateResponses: []*ai.Recipe{&retried},
	}
	critiquer := &captureCritiqueService{
		fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
			switch recipe.Title {
			case "Weak Dinner":
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   6,
					Summary:        "Needs a cleaner finish.",
					Strengths:      []string{"solid idea"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "high", Category: "clarity", Detail: "The sauce step is vague."}},
					SuggestedFixes: []string{"clarify when to reduce the sauce"},
				}, nil
			case "Better Dinner":
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   8,
					Summary:        "Ready to cook.",
					Strengths:      []string{"clear direction"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "low", Category: "timing", Detail: "Could shave a minute or two."}},
					SuggestedFixes: []string{"tighten the simmer time"},
				}, nil
			default:
				t.Fatalf("unexpected recipe critique request for %q", recipe.Title)
				return nil, nil
			}
		},
	}

	saver := &captureRecipeSaver{}
	g := newTestGenerator(t, aiStub, critiquer, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, noopstatuswriter{}, saver)

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 1 || got.Recipes[0].Title != "Better Dinner" {
		t.Fatalf("expected retried shopping list, got %+v", got)
	}
	if aiStub.regenerateCalls != 1 {
		t.Fatalf("expected one critique-driven regenerate call, got %d", aiStub.regenerateCalls)
	}
	wantInstructions := []string{
		"Revise recipe. Description should focus on selling the dish not these corrections.",
		"scored 6/10.\n Issues: [clarity/high] The sauce step is vague.\n Suggested fixes: clarify when to reduce the sauce",
	}
	if got := aiStub.regenerateInstructions[0]; !slices.Equal(got, wantInstructions) {
		t.Fatalf("unexpected critique retry instructions: got %v want %v", got, wantInstructions)
	}
	if got := aiStub.regenerateResponseIDs; !slices.Equal(got, []string{"resp-initial"}) {
		t.Fatalf("unexpected critique retry response IDs: got %v", got)
	}
	if len(critiquer.recipes) != 2 {
		t.Fatalf("expected two critique passes, got %d", len(critiquer.recipes))
	}
	if got := critiquer.recipes[1].Title; got != "Better Dinner" {
		t.Fatalf("expected retried recipe to be critiqued again, got %q", got)
	}
	if got := saver.titles(); !slices.Equal(got, []string{"Weak Dinner", "Better Dinner"}) {
		t.Fatalf("expected original and retried recipes to be saved, got %v", got)
	}
}

func TestGenerateRecipes_RetryKeepsHighScoringRecipes(t *testing.T) {
	weak := ai.Recipe{Title: "Weak Dinner", Description: "Needs work", ResponseID: "resp-weak"}
	good := ai.Recipe{Title: "Solid Dinner", Description: "Already fine"}
	retried := ai.Recipe{Title: "Better Dinner", Description: "Improved", ResponseID: "resp-retried"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			Recipes: []ai.Recipe{weak, good},
		}},
		regenerateResponses: []*ai.Recipe{&retried},
	}
	critiquer := &captureCritiqueService{
		fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
			switch recipe.Title {
			case "Weak Dinner":
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   6,
					Summary:        "Needs a cleaner finish.",
					Strengths:      []string{"solid idea"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "high", Category: "clarity", Detail: "The sauce step is vague."}},
					SuggestedFixes: []string{"clarify when to reduce the sauce"},
				}, nil
			case "Solid Dinner", "Better Dinner":
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   8,
					Summary:        "Ready to cook.",
					Strengths:      []string{"clear direction"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "low", Category: "timing", Detail: "Could shave a minute or two."}},
					SuggestedFixes: []string{"tighten the simmer time"},
				}, nil
			default:
				t.Fatalf("unexpected recipe critique request for %q", recipe.Title)
				return nil, nil
			}
		},
	}
	g := newTestGenerator(t, aiStub, critiquer, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, noopstatuswriter{}, nil)

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 2 {
		t.Fatalf("expected retried recipe plus preserved good recipe, got %+v", got)
	}
	if got.Recipes[0].Title != "Better Dinner" || got.Recipes[1].Title != "Solid Dinner" {
		t.Fatalf("unexpected recipe order after partial retry: %+v", got.Recipes)
	}
}

func TestGenerateRecipes_DoesNotRetryWhenCritiquesMeetThreshold(t *testing.T) {
	steady := ai.Recipe{Title: "Steady Dinner", Description: "Good enough", ResponseID: "resp-stable"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			Recipes: []ai.Recipe{steady},
		}},
	}
	g := newTestGenerator(t, aiStub, &captureCritiqueService{}, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, noopstatuswriter{}, nil)

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 1 || got.Recipes[0].Title != "Steady Dinner" {
		t.Fatalf("unexpected shopping list: %+v", got)
	}
	if aiStub.regenerateCalls != 0 {
		t.Fatalf("expected no critique-driven regenerate calls, got %d", aiStub.regenerateCalls)
	}
}

type statusCounter struct {
	status []string
}

func (s *statusCounter) SaveGenerationStatus(_ context.Context, _, stat string) error {
	s.status = append(s.status, stat)
	return nil
}

func TestGenerateRecipes_WritesStatusStagesForInitialGeneration(t *testing.T) {
	steady := ai.Recipe{Title: "Steady Dinner", Description: "Good enough"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	statuses := &statusCounter{}
	g := newTestGenerator(t, &sequenceAIClient{generateResponses: []*ai.ShoppingList{{Recipes: []ai.Recipe{steady}}}}, &captureCritiqueService{}, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, statuses, nil)

	_, err := g.GenerateRecipes(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, 2, len(statuses.status), "got statuses %v", statuses.status)
}

func TestGenerateRecipes_RegenerateRetriesLowScoringRecipesOnce(t *testing.T) {
	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	dismissed := ai.Recipe{Title: "Original Dinner", Description: "Original", ResponseID: "resp-original"}
	initial := ai.Recipe{Title: "Needs Work Dinner", Description: "First pass", ResponseID: "resp-first-pass"}
	retried := ai.Recipe{Title: "Ready Dinner", Description: "Second pass", ResponseID: "resp-second-pass"}

	aiStub := &sequenceAIClient{
		regenerateResponses: []*ai.Recipe{
			&initial,
			&retried,
		},
	}
	critiquer := &captureCritiqueService{
		fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
			switch recipe.Title {
			case "Needs Work Dinner":
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   5,
					Summary:        "Too loose for a weeknight cook.",
					Strengths:      []string{"promising flavors"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "medium", Category: "timing", Detail: "Cooking times are inconsistent."}},
					SuggestedFixes: []string{"make the timing consistent"},
				}, nil
			case "Ready Dinner":
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   8,
					Summary:        "Clear and usable.",
					Strengths:      []string{"better pacing"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "low", Category: "presentation", Detail: "Could add garnish detail."}},
					SuggestedFixes: []string{"mention a finishing garnish"},
				}, nil
			default:
				t.Fatalf("unexpected recipe critique request for %q", recipe.Title)
				return nil, nil
			}
		},
	}
	g := newTestGenerator(t, aiStub, critiquer, nil, noopstatuswriter{}, nil)

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.Instructions = "make it vegetarian"
	params.Saved = []ai.Recipe{alreadySaved}
	params.Dismissed = []ai.Recipe{dismissed}

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 2 {
		t.Fatalf("expected regenerated list plus saved recipe, got %+v", got)
	}
	if got.Recipes[0].Title != "Ready Dinner" || got.Recipes[1].Title != "Already Saved" {
		t.Fatalf("unexpected recipe order after critique retry: %+v", got.Recipes)
	}
	if got.Recipes[0].ParentHash != initial.ComputeHash() {
		t.Fatalf("expected retried recipe to point to the first-pass recipe, got %+v", got.Recipes[0])
	}
	if aiStub.regenerateCalls != 2 {
		t.Fatalf("expected initial regenerate plus one critique retry, got %d calls", aiStub.regenerateCalls)
	}
	if got := aiStub.regenerateResponseIDs; !slices.Equal(got, []string{"resp-original", "resp-first-pass"}) {
		t.Fatalf("unexpected regenerate response IDs: got %v", got)
	}
	wantRetryInstructions := []string{
		"Revise recipe. Description should focus on selling the dish not these corrections.",
		"scored 5/10.\n Issues: [timing/medium] Cooking times are inconsistent.\n Suggested fixes: make the timing consistent",
	}
	if got := aiStub.regenerateInstructions[1]; !slices.Equal(got, wantRetryInstructions) {
		t.Fatalf("unexpected critique retry instructions: got %v want %v", got, wantRetryInstructions)
	}
}

func TestGenerateRecipes_CritiqueRetryPointsToImmediateParent(t *testing.T) {
	dismissed := ai.Recipe{Title: "Original Dinner", Description: "Original", ResponseID: "resp-original"}
	firstPass := ai.Recipe{Title: "First Pass Dinner", Description: "Needs work", ResponseID: "resp-first-pass"}
	retried := ai.Recipe{Title: "Second Pass Dinner", Description: "Improved", ResponseID: "resp-second-pass"}

	aiStub := &sequenceAIClient{
		regenerateResponses: []*ai.Recipe{
			&firstPass,
			&retried,
		},
	}
	critiquer := &captureCritiqueService{
		fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
			if recipe.Title == "First Pass Dinner" {
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   6,
					Summary:        "Needs revision.",
					Strengths:      []string{"good bones"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "medium", Category: "clarity", Detail: "Need clearer steps."}},
					SuggestedFixes: []string{"clarify the steps"},
				}, nil
			}
			return &ai.RecipeCritique{
				SchemaVersion:  "recipe-critique-v1",
				OverallScore:   8,
				Summary:        "Ready to cook.",
				Strengths:      []string{"clear direction"},
				Issues:         []ai.RecipeCritiqueIssue{{Severity: "low", Category: "timing", Detail: "Minor timing cleanup."}},
				SuggestedFixes: []string{"tighten the simmer time"},
			}, nil
		},
	}
	g := newTestGenerator(t, aiStub, critiquer, nil, noopstatuswriter{}, nil)

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.Instructions = "make it fresher"
	params.Dismissed = []ai.Recipe{dismissed}
	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 1 {
		t.Fatalf("expected one retried recipe, got %+v", got)
	}
	if got.Recipes[0].ParentHash != firstPass.ComputeHash() {
		t.Fatalf("expected retried recipe parent hash %q, got %q", firstPass.ComputeHash(), got.Recipes[0].ParentHash)
	}
}

func TestGenerateRecipes_CritiqueRetryMatchesParentByTitleWords(t *testing.T) {
	firstPassChicken := ai.Recipe{Title: "Lemon Chicken Pasta", Description: "Needs work", ResponseID: "resp-chicken"}
	firstPassTacos := ai.Recipe{Title: "Spicy Bean Tacos", Description: "Needs work", ResponseID: "resp-tacos"}
	retriedTacos := ai.Recipe{Title: "Weeknight Bean Tacos", Description: "Improved", ResponseID: "resp-retried-tacos"}
	retriedChicken := ai.Recipe{Title: "Bright Lemon Chicken Pasta", Description: "Improved", ResponseID: "resp-retried-chicken"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			Recipes: []ai.Recipe{firstPassChicken, firstPassTacos},
		}},
		regenerateResponses: []*ai.Recipe{&retriedChicken, &retriedTacos},
	}
	critiquer := &captureCritiqueService{
		fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
			switch recipe.Title {
			case "Lemon Chicken Pasta", "Spicy Bean Tacos":
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   6,
					Summary:        "Needs revision.",
					Strengths:      []string{"good idea"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "medium", Category: "clarity", Detail: "Needs cleaner steps."}},
					SuggestedFixes: []string{"clarify the method"},
				}, nil
			default:
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   8,
					Summary:        "Ready to cook.",
					Strengths:      []string{"clear direction"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "low", Category: "timing", Detail: "Minor cleanup only."}},
					SuggestedFixes: []string{"tighten the timing"},
				}, nil
			}
		},
	}
	g := newTestGenerator(t, aiStub, critiquer, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, noopstatuswriter{}, nil)

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 2 {
		t.Fatalf("expected two retried recipes, got %+v", got)
	}
	if got.Recipes[0].ParentHash != firstPassChicken.ComputeHash() {
		t.Fatalf("expected chicken retry to match chicken parent, got %+v", got.Recipes[0])
	}
	if got.Recipes[1].ParentHash != firstPassTacos.ComputeHash() {
		t.Fatalf("expected tacos retry to match tacos parent, got %+v", got.Recipes[1])
	}
}

func TestGenerateRecipes_RetriesAtMostOnceEvenIfRetryStillScoresLow(t *testing.T) {
	initial := ai.Recipe{Title: "First Try", Description: "Low score", ResponseID: "resp-one"}
	retried := ai.Recipe{Title: "Second Try", Description: "Still low", ResponseID: "resp-two"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			Recipes: []ai.Recipe{initial},
		}},
		regenerateResponses: []*ai.Recipe{&retried},
	}
	critiquer := &captureCritiqueService{
		fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
			return &ai.RecipeCritique{
				SchemaVersion:  "recipe-critique-v1",
				OverallScore:   6,
				Summary:        "Still not ready.",
				Strengths:      []string{"salvageable"},
				Issues:         []ai.RecipeCritiqueIssue{{Severity: "high", Category: "cookability", Detail: "The method still has gaps."}},
				SuggestedFixes: []string{"rewrite the method more clearly"},
			}, nil
		},
	}
	g := newTestGenerator(t, aiStub, critiquer, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, noopstatuswriter{}, nil)

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 1 || got.Recipes[0].Title != "Second Try" {
		t.Fatalf("unexpected retried shopping list: %+v", got)
	}
	if aiStub.regenerateCalls != 1 {
		t.Fatalf("expected exactly one critique-driven retry, got %d", aiStub.regenerateCalls)
	}
}

func TestNewlySaved(t *testing.T) {
	foo := ai.Recipe{Title: "foo", Description: "blah"}
	salmon := ai.Recipe{Title: "Salmon", Description: "previusly saved"}
	hash := foo.ComputeHash()

	got := newlySaved(
		[]ai.Recipe{foo, salmon, salmon},
		[]string{hash},
	)

	want := []string{salmon.Title}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected saved avoid instruction: got %q want %q", got, want)
	}
}
