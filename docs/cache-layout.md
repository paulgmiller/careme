# Cache Layout

MNFoodClub, HEB, and Whole Foods keep their existing signature schemes for size
mapping changes. Size fields may be empty in older cached catalogs or when the
source does not provide a size; cached catalogs gain size data when fetched again.
Existing finalized shopping lists retain their original snapshots. Farmers market
sizes are extracted on new photo uploads; existing photo inventories are not
reanalyzed automatically.

This project stores cache entries in:
- Local filesystem under `cache/` (default app cache)
- Local filesystem under `aldi/` (ALDI cache)
- Local filesystem under `albertsons/` (Albertsons-family cache)
- Local filesystem under `publix/` (Publix cache)
- Local filesystem under `wegmans/` (Wegmans cache)
- Local filesystem under `heb/` (HEB cache)
- Local filesystem under `publix/` (Publix cache)
- Local filesystem under `wholefoods/` (Whole Foods cache)
- Local filesystem under `farmersmarket/` (Farmers Market cache)
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
- Azure Blob container `farmersmarket` (Farmers Market cache when `AZURE_STORAGE_ACCOUNT_NAME` is set)

Within a given cache backend, keys with `/` become subdirectories (filesystem) or blob prefixes (Azure).

## Key Prefixes

| Prefix | Stored value | Written by | Read by |
| --- | --- | --- | --- |
| `location/` | JSON `locations.Location` keyed by store ID for providers without a location-cache version | `internal/locations/storage.go` location lookups and searches | `internal/locations/storage.go` lookup by ID |
| `location/kroger-v2/` | JSON `locations.Location` keyed by Kroger store ID, preserving the API chain name | Kroger location lookups and searches via `internal/locations/storage.go` | Kroger lookup by ID via `internal/locations/storage.go` |
| `shoppinglist/` | JSON `ai.ShoppingList` keyed by shopping hash | `internal/recipes/io.go` (`SaveShoppingList`) | `internal/recipes/io.go` (`FromCache`) |
| `bigcommerce_cart_transfers/` | Transfer status, item fingerprint, cart ID, and matched/unmatched counts keyed by authenticated user and store/location/list hash | BigCommerce order handler | BigCommerce order and authenticated cart-review handlers |
| `instacart_shopping_links/` | Shopping-list URL and expiry keyed by authenticated user ID and SHA-256 of the outbound payload plus API environment | Instacart order handler | Instacart order handler; reuse until 30-day expiry or changed content |
| `kroger_cart_tokens/` | AES-GCM encrypted Kroger OAuth tokens keyed by Careme user ID | Kroger order handler OAuth callback and refresh | Kroger order handler |
| `kroger_cart_transfers/` | JSON transfer status, submitted product names, and skipped ingredients keyed by Careme user ID and finalized list hash | Kroger order handler | Duplicate prevention and return status |
| `kroger_order_requests/` | Posted shopping items, source store/chain, list key, local return URL, authenticated user ID, and server-issued timestamp keyed by random OAuth state | Kroger order handler before authorization | Kroger callback; accepted only for the same authenticated user within ten minutes |
| `shopping_quantities/v7/` | JSON `[]shoppingPurchaseGroup` containing merged amounts, nullable buy quantities, product names, sizes, and aisle details for food only (wine picks are appended at page render) keyed directly by finalized shopping-list hash (`shopping_quantities/v7/<hash>`) | Finalize (Build Shopping List) handler | Shopping-list page handler (which posts displayed food and wine to Kroger) |
| `ingredients/` | JSON `[]ai.InputIngredient` keyed by location hash for staple caches, or by location/date/normalized wine style set for wine candidate caches | `internal/recipes/io.go` (`SaveInputIngredients`, `SaveIngredients`) via `internal/recipes/staples.go` (`FetchStaples`, `FetchWines`) | `internal/recipes/io.go` (`InputIngredientsFromCache`, `IngredientsFromCache`) via `internal/recipes/staples.go` (`FetchStaples`, `FetchWines`) and `internal/ingredients/server.go` |
| `pantry/query-categories-v1/` | JSON `[]ai.InputIngredient` keyed by the store-day location hash; categories include each originating Kroger pantry search term. This version bypasses older pantry snapshots without query categories. | `internal/recipes/staples.go` (`FetchPantry`) | `internal/recipes/staples.go` (`FetchPantry`) |
| `params/` | JSON `generatorParams` keyed by shopping hash; params no longer embed the resolved staple filter list | `internal/recipes/io.go` (`SaveParams`) | `internal/recipes/io.go` (`ParamsFromCache`) |
| `generation_status/` | JSON generation attempt (`started_at`, optional rolling `message`, optional reported `error`, optional result `redirect`, ordered `slots` containing meal-plan metadata and a final `recipe_hash` once each recipe finishes generation and revision) keyed by shopping hash for a full plan or by the URL-safe regeneration ID for a single-recipe update; older JSON records without slots show the planning stage | `internal/recipes/status/generation_status.go` via full-plan and single-recipe generation handlers, with progress updates from `internal/recipes/generator.go` | `internal/recipes/status/generation_status.go` via progressive shopping-list polling, single-recipe spinner, result redirect, and retry handling |
| `recipe_prompts/` | JSON `ai.PromptRecord` (`created_at`, `response_id`, `model`, optional `instructions`, optional `previous_response_id`, OpenAI `input`) keyed by `<response_id>.json` for recipe generation evals | `internal/recipes/prompts/recorder.go` via `internal/ai/client.go` for successful initial generation and regeneration responses | Admin prompt endpoints in `internal/recipes/prompts/admin.go` and eval-building workflows that find the response ID on `shoppinglist/` records, then join prompt fields with `recipe_critiques/` |
| `recipe/` | JSON `ai.Recipe` (one recipe per hash) | `internal/recipes/io.go` (`SaveShoppingList`) | `internal/recipes/io.go` (`SingleFromCache`) |
| `recipe_images/` | WebP bytes for single-recipe dish images keyed by recipe hash in the dedicated `recipe-images` cache backend | `internal/recipes/image.go` (`SaveRecipeImage`) via `internal/recipes/server.go` (`POST /recipe/{hash}/image`) | `internal/recipes/image.go` (`RecipeImageFromCache`, `RecipeImageExists`) via `internal/recipes/server.go` (`GET /recipe/{hash}/image`, `handleSingle`) |
| `recipes/steps/` in the `recipe-images` backend | Generated instruction sketch bytes keyed by `<recipe_hash>/<one-based_step_number>` | `internal/recipes/step_image_controller.go` after a signed-in step image request | Recipe page and public step image endpoint |
| `wine_recommendations/` | Plain text wine recommendation keyed by recipe hash | `internal/recipes/wine.go` (`SaveWine`) via `internal/recipes/server.go` (`handleWine`) | `internal/recipes/wine.go` (`WineFromCache`) via `internal/recipes/server.go` (`handleWine`) |
| `recipe_selection/` | JSON `recipeSelection` (`saved_hashes`, `dismissed_hashes`, `updated_at`) keyed by `<user_id>/<origin_hash>` | `internal/recipes/selection.go` (`saveRecipeSelection`) via `internal/recipes/server.go` (`handleSaveRecipe`, `handleDismissRecipe`) | `internal/recipes/selection.go` (`loadRecipeSelection`) via `internal/recipes/server.go` (`handleRegenerate`, `handleFinalize`, `handleRecipes`) |
| `recipe_thread/` | JSON `[]RecipeThreadEntry` (Q/A thread for a recipe hash) | `internal/recipes/thread.go` (`SaveThread`) | `internal/recipes/thread.go` (`ThreadFromCache`) |
| `recipe_feedback/` | JSON `feedback.Feedback` (`cooked`, `stars`, `comment`, `updated_at`) per recipe hash | `internal/recipes/feedback.go` (`SaveFeedback`) using `internal/recipes/feedback/model.go` (`Marshal`) via `internal/recipes/server.go` (`handleFeedback`) | `internal/recipes/feedback.go` (`FeedbackFromCache`) using `internal/recipes/feedback/model.go` (`Decode`) and `internal/recipes/server.go` (`handleSingle`, `handleFeedback`) |
| `recipe_critiques/` | JSON `ai.RecipeCritique` (`schema_version`, `overall_score`, `summary`, `strengths`, `issues`, `suggested_fixes`, `model`, `critiqued_at`) per recipe hash | `internal/recipes/critique.go` (`SaveCritique`) via `internal/recipes/generator.go` (`GenerateRecipes`) after OpenAI recipe generation/regeneration | `internal/recipes/critique.go` (`CritiqueFromCache`) for internal analysis and future tuning workflows |
| `ingredient_grades/` | JSON `ai.InputIngredient` with embedded `grade` (`score`, `reason`); embeddings are excluded keyed by `<cache_version>/<ingredient_hash>` | `internal/ingredients/grading/store.go` (`Save`) via `internal/ingredients/grading/cache.go` (`GradeIngredients`) during recipe ingredient prioritization and admin inspection | `internal/ingredients/grading/store.go` (`Load`) via `internal/ingredients/grading/cache.go` (`GradeIngredients`) and `internal/ingredients/server.go` (`GET /ingredients/{hash}/graded`) |
| `ingredient_embeddings/` | JSON embedding vector keyed by `<embedding_model>/<dimensions>/description-v1/<SHA256(trimmed_description)>` | `internal/ingredients/embeddings` after grading | `internal/ingredients/embeddings`, independently of grade cache hits |
| `ingredient_grade_reviews/` | JSON `gradereview.Review` with the graded ingredient snapshot, human verdict (`too_high`, `correct`, or `too_low`), and review time, keyed by the matching `<cache_version>/<ingredient_hash>` | Standalone `cmd/ingredientreview` web app | Offline ingredient-grade evaluation and calibration workflows |
| `locations/` in the `farmersmarket` backend | JSON shared farmers market metadata (`id`, submitted names, average lat/lon, nearest ZIP, photo count, timestamps) keyed by farmers market location ID | `internal/farmersmarket` upload handler/store | `internal/farmersmarket` location backend and upload merge logic |
| `inventory/` in the `farmersmarket` backend | JSON `{cached_at, ingredients}` keyed by `<farmersmarket_location_id>/<YYYY-MM-DD>.json`; item brand is the visible farm/stall/store name when available, otherwise `Farmers market` | `internal/farmersmarket` upload handler/store after GPT image extraction | `internal/farmersmarket` staples provider reads the freshest cached list from the last 24 hours via recipe generation |
| `analysis_jobs/` in the `farmersmarket` backend | JSON farmers market photo analysis progress (`user_id`, `state`, photo/ingredient counts, message, redirect URL, error, timestamps) keyed by random upload job ID | `internal/farmersmarket` htmx upload handler while photo analysis runs | `internal/farmersmarket` status polling endpoint so any web replica can render progress |
| `users/` | JSON `users/types.User` by user ID, including up to two seven-day shopping-list pointers labeled with store names, an optional `partner_id`, and an optional incoming `pending_partner_id`; the reserved partner-sharing-disabled ID occupies the partner slot without identifying another user | `internal/users/storage.go` (`Update` and partner relationship operations) | `internal/users/storage.go` (`GetByID`, `List`, and partner lookup) |
| `email2user/` | Plain text user ID keyed by normalized email | `internal/users/storage.go` (`FindOrCreateFromClerk`) | `internal/users/storage.go` (`GetByEmail`) |
| `location-store-requests/` | JSON `{store_id, zip, requested_at}` for stores present in location search but not yet supported for staples | `internal/locations/locations.go` (`POST /locations/request-store`) | `internal/locations/locations.go` (`RequestedStoreIDs`) and operational triage from shared cache/blob storage |
| `aldi/stores/` | JSON `aldi.StoreSummary` keyed by prefixed ALDI location ID | `cmd/aldi` and `internal/providers/aldi` cache helpers | `internal/providers/aldi` location backend |
| `albertsons/stores/` | JSON `albertsons.StoreSummary` keyed by prefixed Albertsons-family location ID | `cmd/albertsons` and `internal/providers/albertsons` cache helpers | `internal/providers/albertsons` location backend |
| `albertsons/store_locations.json` | JSON `[]storeindex.Entry` spatial index for Albertsons-family stores (`id`, `lat`, `lon`) | `cmd/albertsons` rebuilds after sync | `internal/providers/albertsons` location backend |
| `albertsons/store_url_map.json` | JSON object mapping store URL to prefixed Albertsons-family location ID | `cmd/albertsons` and `internal/providers/albertsons` cache helpers | `cmd/albertsons` incremental sync |
| `albertsons/reese84/latest.json` | JSON `albertsons.CookieRecord` containing the freshest ACME/Albertsons-family `reese84` cookie plus metadata | `cmd/reese84 -site albertsons -container albertsons` | `internal/providers/albertsons` staples/search cookie resolver |
| `albertsons/reese84/history/` | JSON `albertsons.CookieRecord` append-only history keyed by fetch timestamp | `cmd/reese84 -site albertsons -container albertsons` | Operational debugging and manual rollback/reference |
| `aldi/store_locations.json` | JSON `[]storeindex.Entry` spatial index for ALDI stores (`id`, `lat`, `lon`) | `cmd/aldi` rebuilds after sync | `internal/providers/aldi` location backend |
| `heb/stores/` | JSON `heb.StoreSummary` keyed by prefixed HEB location ID | `cmd/heb` and `internal/heb` cache helpers | `internal/heb` location backend |
| `heb/store_locations.json` | JSON `[]storeindex.Entry` spatial index for HEB stores (`id`, `lat`, `lon`) | `cmd/heb` rebuilds after sync | `internal/heb` location backend |
| `heb/store_url_map.json` | JSON object mapping store URL to prefixed HEB location ID | `cmd/heb` and `internal/heb` cache helpers | `cmd/heb` incremental sync |
| `heb/reese84/latest.json` | JSON `heb.Reese84Record` containing the freshest HEB `reese84` cookie plus metadata | `cmd/reese84 -site heb -container heb` | `internal/heb` staples provider |
| `heb/reese84/history/` | JSON `heb.Reese84Record` append-only history keyed by fetch timestamp | `cmd/reese84 -site heb -container heb` | Operational debugging and manual rollback/reference |
| `heb/build_id/latest.json` | JSON `heb.BuildIDRecord` containing the latest HEB Next.js data build ID | `internal/heb` staples provider after discovery | `internal/heb` staples provider before category fetches |
| `publix/stores/` | JSON `publix.StoreSummary` keyed by numeric Publix store ID | `cmd/publix` and `internal/providers/publix` cache helpers | `internal/providers/publix` location backend |
| `publix/store_locations.json` | JSON `[]storeindex.Entry` spatial index for Publix stores (`id`, `lat`, `lon`) | `cmd/publix` rebuilds after sync | `internal/providers/publix` location backend |
| `publix/store_url_map.json` | JSON object mapping numeric Publix store ID to canonical location URL | `cmd/publix` and `internal/providers/publix` cache helpers | `cmd/publix` incremental sync |
| `publix/missing_store_ids.json` | JSON array of numeric Publix store IDs known to redirect back to `/locations` | `cmd/publix` and `internal/providers/publix` cache helpers | `cmd/publix` incremental sync |
| `publix/abck/latest.json` | JSON `publix.AbckRecord` containing the freshest Publix `_abck` cookie plus metadata | `cmd/publixabck` | `internal/providers/publix` staples cookie resolver |
| `publix/abck/history/` | JSON `publix.AbckRecord` append-only history keyed by fetch timestamp | `cmd/publixabck` | Operational debugging and manual rollback/reference |
| `wegmans/stores/` | JSON `wegmans.StoreSummary` keyed by numeric Wegmans store ID | `cmd/wegmans` and `internal/providers/wegmans` cache helpers | `internal/providers/wegmans` location backend |
| `wegmans/store_locations.json` | JSON `[]storeindex.Entry` spatial index for Wegmans stores (`id`, `lat`, `lon`) | `cmd/wegmans` rebuilds after sync | `internal/providers/wegmans` location backend |
| `wholefoods/stores/` | JSON `wholefoods.StoreSummaryResponse` keyed by Whole Foods store ID | `cmd/wholefoods` and `internal/providers/wholefoods` cache helpers | `internal/providers/wholefoods` location backend |
| `wholefoods/store_locations.json` | JSON `[]storeindex.Entry` spatial index for Whole Foods stores (`id`, `lat`, `lon`) | `cmd/wholefoods` rebuilds after sync | `internal/providers/wholefoods` location backend |
| `wholefoods/store_url_map.json` | JSON object mapping store URL to Whole Foods store ID | `cmd/wholefoods` and `internal/providers/wholefoods` cache helpers | `cmd/wholefoods` when `-stores` is not provided |

