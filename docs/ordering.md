# Ordering handoffs

Each ordering destination owns an HTTP workflow. Recipes submits shopping items
and displays the return status and review link; it has no ordering service or
provider workflow interface. Kroger is the only implemented destination.
Application startup calls `providerregistry.Factory.RegisterOrders` with the
shared application routes; it constructs each configured destination and registers
its routes directly, preserving application middleware.

The recipe handler depends on an `OrderViewBuilder` interface. The provider
registry factory implements its `OrderView` method and receives the displayed
items and location. The registry matches `location.ID` against configured
providers. The matching provider returns a form action, hidden fields, labels,
and return notice as `orders/types.OrderView`; recipes renders those generically.
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

### Return query parameters

Kroger redirects to `return_url`, preserving its existing query and fragment,
and setting these URL-encoded parameters:

| Parameter | Meaning |
| --- | --- |
| `kroger_status` | Transfer outcome code listed below; determines the fixed notice shown on the shopping list |
| `kroger_added` | Number of unique matched UPCs successfully sent, one package each; used in the notice for `complete` |
| `kroger_unmatched` | Number of submitted shopping items without a product ID or available product; used in the notice for `complete` |
| `kroger_cart` | Absolute HTTPS cart URL for the submitted chain; accepted for display only when it exactly matches a URL in the shared [chain table](../internal/providers/kroger/chains.go) |

For example:

```text
/recipes?h=list-hash&kroger_added=17&kroger_cart=https%3A%2F%2Fwww.kroger.com%2Fcart&kroger_status=complete&kroger_unmatched=13#shopping-list-section
```

This displays “Sent 17 products to your cart. Review amounts before checkout.”
and “13 items could not be matched and were not sent.” The review link opens
`https://www.kroger.com/cart` in a new tab. A `complete` status hides the add-to-cart
form, even when some items were unmatched. Fred Meyer uses
`https://www.fredmeyer.com/cart`; other mapped chains use their own domains.
Unknown chains currently default to the Kroger cart URL.

| `kroger_status` | Meaning |
| --- | --- |
| `complete` | Cart write succeeded; unmatched items may remain |
| `no_matches` | No products could be matched; nothing was sent |
| `changed` | A previously completed transfer has a different submitted store or items; no new write occurs |
| `pending` | A transfer was reserved but its final result is unavailable; review the cart before retrying |
| `uncertain` | The cart write returned an error; products may have been added |
| `denied` | OAuth authorization returned an error |
| `missing_code` | OAuth callback did not include an authorization code |
| `connect_failed` | Account connection could not be completed |
| `save_failed` | Shopper credentials could not be saved |
| `lookup_failed` | Product lookup failed before any cart write |
| `start_failed` | Transfer records could not be read or reserved; review the cart before retrying |
| `confirmation_failed` | Saving the transfer result failed; products may have been added |

The view builder ignores unknown status codes and shows review links only for
known statuses. Counts must both be nonnegative integers to appear in the
`complete` notice; otherwise it uses a generic completion notice. These return
fields control presentation only and never authorize a cart write. Duplicate
protection relies on the server-side transfer record, not the URL.

## Future destinations

An Instacart or BigCommerce integration can register its own `/instacart/order`
or `/bigcommerce/order` route and own its complete workflow. No common provider
interface or future destination schema is introduced here.
