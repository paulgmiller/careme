# Ingredient grading: Decisions vs Luna — 2026-10-08

Decisions graded faster but missed more fixture bounds and cost more in this run. It passed 87/92 ingredients versus 91/92 for both the fresh and saved Luna runs. Defaults and score bounds were unchanged.

| Metric | Saved Luna (Oct 1) | Fresh Luna Responses | Decisions |
| --- | ---: | ---: | ---: |
| Batches passing every bound | 22/23 | 22/23 | 19/23 |
| Ingredients within bounds | 91/92 | 91/92 | 87/92 |
| API/provider errors or missing grades | 0 | 0 | 0 |
| Mean grading latency per four-item batch | 2.882s | 3.581s | 0.388s |
| Median batch latency | 2.644s | 3.215s | 0.337s |
| Min–max batch latency | 2.037–4.503s | 2.577–10.418s | 0.151–0.935s |
| Estimated API cost for all 92 ingredients | Not recorded | $0.0032029 | $0.0084595 |
| Cost per 1,000 ingredients at this workload | Not recorded | $0.03481 | $0.09195 |
| Successful API requests | 23 | 23 | 92 |
| Input tokens | Not recorded | 18,739 | 84,595 |
| Output tokens | Not recorded | 2,658 | 0 |

Decisions used 86.5% less mean latency than the saved Luna run (7.43× faster). Against the fresh price-instrumented run, Decisions was 9.24× faster and 2.64× as expensive. Separate per-ingredient requests repeat the instruction and ten-level rubric; Decisions used 4.51× as many input tokens. The overall charge was still below one cent.

## Failed expectations

| Ingredient | Expected | Saved Luna | Fresh Luna | Decisions |
| --- | --- | ---: | ---: | ---: |
| Steamfresh Carrots | 4–6 | 7 (fail) | 7 (fail) | 5 |
| Spaghetti Pasta | 7–10 | 7 | 7 | 6 (fail) |
| Whole Wheat Penne Pasta | 7–10 | 7 | 7 | 6 (fail) |
| Garlic Herb Marinated Chicken Breasts | 6–8 | 7 | 7 | 4 (fail) |
| Pearled Farro | 8–9 | 8 | 8 | 7 (fail) |
| Plain Tomato Paste | 6–8 | 7 | 7 | 5 (fail) |

Four Decisions misses (spaghetti, whole wheat penne, marinated chicken, and tomato paste) also fall at or below the production cutoff of 6, so they would be excluded from ingredient selection. Pearled farro still exceeds the cutoff. Steamfresh Carrots meets its bound under Decisions; both Luna runs score it too high. No prompts, score conversion, or fixture bounds were adjusted to improve results.

## Per-batch latency and cost

| Batch | Saved Luna time | Fresh Luna time | Decisions time | Fresh Luna USD | Decisions USD |
| --- | ---: | ---: | ---: | ---: | ---: |
| Fresh vegetables vs prepared vegetables | 2.568s | 3.024s | 0.374s | $0.0001360 | $0.0003682 |
| Whole grains vs prepared grains | 2.954s | 2.682s | 0.769s | $0.0001383 | $0.0003682 |
| Fresh fruit vs prepared fruit | 2.575s | 3.202s | 0.151s | $0.0001368 | $0.0003680 |
| Plain vegetables vs increasingly prepared forms | 2.121s | 2.881s | 0.463s | $0.0001431 | $0.0003685 |
| Pasta and convenience foods | 2.182s | 3.154s | 0.349s | $0.0001400 | $0.0003677 |
| Raw meat vs seasoned and fully prepared meat | 2.644s | 4.571s | 0.478s | $0.0001394 | $0.0003684 |
| Raw, cooked, and snack-format seafood | 2.473s | 2.577s | 0.935s | $0.0001408 | $0.0003678 |
| Uncommon vegetables remain strong ingredients | 4.503s | 3.475s | 0.690s | $0.0001329 | $0.0003669 |
| Organ meats and bones are valid cooking ingredients | 4.081s | 3.296s | 0.176s | $0.0001374 | $0.0003669 |
| Flexible dairy and eggs | 2.442s | 10.418s | 0.217s | $0.0001312 | $0.0003667 |
| Dry legumes vs convenient prepared legumes | 3.556s | 3.071s | 0.174s | $0.0001409 | $0.0003674 |
| Herbs and spices vs seasoning mixes | 4.001s | 3.681s | 0.551s | $0.0001291 | $0.0003666 |
| Explicit dry pasta score anchors | 3.785s | 3.215s | 0.348s | $0.0001356 | $0.0003690 |
| Real pasta quality signals vs generic marketing claims | 2.845s | 2.803s | 0.337s | $0.0001380 | $0.0003703 |
| Grain cooking signals vs instant flavored mixes | 2.701s | 2.938s | 0.717s | $0.0001450 | $0.0003675 |
| Bread and prepared sauce anchors | 3.270s | 3.420s | 0.333s | $0.0001479 | $0.0003674 |
| Ready-to-eat meals, kits, bowls, and trays | 2.777s | 4.282s | 0.205s | $0.0001358 | $0.0003683 |
| Dips, gravies, mixes, and prepared sides | 2.341s | 3.693s | 0.237s | $0.0001372 | $0.0003677 |
| Whole, pre-cut, and cooking-friendly fruit formats | 2.456s | 2.924s | 0.183s | $0.0001443 | $0.0003678 |
| Diverse flexible ingredients that are difficult to make at home | 2.607s | 3.449s | 0.177s | $0.0001423 | $0.0003673 |
| Plain ground meat vs sausage and cured meat | 2.037s | 2.824s | 0.682s | $0.0001380 | $0.0003675 |
| Seasonal produce remains a strong ingredient | 2.732s | 3.410s | 0.194s | $0.0001428 | $0.0003673 |
| Minimally processed staples vs sauced versions | 2.629s | 3.376s | 0.175s | $0.0001501 | $0.0003681 |