## Recipe identity and structured properties

`ai.Recipe.ComputeHash` produces the URL-safe, padded Base64 encoding of an FNV-128a content hash. Recipe hashes are identities, not merely cache optimizations: they appear in recipe routes and connect a recipe to its dependent records and user state.

The legacy portion of the hash writes these values in order:

1. Title and description.
2. Legacy cook-time and cost strings.
3. Each ingredient's name, quantity, and price, preserving ingredient order.
4. Each instruction string, preserving instruction order.
5. Health text and drink pairing.

When any structured recipe property is present, the hash appends this pipe-delimited payload:

```text
|35|4|21|540|2|stovetop|oven|
```

Fields are total minutes, servings, estimated total cost in dollars, calories per serving, cooking-method count, and each cooking method in order. The decimal integers and cooking-method enum values cannot contain `|`, so the separators give every value an unambiguous boundary. The method count makes the variable-length tail explicit. Field and cooking-method order are significant.

The suffix is enabled by comparing the complete `RecipeProperties` value with its Go zero value, so adding a property cannot be missed by a hand-maintained presence predicate. That detection does not make a new field part of the payload automatically: adding a hash input requires an explicit format change.

Recipe hashes are used by or stored in:

- `recipe/<hash>` recipe records and `/recipe/{hash}` routes.
- `recipes/<hash>` in the dedicated `recipe-images` backend.
- `wine_recommendations/<hash>`, `recipe_thread/<hash>`, `recipe_feedback/<hash>`, and `recipe_critiques/<hash>`.
- recipe-regeneration job identity.
- `recipe_selection/<user_id>/<origin_hash>` values in `saved_hashes` and `dismissed_hashes`.
- `users/<user_id>` entries in `last_recipes[].hash`.

