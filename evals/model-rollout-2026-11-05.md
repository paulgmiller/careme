# Model rollout regression results — November 5, 2026

Live calls used the production paths at `72fc789` plus the new farmers market eval harness. Shopping merging uses `gpt-6-luna` with `reasoning.effort=none`; farmers market extraction uses `gpt-6.1-terra` with its existing unspecified reasoning setting. No production prompts or model settings changed during evaluation.

Each suite has three cases, repeated three times at concurrency one with Promptfoo 0.123.0. Promptfoo caching was disabled; OpenAI prompt caching remained active (cache hits occurred in the farmers market runs). Credentials loaded through config/kage and `secrets/envtest`.

## Final runs

| Suite | Model | Passed | Median latency | Range |
| --- | --- | --- | --- | --- |
| shopping-merge | `gpt-6-luna` | 9/9 | 1,303 ms | 996–2,499 ms |
| farmers-market | `gpt-6.1-terra` | 9/9 | 1,614 ms | 1,274–2,260 ms |

Latency is measured around the production call, including retries and decoding, excluding configuration and Go compilation. Neither provider exports cost or token usage; Promptfoo cost totals are not billing estimates.

## Coverage and observations

- Shopping merge: onion singular/plural and approximate-count annotations produce 8 oz; divided oil and Unicode fractions produce 4½ tablespoons; a 40 oz need rounds to three 16 oz packages. All nine outputs matched the expected IDs, quantities, and buy values.
- Farmers market: labeled eggs preserve River Farm, $6, and 12 eggs per carton; unlabeled price/size on Hill Farm tomatoes stays absent; soap produces no ingredients. Fixtures are deterministic synthetic illustrations with printed labels. These results do not establish accuracy on real photos, handwriting, occlusion, crowded stalls, or ambiguous pricing.
- These are regression checks of the new models, not an old/new model comparison. Nine samples per suite are too few to estimate reliability.

## Assertion correction

The first farmers market run passed 6/9 with zero API errors. All three egg cases returned the faithful visible size `12 eggs per carton`, but the initial assertion required exactly `12 eggs`. The assertion now accepts either spelling. The production response and prompts were unchanged, and the corrected suite was rerun with fresh API calls. Both runs are retained in the CSV; initial assertion failures are not model extraction failures.

## Reproduction

```sh
./task.sh evals EVAL=shopping-merge -- --no-cache --repeat 3 --output /tmp/careme-shopping-merge-gpt6-luna.json
./task.sh evals EVAL=farmers-market -- --no-cache --repeat 3 --output /tmp/careme-farmers-market-gpt61-terra-corrected.json
```

| Suite / stage | Eval ID |
| --- | --- |
| shopping-merge / final | `eval-wbn-2026-11-05T22:14:37` |
| farmers-market / initial assertion | `eval-Xij-2026-11-05T22:17:09` |
| farmers-market / final | `eval-1ie-2026-11-05T22:18:33` |

[Per-case results, outputs, latency, and assertion outcomes](model-rollout-2026-11-05.csv) are saved alongside this report. Full Promptfoo JSON remains under `/tmp` and in the local Promptfoo history. No generated recipe bodies or credentials are recorded.

Validation: `go test ./evals/farmers-market`, `./task.sh verify-go`, and `git diff --check` passed. The initial farmers market eval exited Promptfoo 100 (task 201) for the assertion mismatch; both final eval commands exited 0.
