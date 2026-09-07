# Promptfoo evals

List every checked-in Promptfoo suite without running one:

```sh
./task.sh evals
```

Select one suite by its directory name:

```sh
./task.sh evals EVAL=recipe-critique
```

Because evaluations can make paid model calls, running every suite requires an explicit selection:

```sh
./task.sh evals EVAL=all
```

Generate recipe test cases from a cached shopping-list hash:

```sh
go run ./cmd/evalcase -hash HASH -secret-file secrets/envprod
```

The command reads the selected cache and emits one YAML case per stored recipe plan. Paste the sequence under `tests:` in `recipe-generation/promptfooconfig.yaml`.

Each generated case contains one recipe plan, the menu response ID, and its prompt-cache key. Recipe evals use only those checked-in values and the AI API; the provider does not connect to production cache storage.

## Recipe generation

Select the recipe model without editing the suite:

```sh
./task.sh evals EVAL=recipe-generation MODEL=your-api-model-id -- --no-cache --output /tmp/recipe-eval.json
```

Omit `MODEL` to use the production default. Direct Promptfoo runs also accept `RECIPE_EVAL_MODEL`. For side-by-side columns in one report, duplicate the `file://provider.go` provider entry with a distinct label and `config.model` for each candidate. An explicit `config.model` takes precedence over the environment. Pass the actual API model ID; the provider does not translate display names or silently substitute unavailable models.

The suite requires `AI_API_KEY` and `OPENROUTER_API_KEY`, loaded through the existing configuration/kage path. Each case makes one generation call and one call to the production critiquer. `config.judge_model` pins Gemini for every candidate. The judge sees only the newly generated recipe, with response IDs and provenance removed. Its full critique, model, and timestamp are retained in response metadata. `critique-quality` reports the score divided by ten and passes at 8/10; judge errors fail the evaluation.

`latencyMs` measures the generation call only, including SDK retries, excluding configuration, Go compilation, and judging. The latency assertion uses a 60-second budget. `metadata.judgeLatencyMs` records judging separately. Use `--no-cache` for latency comparisons, keep concurrency at one, and use `--repeat 3` for repeated samples. Promptfoo caching is disabled by that flag, but the production OpenAI prompt cache and retained menu context are still used; these are continuation timings, and API cache hits are not guaranteed.

A one-case integration smoke check on 2026-09-04 passed using the existing GPT-5.6 Sol default: generation 50.369 seconds, Gemini judging 25.366 seconds, quality 9/10. This verifies the harness, not a model comparison.

The eight cases include the original chicken and tri-tip regressions plus six production saved-recipe examples selected on 2026-09-04:

| Case | Historical score | Coverage |
| --- | --- | --- |
| Moroccan braised lamb | 10, Gemini | Six servings, one-hour request versus realistic braising time |
| Sichuan chicken and bok choy | 9, Gemini | Six servings, stir-fry preparation and doneness |
| Lemon-Parmesan coho and orzo | 9, Gemini | Fish doneness and oven/pasta coordination |
| Jicama cucumber side | 7, Opus | Side portions, seasoning, no-cook method |
| Chilean pebre tri-tip | 7, Opus | Partial-roast quantities, seasoning, total time |
| Sumac-pistachio coho | 7, Gemini | Salt allocation and mixture instructions |

Each added case records source recipe/list hashes, historical judge/score/date, and its focus. Historical scores explain selection, not a baseline to subtract from new scores: judges and prompts have changed. Saved does not mean defect-free. The cases replay the associated original menu direction, not subsequent edits to the saved recipe. No user identities or saved recipe bodies are checked in. Serving-count and time-budget assertions complement the critiquer, which judges cookability but does not receive the original user request.

These continuation cases depend on OpenAI retaining their menu response IDs and on access through the originating API project. An expired or inaccessible response fails explicitly; refresh affected cases using `cmd/evalcase`. They are not self-contained prompt replays.

Provider options and critique metadata assertions follow Promptfoo's [Go provider interface](https://www.promptfoo.dev/docs/providers/go/) and [JavaScript assertion context](https://www.promptfoo.dev/docs/configuration/expected-outputs/javascript/).

## Recipe critique

`recipe-critique/promptfooconfig.yaml` runs the production recipe critique prompt and schema against hand-reviewed recipe fixtures. A case accepts either a complete `vars.recipe` object or a `vars.recipe_hash`, but not both. Hash cases read `recipe/<hash>` from the configured cache, so export the cache credentials before running Promptfoo when the recipe is not in the local file cache.

Run just this suite from the repository root:

```sh
./task.sh evals EVAL=recipe-critique
```

The current suite evaluates critique structure, defect detection, suggested fixes, false positives, brined/salty ingredient context, and a 30-second model-call latency budget. The Go provider reports only the production critique call duration, excluding Promptfoo's provider startup and build time. The planned recipe-revision stage remains separate so it can later send both the recipe and critique to the recipe-generation model and measure whether the feedback is actionable.
