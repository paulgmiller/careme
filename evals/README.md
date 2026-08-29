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

`recipe-critique/promptfooconfig.yaml` runs the production recipe critique prompt and schema against hand-reviewed recipe fixtures. A case accepts either a complete `vars.recipe` object or a `vars.recipe_hash`, but not both. Hash cases read `recipe/<hash>` from the configured cache, so export the cache credentials before running Promptfoo when the recipe is not in the local file cache.

Run just this suite from the repository root:

```sh
./task.sh evals EVAL=recipe-critique
```

The current suite evaluates critique structure, defect detection, suggested fixes, false positives, and brined/salty ingredient context. The planned recipe-revision stage remains separate so it can later send both the recipe and critique to the recipe-generation model and measure whether the feedback is actionable.
