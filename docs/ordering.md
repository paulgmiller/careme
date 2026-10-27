# Cart transfers

Recipes sends its displayed shopping list through a shared cart-transfer HTTP
contract. Destinations own authentication, product matching, cart writes, and
idempotency. The recipe page only needs creation and status URLs; it does not
interpret provider query parameters or implement provider OAuth flows.

Kroger is the first implementation. MN Food Club, Instacart, and other future
providers can implement the same Careme-facing contract while adapting their own
upstream APIs. The [MN Food Club proposal](mnfoodclub-ordering.openapi.yaml) is an
upstream partner contract, not a deployed ordering integration.

## Request and response

`POST /<destination>/cart-transfers` accepts the shopping page's URL-encoded
HTMX form. The `items` and `location` hidden inputs contain JSON; `return_url`
is a local URL. The handler decodes these fields into `orders/types.TransferRequest`.
The form supplies `Idempotency-Key` through `hx-headers`, typically set to the
finalized shopping-list hash. JSON request bodies are not supported.

- `items` is a nonempty array of `ai.ShoppingItem`. Recipes includes the merged
  food list and selected wines without regenerating quantities.
- `location` is the full source location, used as a destination store hint.
- The `Idempotency-Key` header identifies one intended addition for the authenticated shopper and
  destination. Reuse it for retries. Changing items, store ID, chain, or return URL under an existing key
  returns HTTP 409 with `code: idempotency_conflict`. It is not an order number.
- `return_url` is an application-local path, query, and optional fragment. It is
  used only after browser account connection; POST itself never redirects.
- The shopper identity comes from Careme authentication, never the request body.

A successful POST returns HTTP 200, `Cache-Control: no-store`, and a shared
`orders/types.Transfer` object:

```json
{
  "transfer_id": "opaque-transfer-id",
  "status": "applied",
  "cart_url": "https://www.qfc.com/cart",
  "added": 17,
  "unmatched": 3,
  "code": "complete",
  "message": "Sent 17 products to your cart. Review amounts before checkout. 3 items could not be matched and were not sent."
}
```

`added` counts packages sent; `unmatched` counts submitted shopping items that
could not be matched. `message` is destination-owned display text. `code` is an
optional destination-specific detail; the generic UI uses `status`:

| Status | Meaning |
| --- | --- |
| `pending_login` | Follow `cart_url` to connect the shopper's account and resume the saved list |
| `applied` | Processing completed; inspect counts and review the destination cart, including when no products matched |
| `failed` | Preparation failed before a cart write; retry the same request and list key |
| `uncertain` | The transfer has no confirmed result; it may still be running or a cart write may have occurred. Reload to check status and review the cart before taking further action |

An applied transfer means a cart handoff completed, **not** that checkout occurred
or an order was placed. Kroger does not expose checkout confirmation through this
integration. Cart edits after a transfer do not change its saved counts.

## Status and browser behavior

`GET /<destination>/cart-transfers/{transfer_id}` returns the current transfer in
the same shape and with `Cache-Control: no-store`. It requires the same shopper
authentication as POST. Unknown IDs and IDs belonging to another shopper return
404. GET never resolves products, starts OAuth, or writes to the cart.

Kroger IDs are stable SHA-256 digests of the finalized list key, scoped to the
requesting shopper. Clients must treat them as opaque, not as credentials. The
provider's `OrderView.TransferURL` supplies the transfer POST route, and
`OrderView.TransferStatusURL` supplies the GET route for its saved result. This
lets the recipe page check a saved list on later visits without local storage
or URL outcome fields.
The POST waits for Kroger and returns its result directly.

The shared shopping-list template uses HTMX to submit the form and request
status on page load. `OrderView.TransferHeadersJSON` supplies the POST headers
through `hx-headers`, including `Idempotency-Key`;
the key is not a form field. With `HX-Request: true`, the same endpoints render HTML
fragments from the saved transfer; ordinary API requests still return JSON.
An initial HTMX status lookup with no saved transfer returns 204, leaving the
form in place. Saved results do not poll or schedule further GETs. POST uses
`HX-Redirect` when the shopper needs to connect their account. The OAuth callback returns to the clean
shopping page, whose load-triggered GET retrieves the saved outcome.

There is no custom transfer JavaScript asset. One commented inline HTMX error
hook shows a message and disables submission when a request fails, including a
network failure; the shopper reloads to check status before trying again.
Neither a failed lookup nor a missing transfer automatically submits products.

Kroger POST retries return the **current** saved transfer. This intentionally
differs from the Food Club proposal's immutable original POST response. Both APIs use the `Idempotency-Key` header.
Kroger continues adding one package per unique matched UPC; the shared request's
`buy` value is descriptive. Food Club's per-product accepted/rejected quantities
and reusable partner customer ID are not promised by the Kroger adapter.

## Kroger OAuth and duplicate protection

`POST /kroger/cart-transfers` persists the request before preparing an addition.
If account connection is needed, it returns `pending_login` and a same-origin
`/kroger/cart-transfers/{transfer_id}/connect` link. Visiting that link starts
Kroger OAuth; the destination handler owns all OAuth redirects.

`/kroger/callback` verifies the state cookie, authenticated shopper, and the
transfer's stored OAuth state and ten-minute expiry, exchanges the code, saves
encrypted tokens, and resumes the saved list. It then redirects to the original
`return_url` unchanged. The recipe page retrieves the result through GET. OAuth
denials and connection errors are saved and offer a new connection attempt.
Expired access tokens are refreshed internally.

Before any cart write, Kroger conditionally creates a `/submission` subrecord
under the authenticated user's transfer key. Uncertain and completed submissions
prevent another write. The submission remains authoritative even if
saving the lifecycle record fails or an old OAuth callback arrives late. A failed
confirmation save leaves the reservation uncertain, preventing automatic replay. Failed product
lookup can be retried with the same key because no cart write occurred.

Records currently have no automatic expiry. OAuth continuations expire logically
after ten minutes. See [cache layout](cache-layout.md) for storage details.

## Adding a destination

Register its routes through `providerregistry.Factory.RegisterOrders`, implement
creation and read-only status using the shared request/response types, and add
its location match and `OrderView` builder. Supply an action URL, a status URL,
form fields, and destination copy. Authentication links and callbacks stay with
the destination. No provider-specific logic belongs in the recipe handler or
shared browser code.

The [MN Food Club mock walkthrough](mnfoodclub-mock.md) remains available at
`/mnfoodclub-demo/` with mocks enabled. It demonstrates the proposed upstream
partner API, not a production recipe ordering destination.
