package recipes

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
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
	instructions   []string
	conversationID string
	shoppingList   *ai.ShoppingList
}

type captureGenerateAIClient struct {
	shoppingList *ai.ShoppingList
}

type sequenceAIClient struct {
	mu                     sync.Mutex
	generateCalls          int
	generateInstructions   [][]string
	regenerateCalls        int
	regenerateInstructions [][]string
	regenerateConversation []string
	generateResponses      []*ai.ShoppingList
	regenerateResponses    []*ai.ShoppingList
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
	responses map[string][]kroger.Ingredient
}

func (c *captureWineQuestionAIClient) GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error) {
	panic("unexpected call to GenerateRecipes")
}

func (c *captureWineQuestionAIClient) Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error) {
	panic("unexpected call to Regenerate")
}

func (c *captureWineQuestionAIClient) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	c.question = question
	return c.answer, nil
}

func (c *captureWineQuestionAIClient) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *captureWineQuestionAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []kroger.Ingredient) (*ai.WineSelection, error) {
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

func (c *captureRegenerateAIClient) GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error) {
	panic("unexpected call to GenerateRecipes")
}

func (c *captureRegenerateAIClient) Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error) {
	c.instructions = append([]string(nil), newinstructions...)
	c.conversationID = conversationID
	if c.shoppingList != nil {
		return c.shoppingList, nil
	}
	return &ai.ShoppingList{}, nil
}

func (c *captureRegenerateAIClient) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	panic("unexpected call to AskQuestion")
}

func (c *captureRegenerateAIClient) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *captureRegenerateAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []kroger.Ingredient) (*ai.WineSelection, error) {
	panic("unexpected call to PickWine")
}

func (c *captureRegenerateAIClient) Ready(ctx context.Context) error {
	return nil
}

func (c *captureGenerateAIClient) GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error) {
	if c.shoppingList != nil {
		return c.shoppingList, nil
	}
	return &ai.ShoppingList{}, nil
}

func (c *captureGenerateAIClient) Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error) {
	panic("unexpected call to Regenerate")
}

func (c *captureGenerateAIClient) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	panic("unexpected call to AskQuestion")
}

func (c *captureGenerateAIClient) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *captureGenerateAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []kroger.Ingredient) (*ai.WineSelection, error) {
	panic("unexpected call to PickWine")
}

func (c *captureGenerateAIClient) Ready(ctx context.Context) error {
	return nil
}

func (c *sequenceAIClient) GenerateRecipes(ctx context.Context, location *locations.Location, ingredients []kroger.Ingredient, instructions []string, date time.Time, lastRecipes []string) (*ai.ShoppingList, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.generateCalls++
	c.generateInstructions = append(c.generateInstructions, append([]string(nil), instructions...))
	if len(c.generateResponses) == 0 {
		return &ai.ShoppingList{}, nil
	}
	resp := c.generateResponses[0]
	c.generateResponses = c.generateResponses[1:]
	return resp, nil
}

func (c *sequenceAIClient) Regenerate(ctx context.Context, newinstructions []string, conversationID string) (*ai.ShoppingList, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.regenerateCalls++
	c.regenerateInstructions = append(c.regenerateInstructions, append([]string(nil), newinstructions...))
	c.regenerateConversation = append(c.regenerateConversation, conversationID)
	if len(c.regenerateResponses) == 0 {
		return &ai.ShoppingList{}, nil
	}
	resp := c.regenerateResponses[0]
	c.regenerateResponses = c.regenerateResponses[1:]
	return resp, nil
}

func (c *sequenceAIClient) AskQuestion(ctx context.Context, question string, conversationID string) (string, error) {
	panic("unexpected call to AskQuestion")
}

func (c *sequenceAIClient) GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error) {
	panic("unexpected call to GenerateRecipeImage")
}

func (c *sequenceAIClient) PickWine(ctx context.Context, recipe ai.Recipe, wines []kroger.Ingredient) (*ai.WineSelection, error) {
	panic("unexpected call to PickWine")
}

func (c *sequenceAIClient) Ready(ctx context.Context) error {
	return nil
}

