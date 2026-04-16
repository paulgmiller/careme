# Cache Layout

This project stores cache entries in:
- Local filesystem under `cache/` (default app cache)
- Local filesystem under `aldi/` (ALDI cache)
- Local filesystem under `albertsons/` (Albertsons-family cache)
- Local filesystem under `publix/` (Publix cache)
- Local filesystem under `wegmans/` (Wegmans cache)
- Local filesystem under `heb/` (HEB cache)
- Local filesystem under `publix/` (Publix cache)
- Local filesystem under `wholefoods/` (Whole Foods cache)
- Local filesystem under `recipe-images/` (recipe image cache)
- Azure Blob container `recipes` (default app cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `recipe-images` (recipe image cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `aldi` (ALDI cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `albertsons` (Albertsons-family cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `publix` (Publix cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
- Azure Blob container `wegmans` (Wegmans cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)
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
| `recipe/` | JSON `ai.Recipe` (one recipe per hash) | `internal/recipes/io.go` (`SaveShoppingList`) | `internal/recipes/io.go` (`SingleFromCache`) |
| `recipe_images/` | WebP bytes for single-recipe dish images keyed by recipe hash in the dedicated `recipe-images` cache backend | `internal/recipes/image.go` (`SaveRecipeImage`) via `internal/recipes/server.go` (`POST /recipe/{hash}/image`) | `internal/recipes/image.go` (`RecipeImageFromCache`, `RecipeImageExists`) via `internal/recipes/server.go` (`GET /recipe/{hash}/image`, `handleSingle`) |
| `wine_recommendations/` | Plain text wine recommendation keyed by recipe hash | `internal/recipes/wine.go` (`SaveWine`) via `internal/recipes/server.go` (`handleWine`) | `internal/recipes/wine.go` (`WineFromCache`) via `internal/recipes/server.go` (`handleWine`) |
| `recipe_selection/` | JSON `recipeSelection` (`saved_hashes`, `dismissed_hashes`, `updated_at`) keyed by `<user_id>/<origin_hash>` | `internal/recipes/selection.go` (`saveRecipeSelection`) via `internal/recipes/server.go` (`handleSaveRecipe`, `handleDismissRecipe`) | `internal/recipes/selection.go` (`loadRecipeSelection`) via `internal/recipes/server.go` (`handleRegenerate`, `handleFinalize`, `handleRecipes`) |
| `recipe_thread/` | JSON `[]RecipeThreadEntry` (Q/A thread for a recipe hash) | `internal/recipes/thread.go` (`SaveThread`) | `internal/recipes/thread.go` (`ThreadFromCache`) |
| `recipe_feedback/` | JSON `feedback.Feedback` (`cooked`, `stars`, `comment`, `updated_at`) per recipe hash | `internal/recipes/feedback.go` (`SaveFeedback`) using `internal/recipes/feedback/model.go` (`Marshal`) via `internal/recipes/server.go` (`handleFeedback`) | `internal/recipes/feedback.go` (`FeedbackFromCache`) using `internal/recipes/feedback/model.go` (`Decode`) and `internal/recipes/server.go` (`handleSingle`, `handleFeedback`) |
| `recipe_critiques/` | JSON `ai.RecipeCritique` (`schema_version`, `overall_score`, `summary`, `strengths`, `issues`, `suggested_fixes`, `model`, `critiqued_at`) per recipe hash | `internal/recipes/critique.go` (`SaveCritique`) via `internal/recipes/generator.go` (`GenerateRecipes`) after OpenAI recipe generation/regeneration | `internal/recipes/critique.go` (`CritiqueFromCache`) for internal analysis and future tuning workflows |
| `users/` | JSON `users/types.User` by user ID | `internal/users/storage.go` (`Update`) | `internal/users/storage.go` (`GetByID`, `List`) |
| `email2user/` | Plain text user ID keyed by normalized email | `internal/users/storage.go` (`FindOrCreateFromClerk`) | `internal/users/storage.go` (`GetByEmail`) |
| `location-store-requests/` | JSON `{store_id, zip, requested_at}` for stores present in location search but not yet supported for staples | `internal/locations/locations.go` (`POST /locations/request-store`) | `internal/locations/locations.go` (`RequestedStoreIDs`) and operational triage from shared cache/blob storage |
| `aldi/stores/` | JSON `aldi.StoreSummary` keyed by prefixed ALDI location ID | `cmd/aldi` and `internal/aldi` cache helpers | `internal/aldi` location backend |
| `albertsons/stores/` | JSON `albertsons.StoreSummary` keyed by prefixed Albertsons-family location ID | `cmd/albertsons` and `internal/albertsons` cache helpers | `internal/albertsons` location backend |
| `albertsons/store_locations.json` | JSON `[]storeindex.Entry` spatial index for Albertsons-family stores (`id`, `lat`, `lon`) | `cmd/albertsons` rebuilds after sync | `internal/albertsons` location backend |
| `albertsons/store_url_map.json` | JSON object mapping store URL to prefixed Albertsons-family location ID | `cmd/albertsons` and `internal/albertsons` cache helpers | `cmd/albertsons` incremental sync |
| `albertsons/reese84/latest.json` | JSON `albertsons.Reese84Record` containing the freshest ACME/Albertsons-family `reese84` cookie plus metadata | `cmd/albertsonsreese84` | `internal/albertsons` staples/search cookie resolver |
| `albertsons/reese84/history/` | JSON `albertsons.Reese84Record` append-only history keyed by fetch timestamp | `cmd/albertsonsreese84` | Operational debugging and manual rollback/reference |
| `aldi/store_locations.json` | JSON `[]storeindex.Entry` spatial index for ALDI stores (`id`, `lat`, `lon`) | `cmd/aldi` rebuilds after sync | `internal/aldi` location backend |
| `heb/stores/` | JSON `heb.StoreSummary` keyed by prefixed HEB location ID | `cmd/heb` and `internal/heb` cache helpers | `internal/heb` location backend |
| `heb/store_locations.json` | JSON `[]storeindex.Entry` spatial index for HEB stores (`id`, `lat`, `lon`) | `cmd/heb` rebuilds after sync | `internal/heb` location backend |
| `heb/store_url_map.json` | JSON object mapping store URL to prefixed HEB location ID | `cmd/heb` and `internal/heb` cache helpers | `cmd/heb` incremental sync |
| `publix/stores/` | JSON `publix.StoreSummary` keyed by numeric Publix store ID | `cmd/publix` and `internal/publix` cache helpers | `internal/publix` location backend |
| `publix/store_locations.json` | JSON `[]storeindex.Entry` spatial index for Publix stores (`id`, `lat`, `lon`) | `cmd/publix` rebuilds after sync | `internal/publix` location backend |
| `publix/store_url_map.json` | JSON object mapping numeric Publix store ID to canonical location URL | `cmd/publix` and `internal/publix` cache helpers | `cmd/publix` incremental sync |
| `publix/missing_store_ids.json` | JSON array of numeric Publix store IDs known to redirect back to `/locations` | `cmd/publix` and `internal/publix` cache helpers | `cmd/publix` incremental sync |
| `wegmans/stores/` | JSON `wegmans.StoreSummary` keyed by numeric Wegmans store ID | `cmd/wegmans` and `internal/wegmans` cache helpers | `internal/wegmans` location backend |
| `wegmans/store_locations.json` | JSON `[]storeindex.Entry` spatial index for Wegmans stores (`id`, `lat`, `lon`) | `cmd/wegmans` rebuilds after sync | `internal/wegmans` location backend |
| `wholefoods/stores/` | JSON `wholefoods.StoreSummaryResponse` keyed by Whole Foods store ID | `cmd/wholefoods` and `internal/wholefoods` cache helpers | `internal/wholefoods` location backend |
| `wholefoods/store_locations.json` | JSON `[]storeindex.Entry` spatial index for Whole Foods stores (`id`, `lat`, `lon`) | `cmd/wholefoods` rebuilds after sync | `internal/wholefoods` location backend |
| `wholefoods/store_url_map.json` | JSON object mapping store URL to Whole Foods store ID | `cmd/wholefoods` and `internal/wholefoods` cache helpers | `cmd/wholefoods` when `-stores` is not provided |

## Notes

- Cache backend selection is in `internal/cache/azure.go` (`MakeCache`).
- Most app caches use the default cache created via `cache.MakeCache()` / `cache.EnsureCache("recipes")`.
- ALDI locations use a separate cache created via `cache.EnsureCache("aldi")`.
- Albertsons-family locations use a separate cache created via `cache.EnsureCache("albertsons")`.
- Albertsons-family `reese84` cookie refresh also uses `cache.EnsureCache("albertsons")`; the latest record is overwritten while timestamped history remains append-only.
- Wegmans locations use a separate cache created via `cache.EnsureCache("wegmans")`.
- HEB locations use a separate cache created via `cache.EnsureCache("heb")`.
- Publix uses a separate cache created via `cache.EnsureCache("publix")`; it does not share the `recipes` container/directory.
- Recipe images use a separate cache created via `cache.EnsureCache("recipe-images")`; they do not share the main `recipes` container/directory.
- Whole Foods uses a separate cache created via `cache.EnsureCache("wholefoods")`; it does not share the `recipes` container/directory.
- Local cache paths when filesystem backend is used. are 
  - `recipes/` for most app data, 
  - `recipe-images/` for recipe images,
  - `aldi/` for ALDI data, 
  - `albertsons/` for Albertsons-family data, 
  - `heb/` for HEB data, 
  - `publix/` for Publix data, 
  - `wegmans/` for Wegmans data
  - `wholefoods/` for Whole Foods data 
- Blob names in Azure match the same key strings listed above inside their respective containers.
- Staple `ingredients/` cache keys derive from location ID, date, and a versioned backend staple signature (for example `kroger-staples-v1` or `wholefoods-staples-v1`), so Kroger and Whole Foods locations do not share staple caches and staple-definition changes can invalidate caches intentionally.
- Recipe image cache keys are stable per recipe hash, so prompt or model changes do not orphan previously generated images.
- Do not create nested keys under `recipe/<hash>` (for example `recipe/<hash>/wine`) because `FileCache` stores `recipe/<hash>` as a file path.
