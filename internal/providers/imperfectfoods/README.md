# Imperfect Foods static catalog

`imperfectfoods_demo` serves the product-only `snapshot.json` captured on
November 4, 2026 (Pacific time). It needs no API credentials or network access
at runtime. Access is by explicit location ID; the demo is not advertised as a
verified delivery option in location searches. Prices and availability are fixed
at capture time and are not a promise of current inventory.

The snapshot contains 483 food products and 28 wines after deduplication. The
provider merges source aisle categories for products appearing more than once.
Wine selection receives all captured wines, including wines in Rescued.

The source requests use `/api/marketplace/v1/<charge ID>/aisles/items` on
`api.misfitsmarket.com`:

| Source | Query |
| --- | --- |
| Vegetables | `?id=192` |
| Fruit | `?id=191` |
| Meat & Seafood | `?id=283` |
| Wine Shop | `?id=982` |
| Pantry | `?id=292` |
| Dairy & Eggs | `?id=193` |
| Rescued | No query, as observed in the storefront |

The no-query response contains an unnamed aisle with the broad catalog; its
Rescued label records the source page rather than an API classification of
every product. Carousel and nested subaisle products are included. Sold-out
products are excluded. Product IDs are the returned marketplace item IDs.
Prices use the returned `price`; MSRP is not treated as a regular selling price.
Sizes come from wine volume, package sizes in product names, or exact unit-price
ratios; unknown sizes remain empty. Wine categories preserve color and varietal.

To refresh, capture each source with an authenticated read-only request, extract
only product data into the existing snapshot shape, and run provider tests and
`./task.sh verify-go`. Keep authentication headers and raw responses outside the
repository: responses contain customer and subscription identifiers. The
embedded file contains no account identifiers, tokens, or raw API descriptions.
Changing it automatically changes the provider's ingredient/recipe cache hash.
