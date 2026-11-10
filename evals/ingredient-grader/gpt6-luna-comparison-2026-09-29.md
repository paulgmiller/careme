# Ingredient grading: GPT-6 Luna vs GPT-5.6 Luna — 2026-09-29

Defaults changed to `gpt-6-luna` for ingredient grading and wine pairing. Both
paths retain `none` reasoning and their existing prompts and structured output
schemas. Ingredient grading still honors `INGREDIENT_GRADING_MODEL` overrides.
Recipe generation and meal planning continue to use `gpt-6.1-sol`.

## Ingredient grading results

GPT-6 Luna passed one more batch and one more ingredient than GPT-5.6 Luna.
Neither run had API/provider errors or missing grades. The new model still failed
one checked-in score expectation; this is not a fully passing grading suite.

| Metric | GPT-5.6 Luna | GPT-6 Luna |
| --- | ---: | ---: |
| Batches passing every score bound | 21/23 | 22/23 |
| Individual ingredients within bounds | 90/92 | 91/92 |
| API/provider errors | 0 | 0 |
| Mean grading time per four-ingredient batch | 2.507s | 2.441s |

| Ingredient | Expected score | GPT-5.6 Luna | GPT-6 Luna |
| --- | --- | ---: | ---: |
| Steamfresh Carrots | 4–6 | 7 (fail) | 7 (fail) |
| Fresh Pineapple Spears | 4–7 | 8 (fail) | 7 (pass) |

Both models treated Steamfresh Carrots as a useful plain frozen cooking ingredient
and scored it above the fixture maximum. The checked-in bounds and production
prompt were unchanged. This report records the mismatch without recalibrating
the fixture. GPT-6 Luna met the pineapple-spears bound.

## Per-batch results

Each pass count is the number of ingredients meeting their score bounds out of four.

| Batch | GPT-5.6 pass count | GPT-6 pass count | GPT-5.6 time | GPT-6 time |
| --- | ---: | ---: | ---: | ---: |
| Fresh vegetables vs prepared vegetables | 4/4 | 4/4 | 2.434s | 2.502s |
| Whole grains vs prepared grains | 4/4 | 4/4 | 2.993s | 1.875s |
| Fresh fruit vs prepared fruit | 4/4 | 4/4 | 2.055s | 2.109s |
| Plain vegetables vs increasingly prepared forms | 3/4 | 3/4 | 3.066s | 1.945s |
| Pasta and convenience foods | 4/4 | 4/4 | 2.551s | 2.496s |
| Raw meat vs seasoned and fully prepared meat | 4/4 | 4/4 | 3.064s | 2.557s |
| Raw, cooked, and snack-format seafood | 4/4 | 4/4 | 2.248s | 2.208s |
| Uncommon vegetables remain strong ingredients | 4/4 | 4/4 | 2.366s | 2.329s |
| Organ meats and bones are valid cooking ingredients | 4/4 | 4/4 | 2.134s | 2.402s |
| Flexible dairy and eggs | 4/4 | 4/4 | 2.080s | 1.794s |
| Dry legumes vs convenient prepared legumes | 4/4 | 4/4 | 1.964s | 2.615s |
| Herbs and spices vs seasoning mixes | 4/4 | 4/4 | 2.826s | 2.149s |
| Explicit dry pasta score anchors | 4/4 | 4/4 | 2.653s | 2.336s |
| Real pasta quality signals vs generic marketing claims | 4/4 | 4/4 | 2.332s | 2.661s |
| Grain cooking signals vs instant flavored mixes | 4/4 | 4/4 | 2.156s | 3.126s |
| Bread and prepared sauce anchors | 4/4 | 4/4 | 2.356s | 2.219s |
| Ready-to-eat meals, kits, bowls, and trays | 4/4 | 4/4 | 2.054s | 2.103s |
| Dips, gravies, mixes, and prepared sides | 4/4 | 4/4 | 2.434s | 2.789s |
| Whole, pre-cut, and cooking-friendly fruit formats | 3/4 | 4/4 | 2.076s | 2.076s |
| Diverse flexible ingredients that are difficult to make at home | 4/4 | 4/4 | 5.145s | 2.323s |
| Plain ground meat vs sausage and cured meat | 4/4 | 4/4 | 2.563s | 3.538s |
| Seasonal produce remains a strong ingredient | 4/4 | 4/4 | 1.957s | 3.267s |
| Minimally processed staples vs sauced versions | 4/4 | 4/4 | 2.148s | 2.717s |

