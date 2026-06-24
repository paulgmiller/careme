# Recipe Generation Walkthrough

This document covers the first-time generation path inside `generatorService.GenerateRecipes`, from fetching staples to fanning generated recipes back into an `ai.ShoppingList`.

## Flow

```mermaid
flowchart TD
    subgraph Legend["Model color"]
        MiniLegend["gpt-5-mini<br/>Grading"]
        GPT5Legend["gpt-5.5<br/>Menu planning + recipe generation + retry"]
        GeminiLegend["Gemini<br/>Recipe critique"]
    end

    A["GenerateRecipes"] --> B["FetchStaples"]
    B --> C{"staples already fetched?"}
    C -- "yes" --> D["Load cached staples"]
    C -- "no" --> E["Fetch from Kroger / Albertsons / Whole Foods backend"]
    E --> G["GradeIngredients"]
    D --> G

    G --> H{"ingredient grade cached?"}
    H -- "yes" --> I["Use cached grade"]
    H -- "no" --> J["Grade missing ingredients in batches"]
    I --> L["Filter ingredients to grade above 6"]
    J --> L

    L --> M["Shuffle ingredients"]
    M --> N["CreateMenuPlan for 3 plans"]
    N --> O["Fan out recipe generation"]

    O --> P1["Plan 1 -> GenerateRecipe"]
    O --> P2["Plan 2 -> GenerateRecipe"]
    O --> P3["Plan 3 -> GenerateRecipe"]

    P1 --> R1["CritiqueRecipe"]
    P2 --> R2["CritiqueRecipe"]
    P3 --> R3["CritiqueRecipe"]

    R1 --> S1{"score at least 8?"}
    R2 --> S2{"score at least 8?"}
    R3 --> S3{"score at least 8?"}

    S1 -- "yes" --> T1["Keep recipe"]
    S2 -- "yes" --> T2["Keep recipe"]
    S3 -- "yes" --> T3["Keep recipe"]

    S1 -- "no" --> U1["Retry from critique feedback"]
    S2 -- "no" --> U2["Retry from critique feedback"]
    S3 -- "no" --> U3["Retry from critique feedback"]

    T1 --> W["Fan in finished recipes"]
    T2 --> W
    T3 --> W
    U1 --> W
    U2 --> W
    U3 --> W

    W --> X["Return ai.ShoppingList with menu plan"]

    classDef mini fill:#e0f2fe,stroke:#0284c7,color:#0f172a,stroke-width:2px
    classDef gpt5 fill:#dcfce7,stroke:#16a34a,color:#0f172a,stroke-width:2px
    classDef gemini fill:#f3e8ff,stroke:#7e22ce,color:#0f172a,stroke-width:2px

    class MiniLegend,J,N mini
    class GPT5Legend,P1,P2,P3,U1,U2,U3 gpt5
    class GeminiLegend,R1,R2,R3 gemini
```

## Staples And Grading

`FetchStaples` lives in `internal/recipes/staples.go`. It can reuse staples for the same store, date, and staples backend signature even when user recipe instructions differ.

On a cache miss, the routed staples provider picks the store backend and fetches staple candidates. The backend can be Kroger, Albertsons-family, or Whole Foods depending on the selected store. On both cache hits and misses, the result goes through `GradeIngredients`.

Ingredient grading uses the cache in `internal/ingredients/grading/cache.go`:

1. Keep ingredients that already have a grade.
2. Reuse cached grades for known ingredients.
3. Send only missing ingredients to the underlying grader.

Back in `GenerateRecipes`, ingredients with `Grade.Score <= 6` are removed. Ungraded ingredients are still allowed through.

The model boundary in this section is ingredient grading. Fetching staples is store data retrieval; grading missing ingredients uses the configured ingredient grading model, defaulting to `gpt-5-mini`.

## Menu Plan And Recipe Fan-Out

After grading, `GenerateRecipes` shuffles the ingredient list and calls the menu-planning model through `CreateMenuPlan` for exactly three plans. The menu plan request includes the location, filtered ingredients, user directive, user instructions, recipe date, and recently cooked recipe titles. Menu planning uses `gpt-5.5`.

The returned `menuPlan.Plans` are processed with `parallelism.MapWithErrors`. Each plan becomes one worker and makes its own `gpt-5.5` recipe model call:

- append the plan instructions to the base instructions
- call `GenerateRecipe`
- set `OriginHash`
- call `critiqueAndMaybeRetryRecipe`

## Critique And Fan-In

`critiqueAndMaybeRetryRecipe` asks the critique model for feedback. If critiques are disabled, the rubberstamp service returns a passing score without a model call.

When a critique score is at least `critique.MinimumRecipeScore` (`8`), the recipe is kept. When the score is below `8`, the generator does one more `gpt-5.5` recipe model call using the critique feedback and original recipe response ID, then uses that retry in place of the original recipe.

Once all workers finish, `GenerateRecipes` fans the recipe results back into:

```go
&ai.ShoppingList{
    Recipes: lo.FromSlicePtr(results),
    Plan:    menuPlan,
}
```
