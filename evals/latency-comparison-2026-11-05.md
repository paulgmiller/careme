# Old/new model latency comparison — November 5, 2026

This compares GPT-5.6 Luna with GPT-6 Luna for shopping quantity merging, and GPT-Image 2.5 Flare with GPT-Image 2.6 Flare for recipe images. Farmers market extraction was not rerun.

Each model used three repeats per case at concurrency one with Promptfoo 0.123.0 and `--no-cache`: nine merge calls and six image calls per model. These are fresh comparison runs, separate from the earlier rollout smoke checks. Runs were sequential: old merge, new merge, old image, new image. API-side prompt caching remained enabled.

## Results

| Suite | Model | Passed | Median | Min–max |
| --- | --- | --- | --- | --- |
| shopping-merge | `gpt-5.6-luna` | 9/9 | 1.263s | 0.955–1.476s |
| shopping-merge | `gpt-6-luna` | 9/9 | 1.303s | 1.098–3.452s |
| image-latency | `gpt-image-2.5-flare` | 3/6 | 11.046s | 9.702–12.083s |
| image-latency | `gpt-image-2.6-flare` | 6/6 | 8.062s | 7.404–9.280s |

All four runs had zero API/provider errors. GPT-Image 2.5 Flare missed the 11-second budget on one photo and two sketches (Promptfoo exit 100, task exit 201); the other three commands exited 0.

Image passes require latency below 11 seconds; merge passes require the existing quantity, product ID, and buy-multiple assertions. Image results test latency and successful image generation, not visual quality.

| Workload | Old median | New median | Change |
| --- | --- | --- | --- |
| Shopping merge (all cases) | 1.263s | 1.303s | +3.2% |
| Image photo | 10.801s | 8.089s | -25.1% |
| Image sketch | 11.632s | 8.034s | -30.9% |

Negative change means lower latency. Three image samples per style and nine merge samples per model are small samples; run order, cache state, and service load can affect the comparison.

## Matching settings and measurement

- Source: `72fc789`, with the same image eval timing fix applied in both checkouts. The working checkout retained GPT-6 Luna and GPT-Image 2.6 Flare. An isolated checkout changed only `shoppingMergeModel` to `openai.ChatModelGPT5_6Luna` and `DefaultImageModel` to `openai.ImageModelGPTImage2_5Flare`, plus the shared timing fix.
- Merge used the unchanged three checked-in cases, production prompt, and `reasoning.effort=none`. Only the model differed. Latency surrounds `MergeShoppingQuantities`, excluding configuration and Go compilation.
- Images used the same sheet-pan chicken recipe and the production photo/sketch prompts, medium quality, 1024×1024, WebP, one image per call. Only the model differed.
- The image provider now returns measured `latencyMs` around generation and reading the entire decoded image. Both old/new runs use this interval. Earlier image evals without this field may include Go wrapper overhead and should not be compared directly.
- Credentials used config/kage with `KAGE_SECRET_FILE` pointing to the existing `secrets/envtest`. No API keys or image bodies are stored in the report.
- Neither suite exports cost. Old merge logs report `price_not_configured` because GPT-5 pricing was removed; this does not fail merging or affect latency assertions.

## Reproduction

In the isolated old-model checkout and then the normal checkout, run:

```sh
./task.sh evals EVAL=shopping-merge -- --no-cache --repeat 3 --output /tmp/merge-comparison.json
./task.sh evals EVAL=image-latency -- --no-cache --repeat 3 --output /tmp/image-comparison.json
```

The old-model checkout must apply the same image provider timing fix. Its two model-constant substitutions are listed above; production defaults remain unchanged.

| Suite / model | Eval ID |
| --- | --- |
| shopping-merge / `gpt-5.6-luna` | `eval-7nb-2026-11-05T22:24:11` |
| shopping-merge / `gpt-6-luna` | `eval-U1b-2026-11-05T22:25:31` |
| image-latency / `gpt-image-2.5-flare` | `eval-WWN-2026-11-05T22:26:46` |
| image-latency / `gpt-image-2.6-flare` | `eval-tLf-2026-11-05T22:28:21` |

[Per-case results and latency](latency-comparison-2026-11-05.csv) include every measured sample. Raw JSON and logs are under `/tmp/careme-comparison-*` and in local Promptfoo history.

Validation: `./task.sh verify-go` and `git diff --check` passed. A deterministic clock test verifies image timing.
