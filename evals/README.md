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

## Recipe critique

`recipe-critique/promptfooconfig.yaml` runs the production recipe critique prompt and schema against synthetic guardrails and checked-in recipes that users cooked and rated. A case accepts either a complete `vars.recipe` object or a `vars.recipe_hash`, but not both. The checked-in cases are self-contained; hash cases read `recipe/<hash>` from the configured cache when doing exploratory work.

Candidate models are selected in the Promptfoo `providers` list through each provider's `config.model`; `OPENROUTER_CRITIQUE_MODEL` does not select the model for this suite. The custom usefulness grader calls `gpt-5.6-sol` directly through the OpenAI Responses API. Both providers load `OPENROUTER_API_KEY` and `AI_API_KEY` through kage.

Run just this suite from the repository root:

```sh
./task.sh evals EVAL=recipe-critique
```

The suite evaluates critique structure, defect detection, suggested fixes, false positives, brined/salty ingredient context, usefulness as judged by GPT-5.6 Sol, and a 30-second model-call latency budget. The candidate provider reports only the production critique call duration, excluding Promptfoo's provider startup, judge calls, and build time. Use `--no-cache` for model-selection runs so latency is measured from real requests.
