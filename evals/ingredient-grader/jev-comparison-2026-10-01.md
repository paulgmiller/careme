# Ingredient grading: GPT-6 Luna vs JEV — 2026-10-01

JEV passed every checked-in score bound and was faster in this run. GPT-6 Luna
missed one bound. The production ingredient grader remains `gpt-6-luna`; this
comparison does not change defaults.

| Metric | GPT-6 Luna | JEV |
| --- | ---: | ---: |
| Batches passing every score bound | 22/23 | 23/23 |
| Ingredients within score bounds | 91/92 | 92/92 |
| API/provider errors | 0 | 0 |
| Mean grading time per four-ingredient batch | 2.882s | 0.179s |
| Minimum–maximum batch grading time | 2.037–4.503s | 0.112–0.264s |

JEV used 93.8% less mean grading time, approximately
16.1 times faster for these batches. The pipelines send requests differently:
Luna grades four ingredients together in one Responses call; JEV sends one
request per ingredient in parallel through `SystemOneBatch`.

## Score differences

GPT-6 Luna scored Steamfresh Carrots 7, above the fixture maximum of 6. JEV scored
the same ingredient 4, within the allowed 4–6 range. All other ingredient scores
met their fixture bounds in both runs. No missing grades or provider errors
occurred. Fixture bounds and grading prompts were unchanged.

## Per-batch results

| Batch | Luna ingredients passing | JEV ingredients passing | Luna time | JEV time |
| --- | ---: | ---: | ---: | ---: |
| Fresh vegetables vs prepared vegetables | 4/4 | 4/4 | 2.568s | 0.262s |
| Whole grains vs prepared grains | 4/4 | 4/4 | 2.954s | 0.195s |
| Fresh fruit vs prepared fruit | 4/4 | 4/4 | 2.575s | 0.171s |
| Plain vegetables vs increasingly prepared forms | 3/4 | 4/4 | 2.121s | 0.191s |
| Pasta and convenience foods | 4/4 | 4/4 | 2.182s | 0.173s |
| Raw meat vs seasoned and fully prepared meat | 4/4 | 4/4 | 2.644s | 0.171s |
| Raw, cooked, and snack-format seafood | 4/4 | 4/4 | 2.473s | 0.169s |
| Uncommon vegetables remain strong ingredients | 4/4 | 4/4 | 4.503s | 0.221s |
| Organ meats and bones are valid cooking ingredients | 4/4 | 4/4 | 4.081s | 0.195s |
| Flexible dairy and eggs | 4/4 | 4/4 | 2.442s | 0.230s |
| Dry legumes vs convenient prepared legumes | 4/4 | 4/4 | 3.556s | 0.227s |
| Herbs and spices vs seasoning mixes | 4/4 | 4/4 | 4.001s | 0.179s |
| Explicit dry pasta score anchors | 4/4 | 4/4 | 3.785s | 0.264s |
| Real pasta quality signals vs generic marketing claims | 4/4 | 4/4 | 2.845s | 0.180s |
| Grain cooking signals vs instant flavored mixes | 4/4 | 4/4 | 2.701s | 0.226s |
| Bread and prepared sauce anchors | 4/4 | 4/4 | 3.270s | 0.172s |
| Ready-to-eat meals, kits, bowls, and trays | 4/4 | 4/4 | 2.777s | 0.144s |
| Dips, gravies, mixes, and prepared sides | 4/4 | 4/4 | 2.341s | 0.142s |
| Whole, pre-cut, and cooking-friendly fruit formats | 4/4 | 4/4 | 2.456s | 0.151s |
| Diverse flexible ingredients that are difficult to make at home | 4/4 | 4/4 | 2.607s | 0.113s |
| Plain ground meat vs sausage and cured meat | 4/4 | 4/4 | 2.037s | 0.112s |
| Seasonal produce remains a strong ingredient | 4/4 | 4/4 | 2.732s | 0.117s |
| Minimally processed staples vs sauced versions | 4/4 | 4/4 | 2.629s | 0.123s |

## Configuration cleanup

The ingredient eval now reads `INGREDIENT_GRADING_MODEL` through the existing
`config.Load()` path. The duplicate `INGREDIENT_EVAL_MODEL` setting and provider
`config.model` selector were removed. Task and direct Promptfoo runs use the same
configuration as production. The task runner preserves the configured value;
`MODEL` continues to select models for the recipe and menu suites.

Each case still enables grading and uses a fresh in-memory cache through the
production manager, so stored grades cannot bypass generation. Grade-completeness
checks, score-bound checks, exported grades/counts, and generation latency remain.

## Method and limits

- Promptfoo 0.123.0, concurrency sixteen, `--no-cache`; 23 identical four-ingredient
  batches and 92 ingredients per grader, one sample per case in the final comparison.
- Final runs started October 1, 2026 at 15:43:08 and 15:43:18 UTC and overlapped.
  Working tree based on `f02fe203ee0ec94712f75a45cde41911a5206d47`, with the
  configuration cleanup above. Both exported `metadata.requestedModel` values
  match their requested graders.
- Luna uses `gpt-6-luna` with `none` reasoning and structured grade output.
  JEV uses the TypeSafe integration with the same grading instruction plus ten
  explicit score-level criteria, and converts its zero-based level to a 1–10 score.
  This compares production grading paths, not identical request schemas or prompts.
- The TypeSafe SDK selects its configured service model, defaulting to `jev-latest`.
  This eval exports `jev` as the selected grader, not a pinned service snapshot.
- Latency measures the production grading-manager call, including SDK retries
  and JEV request fan-out, excluding configuration, Go startup, and scheduling.
  Fresh local caches prevent stored-grade reuse; remote service caching is not
  controlled. API costs and tokens are not exported, so no cost comparison is made.
- Checked-in min/max bounds determine pass status; no independent judge or new
  human adjudication was used. One sample and a small fixture set limit broader
  reliability claims.
- Restart recovery left earlier runs on different dates. The final report uses
  fresh October 1 runs through the simplified configuration. Prior Luna runs
  varied between 91/92 and 92/92 ingredients passing, illustrating sample variance.
  Earlier JEV runs also passed 92/92. No individual failed cases were selectively
  retried.

## Reproduction and artifacts

```sh
INGREDIENT_GRADING_MODEL=gpt-6-luna ./task.sh evals EVAL=ingredient-grader -- --no-cache --output /tmp/careme-ingredients-luna-vs-jev-luna-20261001.json
INGREDIENT_GRADING_MODEL=jev ./task.sh evals EVAL=ingredient-grader -- --no-cache --output /tmp/careme-ingredients-luna-vs-jev-jev-20261001.json
```

Luna requires `AI_API_KEY`; JEV requires `TYPESAFE_API_KEY`. Credentials load
via the existing configuration/kage path.

| Grader | Evaluation ID |
| --- | --- |
| luna | `eval-2jg-2026-10-01T15:43:08` |
| jev | `eval-sQU-2026-10-01T15:43:18` |

Luna exited Promptfoo 100 (task 201) for its score assertion failure; JEV exited
zero. Full JSON exports remain in `/tmp` at the paths above. The adjacent
[CSV](jev-comparison-2026-10-01.csv) retains each fixture ingredient score, bounds,
pass status, and batch latency.

## Verification

`./task.sh verify-go` passed formatting, vet, all Go tests, and lint (zero issues).
Task-runner checks confirmed that the existing ingredient model setting reaches
Promptfoo unchanged; final live runs confirmed selection of both graders.
`git diff --check` passed.
