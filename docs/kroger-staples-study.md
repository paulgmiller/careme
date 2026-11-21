# Kroger staples queries

## Broad wine search comparison

Measured November 18, 2026 against the live Products API with
`filter.fulfillment=ais`, `filter.limit=50`, and offsets 0 through 250.
The API schema caps the start offset at 250 and the page size at 50, giving
a 300-product search window. Each search below returned six full pages.

| Search term | QFC 70500874 reported total | Fred Meyer 70100023 reported total | Fetchable per store |
| --- | ---: | ---: | ---: |
| `red wine` | 526 | 562 | 300 |
| `white wine` | 473 | 481 | 300 |

Totals come from each response's `meta.pagination.total`, before ingredient
filtering or product-ID deduplication. They are search matches, which can
overlap or include products other than drinking wine. Both terms exceed the
search window at both stores, so replacing recipe-style searches with just
these two terms would truncate results. The provider now uses these capped
broad searches as a shared store/date catalog and supplements them with cached
recipe-style searches, merging both sources by product ID.
Inventory and search ranking can change these counts.

An exclusion-syntax probe at QFC on the same date found that neither `-` nor
`NOT` excluded styles: `red wine -cabernet` and `red wine NOT cabernet` each
reported 160 matches and returned Cabernet in all 50 first-page descriptions.
The plain `red wine` query returned Cabernet in 19 of its first 50 descriptions.
`red wine -cabernet -merlot -pinot` returned no products. These forms cannot
provide a remainder query after separate style searches; local deduplication
would not recover products outside the broad query's 300-result window.

## Staples search terms

The provider retains the original search terms, removes brand filtering, and
adds `turkey`, `sausage`, and `Dairy & Eggs`. Targeted turkey and sausage searches
replace the broader `Meat & Seafood` search following the comparison below.

The thirteen searches cover fresh vegetables, fresh produce, beef, chicken,
turkey, sausage, fish, pork, shellfish, lamb, grains, pasta, and dairy/eggs. The
vegetable search's original spelling error is corrected; both spellings returned
identical product sets in the measured samples. No local or API brand whitelist
is applied.

## Turkey and sausage comparison

Measured November 17, 2026 using the same grades for both plans, preserving
stock, price, frozen-product filtering, and product-ID deduplication. Replace
`Meat & Seafood` with `turkey` and `sausage`; keep all other searches.

| Measure | QFC 70500874 | Fred Meyer 70100023 |
| --- | ---: | ---: |
| Score before → after | 56 → 59 | 57 → 59 |
| Grade sum above 6 before → after | 5,683 → 5,906 | 5,786 → 5,958 |
| Unique products before → after | 1,874 → 2,138 | 1,968 → 2,315 |
| Products lost / gained | 7 / 271 | 10 / 357 |
| Products above grade 6 lost / gained | 0 / 29 | 1 / 25 |

Both stores gain turkey and sausage options. Fred Meyer loses the specific
Kroger Fresh Farm Raised Atlantic Salmon Fillet Portion (grade 9); its `fish`
search does not recover that product within the API search window. Other lost
products score 6 or below. QFC loses only products scoring 6 or below.
A follow-up Fred Meyer inspection found 28 other eligible salmon products
(excluding pet food) in the first 300 `fish` results, including fresh Atlantic
fillets, center-cut loin, and sockeye fillets. The missing portion appears in
a direct `salmon` search, confirming a search-coverage gap rather than rejection
by the ingredient filters.
Frozen whole turkeys remain excluded; the targeted turkey search does not
change the frozen-product rules. Results reflect inventory and search ranking
at measurement time, rather than a guarantee about future catalogs.

## Original comparison

Measured November 6, 2026 at QFC `70500874` and Fred Meyer `70100023`. Each step
adds to the preceding plan. Scores sum unique products' grades above 6, then
divide by 100 with integer truncation. Products share the same cached grades
across plans.

| Plan | QFC requests | QFC score | Fred Meyer requests | Fred Meyer score |
| --- | ---: | ---: | ---: | ---: |
| Original, brand-filtered | 44 | 46 | 50 | 47 |
| Original terms, all brands | 44 | 53 | 50 | 59 |
| Add Meat & Seafood | 50 | 54 | 56 | 61 |
| Add Dairy & Eggs | 56 | 61 | 62 | 67 |

Removing brands adds 86 useful product IDs at QFC and 137 at Fred Meyer without
extra requests: the previous whitelist was applied after fetching API pages.
The meat search adds another 22/27 useful products, including Italian sausages.
Dairy adds another 87/77; it supplements the existing sources.

The unfiltered comparison misses two QFC product IDs from the older snapshot
(one above the grade cutoff) and one Fred Meyer ID. The meat query recovers the
Fred Meyer item. Availability and fuzzy-search ordering can change between
samples; removing a local filter cannot itself remove an API match.

Earlier attempts to replace the original terms with fewer broad queries raised
the aggregate score but lost fresh fish, beef cuts, chicken, and herbs. The
plan preserved the original searches rather than imposing the earlier
39-request target. Request counts above are measured page-fetch costs, not a
runtime limit; retries and changes in inventory can change them.

## Frozen products and item selection

Only the shellfish search admits frozen products. Other searches reject both
the API's `Frozen` category and products explicitly described as frozen.
Kroger omits the category on some frozen fish fillets, so descriptions must be
checked too. Previously frozen counter items and items described as never
frozen remain eligible unless the API categorizes them as frozen.

Applying that description check to the saved final results excludes another
15 QFC and 14 Fred Meyer products, including frozen fillets and chicken that
lacked the Frozen category. The resulting scores are **60 and 66**, with
725 and 797 useful product IDs, at the same 56/62 request costs.

Stock and package selection stay unchanged: unpriced and explicitly
out-of-stock items are excluded; HIGH inventory wins over LOW, then unknown
stock. The shared catalog deduplicates by product ID. Query-definition changes
refresh the store catalog through its existing cache signature.

## Inspecting a catalog

The ingredient command can compare raw Kroger results with production staples
before product-ID deduplication:

```sh
go run ./cmd/ingredients -location 70500874 \
  -kroger-search "sausage" -kroger-search "ground pork" > /tmp/kroger-inspection.json
```

Repeat `-kroger-search` for multiple terms. This requires Kroger provider
credentials, skips grading and ingredient caches, and preserves every raw item
variant. The JSON contains raw products and matching production staples rows,
so stock, price, frozen-product, and variant-selection differences are visible.
Experimental optimization code has been removed; runtime responses and grades
stay outside source control.
