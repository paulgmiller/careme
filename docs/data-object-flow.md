# Data Object Flow

This document describes the lifecycle of generation data in `internal/recipes`, from query args to regeneration.

## 1) `params` is created from query args (generation starts here)

Entry point:
- `GET /recipes?location=...` without `h` query arg
- Handler: `internal/recipes/server.go` `handleRecipes`

Flow:
1. `handleRecipes` calls `ParseQueryArgs(...)`.
2. `ParseQueryArgs` (`internal/recipes/params.go`) builds `generatorParams` from URL query args:
   - `location` (required)
   - `date` (optional, defaulted by store timezone/day boundary)
   - `instructions` (optional)
   - `conversation_id` (optional)
3. `handleRecipes` persists that object with `SaveParams(...)` under `params/<params_hash>`.
4. This saved `params` object is the start signal for generation. `kickgeneration(...)` is launched immediately after.

## 2) `shoppingList` + `recipes` are generated from `params`

Async generation path:
1. `kickgeneration(...)` calls `generator.GenerateRecipes(ctx, params)`.
2. The generator returns an `ai.ShoppingList` containing `Recipes` (and `ConversationID`).
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
4. Merges selection state into params (`mergeParamsWithSelection`), applies new instructions, and carries conversation id when needed.
5. Computes a new hash from the updated params.

Then:
1. New params is saved at `params/<new_hash>`.
2. `kickgeneration(...)` runs again with that new params.

Result:
- `selection` holds transient decision state for a given origin hash.
- A new generation cycle begins when a new `params` object is created and saved.