Compatibility implications:

- Shopping purchase recommendations use `shopping_quantities/v7/`. Older quantity records are not read. Build merges and caches quantities before finalizing and redirecting; product names, sizes, and aisle details are saved with the merged list so page loads only read the cached result and never reload ingredient catalogs. The first successful merge is stored directly under the finalized shopping-list hash, which includes the selected recipes, store, and date. Later wine picks or catalog changes do not regenerate that snapshot. The catalog is filtered to recipe product IDs before merging. Only cached staples for the saved plan’s store and date supply product sizes for food merging. Pantry and wine candidate catalogs are not read. The first wine pick from each saved recipe bypasses the merger and is appended unchanged to the cached list under Wine, with no buy calculation. Missing catalog snapshots leave needs unresolved; other cache errors stop the request.
- Recipes with no structured properties omit the structured-properties payload, preserving their legacy hash exactly.
- Adding any structured property to an otherwise identical legacy recipe produces a new hash and therefore a new recipe identity.
- Changing a structured-property value or cooking-method order also produces a new identity.
- There is no automatic alias or migration from an old hash to a new hash. Dependent images, wine recommendations, threads, feedback, critiques, saved/dismissed selections, and saved-recipe references remain under the old identity unless an explicit migration rewrites every dependent record. During diagnostics, compare the cached recipe's properties and recomputed hash before treating a missing dependent record as data loss.
- If the structured-property layout changes in the future, introduce an explicit marker such as `recipe-properties-v2` and document its migration behavior here.

