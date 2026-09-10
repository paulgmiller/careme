# Recipe critique model comparison — 2026-08-31

Promptfoo 0.122.0 ran five uncached candidate models against two synthetic guardrails and three generated recipes that users cooked and rated. The candidate provider reported only the OpenRouter critique-call latency. GPT-5.6 Sol graded critique usefulness with the recipe and Careme's editorial rules as authoritative context.

## Broad latency screen

| Model | Mean | Range | Under 30 seconds |
| --- | ---: | ---: | ---: |
| Gemini 3.7 Flash | 10.3s | 6.5–16.8s | 5/5 |
| Gemini 3.1 Pro Preview | 17.3s | 13.7–21.8s | 5/5 |
| Claude Opus 5 | 30.8s | 18.9–40.5s | 2/5 |
| DeepSeek V4 Flash | 41.9s | 22.5–92.2s | 2/5 |
| DeepSeek V4 Pro | 84.6s | 18.5–206.9s | 1/5 |

## Calibrated Gemini finalist run

The two models that met the latency budget on every broad-screen case were rerun uncached. The saved outputs were then graded by GPT-5.6 Sol after the rubric was calibrated to Careme's salt and doneness requirements.

| Model | Mean usefulness | Useful cases | Mean latency | Range | Under 30 seconds |
| --- | ---: | ---: | ---: | ---: | ---: |
| Gemini 3.1 Pro Preview | 0.890 | 5/5 | 20.8s | 15.5–29.5s | 5/5 |
| Gemini 3.7 Flash | 0.876 | 4/5 | 9.1s | 7.5–11.1s | 5/5 |

Gemini 3.7 Flash's failed case correctly found that the Basque pork recipe expressed salt in grams, but supplied unreliable Diamond Crystal conversions. GPT-5.6 scored it 0.66 because the actionable fix could materially change the seasoning. Gemini 3.1 Pro Preview supplied internally consistent conversions and passed that case at 0.86.

## Decision

Keep `google/gemini-3.1-pro-preview` as the critique model. It was the only candidate shown to satisfy both the 30-second budget and the usefulness threshold on every case. Gemini 3.7 Flash is a promising lower-latency option, but should not replace it until the salt-conversion failure is prevented or it passes a broader repeated sample.
