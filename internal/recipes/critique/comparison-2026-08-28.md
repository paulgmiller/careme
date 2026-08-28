# Critique model comparison — 2026-08-28

This report records the comparison run for the ten most recently cooked recipes in the private dataset `cooked-opus-deepseek-2026-08-28` (dataset ID `d42a3e5435cb2e992fdc942654a7f112f780fc578e9cccf7529f2098dd5480e4`). The same critique prompt fingerprint was used for both fresh runs.

| Model | Mean score | Passes at 8 | Rated-recipe MAE |
| --- | ---: | ---: | ---: |
| `google/gemini-3.1-pro-preview` (historical) | 8.4 | 8/10 | 0.444 |
| `anthropic/claude-opus-5` | 5.4 | 0/10 | 1.222 |
| `deepseek/deepseek-v4-pro-20260423` | 7.0 | 2/10 | 0.778 |

GPT-5.6 Sol blindly compared the Gemini and Opus critiques, with model names removed and A/B order varied per recipe. It preferred Opus on 6 recipes and Gemini on 4. It generally found Opus better at catching consequential cooking and metadata problems, but repeatedly considered Opus's salt recommendations too aggressive and some scores too harsh.

## Per-recipe scores

| Recipe | Stars | Gemini | Opus | DeepSeek |
| --- | ---: | ---: | ---: | ---: |
| Sumac-Pistachio Coho with Charred Corn | 4 | 7 | 7 | 6 |
| Turkish Rib Roast with Summer Tomatoes | 4 | 8 | 6 | 7 |
| Grilled Jerk Chicken with Charred Corn | 3 | 9 | 6 | 7 |
| Vietnamese Ginger-Lime Grilled Branzino | 5 | 9 | 6 | 7 |
| Mediterranean Grilled Chicken Quinoa Bowls | 4 | 8 | 7 | 7 |
| Korean Grilled Tri-Tip with Sesame Cucumber Rice | 4 | 8 | 4 | 7 |
| Provençal Salmon with Summer Zucchini | 4 | 8 | 4 | 8 |
| Thai Coconut Shrimp & Yellow Pepper | 3 | 9 | 5 | 8 |
| Dijon-Thyme Pork Tenderloin | 5 | 8 | 5 | 7 |
| Caribbean Jerk Coho with Corn-Peach Salsa | — | 9 | 4 | 6 |

## Korean Grilled Tri-Tip with Sesame Cucumber Rice

Recipe hash: `MnMrPhOaMIhdNVeTptogQg==`; rating: 4 stars.

### Historical Gemini critique — score 8

> A solid, well-seasoned recipe for Korean-style grilled tri-tip with appropriate side dishes. The total times, ingredient quantities, and safety guidelines are accurate, and the seasoning levels are well-calculated. However, the instruction prose in the final steps awkwardly repeats the original weights and volumes of prepared components instead of referring to them concisely.

Strengths:

- Perfect agreement between the ingredient list and the amounts used in the instruction steps.
- Correct preparation and use of salt to draw moisture from the cucumbers before dressing.
- Clear, precise food safety instructions regarding safe cooking temperatures and discarding used marinade.
- Plausible total time that accounts for the lengthy marination period.

Issue:

- Later steps repeatedly and needlessly restate ingredient amounts for prepared items.

Suggested fixes:

- Refer to “the marinated tri-tip,” “the steak,” and the completed rice and cucumber salad rather than repeating their weights and volumes.

### Opus 5 critique — score 4

> An appealing, mostly workable Korean grilled tri-tip plate undermined by a prohibited USDA temperature comparison, a confusing optional-marinade variant baked into the ingredient list, unsalted rice and underseasoned beef, and low calorie and cost estimates.

Strengths:

- Clear, appealing dish concept with a well-balanced plate: grilled beef, rice, and cucumber salad.
- Rice ratio, cooking time, cucumber salting, and ingredient reconciliation are sound.
- Sensible pull-and-rest approach and an apt off-dry Riesling pairing.

Issues:

- Step 6 cites USDA guidance and compares it with the recipe’s pull temperature.
- The marinade contains an ambiguous 17-hour alternative with different quantities.
- Cucumber and green onions are prepared hours before they are used.
- Amounts are needlessly restated after first use.
- Rice and beef may be underseasoned.
- Calories and cost appear understated.
- The final-half-hour timing is difficult to follow.

Suggested fixes:

- Remove the USDA sentence and state one concise pull temperature.
- Drop the 17-hour marinade variant.
- Move cucumber and green onion preparation to the salad step.
- Refer to completed components rather than repeating quantities.
- Recheck salt, calories, cost, and the final cooking timeline.

### Assessment

The Opus critique catches more possible issues, but its score of 4 is not well calibrated against the recipe’s 4-star rating and the historical critique. GPT-5.6 judged Gemini substantially more proportionate here, while agreeing that Opus’s strongest useful observations were the ambiguous marinade variant and redundant instruction prose.