## Harness changes

- The task runner now passes `MODEL` through `INGREDIENT_EVAL_MODEL`. Provider
  `config.model` takes precedence over that environment variable, followed by
  the configured production grader. Explicit API model IDs are not translated.
- Every case enables grading and uses a fresh in-memory cache with the production
  grading manager. Persistent ingredient grades cannot satisfy the comparison
  without live calls. The existing `jev` grader option remains supported.
- Empty batches and incomplete, nil, duplicate, or unexpected grade results fail
  explicitly. The prior harness could pass an empty or incomplete response.
- JSON exports record selected model, full fixture grades, ingredient counts,
  passing ingredient counts, and generation-only latency. An empty
  `metadata.requestedModel` means the production default was selected.

## Method and limits

- Promptfoo 0.123.0, concurrency sixteen, `--no-cache`; 23 identical checked-in
  batches of four ingredients, one sample per batch per model, no failed-case
  reruns. Both models explicitly requested `none` reasoning.
- Runs began September 29 Pacific time (September 30 UTC), ten seconds apart,
  and overlapped. The working tree was based on
  `654e80e` with the new defaults and harness changes.
- The suite compares model-assigned scores to checked-in min/max ranges; no
  separate model judge or human adjudication was used. Small timing differences
  and one improved ingredient score do not establish reliability gains.
- Latency includes production grading-manager work and SDK retries, excluding
  configuration, Go startup, and Promptfoo scheduling. Costs and tokens are not
  exported by this suite; zero Promptfoo token totals do not indicate free calls.
- GPT-6 Luna pricing was added to application usage logging from the
  [official model documentation](https://developers.openai.com/api/docs/models/gpt-6-luna):
  per million tokens, input $0.10, cached input $0.01, cache writes $0.125,
  output $0.50. This run does not report a measured cost comparison.
- Wine pairing was verified with the existing mocked API request and output
  tests, including `gpt-6-luna`, `none` reasoning, and catalog product IDs.
  No live wine-quality comparison was run.
- The model-derived ingredient grade cache version changes for the new default;
  old grade/review entries remain under their previous version. Embedding cache
  keys and wine recommendation keys are unchanged.

## Reproduction and artifacts

```sh
./task.sh evals EVAL=ingredient-grader MODEL=gpt-5.6-luna -- --no-cache --output /tmp/careme-ingredients-gpt56-luna-20260929.json
./task.sh evals EVAL=ingredient-grader MODEL=gpt-6-luna -- --no-cache --output /tmp/careme-ingredients-gpt6-luna-20260929.json
```

| Model | Evaluation ID |
| --- | --- |
| gpt-5.6-luna | `eval-nAo-2026-09-30T00:00:11` |
| gpt-6-luna | `eval-eKn-2026-09-30T00:00:22` |

Both Promptfoo runs exited 100 because score assertions failed, not because of
provider errors. The task runner reported the assertion failure with exit 201.
Full temporary JSON exports remain in `/tmp`. The adjacent
[CSV](gpt6-luna-comparison-2026-09-29.csv) retains fixture ingredient scores,
bounds, pass status, and batch latency.

## Verification

`./task.sh verify-go` passed formatting, vet, all Go tests, and lint (zero issues).
Regression tests cover default model API requests, no reasoning, model override
precedence, malformed provider settings, grading completeness, score bounds,
cache-version separation, and GPT-6 Luna pricing. `git diff --check` passed.
