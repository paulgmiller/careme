# `recipeeval` Goals

This command exists to run a regression suite for recipe generation, not to judge culinary perfection.

## Primary Goal

Catch prompt or model regressions in the recipe generator before they quietly change behavior in production.

In practice that means this eval should answer questions like:

- Did the model still return valid structured recipe JSON?
- Did it obey hard user constraints such as `No shellfish` or `make it vegetarian`?
- Did it return the requested number of recipes?
- Did it keep wine styles in the constrained search-friendly format?
- Did it avoid inventing prices or attaching prices that do not exist in the ingredient TSV?
- Did it avoid obvious title-level repeats of recently cooked meals?

## What This Eval Is Optimized For

- Stable regression detection across prompt and model changes.
- Checks derived from the production prompt contract, not from subjective taste.
- Small reviewed cases that are cheap to run and easy to understand.
- Failures that are actionable by an engineer updating prompts or eval cases.

## What This Eval Is Not For Yet

- Final judgment of recipe quality, taste, plating, or nutrition accuracy.
- Ranking multiple prompts by subtle culinary preference.
- Training or fine-tuning directly from user feedback.
- Large-scale benchmark coverage.

Those can come later, but they should not weaken the regression gate.

## Design Intent

- Prefer deterministic checks for v1.
  The current suite uses Python graders for hard contract checks instead of fuzzy model graders wherever possible.
- Keep the eval item schema extensible.
  `future_labels` exists so later work can attach feedback-derived labels without redesigning the format.
- Keep the eval prompt aligned with production.
  The input messages are built from the same recipe prompt structure and JSON schema used by the app.
- Avoid golden-output brittleness.
  The suite checks contract and safety properties, not exact recipe text.

## Dataset Expectations

- Cases should eventually come from reviewed real generations.
- The checked-in fixture data is only a starter scaffold.
- Each case should encode the requirement explicitly in fields like `forbidden_terms`, `required_terms`, `expected_recipe_count`, and `last_recipes`.
- If a future agent adds a new production promise to recipe generation, the eval cases and graders should be updated to cover it.

## Future Evolution

The likely next steps are:

- replace synthetic TSV fixtures with reviewed real ingredient sets
- expand the case set around known failure modes
- add richer quality scoring in a separate layer from the hard regression gate
- build a separate feedback-to-eval pipeline using cooked, stars, comments, saved, and dismissed signals

Important: keep those future feedback-driven or quality-driven evals separate from the strict regression suite. This command should remain the fast, reliable guardrail.