## Notes

- Cache backend selection is in `internal/cache/azure.go` (`MakeCache`).
- Most app caches use the default cache created via `cache.MakeCache()` / `cache.EnsureCache("recipes")`.
- ALDI locations use a separate cache created via `cache.EnsureCache("aldi")`.
- Albertsons-family locations use a separate cache created via `cache.EnsureCache("albertsons")`.
- Albertsons-family `reese84` cookie refresh also uses `cache.EnsureCache("albertsons")`; the latest record is overwritten while timestamped history remains append-only.
- Wegmans locations use a separate cache created via `cache.EnsureCache("wegmans")`.
- HEB locations use a separate cache created via `cache.EnsureCache("heb")`.
- HEB `reese84` cookie refresh also uses `cache.EnsureCache("heb")`; the latest record is overwritten while timestamped history remains append-only.
- Publix uses a separate cache created via `cache.EnsureCache("publix")`; it does not share the `recipes` container/directory.
- Publix `_abck` cookie refresh also uses `cache.EnsureCache("publix")`; the latest record is overwritten while timestamped history remains append-only.
- Recipe images use a separate cache created via `cache.EnsureCache("recipe-images")`; they do not share the main `recipes` container/directory.
- Whole Foods uses a separate cache created via `cache.EnsureCache("wholefoods")`; it does not share the `recipes` container/directory.
- Farmers Market uses a separate cache created via `cache.EnsureCache("farmersmarket")`; it does not share the `recipes` container/directory.
- Local cache paths when filesystem backend is used. are
  - `recipes/` for most app data,
  - `recipe-images/` for recipe images,
  - `aldi/` for ALDI data,
  - `albertsons/` for Albertsons-family data,
  - `heb/` for HEB data,
  - `publix/` for Publix data,
  - `wegmans/` for Wegmans data
  - `wholefoods/` for Whole Foods data
  - `farmersmarket/` for Farmers Market data
