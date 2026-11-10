# Gemini 4 Argon critique comparison — 2026-10-13

The production recipe critique default is now `google/gemini-4-argon`, replacing
`google/gemini-3.1-pro-preview`. `OPENROUTER_CRITIQUE_MODEL` still overrides it.
The recipe retry cutoff remains 8/10. Recipe and menu evals keep their pinned
Gemini 3.1 judges to preserve generation comparisons.

## Method

Fresh runs of the checked-in `recipe-critique` suite with Promptfoo 0.123.0,
`--no-cache --repeat 3`, concurrency one: two fixtures, six calls per model.
Both use the production critique prompt, JSON schema, and provider routing, with
no explicit reasoning-effort override. Credentials came from `secrets/envtest`
through kage. No API errors or observed retries occurred; usage logs report zero
cached tokens for all twelve calls.

Latency measures only the critique call, excluding Go compilation, provider
startup, and configuration loading. Cost is OpenRouter's successful-response
`usage.cost`, now exported as Promptfoo's `cost`; it excludes failed-call billing
and account adjustments. The two fixture types and three samples each are a
small smoke comparison, not a reliability or broad quality estimate.

## Results

| Model | Mean latency | Median latency | Latency range | Mean USD/call | Total USD | Overall passes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Gemini 3.1 Pro Preview | 17.331 s | 16.976 s | 12.943–21.335 s | $0.023448 | $0.140688 | 5/6 |
| Gemini 4 Argon | 7.7735 s | 8.085 s | 5.542–10.313 s | $0.0106967 | $0.064180 | 3/6 |

Argon reduced mean latency by 55.1% and mean cost by 54.4% in these runs.
Total reported cost for both runs was $0.204868. All returned model IDs matched
the requested model.

Both models passed all six JSON, critique-structure, and 30-second latency checks,
and all three burger-temperature defect checks. Burger scores were 3/10 for both;
chicken scores were 8, 9, 9 for Gemini 3.1 and 8, 8, 8 for Argon.

The only failed assertion was `respects-pre-brined-ingredient`: one Gemini 3.1
sample and all three Argon samples. Manual review found no salt/brining criticism
in those responses. The assertion rejects any mention of salt or brining in
issues or fixes, including legitimate garlic-timing fixes that retain the existing
salt, and prep-step rewrites that name the pre-brined chicken. These are assertion
false positives, not evidence of seasoning regressions. The checked-in assertions
were left unchanged for this comparison. Argon also consistently suggested
separating poultry and produce prep; this limited suite does not establish whether
its additional feedback improves downstream recipe revisions.

Both eval commands exited nonzero because of these assertion failures, with zero
provider errors. `./task.sh verify-go` passed formatting, vet, tests, and lint.
`git diff --check` passed.

## Reproduction and artifacts

```sh
KAGE_SECRET_FILE=/home/pmiller/careme_mealspread/secrets/envtest OPENROUTER_CRITIQUE_MODEL=google/gemini-3.1-pro-preview ./task.sh evals EVAL=recipe-critique -- --no-cache --repeat 3 --output /tmp/critique-gemini31.json
KAGE_SECRET_FILE=/home/pmiller/careme_mealspread/secrets/envtest OPENROUTER_CRITIQUE_MODEL=google/gemini-4-argon ./task.sh evals EVAL=recipe-critique -- --no-cache --repeat 3 --output /tmp/critique-argon.json
```

- Gemini 3.1: `eval-qEO-2026-10-14T04:07:34` (UTC).
- Argon: `eval-N7U-2026-10-14T04:13:43` (UTC).

Full exports remain at the `/tmp` paths above; generated outputs are not checked
in. OpenRouter documents the model slug and structured-output support on its
[Gemini 4 Argon page](https://openrouter.ai/google/gemini-4-argon).
