# Promptfoo evals

Generate recipe test cases from a cached shopping-list hash:

```sh
go run ./cmd/evalcase -hash HASH -secret-file secrets/envprod
```

The command reads the selected cache and emits one YAML case per stored recipe plan. Paste the sequence under `tests:` in `recipe-generation/promptfooconfig.yaml`.

Each generated case contains one recipe plan, the menu response ID, and its prompt-cache key. Recipe evals use only those checked-in values and the AI API; the provider does not connect to production cache storage.
