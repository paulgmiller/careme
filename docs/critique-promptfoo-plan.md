# Critique Promptfoo evaluation plan

Replace the retired bespoke `cmd/critiqueeval` benchmark with a checked-in Promptfoo regression suite. Do not implement this as part of the current model-switch PR.

Each test case should contain a complete recipe fixture and a short description of the known defect (or explicitly known-good status). The critique provider should return the normal `RecipeCritique` JSON using the production prompt and schema.

Assertions should cover both structure and substance:

- valid critique JSON, score range, non-empty summary, and valid issue fields;
- detection of a named defect, such as an unsafe or unsuitable temperature, missing recipe properties, ambiguous quantities, or broken timing;
- a concrete suggested fix for each detected defect;
- no invented defect in a known-good recipe;
- seasoning findings that account for brined, cured, salty, or finishing ingredients rather than applying a fixed salt percentage mechanically.

The second stage should pass the recipe and critique to the recipe-generation model with a revision request. Assert that the identified defect is fixed, the recipe remains schema-valid, and unrelated recipe details are preserved. This measures whether a critique is actionable, not merely whether it sounds plausible.

Keep model comparison separate from the regression cases: run the same Promptfoo cases against candidate critique models, while using the revision stage to compare downstream usefulness. Preserve a small number of hand-reviewed fixtures so model changes cannot silently trade accurate defect detection for harsher scoring.
