package main

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/config"
	"careme/internal/recipes"
	"careme/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestCockroachRecipeAndUserStorageIntegration(t *testing.T) {
	databaseURL := os.Getenv("COCKROACH_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("COCKROACH_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	name := "storage-test-" + uuid.NewString()
	_, err = db.ExecContext(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.ExecContext(ctx, `DROP DATABASE `+pgx.Identifier{name}.Sanitize()+` CASCADE`)
		require.NoError(t, err)
	})
	testURL, err := url.Parse(databaseURL)
	require.NoError(t, err)
	testURL.Path = "/" + name

	recipeStore, err := recipes.NewCockroachStorage(ctx, testURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, recipeStore.Close()) })
	userStore, err := users.NewCockroachStorage(ctx, testURL.String())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, userStore.Close()) })

	recipe := ai.Recipe{
		Title: "Soup", Description: "Dinner",
		Ingredients:  []ai.Ingredient{{Name: "Carrot", Quantity: "1", Price: "1.00"}},
		Instructions: []string{"Slice carrots", "Simmer"},
	}
	require.NoError(t, recipeStore.SaveRecipe(ctx, recipe))
	got, err := recipeStore.SingleFromCache(ctx, recipe.ComputeHash())
	require.NoError(t, err)
	require.Equal(t, recipe, *got)
	list := &ai.ShoppingList{Recipes: []ai.Recipe{recipe}}
	require.NoError(t, recipeStore.SaveShoppingList(ctx, list, "dinner"))
	gotList, err := recipeStore.FromCache(ctx, "dinner")
	require.NoError(t, err)
	require.Equal(t, list, gotList)

	authClient := auth.Mock(&config.Config{Mocks: config.MockConfig{Email: "chef@example.com"}})
	user, err := userStore.FromRequest(ctx, httptest.NewRequest("GET", "/", nil), authClient)
	require.NoError(t, err)
	byID, err := userStore.GetByID(user.ID)
	require.NoError(t, err)
	require.Equal(t, user.ID, byID.ID)
	require.Equal(t, user.Email, byID.Email)
	byEmail, err := userStore.GetByEmail("CHEF@EXAMPLE.COM")
	require.NoError(t, err)
	require.Equal(t, user.ID, byEmail.ID)
	allUsers, err := userStore.List(ctx)
	require.NoError(t, err)
	require.Len(t, allUsers, 1)
	require.Equal(t, user.ID, allUsers[0].ID)

	// The stores own independent pools; closing one leaves the other usable.
	require.NoError(t, userStore.Close())
	_, err = userStore.GetByID(user.ID)
	require.Error(t, err)
	_, err = recipeStore.SingleFromCache(ctx, recipe.ComputeHash())
	require.NoError(t, err)
	require.NoError(t, recipeStore.Close())
	_, err = recipeStore.SingleFromCache(ctx, recipe.ComputeHash())
	require.Error(t, err)
}
