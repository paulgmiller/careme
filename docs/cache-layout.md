# Cache Layout

This project stores cache entries in:
- Local filesystem under `cache/` (default)
- Azure Blob container `recipes` (when `AZURE_STORAGE_ACCOUNT_NAME` is set)

The same cache keys are used in both backends. Keys with `/` become subdirectories (filesystem) or blob prefixes (Azure).

## Subdirectories / Prefixes

| Prefix | Stored value | Written by | Read by |
| --- | --- | --- | --- |
| `recipe/` | JSON `ai.Recipe` (one recipe per hash) | `internal/recipes/io.go` (`SaveRecipes`) | `internal/recipes/io.go` (`SingleFromCache`) |
| `recipe_thread/` | JSON `[]RecipeThreadEntry` (Q/A thread for a recipe hash) | `internal/recipes/thread.go` (`SaveThread`) | `internal/recipes/thread.go` (`ThreadFromCache`) |
| `recipe_feedback/` | JSON `RecipeFeedback` (`cooked`, `stars`, `comment`, `updated_at`) per recipe hash | `internal/recipes/feedback.go` (`SaveFeedback`) via `internal/recipes/server.go` (`handleFeedback`) | `internal/recipes/feedback.go` (`FeedbackFromCache`) and `internal/recipes/server.go` (`handleSingle`, `handleFeedback`) |
| `users/` | JSON `users/types.User` by user ID | `internal/users/storage.go` (`Update`) | `internal/users/storage.go` (`GetByID`, `List`) |
| `email2user/` | Plain text user ID keyed by normalized email | `internal/users/storage.go` (`FindOrCreateFromClerk`) | `internal/users/storage.go` (`GetByEmail`) |

## Root-level keys (no subdirectory)

Not all cache entries live in a prefixed subdirectory:

- `<shopping_hash>`: JSON `ai.ShoppingList`  
  written by `internal/recipes/io.go` (`SaveShoppingList`), read by `internal/recipes/io.go` (`FromCache`)

- `<shopping_hash>.params`: JSON `generatorParams` used to reconstruct request state  
  written by `internal/recipes/io.go` (`SaveParams`), read by `internal/recipes/params.go` (`loadParamsFromHash`)

- `<location_hash>`: JSON `[]kroger.Ingredient` (location/date staple ingredient pool)  
  written and read by `internal/recipes/generator.go` (`GetStaples`)

## Notes

- Cache backend selection is in `internal/cache/azure.go` (`MakeCache`).
- Local cache path is `cache/` when filesystem backend is used.
- Blob names in Azure match the same key strings listed above.
