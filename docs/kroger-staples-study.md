# Kroger staples query study

Measured November 6, 2026 at QFC `70500874` and Fred Meyer `70100023`,
investigating [#927](https://github.com/paulgmiller/careme/issues/927).

The app's produce score includes all useful cooking ingredients, not just
produce. It sums grades above 6 after product-ID deduplication, then divides by
100 with integer truncation. This study used the configured `gpt-6-luna` grader
and shared each product's grade between every query combination within a store.
It evaluated 34 candidates, covering 2,814 unique products at QFC and 3,074 at
Fred Meyer. Search counts refer to query specifications; API requests count
every page and any retries.

## Shared plan comparison

| Store | Plan | Searches | API requests | Useful products | Grade points | Produce score |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| QFC 70500874 | Original defaults | 10 | 44 | 541 | 4,605 | 46 |
| QFC 70500874 | Earlier six-search revision | 6 | 34 | 697 | 5,825 | 58 |
| QFC 70500874 | Selected request-budget plan | 7 | 39 | 759 | 6,332 | 63 |
| Fred Meyer 70100023 | Original defaults | 10 | 50 | 567 | 4,792 | 47 |
| Fred Meyer 70100023 | Earlier six-search revision | 6 | 35 | 661 | 5,518 | 55 |
| Fred Meyer 70100023 | Selected request-budget plan | 7 | 39 | 711 | 5,949 | 59 |

The earlier six-search plan improved Fred Meyer as well as QFC. Prioritizing
API requests over search count yields the selected seven-search plan below.
It stays strictly below 40 measured requests at both stores, improves QFC's
original score by 37%, and improves Fred Meyer's original score by 26%.

The selected searches are:

- `fresh vegetables`, all brands
- `fresh fruit`, all brands
- `Meat & Seafood`, all brands, including frozen products
- `Seafood`, all brands
- `Beans`, all brands
- `Dairy & Eggs`, all brands
- `Dairy & Eggs`, API-filtered to `Simple Truth Organic`, `Kroger`, and `Simple Truth`

The filtered dairy search supplements unrestricted discovery. It retrieves
useful products outside the unrestricted search's first 300 results. The
server applies the brands before pagination; there is no second local brand
check for that search. The other six searches do not restrict brands.

The budget-only objective replaces the earlier dedicated pasta search with
seafood and additional dairy coverage. Stock, price, frozen-category, and
item-selection rules are unchanged: priced items explicitly temporarily out
of stock are excluded, and item selection prefers HIGH, then LOW, then unknown
stock. Only the meat/seafood search admits Frozen-category products. Recipe
generation continues to apply the grade cutoff.

Both Kroger mild ground Italian sausage and mild Italian links are included
at QFC. The old pork query omitted them. Country pork sausage was returned by
the API but excluded because it was temporarily out of stock.

## Optimization and store differences

`cmd/ingredients -optimize-staples` preserves the initial baseline in its study
directory, saves each query separately, and grades the union once. Set
`-staples-request-budget 39` to maximize unique useful grade points under a
strictly-below-40 request budget. `optimalUnderBudget` performs an exact subset
search over the measured candidate set after discarding dominated coverage.
There is no search-count limit. Ties prefer fewer requests, then fewer searches.
The report also retains a greedy coverage-per-request progression, while
`requestFrontier` gives the exact best plans at each improving request cost.

The selected shared plan is QFC's highest-scoring tested combination within
39 requests. Fred Meyer's own optimum reaches 6,228 points (score 62) with
39 requests and eight searches:

- `fresh vegetables` and `fresh fruit`, all brands
- `Produce`, all brands
- `Rice & Grains`, all brands
- `Meat & Seafood`, all brands, including frozen products
- `Dairy & Eggs`, API-filtered to `Simple Truth Organic`, `Kroger`, and `Simple Truth`
- `Beans`, API-filtered to `Kroger`, `Simple Truth Organic`, `Simple Truth`, and `Goya`
- `nuts`, API-filtered to `Kroger`, `Simple Truth`, `Simple Truth Organic`, `Diamond of California`, `Fisher`, and `Private Selection`

That store-specific plan scores 59 at QFC with 36 requests, below the selected
shared plan's 63. The application retains one shared provider configuration;
no store IDs are hardcoded into retrieval behavior.

These optima are within the tested candidates and saved inventory, not across
all possible Kroger searches. The corrected `fresh vegetables` query and the
original typo `fresh vegatable` returned identical product sets in both samples.

## Diminishing returns

The study now computes `requestFrontier`: the exact best measured grade
coverage at each improving request cost, in a single subset search. Using the
same saved products and grades, extending the analysis through 50 requests
requires no additional Kroger calls.

| Request budget | QFC best grade points | QFC score | Fred Meyer best grade points | Fred Meyer score |
| ---: | ---: | ---: | ---: | ---: |
| 20 | 4,412 | 44 | 3,986 | 39 |
| 25 | 5,100 | 51 | 4,845 | 48 |
| 30 | 5,583 | 55 | 5,514 | 55 |
| 35 | 6,036 | 60 | 5,981 | 59 |
| 39 | 6,332 | 63 | 6,228 | 62 |
| 40 | 6,349 | 63 | 6,303 | 63 |
| 45 | 6,744 | 67 | 6,673 | 66 |
| 50 | 7,057 | 70 | 7,000 | 70 |

These curves optimize each store independently; they are not partial fetches
of the shared seven-search plan. Fred Meyer's independently optimized score of
62 at 39 requests differs from the shared plan's score of 59 at the same cost.

There is a gradual slowdown rather than a sharp plateau. The first 20 requests
average about two displayed score points per request. After roughly 30
requests, additional calls yield about 0.7–0.8 points per request. Increasing the
budget from 39 to 50 costs 28% more calls for 7–8 additional displayed points.
A 30–40 request budget is a reasonable tradeoff for these samples; the selected
39-request plan preserves more ingredient breadth while meeting the requested
budget. Larger budgets still add useful products, so 39 is not a saturation
point or a proven universal optimum.

## Running diagnostics

### Inspect products and variants

Compare raw in-store API results with the staples catalog, without grading or
ingredient caches. Only Kroger provider credentials are required:

```sh
go run ./cmd/ingredients -location 70500874 \
  -kroger-search "sausage" -kroger-search "ground pork" \
  -kroger-search "mild italian" -kroger-search "country pork" > /tmp/kroger-inspection.json
```

Replace the location ID with your store and repeat `-kroger-search` for each
term. The JSON includes production search filters, raw products with every item
UPC, size, price and stock level, and matching staples rows before product-ID
deduplication. Raw searches request in-store fulfillment and page through
offsets 0–250 in batches of 50. They bypass frozen, price and stock filtering.

To filter brands on the API before pagination, supply a case-sensitive,
pipe-separated list:

```sh
go run ./cmd/ingredients -location 70500874 -kroger-search "Meat & Seafood" \
  -kroger-brands 'Kroger|Simple Truth|Foster Farms|Draper Valley|PORK|Harris Teeter|Sand Bar'
```

The report records these diagnostic brands separately from the production
staples filters. Without `-kroger-brands`, diagnostic searches accept all brands.
Compare `productId` and `items[].itemId` across descriptions. A product absent
from `matchingStaplesBeforeDeduplication` was not emitted by the staples backend;
check search terms, brands, frozen categories, prices and stock. Repeated IDs
with different descriptions or UPCs in those rows expose what the shared
catalog's first-row-per-product-ID deduplication discards.

### Compare score and request cost

Run a graded query study with a measured product API request budget:

```sh
go run ./cmd/ingredients -location 70500874 \
  -optimize-staples /tmp/qfc-staples-study -staples-request-budget 39 \
  > /tmp/qfc-staples-report.json
```

This explicitly enables real ingredient grading and requires Kroger credentials
and `AI_API_KEY`. The study directory stores the starting query configuration,
query snapshots, a private grade cache, `graded.json`, and `report.json`.
Rerunning with that directory reuses its snapshots and grades; use a new
directory for a fresh inventory sample. Studies do not write production
ingredient catalogs.

The report includes unrounded grade points, product counts, and product API
request counts including pagination and retries. `optimalUnderBudget` is the
best plan within the supplied budget (default 39). `requestFrontier` contains
the exact best plans at each improving request cost; `greedyFrontier` provides
a heuristic comparison. Each plan's `searches` entries index the report's
`queries` array. Larger candidate sets can make the exact subset search slower.

Pass `-staples-candidates /tmp/candidates.json` to replace the experimental
queries with a JSON array, for example:

```json
[
  {
    "term": "Meat & Seafood",
    "brands": ["Kroger"],
    "frozen": true,
    "apiBrands": true
  }
]
```

`apiBrands` applies the brand filter before pagination. Without it, brands are
screened locally to reproduce older production queries. Use `"brands": ["*"]`
for all brands. The production defaults at the start of a new study form its
baseline; later changes to defaults do not replace a saved baseline.

## Limits and reruns

The API's 300-result window still limits broad searches. Query combinations do
not guarantee complete store inventory. Inventory, fuzzy-search ordering,
request retries, and model grades can change. The 39-request figures are
measured retrieval costs, not a runtime limit that truncates inventory if a
later sample needs more pages or retries.

Runtime product responses and grades belong in the study directory, not source
control. A new study directory starts a fresh measurement; reusing one compares
the saved snapshot. The baseline file is decoded independently of current
defaults, so omitted flags cannot inherit a later query configuration's values.
