# GPT-6.1 Sol vs. GPT-6 Sol — 2026-09-29

Defaults switched to `gpt-6.1-sol` for recipe generation and meal planning through
`internal/config/config.go`'s shared `DefaultRecipeModel`. Medium reasoning, prompts,
output schemas, and the Gemini judge remain unchanged. Both models passed all 13
cases. This single-sample comparison shows slightly higher recipe quality and faster
recipe generation for GPT-6.1 Sol, but slower meal planning with equal menu scores.

## Recipe generation

| Metric | GPT-6 Sol | GPT-6.1 Sol |
| --- | ---: | ---: |
| All assertions passed | 10/10 | 10/10 |
| Mean judged quality /10 | 8.6 | 8.7 |
| Mean generation time | 39.39s | 35.18s |
| Generation within 60 seconds | 10/10 | 10/10 |
| Estimated generation cost, ten cases | $0.548173 | $0.463282 |
| API/provider errors | 0 | 0 |

GPT-6.1 Sol generated recipes 4.21 seconds (10.7%) faster on average.
Observed generation cost was 15.5% lower. Cache hits and writes differed, so this
is not a controlled per-token cost comparison. Both pantry regressions passed.

| Case | GPT-6 quality /10 | GPT-6.1 quality /10 | GPT-6 time | GPT-6.1 time |
| --- | ---: | ---: | ---: | ---: |
| Yucatecan chicken | 9 | 9 | 35.96s | 32.65s |
| Korean tri-tip | 9 | 10 | 43.10s | 34.00s |
| Moroccan lamb | 8 | 8 | 53.10s | 33.91s |
| Sichuan chicken | 9 | 8 | 43.79s | 40.82s |
| Lemon-Parmesan coho | 10 | 9 | 30.08s | 36.90s |
| Jicama cucumber side | 9 | 9 | 22.67s | 31.16s |
| Chilean tri-tip | 8 | 8 | 41.78s | 40.95s |
| Sumac-pistachio coho | 8 | 9 | 44.92s | 35.96s |
| Pantry: catalog garlic | 8 | 8 | 40.38s | 36.07s |
| Pantry: omit cooking water | 8 | 9 | 38.09s | 29.41s |

Passing means the suite thresholds were met, not that recipes were defect-free.
The judge still identified concerns, including lamb shoulder tenderness for GPT-6 Sol
and seasoning, timing, or doneness cues for GPT-6.1 Sol. Scores are judge assessments,
not independently adjudicated defects.

## Meal planning

Both models passed 3/3 cases. Every menu received 10/10 on request fidelity,
culinary coherence, and meaningful variety, with no judge issues. All generations
met the checked-in 20-second budget.

GPT-6.1 Sol averaged 11.15s versus 6.67s for GPT-6 Sol: 67.1% slower.
The three-dinner case took 16.11s, leaving less room within the latency budget.

| Menu case | GPT-6 time | GPT-6.1 time | Both passed |
| --- | ---: | ---: | --- |
| Quick seasonal chicken dinner | 6.11s | 8.00s | Yes |
| Vegetarian, long catalog names, stovetop only | 5.19s | 9.32s | Yes |
| Three dinners, limited saffron assigned once | 8.71s | 16.11s | Yes |

## Method and limits

- Ten identical checked-in recipe cases and three identical menu cases, one sample
  per case per model. No failed-case reruns. Recipe effort explicitly set to
  `medium`; the production menu path explicitly uses `medium`.
- Promptfoo 0.123.0; recipe concurrency eight, menu concurrency four; `--no-cache`.
  The fixed judge was `google/gemini-3.1-pro-preview` for both suites and models.
- Runs used the working tree based on `d4351f2ce037556afd7f2d4f79020b5fd5bbbe5d`,
  with GPT-6.1 pricing and menu model selection added. The default change did not
  affect candidates: each network-enabled run selected its model explicitly.
- Recipe baseline started at 23:12 UTC; the other runs started around 23:21 UTC.
  The latter runs overlapped. Service load, local compilation/testing load, cache
  differences, and one sample per case limit causal and reliability claims.
- Recipe cases continue retained menu response IDs; the menu suite separately
  measures fresh planning. This does not test fresh menus expanded end to end,
  recipe revisions, or menu regeneration.
- Latency measures generation including SDK retries, excluding judging and Go
  startup. Promptfoo caching was disabled; OpenAI prompt caching remained active.
- Recipe costs estimate successful generation responses only using standard
  short-context rates, including cache reads/writes and reasoning output.
  Judge cost is excluded. Menu costs and token counts are not exported; zero
  Promptfoo token totals do not indicate free usage.
- GPT-6.1 pricing was verified against the [official model documentation](https://developers.openai.com/api/docs/models/gpt-6.1-sol): per million tokens,
  input $2, cached input $0.10, cache writes $2.50, output $10.
- Initial sandbox attempts failed at npm DNS resolution before model calls.
  Network-enabled runs all completed with exit status zero and no provider errors.

## Reproduction and artifacts

```sh
./task.sh evals EVAL=recipe-generation MODEL=gpt-6-sol REASONING_EFFORT=medium -- --no-cache --output /tmp/careme-recipe-gpt6-20260929.json
./task.sh evals EVAL=recipe-generation MODEL=gpt-6.1-sol REASONING_EFFORT=medium -- --no-cache --output /tmp/careme-recipe-gpt61-20260929.json
./task.sh evals EVAL=menu-plan MODEL=gpt-6-sol -- --no-cache --output /tmp/careme-menu-gpt6-20260929.json
./task.sh evals EVAL=menu-plan MODEL=gpt-6.1-sol -- --no-cache --output /tmp/careme-menu-gpt61-20260929.json
```

| Suite | Model | Evaluation ID |
| --- | --- | --- |
| recipe | gpt-6-sol | `eval-8Gv-2026-09-29T23:12:55` |
| recipe | gpt-6.1-sol | `eval-lsS-2026-09-29T23:21:22` |
| menu | gpt-6-sol | `eval-ddz-2026-09-29T23:21:18` |
| menu | gpt-6.1-sol | `eval-hGW-2026-09-29T23:21:28` |

The adjacent [CSV](gpt61-sol-comparison-2026-09-29.csv) retains per-case scores,
generation times, estimated recipe costs, and pass status without recipe or menu
bodies. Full outputs and critiques remain in local `/tmp` JSON exports; those
files are temporary, not durable repository artifacts.

## Verification

`./task.sh verify-go` passed formatting, vet, all Go tests, and lint (zero issues).
The first sandboxed verification attempt failed because existing HTTP tests could
not bind local sockets; the complete network-enabled rerun passed.
Regression coverage checks the new default in recipe and menu API requests,
medium reasoning, model override precedence, and GPT-6.1 cache pricing.
