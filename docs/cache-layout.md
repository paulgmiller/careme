# Cache Layout

This project stores cache entries in:
- Local filesystem under `cache/` (default app cache)
- Local filesystem under `aldi/` (ALDI cache)
- Local filesystem under `albertsons/` (Albertsons-family cache)
- Local filesystem under `publix/` (Publix cache)
- Local filesystem under `heb/` (HEB cache)
- Local filesystem under `publix/` (Publix cache)
- Local filesystem under `wholefoods/` (Whole Foods cache)
- Azure Blob container `recipes` (default app cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `aldi` (ALDI cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `albertsons` (Albertsons-family cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `publix` (Publix cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `heb` (HEB cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `publix` (Publix cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `wholefoods` (Whole Foods cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)

Within a given cache backend, keys with `/` become subdirectories (filesystem) or blob prefixes (Azure).

## Key Prefixes

| Prefix | Stored value | Written by | Read by |
| --- | --- | --- | --- |
| `shoppinglist/` | JSON `ai.ShoppingList` keyed by shopping hash | `internal/recipes/io.go` (`SaveShoppingList`) | `internal/recipes/io.go` (`FromCache`) |
| `ingredients/` | JSON `[]kroger.Ingredient` keyed by location hash (staples) or by wine style/date/location hash (wine candidate cache) | `internal/recipes/io.go` (`SaveIngredients`) via `internal/recipes/generator.go` (`GetStaples`, `PickAWine`) | `internal/recipes/io.go` (`IngredientsFromCache`) via `internal/recipes/generator.go` (`GetStaples`, `PickAWine`) |
| `params/` | JSON `generatorParams` keyed by shopping hash; params no longer embed the resolved staple filter list | `internal/recipes/io.go` (`SaveParams`) | `internal/recipes/io.go` (`ParamsFromCache`) |
| `recipe/` | JSON `ai.Recipe` (one recipe per hash) | `internal/recipes/io.go` (`SaveRecipes`) | `internal/recipes/io.go` (`SingleFromCache`) |
| `wine_recommendations/` | Plain text wine recommendation keyed by recipe hash | `internal/recipes/wine.go` (`SaveWine`) via `internal/recipes/server.go` (`handleWine`) | `internal/recipes/wine.go` (`WineFromCache`) via `internal/recipes/server.go` (`handleWine`) |
| `recipe_selection/` | JSON `recipeSelection` (`saved_hashes`, `dismissed_hashes`, `updated_at`) keyed by `<user_id>/<origin_hash>` | `internal/recipes/selection.go` (`saveRecipeSelection`) via `internal/recipes/server.go` (`handleSaveRecipe`, `handleDismissRecipe`) | `internal/recipes/selection.go` (`loadRecipeSelection`) via `internal/recipes/server.go` (`handleRegenerate`, `handleFinalize`, `handleRecipes`) |
| `conversation/` | Plain text conversation ID keyed by `<user_id>/<origin_hash>` to isolate post-generation recipe chat per signed-in user while preserving shared initial generation | `internal/recipes/conversation.go` (`saveConversationForUser`) via `internal/recipes/server.go` (`handleQuestion`, `handleWine`, `paramsForAction`) | `internal/recipes/conversation.go` (`loadConversationForUser`) via `internal/recipes/server.go` (`handleSingle`, `handleQuestion`, `handleWine`, `paramsForAction`) |
| `recipe_thread/` | JSON `[]RecipeThreadEntry` (Q/A thread for a recipe hash) | `internal/recipes/thread.go` (`SaveThread`) | `internal/recipes/thread.go` (`ThreadFromCache`) |
| `recipe_feedback/` | JSON `RecipeFeedback` (`cooked`, `stars`, `comment`, `updated_at`) per recipe hash | `internal/recipes/feedback.go` (`SaveFeedback`) via `internal/recipes/server.go` (`handleFeedback`) | `internal/recipes/feedback.go` (`FeedbackFromCache`) and `internal/recipes/server.go` (`handleSingle`, `handleFeedback`) |
| `users/` | JSON `users/types.User` by user ID | `internal/users/storage.go` (`Update`) | `internal/users/storage.go` (`GetByID`, `List`) |
| `email2user/` | Plain text user ID keyed by normalized email | `internal/users/storage.go` (`FindOrCreateFromClerk`) | `internal/users/storage.go` (`GetByEmail`) |
| `aldi/stores/` | JSON `aldi.StoreSummary` keyed by prefixed ALDI location ID | `cmd/aldi` and `internal/aldi` cache helpers | `internal/aldi` location backend |
| `albertsons/stores/` | JSON `albertsons.StoreSummary` keyed by prefixed Albertsons-family location ID | `cmd/albertsons` and `internal/albertsons` cache helpers | `internal/albertsons` location backend |
| `albertsons/store_url_map.json` | JSON object mapping store URL to prefixed Albertsons-family location ID | `cmd/albertsons` and `internal/albertsons` cache helpers | `cmd/albertsons` incremental sync |
| `heb/stores/` | JSON `heb.StoreSummary` keyed by prefixed HEB location ID | `cmd/heb` and `internal/heb` cache helpers | `internal/heb` location backend |
| `heb/store_url_map.json` | JSON object mapping store URL to prefixed HEB location ID | `cmd/heb` and `internal/heb` cache helpers | `cmd/heb` incremental sync |
| `publix/stores/` | JSON `publix.StoreSummary` keyed by numeric Publix store ID | `cmd/publix` and `internal/publix` cache helpers | `internal/publix` location backend |
| `publix/store_url_map.json` | JSON object mapping numeric Publix store ID to canonical location URL | `cmd/publix` and `internal/publix` cache helpers | `cmd/publix` incremental sync |
| `publix/missing_store_ids.json` | JSON array of numeric Publix store IDs known to redirect back to `/locations` | `cmd/publix` and `internal/publix` cache helpers | `cmd/publix` incremental sync |
| `wholefoods/stores/` | JSON `wholefoods.StoreSummaryResponse` keyed by Whole Foods store ID | `cmd/wholefoods` and `internal/wholefoods` cache helpers | `internal/wholefoods` location backend |
| `wholefoods/store_url_map.json` | JSON object mapping store URL to Whole Foods store ID | `cmd/wholefoods` and `internal/wholefoods` cache helpers | `cmd/wholefoods` when `-stores` is not provided |

## Notes

- Cache backend selection is in `internal/cache/azure.go` (`MakeCache`).
- Most app caches use the default cache created via `cache.MakeCache()` / `cache.EnsureCache("recipes")`.
- ALDI locations use a separate cache created via `cache.EnsureCache("aldi")`.
- Albertsons-family locations use a separate cache created via `cache.EnsureCache("albertsons")`.
- HEB locations use a separate cache created via `cache.EnsureCache("heb")`.
- Publix uses a separate cache created via `cache.EnsureCache("publix")`; it does not share the `recipes` container/directory.
- Whole Foods uses a separate cache created via `cache.EnsureCache("wholefoods")`; it does not share the `recipes` container/directory.
- Local cache paths are `recipes/` for most app data, `aldi/` for ALDI data, `albertsons/` for Albertsons-family data, `heb/` for HEB data, `publix/` for Publix data, and `wholefoods/` for Whole Foods data when filesystem backend is used.
- Blob names in Azure match the same key strings listed above inside their respective containers.
- Staple `ingredients/` cache keys derive from location ID, date, and a versioned backend staple signature (for example `kroger-staples-v1` or `wholefoods-staples-v1`), so Kroger and Whole Foods locations do not share staple caches and staple-definition changes can invalidate caches intentionally.
- Do not create nested keys under `recipe/<hash>` (for example `recipe/<hash>/wine`) because `FileCache` stores `recipe/<hash>` as a file path.