func (c *captureCritiqueService) CritiqueRecipes(_ context.Context, recipes []ai.Recipe) <-chan critique.Result {
	results := make(chan critique.Result, len(recipes))
	for _, recipe := range recipes {
		c.mu.Lock()
		c.recipes = append(c.recipes, recipe)
		c.mu.Unlock()

		crit, err := c.critiqueFor(recipe)
		results <- critique.Result{
			Recipe:   &recipe,
			Critique: crit,
			Err:      err,
		}
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

func (s *captureWineStaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error) {
	panic("unexpected call to FetchStaples")
}

func (s *captureWineStaplesProvider) GetIngredients(_ context.Context, _ string, searchTerm string, _ int) ([]kroger.Ingredient, error) {
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
	cached := []kroger.Ingredient{
		{
			Description: loPtr("Cached Pinot Noir"),
			Size:        loPtr("750mL"),
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
	g := &generatorService{
		staples:  &cachedStaplesService{cache: rio, provider: &captureWineStaplesProvider{}},
		aiClient: aiStub,
	}

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
		responses: map[string][]kroger.Ingredient{
			"red-wine":   {{Description: loPtr("Whole Foods Red")}},
			"white-wine": {{Description: loPtr("Whole Foods White")}},
			"sparkling":  {{Description: loPtr("Whole Foods Bubbly")}},
		},
	}
	g := &generatorService{
		staples:  &cachedStaplesService{cache: IO(cache.NewFileCache(t.TempDir())), provider: staplesStub},
		aiClient: aiStub,
	}

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
	dismissed := ai.Recipe{Title: "Dismissed Recipe", Description: "Passed on"}
	newResult := ai.Recipe{Title: "Brand New Dinner", Description: "Fresh idea"}

	aiStub := &captureRegenerateAIClient{
		shoppingList: &ai.ShoppingList{
			ConversationID: "conv-123",
			Recipes:        []ai.Recipe{newResult},
		},
	}
	g := &generatorService{
		aiClient: aiStub,
	}

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.ConversationID = "conv-123"
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
		"Passed on Dismissed Recipe",
		"Enjoyed and saved so don't repeat: Newly Saved",
	}
	if !slices.Equal(aiStub.instructions, wantInstructions) {
		t.Fatalf("unexpected regenerate instructions: got %v want %v", aiStub.instructions, wantInstructions)
	}
	if aiStub.conversationID != "conv-123" {
		t.Fatalf("expected conversation ID %q, got %q", "conv-123", aiStub.conversationID)
	}
	if got == nil || len(got.Recipes) != 3 {
		t.Fatalf("expected regenerated list plus saved recipes, got %+v", got)
	}
	if got.Recipes[0].Title != "Brand New Dinner" || got.Recipes[1].Title != "Already Saved" || got.Recipes[2].Title != "Newly Saved" {
		t.Fatalf("unexpected recipe order after regenerate: %+v", got.Recipes)
	}
}

func TestGenerateRecipes_CritiquesGeneratedRecipes(t *testing.T) {
	generated := []ai.Recipe{
		{Title: "Roast Chicken", Description: "Crisp and simple", Instructions: []string{"Roast the chicken."}},
		{Title: "Pasta Primavera", Description: "Vegetable pasta", Instructions: []string{"Boil pasta.", "Toss with vegetables."}},
	}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []kroger.Ingredient{{Description: loPtr("Chicken")}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &captureGenerateAIClient{
		shoppingList: &ai.ShoppingList{
			ConversationID: "conv-123",
			Recipes:        generated,
		},
	}
	critiquer := &captureCritiqueService{}
	g := &generatorService{
		staples:      &cachedStaplesService{cache: io},
		aiClient:     aiStub,
		critiquer:    critiquer,
		statusWriter: noopstatuswriter{},
	}

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got.ConversationID != "conv-123" {
		t.Fatalf("expected conversation id to survive, got %q", got.ConversationID)
	}
	if len(critiquer.recipes) != len(generated) {
		t.Fatalf("expected %d critiques, got %d", len(generated), len(critiquer.recipes))
	}
	if !reflect.DeepEqual(critiquer.recipes, generated) {
		t.Fatalf("unexpected critiqued recipes: got %+v want %+v", critiquer.recipes, generated)
	}
}

type noopstatuswriter struct{}

func (noopstatuswriter) SaveGenerationStatus(_ context.Context, _, _ string) error { return nil }

func TestGenerateRecipes_RegenerateCritiquesOnlyFreshRecipes(t *testing.T) {
	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	newResult := ai.Recipe{Title: "Brand New Dinner", Description: "Fresh idea"}

	critiquer := &captureCritiqueService{}
	g := &generatorService{
		aiClient:     &captureRegenerateAIClient{shoppingList: &ai.ShoppingList{ConversationID: "conv-123", Recipes: []ai.Recipe{newResult}}},
		critiquer:    critiquer,
		statusWriter: noopstatuswriter{},
	}

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.ConversationID = "conv-123"
	params.Saved = []ai.Recipe{alreadySaved}

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
	initial := ai.Recipe{Title: "Weak Dinner", Description: "Needs work"}
	retried := ai.Recipe{Title: "Better Dinner", Description: "Improved"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []kroger.Ingredient{{Description: loPtr("Chicken")}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			ConversationID: "conv-initial",
			Recipes:        []ai.Recipe{initial},
		}},
		regenerateResponses: []*ai.ShoppingList{{
			ConversationID: "conv-retried",
			Recipes:        []ai.Recipe{retried},
		}},
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

	g := &generatorService{
		staples:      &cachedStaplesService{cache: io},
		aiClient:     aiStub,
		critiquer:    critiquer,
		statusWriter: noopstatuswriter{},
	}

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 1 || got.Recipes[0].Title != "Better Dinner" {
		t.Fatalf("expected retried shopping list, got %+v", got)
	}
	if got.ConversationID != "conv-retried" {
		t.Fatalf("expected final conversation ID %q, got %q", "conv-retried", got.ConversationID)
	}
	if aiStub.regenerateCalls != 1 {
		t.Fatalf("expected one critique-driven regenerate call, got %d", aiStub.regenerateCalls)
	}
	wantInstructions := []string{
		"Revise and return exactly 1 recipes as replacements for the low-scoring recipes listed below. Description should focus on selling the dish not these corrections",
		"Recipe \"Weak Dinner\" scored 6/10.\n Issues: [clarity/high] The sauce step is vague.\n Suggested fixes: clarify when to reduce the sauce",
	}
	if got := aiStub.regenerateInstructions[0]; !slices.Equal(got, wantInstructions) {
		t.Fatalf("unexpected critique retry instructions: got %v want %v", got, wantInstructions)
	}
	if got := aiStub.regenerateConversation; !slices.Equal(got, []string{"conv-initial"}) {
		t.Fatalf("unexpected critique retry conversation IDs: got %v", got)
	}
	if len(critiquer.recipes) != 2 {
		t.Fatalf("expected two critique passes, got %d", len(critiquer.recipes))
	}
	if got := critiquer.recipes[1].Title; got != "Better Dinner" {
		t.Fatalf("expected retried recipe to be critiqued again, got %q", got)
	}
}

func TestGenerateRecipes_RetryKeepsHighScoringRecipes(t *testing.T) {
	weak := ai.Recipe{Title: "Weak Dinner", Description: "Needs work"}
	good := ai.Recipe{Title: "Solid Dinner", Description: "Already fine"}
	retried := ai.Recipe{Title: "Better Dinner", Description: "Improved"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []kroger.Ingredient{{Description: loPtr("Chicken")}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			ConversationID: "conv-initial",
			Recipes:        []ai.Recipe{weak, good},
		}},
		regenerateResponses: []*ai.ShoppingList{{
			ConversationID: "conv-retried",
			Recipes:        []ai.Recipe{retried},
		}},
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
	g := &generatorService{
		staples:      &cachedStaplesService{cache: io},
		aiClient:     aiStub,
		critiquer:    critiquer,
		statusWriter: noopstatuswriter{},
	}

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
	steady := ai.Recipe{Title: "Steady Dinner", Description: "Good enough"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []kroger.Ingredient{{Description: loPtr("Chicken")}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			ConversationID: "conv-stable",
			Recipes:        []ai.Recipe{steady},
		}},
	}
	g := &generatorService{
		staples:      &cachedStaplesService{cache: io},
		aiClient:     aiStub,
		critiquer:    &captureCritiqueService{},
		statusWriter: noopstatuswriter{},
	}

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
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []kroger.Ingredient{{Description: loPtr("Chicken")}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	statuses := &statusCounter{}
	g := &generatorService{
		staples:      &cachedStaplesService{cache: io},
		aiClient:     &sequenceAIClient{generateResponses: []*ai.ShoppingList{{ConversationID: "conv-stable", Recipes: []ai.Recipe{steady}}}},
		critiquer:    &captureCritiqueService{},
		statusWriter: statuses,
	}

	_, err := g.GenerateRecipes(t.Context(), params)
	require.NoError(t, err)
	assert.Equal(t, 2, len(statuses.status), "got statuses %v", statuses.status)
}

func TestGenerateRecipes_RegenerateRetriesLowScoringRecipesOnce(t *testing.T) {
	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	initial := ai.Recipe{Title: "Needs Work", Description: "First pass"}
	retried := ai.Recipe{Title: "Ready Now", Description: "Second pass"}

	aiStub := &sequenceAIClient{
		regenerateResponses: []*ai.ShoppingList{
			{
				ConversationID: "conv-first-pass",
				Recipes:        []ai.Recipe{initial},
			},
			{
				ConversationID: "conv-second-pass",
				Recipes:        []ai.Recipe{retried},
			},
		},
	}
	critiquer := &captureCritiqueService{
		fn: func(recipe ai.Recipe) (*ai.RecipeCritique, error) {
			switch recipe.Title {
			case "Needs Work":
				return &ai.RecipeCritique{
					SchemaVersion:  "recipe-critique-v1",
					OverallScore:   5,
					Summary:        "Too loose for a weeknight cook.",
					Strengths:      []string{"promising flavors"},
					Issues:         []ai.RecipeCritiqueIssue{{Severity: "medium", Category: "timing", Detail: "Cooking times are inconsistent."}},
					SuggestedFixes: []string{"make the timing consistent"},
				}, nil
			case "Ready Now":
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
	g := &generatorService{
		aiClient:     aiStub,
		critiquer:    critiquer,
		statusWriter: noopstatuswriter{},
	}

	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	params.ConversationID = "conv-original"
	params.Instructions = "make it vegetarian"
	params.Saved = []ai.Recipe{alreadySaved}

	got, err := g.GenerateRecipes(t.Context(), params)
	if err != nil {
		t.Fatalf("GenerateRecipes returned error: %v", err)
	}
	if got == nil || len(got.Recipes) != 2 {
		t.Fatalf("expected regenerated list plus saved recipe, got %+v", got)
	}
	if got.Recipes[0].Title != "Ready Now" || got.Recipes[1].Title != "Already Saved" {
		t.Fatalf("unexpected recipe order after critique retry: %+v", got.Recipes)
	}
	if got.ConversationID != "conv-second-pass" {
		t.Fatalf("expected final conversation ID %q, got %q", "conv-second-pass", got.ConversationID)
	}
	if aiStub.regenerateCalls != 2 {
		t.Fatalf("expected initial regenerate plus one critique retry, got %d calls", aiStub.regenerateCalls)
	}
	if got := aiStub.regenerateConversation; !slices.Equal(got, []string{"conv-original", "conv-first-pass"}) {
		t.Fatalf("unexpected regenerate conversation IDs: got %v", got)
	}
	wantRetryInstructions := []string{
		"Revise and return exactly 1 recipes as replacements for the low-scoring recipes listed below. Description should focus on selling the dish not these corrections",
		"Recipe \"Needs Work\" scored 5/10.\n Issues: [timing/medium] Cooking times are inconsistent.\n Suggested fixes: make the timing consistent",
	}
	if got := aiStub.regenerateInstructions[1]; !slices.Equal(got, wantRetryInstructions) {
		t.Fatalf("unexpected critique retry instructions: got %v want %v", got, wantRetryInstructions)
	}
}

func TestGenerateRecipes_RetriesAtMostOnceEvenIfRetryStillScoresLow(t *testing.T) {
	initial := ai.Recipe{Title: "First Try", Description: "Low score"}
	retried := ai.Recipe{Title: "Second Try", Description: "Still low"}

	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []kroger.Ingredient{{Description: loPtr("Chicken")}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	aiStub := &sequenceAIClient{
		generateResponses: []*ai.ShoppingList{{
			ConversationID: "conv-one",
			Recipes:        []ai.Recipe{initial},
		}},
		regenerateResponses: []*ai.ShoppingList{{
			ConversationID: "conv-two",
			Recipes:        []ai.Recipe{retried},
		}},
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
	g := &generatorService{
		staples:      &cachedStaplesService{cache: io},
		aiClient:     aiStub,
		critiquer:    critiquer,
		statusWriter: noopstatuswriter{},
	}

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
