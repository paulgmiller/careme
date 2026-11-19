package recipes

import (
	"bytes"
	"html"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/templates"
	"github.com/stretchr/testify/require"
)

func TestPageHeadMetadata(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{PublicOrigin: "https://test.careme.cooking"}))
	t.Cleanup(func() { require.NoError(t, templates.Init(&config.Config{})) })
	params := DefaultParams(&locations.Location{Name: "Market & Farm"}, time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC))
	recipe := ai.Recipe{Title: "Pasta & greens", Description: "A <fresh> dinner"}
	for _, tc := range []struct {
		name                   string
		shopping, empty, image bool
	}{
		{name: "recipe"},
		{name: "recipe image", image: true},
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
				view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{params: params, list: list, hash: "list_hash=="})
				require.NoError(t, err)
				page = view.Page
				title = "Recipes for Market &amp; Farm"
				if !tc.empty {
					require.Equal(t, "2026-09-15: Pasta & greens.", page.Description)
				} else {
					require.Equal(t, "2026-09-15.", page.Description)
				}
			} else {
				view, err := newRecipePageView(t.Context(), recipeViewInput{params: params, recipe: recipe, hasRecipeImage: tc.image})
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
				require.Contains(t, rendered, `property="og:title" content="`+html.EscapeString(page.Title)+`"`)
				require.Contains(t, rendered, `name="twitter:title" content="`+html.EscapeString(page.Title)+`"`)
				require.Contains(t, rendered, `property="og:description" content="`+html.EscapeString(page.Description)+`"`)
				require.Contains(t, rendered, `name="twitter:description" content="`+html.EscapeString(page.Description)+`"`)
				require.Contains(t, rendered, "/recipe/"+recipe.ComputeHash()+"/image"+`"`)
				require.NotContains(t, rendered, "/favicon.ico")
				path := "/recipe/" + recipe.ComputeHash()
				if tc.shopping {
					path = "/recipes?h=list_hash%3D%3D"
				}
				require.Contains(t, rendered, `property="og:url" content="https://test.careme.cooking`+path+`"`)
				imageURL := "https://test.careme.cooking/cdn-cgi/image/width=480,quality=75,format=jpeg,onerror=redirect/recipe/" + recipe.ComputeHash() + "/image"
				require.Contains(t, rendered, `property="og:image" content="`+imageURL+`"`)
				require.Contains(t, rendered, `name="twitter:image" content="`+imageURL+`"`)
				var thumbnail bytes.Buffer
				require.NoError(t, templates.Recipe.ExecuteTemplate(&thumbnail, "recipe_image", recipeImageView{
					Hash: recipe.ComputeHash(), HasImage: true, Thumbnail: true,
				}))
				require.Contains(t, thumbnail.String(), `src="`+strings.TrimPrefix(imageURL, "https://test.careme.cooking")+`"`)
				require.Contains(t, rendered, `property="og:image:type" content="image/jpeg"`)
				require.Contains(t, rendered, `property="og:image:width" content="480"`)
				require.Contains(t, rendered, `property="og:image:height" content="480"`)
				require.Contains(t, rendered, `property="og:image:alt" content="Generated image of Pasta &amp; greens"`)
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
		{name: "no images", wantPath: "/recipe/" + recipes[0].ComputeHash() + "/image"},
		{name: "first recipe remains stable while image is pending", images: map[string]bool{recipes[0].ComputeHash(): false, recipes[1].ComputeHash(): true}, wantPath: "/recipe/" + recipes[0].ComputeHash() + "/image"},
		{name: "first available image wins", images: map[string]bool{recipes[0].ComputeHash(): true, recipes[2].ComputeHash(): true}, wantPath: "/recipe/" + recipes[0].ComputeHash() + "/image"},
		{name: "unrelated image ignored", images: map[string]bool{"unrelated": true}, wantPath: "/recipe/" + recipes[0].ComputeHash() + "/image"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view, err := newShoppingListPageView(t.Context(), shoppingListViewInput{params: params, list: ai.ShoppingList{Recipes: recipes}, recipeImages: tc.images})
			require.NoError(t, err)
			require.Equal(t, tc.wantPath, "/recipe/"+view.Social.ImageHash+"/image")
			require.Equal(t, "Generated image of "+recipes[0].Title, view.Social.ImageAlt)
			var body bytes.Buffer
			require.NoError(t, templates.ShoppingList.ExecuteTemplate(&body, "shoppinglist.html", view))
			for _, marker := range []string{`property="og:image" content="`, `name="twitter:image" content="`} {
				start := strings.Index(body.String(), marker)
				require.NotEqual(t, -1, start)
				value, _, _ := strings.Cut(body.String()[start+len(marker):], `"`)
				require.True(t, strings.HasSuffix(value, tc.wantPath), "unexpected image URL: %s", value)
			}
		})
	}
}