- Blob names in Azure match the same key strings listed above inside their respective containers.
- `internal/ingredients/cachekey.ForStore` owns the staple ingredient hash suffix, shared by recipe params and cached produce scoring. It uses raw URL-safe Base64 of FNV-64a over location ID, the supplied store date (`YYYY-MM-DD`), and backend staple signature, in that order. The `ingredients/` prefix and existing hashes are unchanged.
- Live `mnfoodclub_*` store IDs use a backend signature computed as the lowercase hexadecimal SHA-256 hash of `mnfoodclub-bigcommerce-v1` followed by the static produce share descriptions, joined with newlines (no trailing newline). This invalidates the former scraped staple snapshots; share inventory changes also change the signature automatically. The API import includes every page of visible, available products across all categories, excludes tracked out-of-stock products and disabled variants, and appends the static produce share ingredients. Each static entry includes its share name and carries a preassigned 10/10 grade; Low Carb cauliflower is marked as a substitution. Wine candidates include every page of BigCommerce category 178 (Wine & Wine Alternatives). They retain the existing style/location/date wine cache keys, so previously cached wine results remain until the next store date. Staples retain the store-day `ingredients/` layout, with each full store ID retaining its own hash; there is no new container or prefix.
- Kroger location entries use `location/kroger-v2/<store ID>`. This version invalidates legacy `location/<Kroger store ID>` entries so Kroger locations reload their API chain names. Legacy entries remain stored but are not read by Kroger lookups. Other providers retain their existing keys; Walmart relies on those entries because its backend does not support lookup by ID. Location snapshots inside saved recipe parameters are separate and retain their original values.
- MNFoodClub delivery searches use the stable `mnfoodclub_delivery` ID with coordinates from the current search. All `mnfoodclub_*` IDs bypass reads and writes in the `location/` cache, including any previously cached search points. Lookup by ID resolves directly through the backend to its Blaine coordinates. Ingredient and recipe caches continue to use the stable store ID and backend staple signature.
- Live `smithbrothersfarms_*` store IDs use the lowercase hexadecimal SHA-256 hash of the ordered produce, meat/poultry, organic harvest-box, and standard harvest-box URLs followed by the parser version (`v1`), joined with newlines with no trailing newline. They use the existing store-day `ingredients/` layout; no new cache container or key prefix is added. Box contents are fetched live on a cache miss and replace whole-box products. Synthetic ingredient IDs use `smithbrothersfarms_<box-slug>_<SHA-256 of bullet text>`, keeping the two boxes distinct and IDs stable across list reordering. Each box ingredient includes its box name and a preassigned 10/10 produce-share grade, with no individual price or quantity.
- Smith Brothers Farms searches use the stable `smithbrothersfarms_delivery` ID with the current search coordinates in either approximate delivery box. All `smithbrothersfarms_*` IDs bypass reads and writes in `location/`. Lookup by the stable ID resolves to the Kent headquarters coordinates. Ingredient and recipe caches use the stable ID and backend signature; seasonal contents refresh on the next uncached store-day fetch.
- Staple `ingredients/` cache keys derive from location ID, date, and a versioned backend staple signature (for example `kroger-staples-v1` or `wholefoods-staples-v1`), so Kroger and Whole Foods locations do not share staple caches and staple-definition changes can invalidate caches intentionally.
- Recipe image cache keys are stable per recipe hash, so prompt or model changes do not orphan previously generated images.
- Recipe records store `instructions` as a string array. Each string may contain the constrained Markdown supported by the instruction renderer: plain paragraphs and `- ` bullet lists.
- Do not create nested keys under `recipe/<hash>` (for example `recipe/<hash>/wine`) because `FileCache` stores `recipe/<hash>` as a file path.

