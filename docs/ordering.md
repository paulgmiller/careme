# Cart transfers

Careme's shopping list submits a normal browser form to its ordering destination.
The destination handler owns authentication, product matching, cart writes,
idempotency, redirects, and any failure page. The shopping list renders either
an “Add to cart” form or a saved cart link,
using a provider lookup during server rendering. It never polls from the browser.

This browser contract differs from the [MN Food Club proposal](mnfoodclub-ordering.openapi.yaml),
which describes a server-to-server JSON API. A future Food Club provider would
receive Careme's form, call that upstream API, and redirect the browser to its
returned cart or account-connection URL. Upstream states stay inside the adapter.

## Browser form

`POST /<destination>/order-transfers` accepts two URL-encoded hidden inputs:

- `transfer_request`: the complete JSON-serialized `orders/types.TransferRequest`,
  containing the displayed `items`, source `location`, and local `return_url`.
- `idempotency_key`: identifies one intended addition for this shopper and
  destination, normally the finalized shopping-list hash. Reuse it for retries.
  Changing items, store ID, chain, or return URL under an existing key returns 409.

`OrderView` supplies the POST URL, serialized request, idempotency key, and button
copy. The template uses a native form with `hx-boost="false"`. There is no HTMX
transfer request, status URL, polling, JSON response, or connect-account control.
A native form cannot set a custom header; an adapter calling Food Club would
forward the form's key in the upstream `Idempotency-Key` header.

The authenticated shopper comes from the verified Careme session, never from
submitted JSON. A shopper whose session has expired is redirected to sign in,
then back to the shopping list to submit again. `return_url` must be an
application-local path; it is used for sign-in return and the failure page's back
link, not for cart-transfer status messages.

## Kroger navigation

1. The POST saves the request before preparing the cart addition.
2. If a Kroger connection is needed, it immediately returns a 303 redirect to
   Kroger OAuth. The shopper does not have to click a separate connect button.
3. `/kroger/callback` validates the state cookie, authenticated ownership, stored
   nonce, and ten-minute age. It exchanges the authorization code, saves the
   encrypted token, and resumes the saved request.
4. A completed addition returns a 303 redirect to the saved local shopping list.
   Expired access tokens are refreshed internally.

No products are added on shopping-list page load. There are no transfer-status
or separate connection GET routes. Reloading the shopping list reads the saved
handoff to choose the form or cart link. Reposting consults the saved transfer
and cannot add the same list twice.

Kroger sends one package per unique matched UPC. Unmatched items are counted in
the saved result; the shopper reviews amounts and checkout in Kroger's cart.
A cart transfer never checks out or places an order.

## Failures and duplicate protection

Failures remain on the provider route as a full HTML page. The page offers links
to review the cart or return to the shopping list. OAuth denial, connection
errors, and product-lookup failures also offer a native “Try again, chef” form
with the same request and idempotency key. A transfer with no matches shows this
page without claiming that products were added.

An unconfirmed write, concurrent submission, or failed write-reservation check
shows a conflict page without a retry button. The shopper must review the cart;
the server does not automatically replay an addition. Invalid forms and cache
failures return HTTP errors on the provider route, without putting error state
into the shopping-list page or its URL.

Before any cart write, Kroger conditionally creates a `/submission` subrecord
under the authenticated user's transfer key. Its saved result is authoritative
if the lifecycle save fails or an OAuth callback arrives late. Uncertain and
completed submissions prevent another write. Failed product lookup can be
retried because it occurs before this reservation and before any cart change.

The shared `TransferRequest` and `Transfer` types describe the saved request and
result inside the handler; they are not a browser JSON API. Transfer IDs use the
idempotency key directly, scoped to the authenticated user and destination.
Records have no automatic expiry; OAuth state expires logically after ten minutes.
See [cache layout](cache-layout.md) for storage details.

## Adding a destination

Register its POST and authentication callback routes through
`providerregistry.Factory.RegisterOrders`, then supply its location match and
cache-bound `OrderView` builder through `Factory.NewOrderViewBuilder(cache)`.
The adapter translates the shared request to the upstream
API and owns redirects and failure handling. Provider state does not belong in
the recipe handler or shared shopping-list UI.

The [MN Food Club mock walkthrough](mnfoodclub-mock.md) remains available at
`/mnfoodclub-demo/` with mocks enabled. It demonstrates the proposed upstream
partner API, not a production recipe ordering destination.

## Saved cart link on the shopping list

The server asks the configured provider for this signed-in viewer's saved
transfer using the finalized list key. Kroger reads the durable submission first,
then lifecycle state when no submission exists. Confirmed or possibly applied
writes replace the form with “Review cart (opens a new tab)”; no counts or status
text are shown. Missing transfers and known pre-write failures keep Add to cart.
Read or decoding errors fail the page request. There is no browser status fetch.
The saved handoff remains after checkout or cart edits; this is transfer history,
not a live cart read. A different finalized list has its own idempotency key.
