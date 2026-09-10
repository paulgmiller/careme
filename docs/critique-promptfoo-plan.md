# Critique Promptfoo evaluation plan

The retired bespoke `cmd/critiqueeval` benchmark has been replaced by a checked-in Promptfoo regression and model-comparison suite.

Each test case should contain a complete recipe fixture and a short description of the known defect (or explicitly known-good status). The critique provider should return the normal `RecipeCritique` JSON using the production prompt and schema.

Assertions should cover both structure and substance:

- valid critique JSON, score range, non-empty summary, and valid issue fields;
- detection of a named defect, such as an unsafe or unsuitable temperature, missing recipe properties, ambiguous quantities, or broken timing;
- a concrete suggested fix for each detected defect;
- no invented defect in a known-good recipe;
- seasoning findings that account for brined, cured, salty, or finishing ingredients rather than applying a fixed salt percentage mechanically.

GPT-5.6 Sol currently grades whether each critique is accurate, complete, proportionate, and actionable. This provides a consistent usefulness measure without including judge latency in the candidate model's latency metric.

Run the same regression cases against every candidate critique model. Preserve the synthetic safety and false-positive guardrails alongside real generated recipes that users cooked and rated, so model changes cannot silently trade accurate defect detection for harsher scoring.

A future end-to-end revision stage can pass the recipe and critique to the recipe-generation model, then assert that identified defects are fixed, the recipe remains schema-valid, and unrelated recipe details are preserved.