### Ingredient embeddings

The opt-in enriching grader used by the ingredients command resolves description embeddings through OpenAI's
`text-embedding-3-small` model with 256 dimensions. Vectors are independent fields
on `ai.InputIngredient` (JSON `embeddings`), and their authoritative cache is
`ingredient_embeddings/`. Grade records exclude vectors; staple snapshots can
include them. Prices, product IDs, brands, and grades are excluded from embedding
input. Identical trimmed descriptions share an embedding across products/stores.

Responses grade cache versions depend on the grading model and prompt; Decisions
versions also include the rubric and score conversion. Embedding keys
include the embedding model, dimensions, input format version, and SHA-256 of the
trimmed description. Changing the grader reuses embeddings, while changing the
embedding configuration leaves grades intact. Existing grades without vectors
receive embeddings without regrading. The embedding service always resolves the
configured cache rather than trusting unversioned vectors in staple snapshots.
Old combined grade/embedding cache entries remain on disk and are not migrated.
Grading-disabled mode does not request or store embeddings.

The default ingredient grader changed from `gpt-5.6-luna` to `gpt-6-luna` on
2026-09-29. This produces a different model-derived grade cache version in the
existing `ingredient_grades/<cache_version>/` namespace; old grades and reviews
remain under their original versions. `INGREDIENT_GRADING_MODEL` still selects an
explicit model. Decisions grades use a separate cache version derived from the API
version, `gpt-6-luna`, instruction, ten-level rubric, and score conversion. The
review command uses the same version when `INGREDIENT_GRADING_MODEL=decisions`.
Decisions sends independent per-ingredient requests with a shared limit of 64
concurrent requests per grader instance.
Decisions grades also store the API's per-level probabilities, preserving each
zero-based value, label, and probability. The `probabilities-v1` cache version
suffix ensures older grades without distributions are refreshed. The rubric,
score conversion, and Responses grade versions are unchanged.
Wine pairing now uses `gpt-6-luna`, while existing
`wine_recommendations/<recipe_hash>` records remain valid and are reused.

