# GPT-6.2 Sol vs. GPT-6.1 Sol on 2026-11-17

## Recipe generation

| Metric | GPT-6.1 Sol | GPT-6.2 Sol |
| --- | ---: | ---: |
| Cases passing all assertions | 9/10 | 5/10 |
| Mean judged quality out of 10 | 8.8 | 7.8 |
| Mean generation time | 37.19 s | 16.71 s |
| Cases within the 60-second budget | 10/10 | 10/10 |
| Estimated generation cost for ten cases | $0.48254 | $0.48461 |
| Provider errors | 0 | 0 |

GPT-6.2 Sol generated every recipe faster and cut mean generation time by 55%.
Estimated generation cost differed by less than 1%. GPT-6.2 Sol scored below
8/10 on Yucatecan chicken, Moroccan lamb, Sichuan chicken, and Lemon-Parmesan
coho. Its Bellevue East tri-tip scored 8/10 and failed the doneness assertion.
GPT-6.1 Sol scored 7/10 on Chilean tri-tip, which passed with a 9/10 score on
GPT-6.2 Sol. Both models passed the two pantry cases.

## Menu planning

| Metric | GPT-6.1 Sol | GPT-6.2 Sol |
| --- | ---: | ---: |
| Cases passing all assertions | 13/23 | 14/23 |
| Mean lowest judge dimension out of 10 | 8.96 | 9.74 |
| Mean generation time | 13.22 s | 5.78 s |
| Median generation time | 10.71 s | 4.91 s |
| Provider errors | 0 | 0 |

GPT-6.2 Sol cut mean menu generation time by 56%. Both versions failed
catalog membership on the same six cases, and both failed category variety on
the saffron and four-seasonal-dinner cases. GPT-6.2 Sol passed the all-healthy
and small Thanksgiving cases that GPT-6.1 Sol missed. It failed the large
Thanksgiving judge check, which GPT-6.1 Sol passed.

## Medium-effort comparison method

- Each model ran the 10 recipe fixtures and 23 menu fixtures at `ec384f34`, with
  one sample per case and medium reasoning. The runs selected each model
  explicitly and used the same judge, `google/gemini-3.1-pro-preview`.
- Promptfoo 0.123.0 ran with `--no-cache`, recipe concurrency eight, and menu
  concurrency four. The four runs ran sequentially. OpenAI prompt caching
  remained active. Single samples and service load limit the latency comparison.
- Generation time includes API retries and excludes judging and Go startup.
  Recipe cost estimates use Standard short-context rates including cache
  reads, cache writes, and reasoning tokens. The menu suite exports no cost.
- Recipe fixtures continue retained menu response IDs. The separate menu suite
  generates fresh plans. The suites leave recipe revisions, menu regeneration,
  and fresh menu-to-recipe expansion unmeasured.

## Run records

| Suite | GPT-6.1 Sol evaluation ID | GPT-6.2 Sol evaluation ID |
| --- | --- | --- |
| Recipe generation | `eval-vsj-2026-11-17T23:17:20` | `eval-Ri8-2026-11-17T23:20:11` |
| Menu planning | `eval-nH3-2026-11-17T23:22:35` | `eval-GbA-2026-11-17T23:27:20` |

The [per-case CSV](gpt62-sol-comparison-2026-11-17.csv) contains scores,
generation latency, estimated recipe costs, pass status, and failed assertion
names for all 76 results, including the high-effort follow-up. Full Promptfoo exports stayed in a private temporary
directory. All four commands completed with assertion failures and zero provider
errors.

```sh
for suite in recipe-generation menu-plan; do
  for model in gpt-6.1-sol gpt-6.2-sol; do
    ./task.sh evals EVAL="$suite" MODEL="$model" REASONING_EFFORT=medium -- \
      --no-cache --output "/tmp/careme-$suite-$model.json"
  done
done
```


## GPT-6.2 Sol at high reasoning effort

A fifth run used the same ten recipe fixtures, judge, and concurrency with
`REASONING_EFFORT=high`. The evaluation ID was `eval-nzd-2026-11-17T23:37:12`.
All ten generations completed within 60 seconds with zero provider errors.

| Metric | GPT-6.1 Sol medium | GPT-6.2 Sol medium | GPT-6.2 Sol high |
| --- | ---: | ---: | ---: |
| Cases passing all assertions | 9/10 | 5/10 | 7/10 |
| Mean judged quality out of 10 | 8.8 | 7.8 | 7.7 |
| Mean generation time | 37.19 s | 16.71 s | 36.80 s |
| Mean reasoning tokens | 685 | 459 | 2,127 |
| Estimated generation cost for ten cases | $0.48254 | $0.48461 | $0.62823 |

High effort recovered Yucatecan chicken, Sichuan chicken, and lemon-Parmesan
coho. The Bellevue tri-tip doneness assertion and Moroccan lamb quality check
still failed, and Chilean tri-tip fell from 9/10 to 5/10. High effort brought
mean latency close to GPT-6.1 Sol at medium and raised estimated generation
cost by about 30% over GPT-6.2 Sol at medium. Each configuration has one
sample per case.

```sh
./task.sh evals EVAL=recipe-generation MODEL=gpt-6.2-sol REASONING_EFFORT=high -- \
  --no-cache --output /tmp/careme-recipe-generation-gpt-6.2-sol-high.json
```
