# Cache Layout

This project stores cache entries in:
- Local filesystem under `cache/` (default)
- Azure Blob container `recipes` (when `AZURE_STORAGE_ACCOUNT_NAME` is set)

The same cache keys are used in both backends. Keys with `/` become subdirectories (filesystem) or blob prefixes (Azure).

## Key Prefixes

| Prefix | Stored value | Written by | Read by |
| --- | --- | --- | --- |
| `shoppinglist/` | JSON `ai.ShoppingList` keyed by shopping hash | `internal/recipes/io.go` (`SaveShoppingList`) | `internal/recipes/io.go` (`FromCache`) |
| `ingredients/` | JSON `[]kroger.Ingredient` keyed by location hash (staples) or by wine style/date/location hash (wine candidate cache) | `internal/recipes/io.go` (`SaveIngredients`) via `internal/recipes/generator.go` (`GetStaples`, `PickAWine`) | `internal/recipes/io.go` (`IngredientsFromCache`) via `internal/recipes/generator.go` (`GetStaples`, `PickAWine`) |
| `params/` | JSON `generatorParams` keyed by shopping hash | `internal/recipes/io.go` (`SaveParams`) | `internal/recipes/io.go` (`ParamsFromCache`) |
| `recipe/` | JSON `ai.Recipe` (one recipe per hash) | `internal/recipes/io.go` (`SaveRecipes`) | `internal/recipes/io.go` (`SingleFromCache`) |
| `wine_recommendations/` | Plain text wine recommendation keyed by recipe hash | `internal/recipes/wine.go` (`SaveWine`) via `internal/recipes/server.go` (`handleWine`) | `internal/recipes/wine.go` (`WineFromCache`) via `internal/recipes/server.go` (`handleWine`) |
| `recipe_selection/` | JSON `recipeSelection` (`saved_hashes`, `dismissed_hashes`, `updated_at`) keyed by `<user_id>/<origin_hash>` | `internal/recipes/selection.go` (`saveRecipeSelection`) via `internal/recipes/server.go` (`handleSaveRecipe`, `handleDismissRecipe`) | `internal/recipes/selection.go` (`loadRecipeSelection`) via `internal/recipes/server.go` (`handleRegenerate`, `handleFinalize`, `handleRecipes`) |
| `recipe_thread/` | JSON `[]RecipeThreadEntry` (Q/A thread for a recipe hash) | `internal/recipes/thread.go` (`SaveThread`) | `internal/recipes/thread.go` (`ThreadFromCache`) |
| `recipe_feedback/` | JSON `RecipeFeedback` (`cooked`, `stars`, `comment`, `updated_at`) per recipe hash | `internal/recipes/feedback.go` (`SaveFeedback`) via `internal/recipes/server.go` (`handleFeedback`) | `internal/recipes/feedback.go` (`FeedbackFromCache`) and `internal/recipes/server.go` (`handleSingle`, `handleFeedback`) |
| `users/` | JSON `users/types.User` by user ID | `internal/users/storage.go` (`Update`) | `internal/users/storage.go` (`GetByID`, `List`) |
| `email2user/` | Plain text user ID keyed by normalized email | `internal/users/storage.go` (`FindOrCreateFromClerk`) | `internal/users/storage.go` (`GetByEmail`) |

## Notes

- Cache backend selection is in `internal/cache/azure.go` (`MakeCache`).
- Local cache path is `cache/` when filesystem backend is used.
- Blob names in Azure match the same key strings listed above.
- Do not create nested keys under `recipe/<hash>` (for example `recipe/<hash>/wine`) because `FileCache` stores `recipe/<hash>` as a file path.
