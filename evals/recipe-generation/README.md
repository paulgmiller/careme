# Recipe generation eval

Latest results: [Astra vs. Sol at explicit medium reasoning, 2026-09-09](model-comparison-medium-2026-09-09.md), including quality, latency, reasoning tokens, and generation cost.

Run with an explicit recipe model and reasoning effort:

```sh
./task.sh evals EVAL=recipe-generation MODEL=gpt-6-astra REASONING_EFFORT=high -- --no-cache --output /tmp/recipe-eval-astra-high.json
```

Omit `REASONING_EFFORT` to preserve the API default. For direct Promptfoo runs, use `RECIPE_EVAL_REASONING_EFFORT` or provider `config.reasoning_effort` (which takes precedence). The requested effort is recorded in metadata and the provider label; unsupported model/effort combinations fail at the API.

The cost column reports generation USD. JSON output also records `metadata.generationCostUSD`, `metadata.judgeCostUSD`, and `metadata.totalCostUSD`. Token usage, including reasoning and cache tokens, remains in AI usage logs. Generation cost is an estimate using standard short-context rates; judge cost comes from OpenRouter. See the [eval overview](../README.md#recipe-generation) for accounting limits and configuration details.

Before the cost-only client refactor, on 2026-09-08, a one-case live check with `MODEL=gpt-5.6-luna REASONING_EFFORT=low -- --no-cache --filter-first-n 1` verified the exported reasoning and cost fields. Generation cost was $0.0061969, judge cost $0.032064, total $0.0382609; generation took 22.809s and used 323 reasoning tokens. The recipe scored 7/10 and failed only the quality assertion. This checks the reporting integration, not a repeated model comparison. Evaluation ID: `eval-dAr-2026-09-08T16:13:45`.

## Recipe model comparison — 2026-09-07

Sol had the highest judged quality; Astra passed the most complete cases because it stayed within the generation latency budget. Luna was fastest but failed six quality checks. This run does not support replacing Sol or Astra with Luna for recipe generation.

| Metric | GPT-6 Astra | GPT-5.6 Sol | GPT-5.6 Luna |
| --- | ---: | ---: | ---: |
| Mean quality /10 | 8.625 | 9.00 | 6.75 |
| Quality ≥8/10 | 7/8 | 8/8 | 2/8 |
| Mean generation time | 37.5s | 55.1s | 18.5s |
| Generation within 60s | 8/8 | 4/8 | 8/8 |
| All assertions passed | 7/8 | 4/8 | 2/8 |
| API/provider errors | 0 | 0 | 0 |

Luna used 50.5% less generation time than Astra and 66.4% less than Sol, but its mean quality was 1.88 points below Astra and 2.25 below Sol. Astra used 32.1% less generation time than Sol.

## Per-case results

Each cell shows Gemini quality /10, generation seconds, and overall pass/fail.

| Case | Astra | Sol | Luna |
| --- | --- | --- | --- |
| Yucatecan chicken | 9, 43.1s, pass | 9, 65.1s, fail | 7, 29.3s, fail |
| Korean tri-tip | 10, 39.4s, pass | 8, 49.7s, pass | 6, 16.1s, fail |
| Moroccan lamb | 8, 43.9s, pass | 8, 60.9s, fail | 4, 19.4s, fail |
| Sichuan chicken | 6, 44.3s, fail | 9, 53.8s, pass | 9, 16.1s, pass |
| Lemon-Parmesan coho | 9, 31.2s, pass | 10, 53.0s, pass | 6, 15.4s, fail |
| Jicama cucumber side | 9, 23.5s, pass | 10, 31.2s, pass | 9, 11.1s, pass |
| Chilean tri-tip | 9, 34.3s, pass | 9, 65.4s, fail | 7, 26.5s, fail |
| Sumac-pistachio coho | 9, 39.9s, pass | 9, 62.0s, fail | 6, 14.4s, fail |

## Failures

All failures were either quality or generation latency. All other assertions passed for all three models. The following quality findings are the Gemini judge’s assessments, not independently adjudicated defects.

### Astra

- **Sichuan chicken:** The recipe is well-structured and uses clear formatting for the stir-fry method, but it improperly reduces presalt to account for late-added soy sauce and includes an impractical temperature check for bite-sized chicken pieces.

### Sol

- **Yucatecan chicken:** Generation took 65.126s, exceeding the 60s budget; quality passed at 9/10.
- **Moroccan lamb:** Generation took 60.896s, exceeding the 60s budget; quality passed at 8/10.
- **Chilean tri-tip:** Generation took 65.431s, exceeding the 60s budget; quality passed at 9/10.
- **Sumac-pistachio coho:** Generation took 62.004s, exceeding the 60s budget; quality passed at 9/10.

### Luna

- **Yucatecan chicken:** The recipe features a vibrant flavor profile and accurate chicken cooking temperatures, but needs adjustments to the corn preparation steps and additional salt for the vegetables.
- **Korean tri-tip:** The recipe features solid grilling methods and flavors but is hampered by a contradictory marinade step and slightly excessive salt.
- **Moroccan lamb:** This tagine-inspired recipe has excellent flavor profiles but fundamentally misjudges the time required to braise lamb shoulder and oversalts the meat during preparation.
- **Lemon-Parmesan coho:** The recipe features excellent temperature targets and precise salt calculations, but it is hindered by a mathematical error in dividing the sauce, improper formatting, and a technique that will steam the vegetables.
- **Chilean tri-tip:** The recipe features solid cooking techniques and appealing flavors, but it suffers from inaccurate serving yields and lacks the required bullet list formatting for complex ingredient mixtures.
- **Sumac-pistachio coho:** A vibrant and well-conceived Turkish-inspired salmon dish that requires adjustments to seasoning distribution, formatting, and chronological flow.

## Method and limits

- Eight identical checked-in cases, one sample per case and model; no retries of failed evaluations. Reasoning effort was omitted for all three models, so these measure API-default behavior, not explicitly matched effort.
- Recipe models: `gpt-6-astra`, `gpt-5.6-sol`, `gpt-5.6-luna`; fixed judge: `google/gemini-3.1-pro-preview`.
- Promptfoo 0.122.0, `--no-cache`, concurrency one. Generation latency excludes judging and compilation but includes SDK retries.
- Runs took place September 7 in America/Los_Angeles (September 8 UTC). Models ran separately, not interleaved; service load may affect timings.
- These are retained-menu continuations, not self-contained prompt replays. OpenAI prompt caching remained active; `--no-cache` disables only Promptfoo caching.
- Case labels identify the source menu direction; generated recipes can differ from historical saved recipe titles.
- Quality is a single model judge’s assessment. Scores include style and seasoning rules as well as cookability; the judge does not receive the original user request. Automated serving/time checks inspect declared properties, not whether the method can actually satisfy them.
- No repeated samples or statistical confidence intervals. Treat the observed differences as preliminary.
- No three-way cost comparison: Astra pricing was not configured in application usage logs; Promptfoo token counters are zero and should not be interpreted as free calls.
- No production model, prompt, or configuration changes were made.

## Reproduction and artifacts

Repository revision: `ca14abf7398228899902f0d8cf54e72f8b68f0e4`.

```sh
./task.sh evals EVAL=recipe-generation MODEL=gpt-6-astra -- --no-cache --output /tmp/recipe-eval-astra-20260907.json
./task.sh evals EVAL=recipe-generation MODEL=gpt-5.6-sol -- --no-cache --output /tmp/recipe-eval-sol-20260907.json
./task.sh evals EVAL=recipe-generation MODEL=gpt-5.6-luna -- --no-cache --output /tmp/recipe-eval-luna-20260907.json
```

Requires the existing AI and OpenRouter credentials through configuration/kage. Each command makes eight generation calls and eight judge calls. Exit status 100 from Promptfoo indicates failed assertions in these completed runs, not API errors.

The adjacent [CSV](model-comparison-2026-09-07.csv) preserves per-case numeric results and failed metric names without generated recipe bodies. Full outputs and critiques remain in the local `/tmp` JSON files above; those temporary files are not durable repository artifacts.

| Model | Evaluation ID | Start (UTC) |
| --- | --- | --- |
| Astra | `eval-oLp-2026-09-08T00:01:24` | 2026-09-08T00:01:24.503Z |
| Sol | `eval-uhD-2026-09-08T04:11:30` | 2026-09-08T04:11:30.365Z |
| Luna | `eval-7c3-2026-09-08T04:27:02` | 2026-09-08T04:27:02.369Z |
