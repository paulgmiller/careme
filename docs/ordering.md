# Ordering handoffs

Each ordering destination owns an HTTP workflow. Recipes submits shopping items
and displays the return status and review link; it has no ordering service or
provider workflow interface. Kroger, Instacart, and BigCommerce are implemented destinations.
Application startup calls `providerregistry.Factory.RegisterOrders` with the
shared application routes; it constructs each configured destination and registers
its routes directly, preserving application middleware.

The recipe handler depends on an `OrderViewBuilder` interface. The provider
registry factory implements its `OrderViews` method and receives the displayed
items and location. The registry returns an ordered slice of `orders/types.OrderView`, each with a form
action, hidden fields, labels, and a return notice. Configured Kroger stores receive
the Kroger action first, and configured MNFoodClub stores receive their BigCommerce
action first; every source store also receives Instacart when configured.
Recipes renders each destination generically.
Ordering destinations share the `OrderFields` form struct, which may be extended
when an integration needs additional shopping-list information. Kroger owns its
status interpretation and cart-link validation.

## Kroger form contract

`POST /kroger/order` accepts a same-origin, URL-encoded form:

| Field | Meaning |
| --- | --- |
| `items` | JSON array of `ai.ShoppingItem`: `id`, `name`, `needed`, nullable `buy` |
| `location` | JSON source `locations/types.Location`, including ID, chain, name, address, and coordinates when available; a hint for destination store selection |
| `list_key` | Stable finalized shopping-list hash used for duplicate protection |
| `return_url` | Application-local path, query, and optional fragment |

The full source location is a shared store hint. Kroger currently uses its ID for
product lookup and chain for its authorization banner and cart URL. Future
destinations such as Instacart may use the location to suggest a store.

The recipe page posts its merged food list with the current selected wines
already appended. It does not regenerate quantities for ordering. Kroger reads
the user ID from authentication, never from the form. Submitted buy quantities
remain descriptive: the current workflow adds one package per unique matched
UPC. Items without product IDs or available products are counted as unmatched;
other lookup failures stop the order before any cart write.

## Authorization and completion

The form runs in the same tab. If account connection is required, Kroger retains
the request server-side under random OAuth state. `/kroger/callback` verifies the
state cookie, authenticated user, and server-stored ten-minute expiry, exchanges
the authorization code, saves encrypted tokens, and resumes the posted items.
It never asks recipes to reload the list. Expired tokens are refreshed internally.

Before writing to the cart, Kroger conditionally reserves the user/list transfer
key. Pending, uncertain, and completed results prevent another write, including
for changed lists and legacy records. A failed confirmation save leaves the
reservation in place. See [cache layout](cache-layout.md).

Kroger redirects to `return_url` with `kroger_status`, `kroger_added`,
`kroger_unmatched`, and `kroger_cart`. Its view builder supplies fixed notices
for known status codes and accepts only the exact HTTPS Kroger and QFC cart links. Partial
and uncertain outcomes tell the user to review the cart. These return fields
are informational; they do not authorize any operation.

## Instacart shopping lists

`POST /instacart/order` uses the same form fields as Kroger and accepts any source
store ID. Authentication and same-origin protection are required. The handler
sends every displayed item, including pantry items and selected wines, to
`POST /idp/v1/products/products_link` using a server-side API key. Source product
IDs and catalog package multiples (`buy`) are not Instacart identifiers or amounts.

Each line item uses the name for matching and retains the original `needed` amount
in display text. Positive numeric amounts with recognized units are sent as
`line_item_measurements`; this includes fractions and common Unicode fractions.
Ranges, combined amounts, preparation notes, and unsupported units remain text
for user review. No weight-to-volume or source-package conversion is inferred.

Links expire after 30 days. Successful links are cached per user and outbound
payload (including the API environment) and reused until content changes or the
link expires. Concurrent first requests can create equivalent links; creating a
link does not add items to a cart. Cache and API failures return to the local list
with `instacart_status=failed` and a fixed notice. Success redirects directly to
the validated HTTPS Instacart shopping-list URL.

Users select a supported retailer, review matched products and quantities, and
add items to their cart on Instacart. The source store is not automatically selected.
No user OAuth tokens are needed. Configure `INSTACART_API_KEY` and optionally
`INSTACART_BASE_URL`; see [Instacart's shopping-list API](https://docs.instacart.com/developer_platform_api/api/products/create_shopping_list_page).

## BigCommerce carts

`POST /bigcommerce/order` accepts the same form contract, requires authentication
and same-origin protection, and currently serves `mnfoodclub_*` source stores.
Enable it explicitly with `MNFOODCLUB_CART_ENABLE=true`; set
`MNFOODCLUB_CART_ORIGIN` to the exact HTTPS origin returned by the store’s cart
redirect API. It reuses the MNFoodClub store API path and access token. The token
needs Products read and Carts modify (`store_cart`) scopes. No customer login or
payment credentials are transmitted, and Careme does not place orders.

The provider resolves each numeric source product ID against the configured store
catalog, choosing the lowest-priced purchasable variant using the same stock and
price rules as ingredient sourcing. Products with required modifiers cannot be
ordered automatically. Deleted, hidden, unavailable, or out-of-stock products,
pantry items, and static produce-share contents are counted as unmatched and
reported in the return notice. Other lookup failures stop the transfer before
creating a cart. Duplicate IDs in a merged list fail instead of inventing quantities.

Known `buy` multiples are rounded up to positive integer package counts; unknown
amounts use one package. Users must review product options and quantities on the
storefront. Source names and prices never override catalog values. All matched
products, including selected wines, are sent together to `POST /v3/carts` in a
new guest cart, without modifying an existing shopper cart.

Before creation, the handler reserves a per-user/store/list transfer record.
Pending, uncertain, and completed records prevent repeated creation; a changed
payload for the same list returns a changed-list notice. Failed confirmation
writes leave the reservation in place. Cart creation is never automatically retried.
A new finalized list uses a new transfer key.

Completion returns to the Careme list with fixed `bigcommerce_status` notices,
matched/unmatched counts, and a transfer reference. The review link points to
authenticated `GET /bigcommerce/cart?transfer=<hash>`. That route loads the
current user’s completed record and requests a fresh single-use redirect from
`POST /v3/carts/<cart ID>/redirect_urls`. The URL must match the configured cart
origin. Redirect tokens are never cached or returned in Careme’s status query.
Expired, checked-out, or deleted carts report a review error; they are not
silently recreated. Uncertain creation requires the user to check the store
before creating another list.

The client and handler are in `internal/providers/bigcommerce`; the provider
registry supplies MNFoodClub’s source location prefix and store name. Additional
BigCommerce stores require explicit catalog identity and credential wiring.

References: [Create Cart](https://docs.bigcommerce.com/developer/api-reference/rest/admin/management/carts/carts-single/create-cart),
[Create Cart Redirect URL](https://docs.bigcommerce.com/developer/api-reference/rest/admin/management/carts/redirects/create-cart-redirect-url).

## Future destinations

Additional destinations can register their own route and own their complete
workflow without implementing a common provider interface.