The ingredients CLI fetches the selected store's current staple catalog, reuses
cached grades and embeddings, embeds the query description, and ranks that
catalog by cosine similarity. It does not search other stores or filter by grade.

## Ordering handoffs

- Kroger keeps `kroger_cart_tokens/<user ID>` and `kroger_cart_transfers/<user ID>/<finalized-list hash>`. Token encryption and associated user ID are unchanged. Legacy transfer records remain readable and block duplicate cart writes.
- The Kroger handler conditionally creates a pending transfer record before writing to the cart. Pending, uncertain, and completed records prevent repeat submissions. The content fingerprint covers the submitted store ID and items; a completed record with a different fingerprint returns a changed-list notice without writing again. Older fingerprint formats are treated conservatively as changed lists.
- `kroger_order_requests/<random state>` retains the posted request across OAuth. A secure, HTTP-only, same-site cookie carries the opaque state. The callback verifies state, authenticated ownership, and the server-stored ten-minute age before resuming the stored request. The cache has no per-record expiry or delete operation; expired requests remain stored but cannot be resumed.
- Recipes never reads transfer or OAuth records. It renders the generic order view supplied by the provider registry. Kroger’s view builder interprets redirect query parameters into fixed messages and a validated Kroger/QFC cart link. These fields are informational and never authorize a cart write.
- The former generic `ordering_transfers/` prefix is no longer read or written. Kroger continues to use its original provider-specific keys.

