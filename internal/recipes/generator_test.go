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
	instructions               []string
	responseID                 string
	recipe                     *ai.Recipe
	menuPlanInstructions       []string
	menuPlanResponseID         string
	menuPlanCount              int
	menuPlan                   *ai.MenuPlan
	createMenuPlanInstructions []string
	createMenuPlanCount        int
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
	menuPlanInstructions   [][]string
	menuPlanResponseIDs    []string
	menuPlanCounts         []int
	regenerateCalls        int
	regenerateInstructions [][]string
	regenerateResponseIDs  []string
	generateResponses      []*ai.ShoppingList
	menuPlanResponses      []*ai.MenuPlan
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

func (c *captureWineQuestionAIClient) CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error) {
	panic("unexpected call to CreateMenuPlan")
}

func (c *captureWineQuestionAIClient) RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*ai.MenuPlan, error) {
	panic("unexpected call to RegenerateMenuPlan")
}

func (c *captureWineQuestionAIClient) GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.Recipe, error) {
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

func (c *captureRegenerateAIClient) CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error) {
	c.createMenuPlanInstructions = append([]string(nil), instructions...)
	c.createMenuPlanCount = count
	if c.menuPlan != nil {
		return c.menuPlan, nil
	}
	return &ai.MenuPlan{}, nil
}

func (c *captureRegenerateAIClient) RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*ai.MenuPlan, error) {
	c.menuPlanInstructions = append([]string(nil), instructions...)
	c.menuPlanResponseID = previousResponseID
	c.menuPlanCount = count
	if c.menuPlan != nil {
		return c.menuPlan, nil
	}
	return &ai.MenuPlan{}, nil
}

func (c *captureRegenerateAIClient) GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.Recipe, error) {
	c.instructions = append([]string(nil), instructions...)
	if c.recipe != nil {
		return c.recipe, nil
	}
	return &ai.Recipe{}, nil
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

func (c *captureGenerateAIClient) CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error) {
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

func (c *captureGenerateAIClient) RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*ai.MenuPlan, error) {
	panic("unexpected call to RegenerateMenuPlan")
}

