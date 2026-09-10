# New vs. old recipe prompt — 2026-09-10

Decision: restore the previous recipe and menu prompt text. The numeric results
below describe the experimental prompt before that revert. The menu prompt itself
was not evaluated here.

The new prompt has mixed quality results: Astra decreased on the seven matched scored cases, while Sol improved slightly across eight. These single samples do not establish a reliable prompt improvement.

| Matched-case metric | Astra old → new (7 cases) | Sol old → new (8 cases) |
| --- | ---: | ---: |
| Mean quality /10 | 9.57 → 8.86 | 8.13 → 8.38 |
| Mean generation seconds | 36.96 → 36.30 | 66.72 → 60.04 |
| Generation USD, matched cases | 2.041818 → 1.796802 | 1.154711 → 1.103858 |

Astra passed all seven scored cases; Sichuan chicken generation succeeded but its judge returned an empty response and failed JSON parsing. Its missing score is excluded from both sides of the matched comparison, not counted as zero. Sol passed three complete cases, failed quality on Moroccan lamb (5/10) and Sichuan chicken (6/10), and exceeded 60 seconds on Korean tri-tip, Chilean tri-tip, and Sumac-pistachio coho. Six of eight Sol quality scores met 8/10, unchanged from the baseline.

## Per-case comparison

Generation USD uses the same application rates as the corrected September 9 baseline. See the [numeric CSV](prompt-comparison-medium-2026-09-10.csv). Blank new values mark unavailable provider results.

| Model | Case | Quality old → new | Seconds old → new | Generation USD old → new |
| --- | --- | ---: | ---: | ---: |
| gpt-6-astra | Yucatecan chicken | 9 → 9 | 37.45 → 35.22 | 0.325235 → 0.316535 |
| gpt-6-astra | Korean tri-tip | 10 → 9 | 35.32 → 36.87 | 0.334047 → 0.330847 |
| gpt-6-astra | Moroccan lamb | 8 → 9 | 51.44 → 48.47 | 0.367063 → 0.138289 |
| gpt-6-astra | Sichuan chicken | 6 → judge error | 32.98 → unavailable | 0.107451 → unavailable |
| gpt-6-astra | Lemon-Parmesan coho | 10 → 10 | 39.27 → 33.51 | 0.319895 → 0.303795 |
| gpt-6-astra | Jicama cucumber side | 10 → 9 | 25.10 → 21.70 | 0.269703 → 0.262902 |
| gpt-6-astra | Chilean tri-tip | 10 → 8 | 34.38 → 35.03 | 0.110045 → 0.108803 |
| gpt-6-astra | Sumac-pistachio coho | 10 → 8 | 35.76 → 43.32 | 0.315830 → 0.335630 |
| gpt-5.6-sol | Yucatecan chicken | 9 → 9 | 77.80 → 52.45 | 0.158798 → 0.155878 |
| gpt-5.6-sol | Korean tri-tip | 9 → 10 | 69.40 → 68.27 | 0.170779 → 0.173959 |
| gpt-5.6-sol | Moroccan lamb | 6 → 5 | 63.43 → 55.10 | 0.175133 → 0.170953 |
| gpt-5.6-sol | Sichuan chicken | 8 → 6 | 71.13 → 40.69 | 0.087672 → 0.060696 |
| gpt-5.6-sol | Lemon-Parmesan coho | 9 → 8 | 53.78 → 51.11 | 0.140674 → 0.136494 |
| gpt-5.6-sol | Jicama cucumber side | 9 → 10 | 42.65 → 54.20 | 0.135741 → 0.062329 |
| gpt-5.6-sol | Chilean tri-tip | 6 → 10 | 91.91 → 85.98 | 0.115890 → 0.176345 |
| gpt-5.6-sol | Sumac-pistachio coho | 9 → 9 | 63.64 → 72.54 | 0.170024 → 0.167204 |

## Method and limits

- Baseline: [September 9 report](model-comparison-medium-2026-09-09.md), revision `c447c31c58a30b0aafe866f0ff1e5a95a41c7658`. New revision: `549adab3f14c4eac7697c9a2aa6548ddd2aeb613`.
- Same eight checked-in cases, explicit medium reasoning, fixed Gemini judge, Promptfoo 0.122.0, and disabled Promptfoo cache. Recipe prompt changed; judge prompt and case assertions are unchanged.
- Each model ran at concurrency eight, with both model runs overlapping (up to sixteen cases total). Historical concurrency was one. Remote execution dominates; concurrency does not inherently increase per-call latency, though rate limits, retries, or service queueing can affect it.
- Logs confirmed default service tier for all sixteen generations. No generation errors occurred. Astra had one judge error; Sol had no provider errors. Neither completed nor failed cases were rerun. Task exit status was nonzero for both runs because of the judge error and assertion failures.
- Retained menu responses hold historical menu context. This measures recipe expansion, not the changed menu-planning prompt or recipe revisions.
- OpenAI prompt cache remains active. Cache reads shifted between cases: Astra's lamb became a cache hit while Sichuan chicken became a write; Sol's jicama became a hit while Chilean tri-tip became a write. Cost changes therefore include cache effects.
- Astra's failed-judge case is omitted from the matched cost total, but generation was billed: its usage log records $0.3610825. All eight Astra generations therefore cost $2.1578845, versus $2.1492685 historically (roughly unchanged). The failed provider result discards generation latency and cost; the CSV leaves them blank rather than substituting total provider wall time.
- Generation latency excludes judging and Go startup but includes SDK retries. One sample per case and a model judge provide preliminary evidence, not statistical confidence.
- Current provider exports generation cost only, with critique metadata; judge cost, judge latency, and token usage remain in logs. No recipe bodies are checked in.

## Reproduction and artifacts

Use the existing encrypted credentials via `KAGE_SECRET_FILE`:

```sh
./task.sh evals EVAL=recipe-generation MODEL=gpt-6-astra REASONING_EFFORT=medium -- --no-cache --max-concurrency 8 --output /tmp/recipe-eval-astra-new-medium-20260910.json
./task.sh evals EVAL=recipe-generation MODEL=gpt-5.6-sol REASONING_EFFORT=medium -- --no-cache --max-concurrency 8 --output /tmp/recipe-eval-sol-new-medium-20260910.json
```

The two commands ran concurrently. Evaluation IDs: `eval-Lz6-2026-09-10T16:50:18` (Astra), `eval-HL3-2026-09-10T16:50:34` (Sol). Full outputs remain in the temporary JSON exports and local Promptfoo database. The recipe suite now defaults to concurrency eight.
