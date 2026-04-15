package recipes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/recipes/critique"
)

func TestAdminCritiquesPageRendersNewestFirst(t *testing.T) {
	t.Parallel()

	fc := cache.NewFileCache(t.TempDir())
	rio := IO(fc)

	recipes := []ai.Recipe{
		{
			Title:        "Spring Chicken",
			Description:  "Bright and quick.",
			CookTime:     "35 minutes",
			CostEstimate: "$18-24",
			Ingredients: []ai.Ingredient{
				{Name: "Chicken thighs", Quantity: "2 lb", Price: "$9.99"},
			},
			Instructions: []string{"Season the chicken.", "Roast until cooked through."},
			Health:       "Balanced dinner.",
			DrinkPairing: "Chardonnay",
		},
		{
			Title:        "Herby Beans",
			Description:  "Comforting and savory.",
			CookTime:     "25 minutes",
			CostEstimate: "$12-16",
			Ingredients: []ai.Ingredient{
				{Name: "Cannellini beans", Quantity: "2 cans", Price: "$4.99"},
			},
			Instructions: []string{"Warm the beans.", "Finish with herbs."},
			Health:       "Fiber rich.",
			DrinkPairing: "Pinot Grigio",
		},
	}
	saveRecipesForOrigin(t, rio, "origin-hash", recipes...)
	critiqueStore := critique.NewStore(fc)

	newestHash := recipes[0].ComputeHash()
	olderHash := recipes[1].ComputeHash()

	if err := critiqueStore.Save(t.Context(), newestHash, &ai.RecipeCritique{
		SchemaVersion:  "recipe-critique-v1",
		OverallScore:   9,
		Summary:        "Strong weeknight draft.",
		Strengths:      []string{"clear sequencing", "good contrast"},
		Issues:         []ai.RecipeCritiqueIssue{{Severity: "high", Category: "timing", Detail: "Rest the chicken before slicing."}},
		SuggestedFixes: []string{"Add a two minute resting step before plating."},
		Model:          "gemini-3.1-pro-preview",
		CritiquedAt:    time.Date(2026, time.April, 13, 20, 15, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("save newest critique: %v", err)
	}
	if err := critiqueStore.Save(t.Context(), olderHash, &ai.RecipeCritique{
		SchemaVersion:  "recipe-critique-v1",
		OverallScore:   6,
		Summary:        "Needs more brightness.",
		Strengths:      []string{"budget friendly"},
		Issues:         []ai.RecipeCritiqueIssue{{Severity: "medium", Category: "flavor", Detail: "Add acid near the end."}},
		SuggestedFixes: []string{"Finish with lemon juice."},
		Model:          "gemini-3.1-pro-preview",
		CritiquedAt:    time.Date(2026, time.April, 11, 14, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("save older critique: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/critiques", nil)
	rr := httptest.NewRecorder()

	AdminCritiquesPage(fc).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content-type = %q, want text/html", got)
	}

	body := rr.Body.String()
	for _, want := range []string{
		"Spring Chicken",
		"Herby Beans",
		"Strong weeknight draft.",
		"Needs more brightness.",
		"Rest the chicken before slicing.",
		"Finish with lemon juice.",
		"/recipe/" + newestHash,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response body missing %q: %s", want, body)
		}
	}
}

func TestAdminCritiquesPageMethodNotAllowed(t *testing.T) {
	t.Parallel()

	fc := cache.NewFileCache(t.TempDir())

	req := httptest.NewRequest(http.MethodPost, "/critiques", nil)
	rr := httptest.NewRecorder()

	AdminCritiquesPage(fc).ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
}
