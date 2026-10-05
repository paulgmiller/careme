package recipes

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/templates"
	"github.com/stretchr/testify/require"
)

func TestPageHeadMetadata(t *testing.T) {
	params := DefaultParams(&locations.Location{Name: "Market & Farm"}, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	recipe := ai.Recipe{Title: "Pasta & greens", Description: "A <fresh> dinner"}
	for _, tc := range []struct {
		name                   string
		shopping, empty, image bool
	}{
		{name: "recipe"},
		{name: "recipe image", image: true},
		{name: "untitled recipe", empty: true},
		{name: "shopping", shopping: true},
		{name: "empty shopping", shopping: true, empty: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var page templates.Page
			title := "Pasta &amp; greens"
			if tc.shopping {
				list := ai.ShoppingList{}
				if !tc.empty {
					list.Recipes = []ai.Recipe{recipe}
				}
				view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{params: params, list: list})
				require.NoError(t, err)
				page = view.Page
				title = "Recipes for Market &amp; Farm"
			} else {
				value := recipe
				if tc.empty {
					value.Title = ""
					title = ""
				}
				view, err := newRecipePageView(t.Context(), recipeViewInput{params: params, recipe: value, hasRecipeImage: tc.image})
				require.NoError(t, err)
				page = view.Page
			}
			var body bytes.Buffer
			require.NoError(t, templates.Recipe.ExecuteTemplate(&body, "page_head", page))
			rendered := body.String()
			require.Contains(t, rendered, "<title>"+title+" | Careme</title>")
			require.Contains(t, rendered, `<meta name="viewport"`)
			require.Contains(t, rendered, `<meta name="description"`)
			if tc.empty {
				require.NotContains(t, rendered, `property="og:title"`)
				require.NotContains(t, rendered, `name="twitter:title"`)
			} else {
				require.Contains(t, rendered, `property="og:title" content="Pasta &amp; greens"`)
				require.Contains(t, rendered, `name="twitter:description" content="A &lt;fresh&gt; dinner"`)
				path := "/favicon.ico"
				if tc.image {
					path = "/recipe/" + recipe.ComputeHash() + "/image"
				}
				require.Contains(t, rendered, path+`"`)
			}
		})
	}
}

func TestShoppingListSocialImage(t *testing.T) {
	recipes := []ai.Recipe{{Title: "Pasta"}, {Title: "Soup"}, {Title: "Salad"}}
	params := DefaultParams(&locations.Location{Name: "Market"}, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		name     string
		images   map[string]bool
		wantPath string
	}{
		{name: "no images", wantPath: "/favicon.ico"},
		{name: "later recipe has image", images: map[string]bool{recipes[0].ComputeHash(): false, recipes[1].ComputeHash(): true}, wantPath: "/recipe/" + recipes[1].ComputeHash() + "/image"},
		{name: "first available image wins", images: map[string]bool{recipes[0].ComputeHash(): true, recipes[2].ComputeHash(): true}, wantPath: "/recipe/" + recipes[0].ComputeHash() + "/image"},
		{name: "unrelated image ignored", images: map[string]bool{"unrelated": true}, wantPath: "/favicon.ico"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{params: params, list: ai.ShoppingList{Recipes: recipes}, recipeImages: tc.images})
			require.NoError(t, err)
			require.Equal(t, tc.wantPath, view.Social.ImagePath)
			require.Equal(t, recipes[0].Title, view.Social.Title)
			var body bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shoppinglist.html", view))
			for _, marker := range []string{`property="og:image" content="`, `name="twitter:image" content="`} {
				start := strings.Index(body.String(), marker)
				require.NotEqual(t, -1, start)
				value := strings.SplitN(body.String()[start+len(marker):], `"`, 2)[0]
				require.True(t, strings.HasSuffix(value, tc.wantPath), "unexpected image URL: %s", value)
			}
		})
	}
}
