# Cache Layout

This project stores cache entries in:
- Local filesystem under `cache/` (default)
- Azure Blob container `recipes` (when `AZURE_STORAGE_ACCOUNT_NAME` is set)

The same cache keys are used in both backends. Keys with `/` become subdirectories (filesystem) or blob prefixes (Azure).

## Subdirectories / Prefixes

| Prefix | Stored value | Written by | Read by |
| --- | --- | --- | --- |
| `shoppinglist/` | JSON `ai.ShoppingList` keyed by shopping hash | `internal/recipes/io.go` (`SaveShoppingList`) | `internal/recipes/io.go` (`FromCache`) |
| `ingredients/` | JSON `[]kroger.Ingredient` keyed by location hash | `internal/recipes/io.go` (`SaveIngredients`) via `internal/recipes/generator.go` (`GetStaples`) | `internal/recipes/io.go` (`IngredientsFromCache`) via `internal/recipes/generator.go` (`GetStaples`) |
| `params/` | JSON `generatorParams` keyed by shopping hash | `internal/recipes/io.go` (`SaveParams`) | `internal/recipes/io.go` (`ParamsFromCache`) |
| `recipe/` | JSON `ai.Recipe` (one recipe per hash) | `internal/recipes/io.go` (`SaveRecipes`) | `internal/recipes/io.go` (`SingleFromCache`) |
| `recipe_thread/` | JSON `[]RecipeThreadEntry` (Q/A thread for a recipe hash) | `internal/recipes/thread.go` (`SaveThread`) | `internal/recipes/thread.go` (`ThreadFromCache`) |
| `recipe_feedback/` | JSON `RecipeFeedback` (`cooked`, `stars`, `comment`, `updated_at`) per recipe hash | `internal/recipes/feedback.go` (`SaveFeedback`) via `internal/recipes/server.go` (`handleFeedback`) | `internal/recipes/feedback.go` (`FeedbackFromCache`) and `internal/recipes/server.go` (`handleSingle`, `handleFeedback`) |
| `users/` | JSON `users/types.User` by user ID | `internal/users/storage.go` (`Update`) | `internal/users/storage.go` (`GetByID`, `List`) |
| `email2user/` | Plain text user ID keyed by normalized email | `internal/users/storage.go` (`FindOrCreateFromClerk`) | `internal/users/storage.go` (`GetByEmail`) |

## Notes

- Cache backend selection is in `internal/cache/azure.go` (`MakeCache`).
- Local cache path is `cache/` when filesystem backend is used.
- Blob names in Azure match the same key strings listed above.
- Back compatibility: shopping list reads check `shoppinglist/<canonical_hash>` first, then legacy root `<legacy_seeded_shopping_hash>`. Ingredient reads check `ingredients/<canonical_hash>` first, then legacy root `<legacy_seeded_location_hash>`. Params reads check `params/<canonical_hash>` first, then legacy root `<legacy_seeded_shopping_hash>.params`.
- Hash compatibility: canonical shopping and location hashes are now raw FNV64 URL-safe base64. Legacy seeded hashes (prefixed with `recipe`/`ingredients` in decoded bytes) are still supported for reads; `/recipes?h=...` redirects legacy shopping hashes to canonical hashes.
