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
Kroger owns its form encoding, status interpretation, and cart-link validation.

## Kroger form contract

`POST /kroger/order` accepts a same-origin, URL-encoded form:

| Field | Meaning |
| --- | --- |
| `items` | JSON array of `ai.ShoppingItem`: `id`, `name`, `needed`, nullable `buy` |
| `store_id` | Kroger catalog location ID |
| `chain` | Chain hint, including QFC for its authorization banner and cart URL |
| `list_key` | Stable finalized shopping-list hash used for duplicate protection |
| `return_url` | Application-local path, query, and optional fragment |

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

## Future destinations

An Instacart or BigCommerce integration can register its own `/instacart/order`
or `/bigcommerce/order` route and own its complete workflow. No common provider
interface or future destination schema is introduced here.
