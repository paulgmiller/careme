# Astra recipe prompting

The menu and recipe prompts in `internal/ai` define output contracts and constraints
with minimal repetition. Recipe quality checks cover cross-field consistency. Menu ingredient labels retain full catalog descriptions;
variety and fancy options yield to dietary, time, and equipment constraints.
Recipe expansion is scoped to the selected plan, including its assigned user
directions. Both stages complete structured output without routine follow-up
questions. Recipe checks cover ingredient usage and elapsed time with parallel work.

These changes follow [OpenAI's Astra guidance](https://developers.openai.com/api/docs/guides/latest-model)
on explicit completion, instruction priorities, and output style. They are prompt
improvement hypotheses, not measured quality gains.

## Recommended starting efforts

| Operation | Start with | Reason |
| --- | --- | --- |
| Create menu | `medium` | Balance constraints and variety across several dishes, and allocate limited ingredients. |
| Regenerate menu | `medium` | Reconcile feedback with retained preferences and saved meals. |
| Repair unavailable menu ingredients | `low` | A narrow correction against an existing catalog; compare with medium if other constraints regress. |
| Generate recipe | `medium` | Coordinate quantities, servings, timing, methods, and the selected plan. |
| Regenerate recipe | `medium` | Ingredient or serving changes affect quantities, timing, and nutrition together. |
| Answer recipe question | `low` | Usually a short explanation grounded in an existing recipe. |

These are workload-specific recommendations, not OpenAI benchmarks. Compare `low`
and `medium` first; test `high` only if repeated failures on complicated recipes
justify the latency and cost. Astra supports `low`, `medium`, `high`, `xhigh`, and
`max`, but not `none`. See the [Astra model reference](https://developers.openai.com/api/docs/models/gpt-6-astra).

The production recipe model is now `gpt-6-astra`. Menu creation, regeneration,
menu ingredient repair, recipe generation/revision, and recipe questions explicitly
use `medium` effort. This keeps effort consistent across stored continuations.
The lower-effort recommendations above remain future evaluation candidates.
The recipe eval accepts `REASONING_EFFORT=low`, `medium`, or another supported
effort; omit it to use the production medium default.

## Validation before changing defaults

Compare the original and revised prompts on the same model and effort first,
then compare efforts with the chosen prompt. Use repeated samples and record
quality, latency, reasoning tokens, and cost. Include long catalog descriptions,
vegetarian restrictions, limited ingredients assigned to one dish, saved fancy
meals, conflicting time defaults, and recipe revisions. Check count, exact catalog
matches, dietary compliance, per-plan assignments, ingredient quantities, and
realistic timing in addition to the recipe critique score.

The menu eval includes a long-description vegetarian regression case. The recipe
eval can select Astra with:

```sh
./task.sh evals EVAL=recipe-generation MODEL=gpt-6-astra -- --no-cache --repeat 3 --output /tmp/astra-recipe-eval.json
```

See [eval setup and credential requirements](../evals/README.md). Live evals make
paid API calls and recipe cases require accessible retained menu responses.