func (c *captureGenerateAIClient) GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.Recipe, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.shoppingList == nil {
		return &ai.Recipe{}, nil
	}
	for _, recipe := range c.shoppingList.Recipes {
		if recipeInstructionsContainAnchor(instructions, recipe.Title) {
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

func (c *sequenceAIClient) CreateMenuPlan(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string, count int) (*ai.MenuPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.generateCalls++
	c.menuPlanInstructions = append(c.menuPlanInstructions, append([]string(nil), instructions...))
	c.menuPlanCounts = append(c.menuPlanCounts, count)
	if len(c.generateResponses) == 0 {
		c.plannedRecipes = nil
		return &ai.MenuPlan{}, nil
	}
	resp := c.generateResponses[0]
	c.generateResponses = c.generateResponses[1:]
	c.plannedRecipes = slices.Clone(resp.Recipes)
	return menuPlanForRecipes(resp.Recipes), nil
}

func (c *sequenceAIClient) RegenerateMenuPlan(ctx context.Context, instructions []string, previousResponseID string, count int) (*ai.MenuPlan, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.menuPlanInstructions = append(c.menuPlanInstructions, append([]string(nil), instructions...))
	c.menuPlanResponseIDs = append(c.menuPlanResponseIDs, previousResponseID)
	c.menuPlanCounts = append(c.menuPlanCounts, count)
	if len(c.menuPlanResponses) > 0 {
		resp := c.menuPlanResponses[0]
		c.menuPlanResponses = c.menuPlanResponses[1:]
		return resp, nil
	}
	if len(c.generateResponses) == 0 {
		c.plannedRecipes = nil
		return &ai.MenuPlan{}, nil
	}
	resp := c.generateResponses[0]
	c.generateResponses = c.generateResponses[1:]
	c.plannedRecipes = slices.Clone(resp.Recipes)
	return menuPlanForRecipes(resp.Recipes), nil
}

func (c *sequenceAIClient) GenerateRecipe(ctx context.Context, location *locations.Location, ingredients []ai.InputIngredient, instructions []string, date time.Time, lastRecipes []string) (*ai.Recipe, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.generateInstructions = append(c.generateInstructions, append([]string(nil), instructions...))
	for _, recipe := range c.plannedRecipes {
		if recipeInstructionsContainAnchor(instructions, recipe.Title) {
			return &recipe, nil
		}
	}
	return &ai.Recipe{}, nil
}

func recipeInstructionsContainAnchor(instructions []string, title string) bool {
	needle := "Anchor ingredient direction for this recipe: " + title + "."
	return slices.Contains(instructions, needle)
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
	salePrice := float32(18.99)
	cached := []ai.InputIngredient{
		{
			ProductID:   "cached-pinot-noir",
			Description: "Cached Pinot Noir",
			Size:        "750mL",
			AisleNumber: "wine",
			PriceSale:   &salePrice,
		},
	}
	if err := rio.SaveIngredients(t.Context(), wineIngredientsCacheKey(style, location, cacheDate), cached); err != nil {
		t.Fatalf("failed to seed wine ingredients cache: %v", err)
	}

	aiStub := &captureWineQuestionAIClient{
		answer: "Great with your dish.",
		selection: &ai.WineSelection{
			Wines:      []ai.Ingredient{{ProductID: "cached-pinot-noir", Name: "Cached Pinot Noir", Quantity: "750mL"}},
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
	assert.Equal(t, "wine", got.Wines[0].AisleNumber)
	assert.Equal(t, "$18.99", got.Wines[0].Price)
	if aiStub.recipe.Title != "Roast Chicken" {
		t.Fatalf("expected recipe title %q, got %q", "Roast Chicken", aiStub.recipe.Title)
	}
}

func TestPickAWine_WholeFoodsUsesHardcodedWineCategories(t *testing.T) {
	aiStub := &captureWineQuestionAIClient{
		answer: "Try one of these bottles.",
		selection: &ai.WineSelection{
			Wines: []ai.Ingredient{
				{ProductID: "wholefoods-red", Name: "Whole Foods Red"},
				{ProductID: "wholefoods-white", Name: "Whole Foods White"},
				{ProductID: "wholefoods-bubbly", Name: "Whole Foods Bubbly"},
			},
			Commentary: "Try one of these bottles.",
		},
	}
	staplesStub := &captureWineStaplesProvider{
		responses: map[string][]ai.InputIngredient{
			"red-wine":   {{ProductID: "wholefoods-red", Description: "Whole Foods Red", AisleNumber: "red-wine"}},
			"white-wine": {{ProductID: "wholefoods-white", Description: "Whole Foods White", AisleNumber: "white-wine"}},
			"sparkling":  {{ProductID: "wholefoods-bubbly", Description: "Whole Foods Bubbly", AisleNumber: "sparkling"}},
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
	assert.Equal(t, "red-wine", got.Wines[0].AisleNumber)
	assert.Equal(t, "white-wine", got.Wines[1].AisleNumber)
	assert.Equal(t, "sparkling", got.Wines[2].AisleNumber)
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
		menuPlan: &ai.MenuPlan{Plans: []ai.RecipePlan{{
			Cuisine:          "test",
			AnchorIngredient: "Brand New Dinner",
			Technique:        "test",
		}}, ResponseID: "resp-menu-next"},
	}
	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	g := newTestGenerator(t, aiStub, nil, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, noopstatuswriter{}, nil)

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}
	params.Instructions = "make it vegetarian"
	params.Directive = "Use the store's sale ingredients."
	params.Saved = []ai.Recipe{alreadySaved, newlySaved}
	params.Dismissed = []ai.Recipe{dismissed}
	params.PriorSavedHashes = []string{alreadySaved.ComputeHash()}
	params.PreviousMenuPlanResponseID = "resp-menu-old"

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}

	wantInstructions := []string{
		"make it vegetarian",
		"Enjoyed and saved so don't repeat: Newly Saved",
		"Passed on Dismissed Recipe",
	}
	wantRecipeInstructions := append(slices.Clone(wantInstructions), ai.RecipePlan{
		Cuisine:          "test",
		AnchorIngredient: "Brand New Dinner",
		Technique:        "test",
	}.Instructions()...)
	if !slices.Equal(aiStub.instructions, wantRecipeInstructions) {
		t.Fatalf("unexpected recipe instructions: got %v want %v", aiStub.instructions, wantRecipeInstructions)
	}
	if !slices.Equal(aiStub.menuPlanInstructions, wantInstructions) {
		t.Fatalf("unexpected menu plan instructions: got %v want %v", aiStub.menuPlanInstructions, wantInstructions)
	}
	if aiStub.responseID != "resp-123" {
		t.Fatalf("expected recipe response ID %q, got %q", "resp-123", aiStub.responseID)
	}
	if aiStub.menuPlanResponseID != "resp-menu-old" {
		t.Fatalf("expected menu plan response ID %q, got %q", "resp-menu-old", aiStub.menuPlanResponseID)
	}
	if aiStub.menuPlanCount != 1 {
		t.Fatalf("expected one replacement plan, got %d", aiStub.menuPlanCount)
	}
	if got == nil || len(got.Recipes) != 3 {
		t.Fatalf("expected regenerated list plus saved recipes, got %+v", got)
	}
	if got.Recipes[0].Title != "Brand New Dinner" || got.Recipes[1].Title != "Already Saved" || got.Recipes[2].Title != "Newly Saved" {
		t.Fatalf("unexpected recipe order after regenerate: %+v", got.Recipes)
	}
}

func TestGenerateRecipes_RegenerateBackCompatFallbackUsesFakePlan(t *testing.T) {
	dismissed := ai.Recipe{Title: "Dismissed Recipe", Description: "Passed on", ResponseID: "resp-123"}
	newResult := ai.Recipe{Title: "Brand New Dinner", Description: "Fresh idea", ResponseID: "resp-new"}
	aiStub := &captureRegenerateAIClient{
		recipe: &newResult,
	}

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Date(2026, time.May, 22, 0, 0, 0, 0, time.UTC))
	params.Directive = "Use the store's sale ingredients."
	params.Instructions = "make it brighter"
	params.Dismissed = []ai.Recipe{dismissed}

	g := newTestGenerator(t, aiStub, nil, seededStaples(t, params), noopstatuswriter{}, nil)
	_, err := g.GenerateRecipes(t.Context(), params)
	require.NoError(t, err)

	wantInstructions := []string{
		"make it brighter",
		"Passed on Dismissed Recipe",
	}
	if aiStub.createMenuPlanCount != 0 || len(aiStub.createMenuPlanInstructions) != 0 {
		t.Fatalf("back-compat fallback should not create a fresh menu plan, got count=%d instructions=%v", aiStub.createMenuPlanCount, aiStub.createMenuPlanInstructions)
	}
	if aiStub.menuPlanCount != 0 || len(aiStub.menuPlanInstructions) != 0 {
		t.Fatalf("back-compat fallback should not regenerate a menu plan, got count=%d instructions=%v", aiStub.menuPlanCount, aiStub.menuPlanInstructions)
	}
	wantRecipeInstructions := append(slices.Clone(wantInstructions), ai.RecipePlan{
		Cuisine:          "anything",
		AnchorIngredient: "anything",
		Technique:        "anything",
	}.Instructions()...)
	if !slices.Equal(aiStub.instructions, wantRecipeInstructions) {
		t.Fatalf("unexpected fallback recipe instructions: got %v want %v", aiStub.instructions, wantRecipeInstructions)
	}
}

func TestGenerateRecipes_RegenerateBackCompatFallbackErrorsAfterCutoff(t *testing.T) {
	dismissed := ai.Recipe{Title: "Dismissed Recipe", Description: "Passed on", ResponseID: "resp-123"}
	newResult := ai.Recipe{Title: "Brand New Dinner", Description: "Fresh idea", ResponseID: "resp-new"}
	aiStub := &captureRegenerateAIClient{
		recipe: &newResult,
	}

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Date(2026, time.May, 23, 0, 0, 0, 0, time.UTC))
	params.Directive = "Use the store's sale ingredients."
	params.Instructions = "make it brighter"
	params.Dismissed = []ai.Recipe{dismissed}

	g := newTestGenerator(t, aiStub, nil, seededStaples(t, params), noopstatuswriter{}, nil)
	_, err := g.GenerateRecipes(t.Context(), params)
	require.ErrorContains(t, err, "missing previous menu plan response ID for menu date 2026-05-23")

	if aiStub.createMenuPlanCount != 0 || aiStub.menuPlanCount != 0 {
		t.Fatalf("missing response ID should fail before menu planning calls, got create=%d regenerate=%d", aiStub.createMenuPlanCount, aiStub.menuPlanCount)
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

func TestGenerateRecipes_EnrichesGeneratedIngredientsFromCatalogProductID(t *testing.T) {
	generated := []ai.Recipe{
		{
			Title:       "Roast Chicken",
			Description: "Crisp and simple",
			Ingredients: []ai.Ingredient{
				{ProductID: "chicken-1", Name: "Chicken thighs", Quantity: "1 lb", Price: "$99.99"},
				{Name: "Salt", Quantity: "1 tsp"},
			},
			Instructions: []string{"Roast the chicken."},
			ResponseID:   "resp-chicken",
		},
	}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	regularPrice := float32(8.99)
	salePrice := float32(6.49)
	require.NoError(t, io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{
		ProductID:    "chicken-1",
		Description:  "Chicken thighs",
		AisleNumber:  "7",
		PriceRegular: &regularPrice,
		PriceSale:    &salePrice,
	}}))

	aiStub := &captureGenerateAIClient{
		shoppingList: &ai.ShoppingList{
			Recipes: generated,
		},
	}
	saver := &captureRecipeSaver{}
	critiquer := &captureCritiqueService{}
	g := newTestGenerator(t, aiStub, critiquer, &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}, noopstatuswriter{}, saver)

	got, err := g.GenerateRecipes(t.Context(), params)
	require.NoError(t, err)
	require.Len(t, got.Recipes, 1)
	require.Len(t, got.Recipes[0].Ingredients, 2)
	assert.Equal(t, "chicken-1", got.Recipes[0].Ingredients[0].ProductID)
	assert.Equal(t, "7", got.Recipes[0].Ingredients[0].AisleNumber)
	assert.Equal(t, "$6.49", got.Recipes[0].Ingredients[0].Price)
	assert.Empty(t, got.Recipes[0].Ingredients[1].ProductID)
	assert.Empty(t, got.Recipes[0].Ingredients[1].AisleNumber)
	assert.Empty(t, got.Recipes[0].Ingredients[1].Price)

	require.Len(t, saver.recipes, 1)
	assert.Equal(t, "$6.49", saver.recipes[0].Ingredients[0].Price)
	require.Len(t, critiquer.recipes, 1)
	assert.Equal(t, "7", critiquer.recipes[0].Ingredients[0].AisleNumber)
}

