# Kroger staples queries

The provider retains the original search terms, removes brand filtering, and
adds `Meat & Seafood` and `Dairy & Eggs`. This addresses
[#927](https://github.com/paulgmiller/careme/issues/927) without replacing the
original selection with unrelated high-scoring products.

The twelve searches cover fresh vegetables, fresh produce, beef, chicken,
fish, pork, shellfish, lamb, grains, pasta, meat/seafood, and dairy/eggs. The
vegetable search's original spelling error is corrected; both spellings returned
identical product sets in the measured samples. No local or API brand whitelist
is applied.

## Comparison

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
final plan preserves the original searches rather than imposing the earlier
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