## Method and pricing

- Promptfoo 0.123.0, evaluator concurrency 16, `--no-cache`, the same 23 four-ingredient batches and 92 fixture ingredients, one sample per case. Each case uses a fresh in-memory grade cache and the production manager.
- Decisions uses `gpt-6-luna`, one independent request per ingredient, the existing ten-level rubric, and rounded zero-based score plus one. Its limit is 64 concurrent requests **per grader instance**. Each eval case creates its own grader and has only four items, so this suite does not exercise the per-instance ceiling of 64; evaluator concurrency can produce roughly 64 simultaneous calls across 16 cases.
- Luna uses `gpt-6-luna`, `none` reasoning and one structured Responses request per four-item batch. These are different request shapes and scoring instructions, not an isolated endpoint benchmark.
- Latency is the exported provider `response.latencyMs`: grading-manager elapsed time, including concurrent requests and SDK retries, excluding configuration, Go compilation/startup, and Promptfoo scheduling. Use this field rather than wrapper timing. Fresh and historical results were collected on different dates; one sample per case is not a reliability estimate.
- Costs use actual API-reported token usage captured by the eval HTTP transport. No request headers, credentials, or generated recipe outputs are retained. The saved Luna run did not export usage; its historical price cannot be recovered, so the fresh Luna run supplies the measured cost comparison.
- [Decisions pricing](https://developers.openai.com/api/docs/guides/decisions): $0.10 per million input tokens, no output/cache-read/cache-write charges. [Luna model pricing](https://developers.openai.com/api/docs/models/gpt-6-luna): standard $0.10 input, $0.01 cached input, $0.125 cache writes, and $0.50 output per million tokens. Fresh Responses returned `service_tier=default`; both runs reported zero cached or cache-write tokens. Estimates exclude taxes, account-specific discounts, and any usage not reported by successful API responses.

## Reproduction and artifacts

```sh
INGREDIENT_GRADING_MODEL=decisions ./task.sh evals EVAL=ingredient-grader -- --no-cache --output /tmp/careme-decisions-20261008.json
INGREDIENT_GRADING_MODEL=gpt-6-luna ./task.sh evals EVAL=ingredient-grader -- --no-cache --output /tmp/careme-luna-price-20261008.json
```

Credentials load via the existing config/kage `secrets/envtest` path. Both evals completed all cases and exited Promptfoo 100 (task 201) because score assertions failed; these were grading mismatches, not environment or API failures.

| Run | UTC start | Evaluation ID |
| --- | --- | --- |
| Saved Luna | 2026-10-01T15:43:08 | `eval-2jg-2026-10-01T15:43:08` |
| decisions | 2026-10-08T18:01:29.000Z | `eval-x6W-2026-10-08T18:01:29` |
| luna-fresh | 2026-10-08T18:02:02.410Z | `eval-EEk-2026-10-08T18:02:02` |

- [Ingredient scores CSV](decisions-comparison-2026-10-08.csv): all three runs, with fixture bounds and pass status.
- [Batch metrics CSV](decisions-comparison-2026-10-08-batches.csv): latency, cost, API request counts and token usage; historical usage/cost cells are empty because they were not recorded.
- Full JSON exports remain under `/tmp` at the paths above. Historical source: [October 1 comparison](jev-comparison-2026-10-01.md) and its checked-in CSV.

## Verification

`./task.sh verify-go -- -cover` passed formatting, vet, tests, and lint (zero issues). Cost tests cover Decisions input-only billing, Responses input/cache/output and processing-tier rates, response-body preservation, aggregation, and missing-usage rejection. `git diff --check -- evals` passed. The full diff check flags trailing whitespace
in an unrelated concurrent edit to `docs/cache-layout.md`; that edit was left untouched.