type noopstatuswriter struct{}

func (noopstatuswriter) SaveGenerationStatus(_ context.Context, _, _ string) error { return nil }

func seededStaples(t *testing.T, params *generatorParams) staplesService {
	t.Helper()
	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}
	return &cachedStaplesService{cache: io, grader: ingredientgrading.NewManager(nil, nil, nil)}
}

func TestGenerateRecipes_RegenerateCritiquesOnlyFreshRecipes(t *testing.T) {
	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	dismissed := ai.Recipe{Title: "Dismissed Dinner", Description: "Passed on", ResponseID: "resp-123"}
	newResult := ai.Recipe{Title: "Brand New Dinner", Description: "Fresh idea", ResponseID: "resp-new"}

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.Saved = []ai.Recipe{alreadySaved}
	params.Dismissed = []ai.Recipe{dismissed}

	critiquer := &captureCritiqueService{}
	aiStub := &captureRegenerateAIClient{
		recipe: &newResult,
		menuPlan: &ai.MenuPlan{Plans: []ai.RecipePlan{{
			Cuisine:          "test",
			AnchorIngredient: "Brand New Dinner",
			Technique:        "test",
		}}},
	}
	g := newTestGenerator(t, aiStub, critiquer, seededStaples(t, params), noopstatuswriter{}, nil)

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
	if aiStub.createMenuPlanCount != 0 {
		t.Fatalf("expected back-compat fallback not to create a fresh menu plan, got %d calls", aiStub.createMenuPlanCount)
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
	assert.Equal(t, 3, len(statuses.status), "got statuses %v", statuses.status)
}

func TestGenerateRecipes_RegenerateRetriesLowScoringRecipesOnce(t *testing.T) {
	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	dismissed := ai.Recipe{Title: "Original Dinner", Description: "Original", ResponseID: "resp-original"}
	initial := ai.Recipe{Title: "Needs Work Dinner", Description: "First pass", ResponseID: "resp-first-pass"}
	retried := ai.Recipe{Title: "Ready Dinner", Description: "Second pass", ResponseID: "resp-second-pass"}

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.Instructions = "make it vegetarian"
	params.Saved = []ai.Recipe{alreadySaved}
	params.Dismissed = []ai.Recipe{dismissed}
	params.PreviousMenuPlanResponseID = "resp-menu-original"

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			Recipes: []ai.Recipe{initial},
		}},
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
	g := newTestGenerator(t, aiStub, critiquer, seededStaples(t, params), noopstatuswriter{}, nil)

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
	if got := aiStub.menuPlanResponseIDs; !slices.Equal(got, []string{"resp-menu-original"}) {
		t.Fatalf("unexpected menu plan response IDs: got %v", got)
	}
	if got := aiStub.menuPlanCounts; !slices.Equal(got, []int{1}) {
		t.Fatalf("unexpected menu plan counts: got %v", got)
	}
	if aiStub.regenerateCalls != 2 {
		t.Fatalf("expected initial replacement plus one critique retry, got %d calls", aiStub.regenerateCalls)
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

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.Instructions = "make it fresher"
	params.Dismissed = []ai.Recipe{dismissed}
	params.PreviousMenuPlanResponseID = "resp-menu-original"

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			Recipes: []ai.Recipe{firstPass},
		}},
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
	g := newTestGenerator(t, aiStub, critiquer, seededStaples(t, params), noopstatuswriter{}, nil)

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
