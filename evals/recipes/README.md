# Recipe Evals

`regression_cases.jsonl` is the starter regression suite for recipe generation.

Each line defines one case with:

- `case_id`
- `date`
- `location_state`
- `ingredients_path`
- `directive`
- `instructions`
- `last_recipes`
- `expected_recipe_count`
- `forbidden_terms`
- `required_terms`
- `notes`

`ingredients_path` is resolved relative to this directory when the suite is loaded.

Use:

```sh
go run ./cmd/recipeeval -dry-run
go run ./cmd/recipeeval -create
go run ./cmd/recipeeval -run -eval-id eval_123
```

The checked-in fixture data is intentionally small and synthetic. Replace or extend it with reviewed real generations as you build out the suite.
