# Critique model comparison — 2026-08-28

This is a historical record of why the default critique model changed to Claude Opus 5. The run evaluated the ten most recently cooked recipes in private snapshot `cooked-opus-deepseek-2026-08-28` (dataset ID `d42a3e5435cb2e992fdc942654a7f112f780fc578e9cccf7529f2098dd5480e4`). Results were saved in the private cache; the bespoke evaluator was later retired in favor of a planned Promptfoo regression suite.

| Model | Mean score | Passes at 8 | Rated-recipe MAE |
| --- | ---: | ---: | ---: |
| `google/gemini-3.1-pro-preview` (historical) | 8.4 | 8/10 | 0.444 |
| `anthropic/claude-opus-5` | 5.4 | 0/10 | 1.222 |
| `deepseek/deepseek-v4-pro-20260423` | 7.0 | 2/10 | 0.778 |

Opus was substantially more expensive (about $0.94 for ten recipes versus about $0.09 for DeepSeek), but it identified more consequential defects than Gemini, especially broken recipe metadata, unsuitable doneness targets, carryover cooking, scorching risks, and timing problems. It was also more punitive and frequently recommended too much salt. The production retry cutoff is therefore model-specific: 6 for the Claude Opus family and 8 for other models.

## Blind second opinion

GPT-5.6 Sol compared the Gemini and Opus critiques with model names removed and A/B order varied per recipe. It preferred Opus on 6 recipes, Gemini on 4, with no ties. Its conclusion was that Opus is generally the better defect finder, but needs calibration against disproportionate scores and aggressive salt recommendations.

## Korean Grilled Tri-Tip with Sesame Cucumber Rice

Recipe hash: `MnMrPhOaMIhdNVeTptogQg==`; rating: 4 stars.

Gemini scored it 8 and found a mostly sound recipe, recommending only that later instructions stop repeating the weights of already-prepared components.

Opus scored it 4 and identified more possible issues: the USDA comparison, an ambiguous optional marinade variant, preparing cucumber too early, repeated quantities, possible underseasoning, understated calories/cost, and confusing final-half-hour timing.

GPT-5.6 preferred Gemini for this recipe, calling Opus’s score disproportionately low and its extra salt recommendations too aggressive. It agreed that Opus’s strongest additions were the ambiguous marinade variant and redundant instruction prose.

This comparison supports switching to Opus for finding defects while lowering its retry cutoff to 6 and moving cutoff policy out of the critique prompt. Future Promptfoo cases should explicitly test calibration, salty/brined ingredients, and false-positive findings.
