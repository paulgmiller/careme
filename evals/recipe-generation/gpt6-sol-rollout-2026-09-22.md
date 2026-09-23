# GPT-6 Sol production eval — 2026-09-22

Decision: use `gpt-6-sol` for menu planning and recipe generation at the existing
medium reasoning effort. The shared production default is
`internal/config/config.go`'s `DefaultRecipeModel`; the recipe and menu providers
both used that default in these runs.

## Recipe generation

The ten-case GPT-6 Sol run passed 8/10 cases with no provider errors. Mean judged
quality was 8.5/10 and mean generation time was 27.55 seconds. All ten generations
met the 60-second budget. Moroccan lamb and Chilean tri-tip scored 7/10 and failed
only `critique-quality`. Both pantry regression cases passed. Estimated generation
cost across the ten successful API responses was $0.543984.

The first eight cases match the [September 9 GPT-5.6 Sol and GPT-6 Astra
comparison](model-comparison-medium-2026-09-09.md). Restricting the new run to
those cases gives:

| Metric | GPT-5.6 Sol, Sep 9 | GPT-6 Astra, Sep 9 | GPT-6 Sol, Sep 22 |
| --- | ---: | ---: | ---: |
| Mean judged quality /10 | 8.125 | 9.125 | 8.375 |
| Quality at least 8/10 | 6/8 | 7/8 | 6/8 |
| Mean generation time | 66.72s | 36.46s | 26.92s |
| Generation under 60s | 2/8 | 8/8 | 8/8 |
| All assertions passed | 2/8 | 7/8 | 6/8 |
| Estimated generation cost, eight cases | $1.154711 | $2.149268 | $0.483882 |

On these single samples, GPT-6 Sol was 9.54 seconds (26.2%) faster than Astra on
average, while mean judged quality was 0.75 points lower. The observed generation
cost was 77.5% lower. Cost includes observed prompt-cache reads and writes and is
not a controlled per-token price comparison.

| Case | Astra quality /10 | Sol quality /10 | Astra generation | Sol generation |
| --- | ---: | ---: | ---: | ---: |
| Yucatecan chicken | 9 | 8 | 37.45s | 25.67s |
| Korean tri-tip | 10 | 9 | 35.32s | 25.77s |
| Moroccan lamb | 8 | 7 | 51.44s | 35.07s |
| Sichuan chicken | 6 | 9 | 32.98s | 26.81s |
| Lemon-Parmesan coho | 10 | 9 | 39.27s | 37.00s |
| Jicama cucumber side | 10 | 10 | 25.10s | 18.11s |
| Chilean tri-tip | 10 | 7 | 34.38s | 26.75s |
| Sumac-pistachio coho | 10 | 8 | 35.76s | 20.20s |

The adjacent [CSV](gpt6-sol-rollout-2026-09-22.csv) retains all ten new recipe
scores, generation times, estimated generation costs, pass status, and failed
metrics without recipe bodies. Historical Astra numbers remain in the [September 9
CSV](model-comparison-medium-2026-09-09.csv).

## Menu planning

The production-model menu suite passed 3/3 cases with no provider errors. Menu
generation averaged 7.21 seconds (range 5.04–10.81 seconds), below the 20-second
assertion budget for every case. The minimum judge dimension was 8/10 for the
quick chicken case and 10/10 for each of the other two cases. This suite was not
part of the September 9 recipe-model comparison.

| Menu case | Minimum judge dimension /10 | Generation time | Passed |
| --- | ---: | ---: | --- |
| Quick seasonal chicken dinner | 8 | 5.79s | Yes |
| Vegetarian, long catalog names, stovetop only | 10 | 5.04s | Yes |
| Three dinners, limited saffron assigned once | 10 | 10.81s | Yes |

## Method and limits

- Recipe eval ID: `eval-yjR-2026-09-23T02:36:52`; menu eval ID:
  `eval-ZG7-2026-09-23T02:32:35` (UTC IDs; runs began September 22 Pacific time).
- Commands: `./task.sh evals EVAL=recipe-generation -- --no-cache --output /tmp/recipe-generation-gpt-6-sol-20260922.json`
  and `./task.sh evals EVAL=menu-plan -- --no-cache --output /tmp/menu-plan-gpt-6-sol-20260922.json`.
  Promptfoo 0.123.0 used concurrency eight for recipes and four for menus. The
  recipe eval left its effort override empty, so production code sent `medium`.
  The judge remained `google/gemini-3.1-pro-preview`.
- The September 9 comparison explicitly requested medium effort and ran at
  concurrency one on revision `c447c31c58a30b0aafe866f0ff1e5a95a41c7658`.
  The new run used the current working tree based on
  `6dfbaa54e0b7197b582d26395e61692cc937d046`, including the Sol default and
  later prompt and assertion changes. Both use retained menu continuations,
  which require accessible OpenAI response IDs. Promptfoo caching was disabled;
  OpenAI prompt caching remained active.
- Each case has one sample and a model judge. Differences in code, concurrency,
  service load, and cache hits limit causal claims about model quality and
  latency. Generation latency excludes judging and Go startup but includes SDK
  retries. Promptfoo's zero token totals do not mean the API calls used no
  tokens; new exports do not include token counts.
- Full outputs and critiques remain in local `/tmp` JSON exports, not the
  repository. Promptfoo exited 100 for the recipe run because two assertions
  failed; the menu run exited zero.
