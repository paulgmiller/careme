# Data Object Flow

This document describes the lifecycle of generation data in `internal/recipes`, from the generation form to regeneration.

## 1) `params` is created when generation starts

Entry point:
- `POST /recipes`
- Handler: `internal/recipes/server.go` `handleGenerate`

Flow:
1. `handleGenerate` calls `ParseGenerationForm(...)`.
2. `ParseGenerationForm` (`internal/recipes/params.go`) builds `generatorParams` from form fields:
   - `location` (required)
   - `date` (optional, defaulted by store timezone/day boundary)
   - `instructions` (optional)
   - `response_id` (optional)
3. `handleGenerate` persists that object with `SaveParams(...)` under `params/<params_hash>`.
4. This saved `params` object is the start signal for generation. `kickgeneration(...)` is launched immediately after.

Farmers market uploads use a server-side entry point instead of posting another form:

1. The upload analysis saves the market and its inventory.
2. `StartRecipeGeneration(...)` builds and saves the same `generatorParams` object.
3. The analysis job stores the returned `/recipes?h=...&start=...` polling URL.
4. The next farmers market status poll redirects the browser to that URL.

## 2) `shoppingList` + `recipes` are generated from `params`

Async generation path:
1. `kickgeneration(...)` calls `generator.GenerateRecipes(ctx, params)`.
2. The generator returns an `ai.ShoppingList` containing `Recipes` (and `ResponseID`).
3. `SaveShoppingList(...)` persists:
   - `shoppinglist/<params_hash>` -> full `ai.ShoppingList`
   - `recipe/<recipe_hash>` -> each recipe object (with `OriginHash = params_hash`)

At this point, `/recipes?h=<params_hash>` can render the generated list.

## 3) Optional `selection` state is created to hold user choices

After the list exists, a signed-in user can save/dismiss recipes:
- `POST /recipe/{hash}/save`
- `POST /recipe/{hash}/dismiss`

Both handlers (`handleSaveRecipe`, `handleDismissRecipe`) update `recipeSelection` (`internal/recipes/selection.go`):
- `SavedHashes []string`
- `DismissedHashes []string`
- `UpdatedAt time.Time`

Storage key:
- `recipe_selection/<user_id>/<origin_hash>`

This object is optional and exists only when the user starts interacting with save/dismiss actions.

## 4) Regeneration creates a new `params` from old `params` + `selection`

Regeneration entry:
- `POST /recipes/{hash}/regenerate`
- Handler: `handleRegenerate`

`handleRegenerate` calls `paramsForAction(...)`, which:
1. Loads old `params` from `params/<hash>`.
2. Loads current `shoppingList` from `shoppinglist/<hash>`.
3. Loads `recipeSelection` for `(user_id, hash)`.
4. Merges selection state into params (`mergeParamsWithSelection`), applies new instructions, and carries the latest response id when needed.
5. Computes a new hash from the updated params.

Then:
1. New params is saved at `params/<new_hash>`.
2. `kickgeneration(...)` runs again with that new params.

Result:
- `selection` holds transient decision state for a given origin hash.
- A new generation cycle begins when a new `params` object is created and saved.
- Recipe follow-up questions are chained by the latest `response_id` stored on the recipe thread; each answer updates that thread-level response id for the next turn.
