package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"
	"context"
	"testing"
	"time"
)

type panicKrogerClient struct {
	kroger.ClientWithResponsesInterface
}

func (panicKrogerClient) ProductSearchWithResponse(ctx context.Context, params *kroger.ProductSearchParams, reqEditors ...kroger.RequestEditorFn) (*kroger.ProductSearchResponse, error) {
	panic("unexpected call to ProductSearchWithResponse")
}

type captureWineQuestionAIClient struct {
	question    string
	answer      string
	recipeTitle string
	selection   *ai.WineSelection
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

func (c *captureWineQuestionAIClient) PickWine(ctx context.Context, conversationID string, recipeTitle string, wines []kroger.Ingredient) (*ai.WineSelection, error) {
	c.recipeTitle = recipeTitle
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

func TestWineIngredientsCacheKey_UsesStyleDateAndLocation(t *testing.T) {
	got := wineIngredientsCacheKey(" Pinot Noir ", "70500874", time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC))
	want := "wines/0XY3COdxwHk"
	if got != want {
		t.Fatalf("unexpected cache key: got %q want %q", got, want)
	}
}

func TestPickAWine_UsesCachedIngredientsForStyleDateAndLocation(t *testing.T) {
	const (
		location     = "70500874"
		conversation = "conv-1"
		style        = "Pinot Noir"
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
		io:           IO(cacheStore),
		aiClient:     aiStub,
		krogerClient: panicKrogerClient{},
	}

	got, err := g.PickAWine(t.Context(), conversation, location, ai.Recipe{
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
	if aiStub.recipeTitle != "Roast Chicken" {
		t.Fatalf("expected recipe title %q, got %q", "Roast Chicken", aiStub.recipeTitle)
	}
}