### Instacart ordering links

- `instacart_shopping_links/<user ID>/<payload hash>` stores `url` and `expires_at`.
  The hash covers the full outbound JSON and API base URL, separating development
  and production. The payload includes the Careme linkback and displayed items.
- Links are created with a 30-day expiry. Expired records are replaced on the next
  submission; changed payloads use a new key. Records are private to the authenticated
  user, and cache failures stop the handoff with an error notice.
- Only successfully created and validated HTTPS Instacart links are saved. This is
  link reuse, not cart-write duplicate prevention. Concurrent initial requests may
  create equivalent shopping pages without changing a cart.

### BigCommerce cart transfers

- `bigcommerce_cart_transfers/<user ID>/<transfer hash>` uses SHA-256 of the store
  API path, source location ID, and finalized list key, separated by newlines.
  The record stores `status`, `fingerprint`, `cart_id`, `added`, and `unmatched`.
  The fingerprint is SHA-256 of the complete submitted item JSON after decoding.
- A conditional pending reservation precedes cart creation. Complete, pending,
  and uncertain records block repeat cart creation. Changed items under the same
  transfer key return a changed-list notice. Confirmation failures leave the
  reservation intact; no background retries or automatic recreation occur.
- Cart IDs remain server-side. Authenticated review requests load only the
  current user’s completed transfer. Single-use BigCommerce redirect URLs and
  their tokens are generated afresh and never persisted. Expired or checked-out
  carts return an error rather than creating a replacement cart.
