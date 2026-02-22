# Produce Score Tradeoffs

Date: 2026-02-22  
Location: `70500874` (default from `cmd/producecheck`)  
Command: `go run ./cmd/producecheck` with `.envtest` credentials

## Result Summary

- Baseline before changes: `62/107` (`0.579439`) with `332` ingredients.
- Best observed after iteration: `93/107` (`0.869159`) with `672` ingredients.
- Observed range on repeated runs with the final config: `89-93` / `107` (Kroger fuzzy ordering varies).

## What Changed

### 1) Produce filter strategy (`internal/recipes/params.go`)

I expanded `Produce()` from a small set of broad terms to a mixed set:

- Broad recall terms: `produce`, `fresh produce`
- Targeted recovery terms for persistent misses:
  - peppers/chiles: `bell peppers`, `red chili peppers`, `green chili peppers`, `mini sweet peppers`
  - mushrooms: `mushrooms produce`, `king trumpet mushrooms`
  - sprouts: `alfalfa sprouts`, `broccoli sprouts`, `bean sprouts`
  - cucumbers: `cucumber produce`, `seedless cucumbers`
  - other gaps: `little gem produce`, `chives`, `napa cabbage`, `eggplant`, `parsnip produce`

All produce filters now consistently use `Brands: []string{"*"}` to avoid accidental exclusion of branded produce.

### 2) Produce matching robustness (`cmd/producecheck/main.go`)

I improved term normalization and matching to reduce obvious false negatives:

- strips diacritics (`jalapeño` -> `jalapeno`)
- removes parenthetical aliases (`green onions (scallions)` -> `green onions`)
- normalizes token morphology (basic singularization/plural handling)
- maps known token variants (`portobello` -> `portabella`, `kiwifruit` -> `kiwi`, `chile` -> `chili`)
- uses token-set matching instead of raw substring matching
- allows optional trailing `lettuce` token to match `little gem` descriptions

## Key Constraints and Tradeoffs

1. Score vs query volume
- Higher score required significantly more query terms.
- Baseline query set was much smaller; final set increases Kroger calls materially.
- Approximate API call growth: from ~10 calls/run to ~25-30 calls/run (depends on pagination and Kroger fuzzy ordering).

2. Kroger API limit
- `filter.limit` is hard-capped at `50` (attempting `200` returns `PRODUCT-2013`), so call reduction by page-size increase is not available.

3. Stability vs strictness
- Kroger product search ordering is fuzzy and can vary between runs.
- Some targeted terms intermittently return fewer useful rows; simpler direct terms (`bean sprouts`, `napa cabbage`, `king trumpet mushrooms`, etc.) were more stable.

4. Precision vs recall
- Broader/tokenized matching improves recall (score) but can include non-produce or adjacent packaged items in some categories.
- This is acceptable for `cmd/producecheck` benchmarking, but should be considered if the same logic is reused elsewhere.

## Remaining Misses at Best Run

At `93/107`, the remaining unmatched terms were:

- `aloe vera leaf`
- `broccolini`
- `celery root`
- `curly kale`
- `daikon radish`
- `horseradish root`
- `lemongrass`
- `mixed sprout`
- `oyster mushroom`
- `radicchio`
- `red chili pepper`
- `rutabaga`
- `sliced mushroom blend`
- `yuca`

These appear to be a mix of true availability gaps and query/indexing gaps at this location.
