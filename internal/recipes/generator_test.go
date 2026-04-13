package recipes

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"
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

type captureCritiquer struct {
	mu      sync.Mutex
	err     error
	recipes []ai.Recipe
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

func (c *captureCritiquer) CritiqueRecipe(ctx context.Context, recipe ai.Recipe) (*ai.RecipeCritique, error) {
	c.mu.Lock()
	c.recipes = append(c.recipes, recipe)
	c.mu.Unlock()
	if c.err != nil {
		return nil, c.err
	}
	return &ai.RecipeCritique{
		SchemaVersion:  "recipe-critique-v1",
		OverallScore:   7,
		Summary:        "Solid draft.",
		Strengths:      []string{"clear direction"},
		Issues:         []ai.RecipeCritiqueIssue{{Severity: "medium", Category: "timing", Detail: "Timing could be tighter."}},
		SuggestedFixes: []string{"tighten the timing"},
	}, nil
}

func (c *captureCritiquer) Ready(ctx context.Context) error {
	return c.err
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
	g := &Generator{
		io:       IO(cacheStore),
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
	g := &Generator{
		io:              IO(cache.NewFileCache(t.TempDir())),
		aiClient:        aiStub,
		staplesProvider: staplesStub,
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
	g := &Generator{
		io:       IO(cache.NewFileCache(t.TempDir())),
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

func TestGenerateRecipes_SavesCritiquesForGeneratedRecipes(t *testing.T) {
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
	critiquer := &captureCritiquer{}
	g := &Generator{
		io:        io,
		cio:       io,
		aiClient:  aiStub,
		critiquer: critiquer,
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
	for _, recipe := range generated {
		critique, err := io.CritiqueFromCache(t.Context(), recipe.ComputeHash())
		if err != nil {
			t.Fatalf("expected critique for %q: %v", recipe.Title, err)
		}
		if critique.Summary != "Solid draft." {
			t.Fatalf("unexpected critique summary for %q: %#v", recipe.Title, critique)
		}
	}
}

func TestGenerateRecipes_CritiqueFailuresFailGeneration(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store"}, time.Now())
	if err := io.SaveIngredients(t.Context(), params.LocationHash(), []kroger.Ingredient{{Description: loPtr("Chicken")}}); err != nil {
		t.Fatalf("failed to seed ingredients cache: %v", err)
	}

	recipe := ai.Recipe{Title: "Roast Chicken", Description: "Crisp and simple", Instructions: []string{"Roast the chicken."}}
	g := &Generator{
		io:  io,
		cio: io,
		aiClient: &captureGenerateAIClient{
			shoppingList: &ai.ShoppingList{
				ConversationID: "conv-123",
				Recipes:        []ai.Recipe{recipe},
			},
		},
		critiquer: &captureCritiquer{err: errors.New("gemini down")},
	}

	got, err := g.GenerateRecipes(t.Context(), params)
	if err == nil {
		t.Fatal("expected GenerateRecipes to fail when critique caching fails")
	}
	if got != nil {
		t.Fatalf("expected no shopping list on critique failure, got %+v", got)
	}
	if _, err := io.CritiqueFromCache(t.Context(), recipe.ComputeHash()); !errors.Is(err, cache.ErrNotFound) {
		t.Fatalf("expected no cached critique after failure, got %v", err)
	}
}

func TestGenerateRecipes_RegenerateCritiquesOnlyFreshRecipes(t *testing.T) {
	alreadySaved := ai.Recipe{Title: "Already Saved", Description: "Saved earlier"}
	newResult := ai.Recipe{Title: "Brand New Dinner", Description: "Fresh idea"}

	critiquer := &captureCritiquer{}
	g := &Generator{
		io:        IO(cache.NewInMemoryCache()),
		cio:       IO(cache.NewInMemoryCache()),
		aiClient:  &captureRegenerateAIClient{shoppingList: &ai.ShoppingList{ConversationID: "conv-123", Recipes: []ai.Recipe{newResult}}},
		critiquer: critiquer,
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
