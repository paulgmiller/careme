# Astra vs. Sol at medium reasoning — 2026-09-09

Astra used 74.9% fewer reasoning tokens and took 45.3% less generation time on average. Estimated generation cost was $2.1493 for Astra and $1.1547 for Sol across eight cases, using current documented rates.

| Metric | Astra | Sol |
| --- | ---: | ---: |
| Mean quality /10 | 9.125 | 8.125 |
| Mean generation seconds | 36.46 | 66.72 |
| Mean reasoning tokens | 608.5 | 2425.8 |
| Mean output tokens (includes reasoning) | 1634.2 | 3228.0 |
| Generation USD, all eight cases | 2.149268 | 1.154711 |
| Judge USD, all eight cases | 0.342216 | 0.400768 |
| Cached input tokens | 36,706 | 36,706 |
| Cache-write tokens | 111,905 | 111,905 |
| Quality ≥8/10 | 7/8 | 6/8 |
| Generation within 60 seconds | 8/8 | 2/8 |
| All assertions passed | 7/8 | 2/8 |
| API/provider errors in completed cases | 0 | 0 |

## Per-case results

Generation costs below use current documented rates. Full numeric results, including the originally recorded costs, are in the [CSV](model-comparison-medium-2026-09-09.csv).

| Case | Astra quality | Sol quality | Astra seconds | Sol seconds | Astra reasoning | Sol reasoning | Astra USD | Sol USD |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Yucatecan chicken | 9 | 9 | 37.45 | 77.80 | 672 | 2237 | 0.325235 | 0.158798 |
| Korean tri-tip | 10 | 9 | 35.32 | 69.40 | 516 | 2286 | 0.334047 | 0.170779 |
| Moroccan lamb | 8 | 6 | 51.44 | 63.43 | 987 | 2489 | 0.367063 | 0.175133 |
| Sichuan chicken | 6 | 8 | 32.98 | 71.13 | 452 | 2859 | 0.107451 | 0.087672 |
| Lemon-Parmesan coho | 10 | 9 | 39.27 | 53.78 | 785 | 1550 | 0.319895 | 0.140674 |
| Jicama cucumber side | 10 | 9 | 25.10 | 42.65 | 424 | 1498 | 0.269703 | 0.135741 |
| Chilean tri-tip | 10 | 6 | 34.38 | 91.91 | 516 | 3780 | 0.110045 | 0.115890 |
| Sumac-pistachio coho | 10 | 9 | 35.76 | 63.64 | 516 | 2707 | 0.315830 | 0.170024 |

## Failed assertions

- gpt-6-astra, Sichuan chicken: critique-quality (quality 6/10, generation 32.98s).
- gpt-5.6-sol, Yucatecan chicken: generation-latency-under-60s (quality 9/10, generation 77.80s).
- gpt-5.6-sol, Korean tri-tip: generation-latency-under-60s (quality 9/10, generation 69.40s).
- gpt-5.6-sol, Moroccan lamb: generation-latency-under-60s;critique-quality (quality 6/10, generation 63.43s).
- gpt-5.6-sol, Sichuan chicken: generation-latency-under-60s (quality 8/10, generation 71.13s).
- gpt-5.6-sol, Chilean tri-tip: generation-latency-under-60s;critique-quality (quality 6/10, generation 91.91s).
- gpt-5.6-sol, Sumac-pistachio coho: generation-latency-under-60s (quality 9/10, generation 63.64s).

## Method and accounting

- Eight identical checked-in cases per model, one sample each, explicitly requested `medium` reasoning. Fixed judge: `google/gemini-3.1-pro-preview`. No completed cases rerun.
- Promptfoo 0.122.0, concurrency one, Astra followed by Sol, `--no-cache`. Sol was interrupted after four saved cases; its remaining four ran about three hours later with the same original code and settings. This disables Promptfoo caching; OpenAI prompt caching remains active. Both models recorded the same cached-input and cache-write token counts on each matched case; input and output totals differed. Costs describe these observed calls.
- Generation time excludes judging and includes SDK retries. Quality is one model judge’s assessment; a single pass per case does not establish statistical confidence.
- All completed cases used revision `c447c31c58a30b0aafe866f0ff1e5a95a41c7658`, before the cost-only client refactor. These exports therefore contain token metadata. New evals obtain cost directly from the client and leave token details in logs.
- Original exports recorded generation totals of $2.149268 for Astra and $1.572509 for Sol. Sol used the old application rate table. Its costs above were recomputed from each response’s measured input, cached input, cache-write, and output counts; original exports were not modified.
- Rates per million tokens: Astra input $10, cached input $1, cache writes $12.50, output $50; Sol promotional input $4, cached input $0.40, cache writes $5, output $20. Sol’s documented promotion is available at least through November 21, 2026. Sources checked September 9: [Astra](https://developers.openai.com/api/docs/models/gpt-6-astra), [Sol](https://developers.openai.com/api/docs/models/gpt-5.6-sol). The application price table is updated to match.
- Reasoning tokens are included in output tokens and are charged once. Judge cost is OpenRouter’s reported cost. These estimates cover successful responses, not failed requests or account-specific discounts.

## Reproduction and artifacts

For fresh complete runs:

```sh
./task.sh evals EVAL=recipe-generation MODEL=gpt-6-astra REASONING_EFFORT=medium -- --no-cache --output /tmp/recipe-eval-astra-medium-20260909.json
./task.sh evals EVAL=recipe-generation MODEL=gpt-5.6-sol REASONING_EFFORT=medium -- --no-cache --output /tmp/recipe-eval-sol-medium-20260909.json
```

The interrupted Sol run was completed from an archive of the original revision with `--filter-pattern 'Lemon-Parmesan|lower-scoring'`, exporting to `/tmp/recipe-eval-sol-medium-remaining-20260909.json`.

- Astra evaluation: `eval-CGW-2026-09-09T17:31:24`.
- Sol evaluations: `eval-pBf-2026-09-09T17:40:26` (first four), `eval-Omr-2026-09-09T20:46:44` (remaining four).
- An initial resume attempt (`eval-3ms-2026-09-09T20:42:15`) failed credential-file discovery before any API calls; it is excluded. Setting `KAGE_SECRET_FILE` to the existing encrypted file resolved it. Promptfoo exit status 100 indicates assertion failures or provider errors; the task wrapper reports a nonzero exit even when every API call completed. See the per-case results for the failures.
- The combined Sol JSON was assembled from the four original database records and the four resumed results, matching cases by description. CSV rows retain the original evaluation ID for each case.
- Full recipe outputs remain in the local `/tmp` exports and Promptfoo database. Only numeric results are retained here; temporary exports are not durable repository artifacts.
